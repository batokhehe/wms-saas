// Package service holds the tenancy module's business rules.
//
// LAYER RULE: no gin, no gorm, no http, no SQL. Every use case takes
// context.Context and returns DTOs and typed errors, so the same logic is
// reachable from an HTTP handler, a CLI command or a unit test with no
// infrastructure.
package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/validator"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// CompanyService implements the tenant use cases.
//
// It depends on repository interfaces and on ports, never on concrete
// infrastructure — no *gorm.DB, no *redis.Client, no time.Now().
type CompanyService struct {
	companies   repository.CompanyRepository
	memberships repository.MembershipRepository
	clock       port.Clock
	tx          transaction.Manager
	events      EventPublisher
}

// NewCompanyService builds the service.
func NewCompanyService(
	companies repository.CompanyRepository,
	memberships repository.MembershipRepository,
	clock port.Clock,
	tx transaction.Manager,
	events EventPublisher,
) *CompanyService {
	return &CompanyService{
		companies:   companies,
		memberships: memberships,
		clock:       clock,
		tx:          tx,
		events:      events,
	}
}

// Create onboards a new tenant and makes the caller its OWNER.
//
// This is the tail of the registration flow:
//
//	register user (auth module) → create company → create OWNER membership
//
// The two writes are one transaction. Without it, a failure between creating
// the company and creating the membership would leave an orphaned tenant that
// NOBODY can reach — not even the person who just created it, because access is
// granted exclusively through memberships. The company code would also be
// permanently consumed, so the retry would fail with a conflict the user cannot
// resolve.
//
// It requires authentication but NOT a company context: creating your first
// company is precisely the case where you have none.
func (s *CompanyService) Create(
	ctx context.Context,
	req dto.CreateCompanyRequest,
) (dto.CurrentCompanyResponse, error) {
	rc := appcontext.From(ctx)

	userID, err := rc.RequireUser()
	if err != nil {
		return dto.CurrentCompanyResponse{}, err
	}

	if err := validator.ValidateCreateCompany(req); err != nil {
		return dto.CurrentCompanyResponse{}, err
	}

	var response dto.CurrentCompanyResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		code := entity.NormalizeCode(req.Code)

		// Checked explicitly so the common case produces a clear message. The
		// unique index remains the real guarantee — two simultaneous creates
		// both pass this check, and the second INSERT is what actually fails,
		// translated to CONFLICT by the repository.
		taken, err := s.companies.ExistsByCode(ctx, code)
		if err != nil {
			return err
		}
		if taken {
			return apperror.Conflict("A company with this code already exists").
				WithOp("tenancy.company.Create")
		}

		company := mapper.FromCreateCompanyRequest(req)
		if err := s.companies.Create(ctx, &company); err != nil {
			return err
		}

		now := s.clock.Now()

		// The founder's membership is ACTIVE immediately, not PENDING: there is
		// nobody to accept an invitation from, and a PENDING owner could not
		// reach the company they just created.
		//
		// InvitedBy stays nil — nobody invited the founder.
		membership := entity.Membership{
			CompanyID: company.ID,
			UserID:    userID,
			Role:      entity.RoleOwner,
			Status:    entity.MembershipActive,
			JoinedAt:  &now,
		}
		if err := s.memberships.Create(ctx, &membership); err != nil {
			return err
		}

		response = mapper.ToCurrentCompanyResponse(&company, &membership)

		s.publish(ctx, entity.EventCompanyCreated, company.ID, userID, map[string]any{
			"code": company.Code,
		})
		return nil
	})
	if err != nil {
		return dto.CurrentCompanyResponse{}, err
	}

	appcontext.Logger(ctx).Info("company created")

	return response, nil
}

// Get returns one company the caller can access.
//
// The repository enforces reachability, so a company the caller has no ACTIVE
// membership in comes back as NOT_FOUND — never FORBIDDEN, which would confirm
// that a company with that id exists.
func (s *CompanyService) Get(
	ctx context.Context,
	companyID uuid.UUID,
) (dto.CompanyResponse, error) {
	userID, err := appcontext.From(ctx).RequireUser()
	if err != nil {
		return dto.CompanyResponse{}, err
	}

	company, err := s.companies.FindAccessible(ctx, companyID, userID)
	if err != nil {
		return dto.CompanyResponse{}, err
	}

	return mapper.ToCompanyResponse(company), nil
}

// List returns the companies the caller can act in.
//
// Scoped by USER rather than by company, deliberately: this endpoint answers
// "where can I work?", and filtering it by the active company would make it
// return exactly one row and be useless as a switcher.
func (s *CompanyService) List(
	ctx context.Context,
	query dto.ListCompaniesQuery,
) (pagination.Page[dto.CompanyResponse], error) {
	userID, err := appcontext.From(ctx).RequireUser()
	if err != nil {
		return pagination.Page[dto.CompanyResponse]{}, err
	}

	// Apply resolves the sort key against the endpoint's allow-list and
	// normalises paging. It must run before the query reaches the repository;
	// the base repository refuses an unapplied request.
	if err := query.Request.Apply(dto.CompanySortOptions()); err != nil {
		return pagination.Page[dto.CompanyResponse]{}, err
	}

	page, err := s.companies.ListAccessible(ctx, userID, query)
	if err != nil {
		return pagination.Page[dto.CompanyResponse]{}, err
	}

	return mapper.ToCompanyPage(page), nil
}

// Update applies a partial update to a company the caller can access.
func (s *CompanyService) Update(
	ctx context.Context,
	companyID uuid.UUID,
	req dto.UpdateCompanyRequest,
) (dto.CompanyResponse, error) {
	userID, err := appcontext.From(ctx).RequireUser()
	if err != nil {
		return dto.CompanyResponse{}, err
	}

	if err := validator.ValidateUpdateCompany(req); err != nil {
		return dto.CompanyResponse{}, err
	}

	var response dto.CompanyResponse

	// Read-modify-write must be atomic: without a transaction, two concurrent
	// updates both read the same row and the second silently overwrites the
	// first's changes.
	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		company, err := s.companies.FindAccessible(ctx, companyID, userID)
		if err != nil {
			return err
		}

		mapper.ApplyUpdateCompanyRequest(company, req)

		if err := s.companies.Update(ctx, company); err != nil {
			return err
		}

		response = mapper.ToCompanyResponse(company)

		s.publish(ctx, entity.EventCompanyUpdated, company.ID, userID, nil)
		return nil
	})
	if err != nil {
		return dto.CompanyResponse{}, err
	}

	return response, nil
}

// Delete soft-deletes a company the caller can access.
//
// Memberships are deliberately NOT cascaded. The database CASCADE fires only on
// a hard delete, and soft-deleting the members here would destroy the record of
// who belonged to the company — exactly the history an audit needs after a
// tenant is closed. The memberships become unreachable anyway, because every
// company read filters on the company's own deleted_at.
//
// Ownership is not checked. RBAC is the next sprint; today any member can
// delete, which is documented in MultiTenancy.md as a known gap rather than
// silently accepted.
func (s *CompanyService) Delete(ctx context.Context, companyID uuid.UUID) error {
	userID, err := appcontext.From(ctx).RequireUser()
	if err != nil {
		return err
	}

	return s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.companies.Delete(ctx, companyID, userID); err != nil {
			return err
		}

		s.publish(ctx, entity.EventCompanyDeleted, companyID, userID, nil)
		return nil
	})
}

// Current returns the caller's active company and their standing in it.
//
// Both come from the RequestContext, which the company middleware populated —
// so this is a read of already-resolved state plus one lookup for the display
// fields, not a second resolution.
func (s *CompanyService) Current(ctx context.Context) (dto.CurrentCompanyResponse, error) {
	rc := appcontext.From(ctx)

	userID, err := rc.RequireUser()
	if err != nil {
		return dto.CurrentCompanyResponse{}, err
	}

	companyID, err := rc.RequireTenant()
	if err != nil {
		return dto.CurrentCompanyResponse{}, err
	}

	company, err := s.companies.FindAccessible(ctx, companyID, userID)
	if err != nil {
		return dto.CurrentCompanyResponse{}, err
	}

	membership, err := s.memberships.FindActiveByUserAndCompany(ctx, userID, companyID)
	if err != nil {
		return dto.CurrentCompanyResponse{}, err
	}

	return mapper.ToCurrentCompanyResponse(company, membership), nil
}

// Switch changes the caller's active tenant.
//
// # Why this does not mint a new token
//
// Access tokens carry no company claim — see Authentication.md §2, which this
// sprint does not change. The active company travels in the X-Company-ID header
// on each request instead. So "switching" is a VALIDATION step: it confirms the
// caller holds an ACTIVE membership in the target and returns the resolved
// context, after which the client sends that company id on subsequent requests.
//
// The alternative — putting the company in the token — would mean reissuing
// credentials on every switch, and would make a stolen token permanently scoped
// to one tenant rather than to none.
//
// It requires authentication but NOT an existing company context: switching is
// exactly what a caller with no active company needs to do.
func (s *CompanyService) Switch(
	ctx context.Context,
	req dto.SwitchCompanyRequest,
) (dto.CurrentCompanyResponse, error) {
	rc := appcontext.From(ctx)

	userID, err := rc.RequireUser()
	if err != nil {
		return dto.CurrentCompanyResponse{}, err
	}

	membership, err := s.memberships.FindActiveByUserAndCompany(ctx, userID, req.CompanyID)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			// Same message whether the company does not exist or the caller is
			// not a member. Distinguishing them would let anyone holding one
			// membership probe for the existence of other tenants.
			return dto.CurrentCompanyResponse{}, apperror.Forbidden(
				"You do not have access to this company").
				WithOp("tenancy.company.Switch")
		}
		return dto.CurrentCompanyResponse{}, err
	}

	company, err := s.companies.FindByIDUnscoped(ctx, req.CompanyID)
	if err != nil {
		return dto.CurrentCompanyResponse{}, err
	}

	// A suspended tenant must not become anyone's working context, even for a
	// member in good standing. Checked here rather than in the middleware so
	// the caller gets a specific, actionable message at the moment they choose.
	if !company.IsOperational() {
		return dto.CurrentCompanyResponse{}, apperror.Forbidden(
			"This company is not active").
			WithOp("tenancy.company.Switch")
	}

	s.publish(ctx, entity.EventCompanySwitched, company.ID, userID, map[string]any{
		"role": string(membership.Role),
	})

	return mapper.ToCurrentCompanyResponse(company, membership), nil
}

// publish emits a domain event tagged with the current request id.
func (s *CompanyService) publish(
	ctx context.Context,
	name entity.EventName,
	companyID, actorID uuid.UUID,
	attributes map[string]any,
) {
	event := entity.NewEvent(name, companyID, actorID, s.clock.Now(), appcontext.RequestID(ctx))
	for key, value := range attributes {
		event = event.With(key, value)
	}

	s.events.Publish(ctx, event)
}
