// Package repository is the tenancy module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository.
//
// # How tenant isolation is enforced here
//
// The companies table is the tenant ROOT — it has no company_id of its own, so
// `ForCompany(id)` is meaningless on it. Isolation is instead expressed as
// reachability: a caller may read a company only if they hold an ACTIVE
// membership in it.
//
// That rule is applied by the `accessibleTo` scope, and every read method below
// takes a userID so it cannot be omitted. A method that returned a company
// without one would not compile.
package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// CompanyRepository is the persistence contract for tenants.
//
// Every read takes a userID. This is the companies-table equivalent of the
// mandatory companyID parameter that RepositoryConvention §3 requires of
// ordinary tenant-owned repositories: making the caller's identity a required
// argument means a cross-tenant read cannot be written by accident.
type CompanyRepository interface {
	Create(ctx context.Context, company *entity.Company) error
	Update(ctx context.Context, company *entity.Company) error

	// FindAccessible returns a company only when userID holds an ACTIVE
	// membership in it. Otherwise NOT_FOUND — never FORBIDDEN, which would
	// confirm that a company with that id exists.
	FindAccessible(ctx context.Context, companyID, userID uuid.UUID) (*entity.Company, error)

	// ListAccessible returns the companies userID can act in.
	ListAccessible(
		ctx context.Context, userID uuid.UUID, query dto.ListCompaniesQuery,
	) (pagination.Page[entity.Company], error)

	// Delete soft-deletes a company the user can access.
	Delete(ctx context.Context, companyID, userID uuid.UUID) error

	// ExistsByCode reports whether a code is taken.
	//
	// Deliberately NOT scoped to the caller: company codes are globally unique,
	// so answering "is this taken?" requires looking across all tenants. It
	// returns a bare boolean and never a row, so it leaks only what the unique
	// index would reveal on insert anyway — and returning a clean CONFLICT is
	// better than a 500 from a constraint violation.
	ExistsByCode(ctx context.Context, code string) (bool, error)

	// FindByIDUnscoped reads a company without an access check.
	//
	// Reserved for callers that have ALREADY established access by another
	// route — specifically the company-context middleware, which resolves the
	// membership first and then loads the company it points at. Naming it
	// "Unscoped" makes every call site conspicuous in review.
	FindByIDUnscoped(ctx context.Context, companyID uuid.UUID) (*entity.Company, error)
}

type companyRepository struct {
	*base.Base[entity.Company, *entity.Company]
}

var _ CompanyRepository = (*companyRepository)(nil)

// NewCompanyRepository builds the repository.
func NewCompanyRepository(db *gorm.DB, ids port.IDGenerator) CompanyRepository {
	return &companyRepository{
		Base: base.New[entity.Company, *entity.Company](db, ids, "tenancy.company_repository"),
	}
}

// accessibleTo restricts a company query to those the user can act in.
//
// It is a correlated subquery rather than a JOIN, deliberately: a JOIN would
// duplicate company rows if the membership uniqueness constraint were ever
// violated, silently inflating both the page and the total count. IN (SELECT …)
// cannot duplicate, so the pagination metadata stays correct even under data
// corruption.
//
// PostgreSQL plans this as a semi-join using idx_memberships_user_company, so
// the defensive shape costs nothing.
func accessibleTo(userID uuid.UUID) base.Scope {
	return base.Where(
		`companies.id IN (
			SELECT m.company_id FROM memberships m
			WHERE m.user_id = ?
			  AND m.status = ?
			  AND m.deleted_at IS NULL
		)`,
		userID, entity.MembershipActive,
	)
}

func (r *companyRepository) Create(ctx context.Context, company *entity.Company) error {
	return r.Base.Create(ctx, company)
}

func (r *companyRepository) Update(ctx context.Context, company *entity.Company) error {
	return r.Base.Update(ctx, company)
}

func (r *companyRepository) FindAccessible(
	ctx context.Context,
	companyID, userID uuid.UUID,
) (*entity.Company, error) {
	return r.Base.FindByID(ctx, companyID, accessibleTo(userID))
}

func (r *companyRepository) ListAccessible(
	ctx context.Context,
	userID uuid.UUID,
	query dto.ListCompaniesQuery,
) (pagination.Page[entity.Company], error) {
	scopes := []base.Scope{accessibleTo(userID)}

	if query.Status != "" {
		scopes = append(scopes, base.Where("companies.status = ?", query.Status))
	}
	if query.HasSearch() {
		scopes = append(scopes, base.Search(query.Search, dto.CompanySearchColumns()...))
	}

	// The base rejects a pagination.Request that has not been through Apply, so
	// an unvalidated sort column can never reach the SQL.
	return r.Base.FindAll(ctx, query.Request, scopes...)
}

func (r *companyRepository) Delete(ctx context.Context, companyID, userID uuid.UUID) error {
	return r.Base.Delete(ctx, companyID, accessibleTo(userID))
}

func (r *companyRepository) ExistsByCode(ctx context.Context, code string) (bool, error) {
	return r.Base.ExistsBy(ctx, base.Where("code = ?", entity.NormalizeCode(code)))
}

func (r *companyRepository) FindByIDUnscoped(
	ctx context.Context,
	companyID uuid.UUID,
) (*entity.Company, error) {
	return r.Base.FindByID(ctx, companyID)
}
