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

// MembershipRepository is the persistence contract for access grants.
//
// Memberships IS a tenant-owned table, so RepositoryConvention §3 applies in
// its ordinary form: every company-scoped method takes a companyID, and
// forgetting it does not compile.
//
// The exceptions are the two user-scoped methods — FindActiveByUserAndCompany
// and ListActiveByUser — which answer "where can this person act?". They are
// scoped by USER instead, which is the correct boundary for that question:
// the caller is asking about their own access, and a company filter would be
// circular (you need a company to find your companies).
type MembershipRepository interface {
	Create(ctx context.Context, membership *entity.Membership) error
	Update(ctx context.Context, membership *entity.Membership) error

	// FindActiveByUserAndCompany is the company-context middleware's hot path.
	// It returns only ACTIVE memberships, so a PENDING invitation can never
	// resolve a company context.
	FindActiveByUserAndCompany(
		ctx context.Context, userID, companyID uuid.UUID,
	) (*entity.Membership, error)

	// ListActiveByUser returns every company the user can act in, with the
	// company preloaded for the switcher view.
	ListActiveByUser(ctx context.Context, userID uuid.UUID) ([]entity.Membership, error)

	// FindByID is tenant-scoped: a membership belonging to another company is
	// NOT_FOUND, not FORBIDDEN.
	FindByID(ctx context.Context, membershipID, companyID uuid.UUID) (*entity.Membership, error)

	// ListByCompany returns the member list of one company.
	ListByCompany(
		ctx context.Context, companyID uuid.UUID, query dto.ListMembershipsQuery,
	) (pagination.Page[entity.Membership], error)

	// FindByUserInCompany finds any membership (any status) for a user in a
	// company. Used to detect a duplicate invitation.
	FindByUserInCompany(
		ctx context.Context, companyID, userID uuid.UUID,
	) (*entity.Membership, error)

	// Delete soft-deletes a membership within one company.
	Delete(ctx context.Context, membershipID, companyID uuid.UUID) error

	// CountOwners reports how many ACTIVE owners a company has. It backs the
	// last-owner protection.
	CountOwners(ctx context.Context, companyID uuid.UUID) (int64, error)
}

type membershipRepository struct {
	*base.Base[entity.Membership, *entity.Membership]
}

var _ MembershipRepository = (*membershipRepository)(nil)

// NewMembershipRepository builds the repository.
func NewMembershipRepository(db *gorm.DB, ids port.IDGenerator) MembershipRepository {
	return &membershipRepository{
		Base: base.New[entity.Membership, *entity.Membership](
			db, ids, "tenancy.membership_repository"),
	}
}

// forTenant is the scope every company-scoped method below applies.
//
// Wrapping it in a named helper means a reviewer auditing this file for missing
// tenant filters is looking for one identifier, not for an inline Where clause
// that is easy to skim past.
func forTenant(companyID uuid.UUID) base.Scope {
	return base.ForCompany(companyID)
}

func (r *membershipRepository) Create(ctx context.Context, membership *entity.Membership) error {
	return r.Base.Create(ctx, membership)
}

func (r *membershipRepository) Update(ctx context.Context, membership *entity.Membership) error {
	return r.Base.Update(ctx, membership)
}

func (r *membershipRepository) FindActiveByUserAndCompany(
	ctx context.Context,
	userID, companyID uuid.UUID,
) (*entity.Membership, error) {
	return r.Base.FindOne(ctx,
		forTenant(companyID),
		base.Where("user_id = ?", userID),
		base.Where("status = ?", entity.MembershipActive),
	)
}

// ListActiveByUser is unpaginated by design.
//
// The result set is bounded by construction — a person belongs to a handful of
// companies, not thousands — and it backs a switcher menu that must render in
// one request. FindMany is the sanctioned choice for a bounded set; see
// RepositoryConvention §9.
func (r *membershipRepository) ListActiveByUser(
	ctx context.Context,
	userID uuid.UUID,
) ([]entity.Membership, error) {
	return r.Base.FindMany(ctx,
		base.Where("user_id = ?", userID),
		base.Where("status = ?", entity.MembershipActive),
	)
}

func (r *membershipRepository) FindByID(
	ctx context.Context,
	membershipID, companyID uuid.UUID,
) (*entity.Membership, error) {
	return r.Base.FindByID(ctx, membershipID, forTenant(companyID))
}

func (r *membershipRepository) ListByCompany(
	ctx context.Context,
	companyID uuid.UUID,
	query dto.ListMembershipsQuery,
) (pagination.Page[entity.Membership], error) {
	scopes := []base.Scope{forTenant(companyID)}

	if query.Status != "" {
		scopes = append(scopes, base.Where("memberships.status = ?", query.Status))
	}
	if query.Role != "" {
		scopes = append(scopes, base.Where("memberships.role = ?", query.Role))
	}

	return r.Base.FindAll(ctx, query.Request, scopes...)
}

func (r *membershipRepository) FindByUserInCompany(
	ctx context.Context,
	companyID, userID uuid.UUID,
) (*entity.Membership, error) {
	return r.Base.FindOne(ctx,
		forTenant(companyID),
		base.Where("user_id = ?", userID),
	)
}

func (r *membershipRepository) Delete(
	ctx context.Context,
	membershipID, companyID uuid.UUID,
) error {
	return r.Base.Delete(ctx, membershipID, forTenant(companyID))
}

func (r *membershipRepository) CountOwners(
	ctx context.Context,
	companyID uuid.UUID,
) (int64, error) {
	return r.Base.Count(ctx,
		forTenant(companyID),
		base.Where("role = ?", entity.RoleOwner),
		base.Where("status = ?", entity.MembershipActive),
	)
}
