// Package repository is the RBAC module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository.
//
// Every role method takes a companyID and applies it. Roles are tenant-owned,
// so RepositoryConvention §3 applies in its ordinary form: making the tenant a
// required argument means forgetting it does not compile.
package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// RoleRepository is the persistence contract for roles.
type RoleRepository interface {
	Create(ctx context.Context, role *entity.Role) error
	Update(ctx context.Context, role *entity.Role) error

	// FindByID is tenant-scoped: a role belonging to another company is
	// NOT_FOUND, never FORBIDDEN — a 403 would confirm it exists.
	FindByID(ctx context.Context, roleID, companyID uuid.UUID) (*entity.Role, error)

	// FindByName resolves a role by the name a membership stores. This is the
	// permission evaluator's hot path.
	FindByName(ctx context.Context, companyID uuid.UUID, name string) (*entity.Role, error)

	List(
		ctx context.Context, companyID uuid.UUID, query dto.ListRolesQuery,
	) (pagination.Page[entity.Role], error)

	// ListAll returns every role in a company, unpaginated. The set is bounded
	// by construction — three system roles plus a handful of custom ones — and
	// it backs the provisioner's idempotency check.
	ListAll(ctx context.Context, companyID uuid.UUID) ([]entity.Role, error)

	Delete(ctx context.Context, roleID, companyID uuid.UUID) error

	ExistsByName(ctx context.Context, companyID uuid.UUID, name string) (bool, error)
}

type roleRepository struct {
	*base.Base[entity.Role, *entity.Role]
}

var _ RoleRepository = (*roleRepository)(nil)

// NewRoleRepository builds the repository.
func NewRoleRepository(db *gorm.DB, ids port.IDGenerator) RoleRepository {
	return &roleRepository{
		Base: base.New[entity.Role, *entity.Role](db, ids, "rbac.role_repository"),
	}
}

// forTenant is the scope every method below applies.
//
// Wrapping it in a named helper means a reviewer auditing this file for missing
// tenant filters is looking for one identifier, not for an inline Where clause
// that is easy to skim past.
func forTenant(companyID uuid.UUID) base.Scope {
	return base.ForCompany(companyID)
}

func (r *roleRepository) Create(ctx context.Context, role *entity.Role) error {
	return r.Base.Create(ctx, role)
}

func (r *roleRepository) Update(ctx context.Context, role *entity.Role) error {
	return r.Base.Update(ctx, role)
}

func (r *roleRepository) FindByID(
	ctx context.Context, roleID, companyID uuid.UUID,
) (*entity.Role, error) {
	return r.Base.FindByID(ctx, roleID, forTenant(companyID))
}

func (r *roleRepository) FindByName(
	ctx context.Context, companyID uuid.UUID, name string,
) (*entity.Role, error) {
	return r.Base.FindOne(ctx,
		forTenant(companyID),
		base.Where("name = ?", entity.NormalizeRoleName(name)),
	)
}

func (r *roleRepository) List(
	ctx context.Context, companyID uuid.UUID, query dto.ListRolesQuery,
) (pagination.Page[entity.Role], error) {
	scopes := []base.Scope{forTenant(companyID)}

	if query.IsSystem != nil {
		scopes = append(scopes, base.Where("is_system = ?", *query.IsSystem))
	}
	if query.HasSearch() {
		scopes = append(scopes, base.Search(query.Search, "roles.name", "roles.description"))
	}

	// The base rejects a pagination.Request that has not been through Apply, so
	// an unvalidated sort column can never reach the SQL.
	return r.Base.FindAll(ctx, query.Request, scopes...)
}

func (r *roleRepository) ListAll(
	ctx context.Context, companyID uuid.UUID,
) ([]entity.Role, error) {
	return r.Base.FindMany(ctx, forTenant(companyID))
}

func (r *roleRepository) Delete(ctx context.Context, roleID, companyID uuid.UUID) error {
	return r.Base.Delete(ctx, roleID, forTenant(companyID))
}

func (r *roleRepository) ExistsByName(
	ctx context.Context, companyID uuid.UUID, name string,
) (bool, error) {
	return r.Base.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("name = ?", entity.NormalizeRoleName(name)),
	)
}
