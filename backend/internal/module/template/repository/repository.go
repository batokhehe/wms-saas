// Package repository is the module's persistence layer.
//
// LAYER RULE: this is the ONLY package in a module permitted to import gorm or
// internal/shared/repository. A service that imports gorm cannot be tested
// without a database; a handler that imports gorm has collapsed three layers
// into one.
//
// The interface is declared here, next to its implementation, because Go
// convention places an interface with its consumer — and the consumer of
// Repository is the service in this same module. Declaring it here also means
// the service can be tested against a fake with no database.
package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/template/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/template/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// Repository is the persistence contract for Resource.
//
// Every method takes a companyID. That is deliberate and non-negotiable: making
// the tenant a required argument means a developer cannot forget to scope a
// query — the code will not compile.
//
// Note what the signatures do not contain: no *gorm.DB, no Scope, no query
// builder. GORM stops at this boundary.
type Repository interface {
	Create(ctx context.Context, resource *entity.Resource) error
	Update(ctx context.Context, resource *entity.Resource) error
	Delete(ctx context.Context, companyID, id uuid.UUID) error
	FindByID(ctx context.Context, companyID, id uuid.UUID) (*entity.Resource, error)
	FindAll(ctx context.Context, companyID uuid.UUID, query dto.ListQuery) (pagination.Page[entity.Resource], error)
	Count(ctx context.Context, companyID uuid.UUID) (int64, error)
	Exists(ctx context.Context, companyID, id uuid.UUID) (bool, error)
	ExistsByName(ctx context.Context, companyID uuid.UUID, name string) (bool, error)
}

// resourceRepository composes the generic base and adds tenant scoping.
//
// Composition rather than inheritance-by-copy: the seven CRUD methods come from
// Base, and this type contributes only what is specific to Resource. When the
// base gains a capability, every module repository gets it without edits.
type resourceRepository struct {
	*base.Base[entity.Resource, *entity.Resource]
}

var _ Repository = (*resourceRepository)(nil)

// New builds the repository.
//
// The two type parameters are the standard Go workaround for a language limit:
// the base needs the value type to build slices and the pointer type to call
// SetID, and one parameter cannot express both.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &resourceRepository{
		Base: base.New[entity.Resource, *entity.Resource](db, ids, "template.repository"),
	}
}

// forTenant is the scope every method below applies.
//
// Wrapping it in a named helper means a reviewer scanning this file for missing
// tenant filters is looking for one identifier, not for an inline Where clause
// that is easy to skim past.
func forTenant(companyID uuid.UUID) base.Scope {
	return base.ForCompany(companyID)
}

func (r *resourceRepository) Create(ctx context.Context, resource *entity.Resource) error {
	// The base assigns the ID from the injected generator when it is unset, so
	// no module ever calls uuid.New().
	return r.Base.Create(ctx, resource)
}

func (r *resourceRepository) Update(ctx context.Context, resource *entity.Resource) error {
	return r.Base.Update(ctx, resource)
}

func (r *resourceRepository) Delete(ctx context.Context, companyID, id uuid.UUID) error {
	// Soft delete: the base issues an UPDATE setting deleted_at, because the
	// entity embeds BaseEntity with a gorm.DeletedAt.
	return r.Base.Delete(ctx, id, forTenant(companyID))
}

func (r *resourceRepository) FindByID(
	ctx context.Context,
	companyID, id uuid.UUID,
) (*entity.Resource, error) {
	return r.Base.FindByID(ctx, id, forTenant(companyID))
}

func (r *resourceRepository) FindAll(
	ctx context.Context,
	companyID uuid.UUID,
	query dto.ListQuery,
) (pagination.Page[entity.Resource], error) {
	scopes := []base.Scope{forTenant(companyID)}

	if query.HasSearch() {
		scopes = append(scopes, base.Search(query.Search, dto.SearchColumns()...))
	}

	// Module-specific filters are appended here, for example:
	//   if query.Status != "" {
	//       scopes = append(scopes, base.Where("status = ?", query.Status))
	//   }

	// The base rejects a pagination.Request that has not been through Apply,
	// so an unvalidated sort column can never reach the SQL.
	return r.Base.FindAll(ctx, query.Request, scopes...)
}

func (r *resourceRepository) Count(ctx context.Context, companyID uuid.UUID) (int64, error) {
	return r.Base.Count(ctx, forTenant(companyID))
}

func (r *resourceRepository) Exists(ctx context.Context, companyID, id uuid.UUID) (bool, error) {
	return r.Base.Exists(ctx, id, forTenant(companyID))
}

// ExistsByName is an example of a domain query the base does not provide.
//
// It is written with the base's scope helpers rather than raw GORM, so it stays
// transaction-aware and tenant-scoped for free.
func (r *resourceRepository) ExistsByName(
	ctx context.Context,
	companyID uuid.UUID,
	name string,
) (bool, error) {
	return r.Base.ExistsBy(ctx, forTenant(companyID), base.Where("name = ?", name))
}
