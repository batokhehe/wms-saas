package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/auth/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/infra/postgres"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// RefreshTokenRepository is the persistence contract for sessions.
//
// Every method speaks in terms of a token HASH, never a raw token. The raw
// value never crosses this boundary, so no repository method can accidentally
// log or persist one.
type RefreshTokenRepository interface {
	Create(ctx context.Context, token *entity.RefreshToken) error

	// FindByHash returns a token record regardless of its revocation state.
	//
	// Deliberately not filtered to live tokens: reuse detection depends on
	// finding an already-revoked token so that presenting it can be recognised
	// as theft. A "live only" lookup would return NOT_FOUND and the attack
	// would look identical to a typo.
	FindByHash(ctx context.Context, tokenHash string) (*entity.RefreshToken, error)

	// Revoke invalidates one token. Idempotent.
	Revoke(ctx context.Context, id uuid.UUID, at time.Time) error

	// RevokeIfLive invalidates a token and reports whether it was live at the
	// time — that is, whether THIS call performed the transition.
	//
	// It exists to close a race that Revoke alone cannot: two concurrent
	// refreshes presenting the same token can both pass an IsRevoked() check
	// and both mint a new session. The conditional UPDATE is atomic, so exactly
	// one caller sees true and the loser is treated as token reuse.
	RevokeIfLive(ctx context.Context, id uuid.UUID, at time.Time) (bool, error)

	// RevokeAllForUser invalidates every live token for a user. It backs
	// "sign out everywhere" and the reuse-detection response.
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, at time.Time) (int64, error)

	// CountLiveForUser reports how many usable sessions a user holds, for the
	// concurrent-session cap.
	CountLiveForUser(ctx context.Context, userID uuid.UUID, now time.Time) (int64, error)

	// OldestLiveForUser returns the least recently created usable session, so
	// the cap can evict it rather than rejecting a legitimate new login.
	OldestLiveForUser(ctx context.Context, userID uuid.UUID, now time.Time) (*entity.RefreshToken, error)

	// DeleteExpiredBefore hard-deletes expired rows. Called by a cleanup job,
	// never from an HTTP request.
	DeleteExpiredBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

type refreshTokenRepository struct {
	*base.Base[entity.RefreshToken, *entity.RefreshToken]
}

var _ RefreshTokenRepository = (*refreshTokenRepository)(nil)

// NewRefreshTokenRepository builds the repository.
func NewRefreshTokenRepository(db *gorm.DB, ids port.IDGenerator) RefreshTokenRepository {
	return &refreshTokenRepository{
		Base: base.New[entity.RefreshToken, *entity.RefreshToken](
			db, ids, "auth.refresh_token_repository"),
	}
}

func (r *refreshTokenRepository) Create(ctx context.Context, token *entity.RefreshToken) error {
	return r.Base.Create(ctx, token)
}

func (r *refreshTokenRepository) FindByHash(
	ctx context.Context,
	tokenHash string,
) (*entity.RefreshToken, error) {
	return r.Base.FindOne(ctx, base.Where("token_hash = ?", tokenHash))
}

func (r *refreshTokenRepository) Revoke(ctx context.Context, id uuid.UUID, at time.Time) error {
	// Only live tokens are updated, so re-revoking preserves the original
	// timestamp and the audit trail records when the session actually ended.
	err := r.Base.DB(ctx).
		Model(&entity.RefreshToken{}).
		Where("id = ?", id).
		Where("revoked_at IS NULL").
		Update("revoked_at", at).Error

	return postgres.TranslateError(err, "auth.refresh_token_repository.Revoke")
}

func (r *refreshTokenRepository) RevokeIfLive(
	ctx context.Context,
	id uuid.UUID,
	at time.Time,
) (bool, error) {
	result := r.Base.DB(ctx).
		Model(&entity.RefreshToken{}).
		Where("id = ?", id).
		Where("revoked_at IS NULL").
		Update("revoked_at", at)

	if result.Error != nil {
		return false, postgres.TranslateError(
			result.Error, "auth.refresh_token_repository.RevokeIfLive")
	}

	// RowsAffected is the atomic signal: zero means another transaction won the
	// race and already revoked this token.
	return result.RowsAffected == 1, nil
}

func (r *refreshTokenRepository) RevokeAllForUser(
	ctx context.Context,
	userID uuid.UUID,
	at time.Time,
) (int64, error) {
	result := r.Base.DB(ctx).
		Model(&entity.RefreshToken{}).
		Where("user_id = ?", userID).
		Where("revoked_at IS NULL").
		Update("revoked_at", at)

	if result.Error != nil {
		return 0, postgres.TranslateError(
			result.Error, "auth.refresh_token_repository.RevokeAllForUser")
	}

	return result.RowsAffected, nil
}

func (r *refreshTokenRepository) CountLiveForUser(
	ctx context.Context,
	userID uuid.UUID,
	now time.Time,
) (int64, error) {
	return r.Base.Count(ctx,
		base.Where("user_id = ?", userID),
		base.Where("revoked_at IS NULL"),
		base.Where("expires_at > ?", now),
	)
}

func (r *refreshTokenRepository) OldestLiveForUser(
	ctx context.Context,
	userID uuid.UUID,
	now time.Time,
) (*entity.RefreshToken, error) {
	var token entity.RefreshToken

	err := r.Base.DB(ctx).
		Model(&entity.RefreshToken{}).
		Where("user_id = ?", userID).
		Where("revoked_at IS NULL").
		Where("expires_at > ?", now).
		Where("deleted_at IS NULL").
		Order("created_at ASC").
		First(&token).Error
	if err != nil {
		return nil, postgres.TranslateError(
			err, "auth.refresh_token_repository.OldestLiveForUser")
	}

	return &token, nil
}

// DeleteExpiredBefore permanently removes expired sessions.
//
// This is a hard delete, which SoftDeleteConvention.md restricts to
// administrative purge jobs — and this is one. An expired refresh token has no
// audit value: the login it came from is already recorded as a domain event,
// and the row itself only exists to be matched against, which it never will be
// again.
func (r *refreshTokenRepository) DeleteExpiredBefore(
	ctx context.Context,
	cutoff time.Time,
) (int64, error) {
	result := r.Base.DB(ctx).
		Unscoped().
		Where("expires_at < ?", cutoff).
		Delete(&entity.RefreshToken{})

	if result.Error != nil {
		return 0, postgres.TranslateError(
			result.Error, "auth.refresh_token_repository.DeleteExpiredBefore")
	}

	return result.RowsAffected, nil
}
