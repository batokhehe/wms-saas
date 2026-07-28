package entity

import (
	"time"

	"github.com/google/uuid"

	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// RefreshToken is a server-side session record.
//
// It stores a SHA-256 digest of the token, never the token itself. A database
// leak therefore yields no usable session: the attacker holds digests, and the
// tokens are 256 bits of randomness that cannot be recovered from them.
//
// This is what makes refresh tokens revocable, and it is the reason the system
// uses a stateless access token plus a stateful refresh token rather than one
// long-lived token: the access token is fast to verify with no database round
// trip, while the refresh token is the revocation point.
type RefreshToken struct {
	sharedentity.BaseEntity

	UserID uuid.UUID `gorm:"type:uuid;not null"`

	// TokenHash is the lowercase hex SHA-256 of the raw token: 64 characters.
	TokenHash string `gorm:"type:char(64);not null"`

	ExpiresAt time.Time `gorm:"not null"`

	// RevokedAt is set on rotation, on logout, and when reuse is detected.
	//
	// It is the token's lifecycle state and is distinct from DeletedAt, which
	// means erasure. Modelling revocation as a soft delete would conflate the
	// two and break reuse detection: a rotated token must remain findable so
	// that presenting it again can be recognised as theft.
	RevokedAt *time.Time

	// Session provenance. Supplied by the client and therefore untrusted — it
	// is for display and investigation, never for authorisation.
	Device    string  `gorm:"type:varchar(255)"`
	IPAddress *string `gorm:"type:inet"`
	UserAgent string  `gorm:"type:varchar(512)"`
}

// TableName pins the table name.
func (RefreshToken) TableName() string { return "refresh_tokens" }

// IsRevoked reports whether the token has been explicitly invalidated.
func (t *RefreshToken) IsRevoked() bool { return t.RevokedAt != nil }

// IsExpired reports whether the token has passed its expiry at the given time.
func (t *RefreshToken) IsExpired(now time.Time) bool { return !now.Before(t.ExpiresAt) }

// IsUsable reports whether the token can be exchanged for a new access token.
//
// Both conditions in one predicate, because checking them separately at each
// call site is how one of them eventually gets forgotten.
func (t *RefreshToken) IsUsable(now time.Time) bool {
	return !t.IsRevoked() && !t.IsExpired(now)
}

// Revoke marks the token invalid. It is idempotent: revoking an already-revoked
// token keeps the original timestamp, so the audit trail records when the
// session actually ended rather than when someone last tried to end it.
func (t *RefreshToken) Revoke(now time.Time) {
	if t.RevokedAt == nil {
		t.RevokedAt = &now
	}
}
