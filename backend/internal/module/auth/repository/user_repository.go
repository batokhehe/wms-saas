// Package repository is the auth module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository.
//
// Note the absence of a companyID parameter on every method here. That is the
// deliberate exception to RepositoryConvention §3: identity is not tenant-owned
// (see entity.User), so there is no tenant filter to apply. Every OTHER module's
// repository must take companyID.
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/auth/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// UserRepository is the persistence contract for accounts.
type UserRepository interface {
	Create(ctx context.Context, user *entity.User) error
	Update(ctx context.Context, user *entity.User) error
	FindByID(ctx context.Context, id uuid.UUID) (*entity.User, error)
	FindByEmail(ctx context.Context, email string) (*entity.User, error)
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	UpdateLastLogin(ctx context.Context, id uuid.UUID, at time.Time) error
}

type userRepository struct {
	*base.Base[entity.User, *entity.User]
}

var _ UserRepository = (*userRepository)(nil)

// NewUserRepository builds the repository.
func NewUserRepository(db *gorm.DB, ids port.IDGenerator) UserRepository {
	return &userRepository{
		Base: base.New[entity.User, *entity.User](db, ids, "auth.user_repository"),
	}
}

func (r *userRepository) Create(ctx context.Context, user *entity.User) error {
	return r.Base.Create(ctx, user)
}

func (r *userRepository) Update(ctx context.Context, user *entity.User) error {
	return r.Base.Update(ctx, user)
}

func (r *userRepository) FindByID(ctx context.Context, id uuid.UUID) (*entity.User, error) {
	return r.Base.FindByID(ctx, id)
}

// FindByEmail looks up an account by address.
//
// The email is normalised before the query even though the column is CITEXT:
// relying on the column type alone would break the moment this value is used in
// a join or a cache key that is not itself case-insensitive.
func (r *userRepository) FindByEmail(ctx context.Context, email string) (*entity.User, error) {
	return r.Base.FindOne(ctx, base.Where("email = ?", entity.NormalizeEmail(email)))
}

func (r *userRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	return r.Base.ExistsBy(ctx, base.Where("email = ?", entity.NormalizeEmail(email)))
}

// UpdateLastLogin stamps the successful-authentication time.
//
// It is a targeted UPDATE rather than a full Save of the loaded user. Saving the
// whole row on every login would rewrite password_hash and status from a struct
// read moments earlier, so a concurrent password change could be silently
// reverted by a login that started before it.
func (r *userRepository) UpdateLastLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.Base.UpdateFields(ctx, id, map[string]any{"last_login_at": at})
}
