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

// MembershipService implements the member-management use cases.
type MembershipService struct {
	memberships repository.MembershipRepository
	companies   repository.CompanyRepository
	directory   UserDirectory
	clock       port.Clock
	tx          transaction.Manager
	events      EventPublisher
}

// NewMembershipService builds the service.
func NewMembershipService(
	memberships repository.MembershipRepository,
	companies repository.CompanyRepository,
	directory UserDirectory,
	clock port.Clock,
	tx transaction.Manager,
	events EventPublisher,
) *MembershipService {
	return &MembershipService{
		memberships: memberships,
		companies:   companies,
		directory:   directory,
		clock:       clock,
		tx:          tx,
		events:      events,
	}
}

// List returns the members of the caller's active company.
//
// The company comes from the RequestContext, never from a request parameter, so
// a client cannot list another tenant's members by changing a query string.
func (s *MembershipService) List(
	ctx context.Context,
	query dto.ListMembershipsQuery,
) (pagination.Page[dto.MembershipResponse], error) {
	companyID, err := appcontext.From(ctx).RequireTenant()
	if err != nil {
		return pagination.Page[dto.MembershipResponse]{}, err
	}

	if err := query.Request.Apply(dto.MembershipSortOptions()); err != nil {
		return pagination.Page[dto.MembershipResponse]{}, err
	}

	page, err := s.memberships.ListByCompany(ctx, companyID, query)
	if err != nil {
		return pagination.Page[dto.MembershipResponse]{}, err
	}

	return mapper.ToMembershipPage(page), nil
}

// Mine returns every company the caller can act in, for the switcher menu.
//
// Scoped by user rather than by company — see MembershipRepository for why that
// is the correct boundary for this question.
func (s *MembershipService) Mine(
	ctx context.Context,
) ([]dto.MembershipWithCompanyResponse, error) {
	userID, err := appcontext.From(ctx).RequireUser()
	if err != nil {
		return nil, err
	}

	memberships, err := s.memberships.ListActiveByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Empty rather than nil, so the JSON encoder emits [] and not null.
	result := make([]dto.MembershipWithCompanyResponse, 0, len(memberships))

	for i := range memberships {
		membership := &memberships[i]

		// Unscoped is correct here: the ACTIVE membership just loaded IS the
		// access check. Calling FindAccessible would re-run the same
		// reachability subquery once per row for no additional safety.
		company, err := s.companies.FindByIDUnscoped(ctx, membership.CompanyID)
		if err != nil {
			if errors.Is(err, apperror.ErrNotFound) {
				// The company was soft-deleted while the membership survived.
				// Skip it rather than failing the whole menu — one dead tenant
				// must not stop a person reaching their other companies.
				continue
			}
			return nil, err
		}

		result = append(result, mapper.ToMembershipWithCompany(membership, company))
	}

	return result, nil
}

// Invite adds a person to the caller's active company.
//
// Flow: resolve tenant → validate → look up the invitee → reject duplicates →
// create a PENDING membership.
//
// The new membership is PENDING, not ACTIVE. The row exists so the invitation
// is durable and the seat is reserved, but it grants nothing until accepted —
// which is what stops an invitation being used as an access grant. Only ACTIVE
// memberships resolve a company context.
//
// No acceptance endpoint exists in this sprint, so an invited member cannot yet
// activate. That is a deliberate scope boundary, documented in Membership.md.
func (s *MembershipService) Invite(
	ctx context.Context,
	req dto.InviteMemberRequest,
) (dto.MembershipResponse, error) {
	rc := appcontext.From(ctx)

	inviterID, err := rc.RequireUser()
	if err != nil {
		return dto.MembershipResponse{}, err
	}

	companyID, err := rc.RequireTenant()
	if err != nil {
		return dto.MembershipResponse{}, err
	}

	if err := validator.ValidateInvite(req); err != nil {
		return dto.MembershipResponse{}, err
	}

	// Deliberately vague, and reused for both "no such account" and "already a
	// member". Telling an inviter which of the two applies turns this endpoint
	// into an account enumeration oracle for anyone holding a single valid
	// membership — they could probe arbitrary addresses for registration.
	inviteFailed := apperror.Conflict(
		"That address cannot be invited. It may already be a member, or no account exists.").
		WithOp("tenancy.membership.Invite")

	inviteeID, err := s.directory.FindIDByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return dto.MembershipResponse{}, inviteFailed
		}
		return dto.MembershipResponse{}, err
	}

	var response dto.MembershipResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		// Any existing membership blocks a re-invite, whatever its status: an
		// ACTIVE member is already in, a PENDING one already has an invitation,
		// and a SUSPENDED one must be reinstated rather than re-invited.
		//
		// The partial unique index is the real guarantee; this check exists so
		// the common case produces a clear message instead of a constraint
		// violation.
		existing, err := s.memberships.FindByUserInCompany(ctx, companyID, inviteeID)
		if err != nil && !errors.Is(err, apperror.ErrNotFound) {
			return err
		}
		if existing != nil && err == nil {
			return inviteFailed
		}

		membership := entity.Membership{
			CompanyID: companyID,
			UserID:    inviteeID,
			Role:      entity.Role(req.Role),
			Status:    entity.MembershipPending,
			InvitedBy: &inviterID,
		}
		if err := s.memberships.Create(ctx, &membership); err != nil {
			return err
		}

		response = mapper.ToMembershipResponse(&membership)

		// The invitee's id is recorded, not their email: an audit record must
		// carry identifiers rather than personal data.
		s.publish(ctx, entity.EventMemberInvited, companyID, inviterID, map[string]any{
			"membership_id": membership.ID.String(),
			"invitee_id":    inviteeID.String(),
			"role":          req.Role,
		})
		return nil
	})
	if err != nil {
		return dto.MembershipResponse{}, err
	}

	return response, nil
}

// Remove revokes a membership in the caller's active company.
//
// Tenant-scoped: a membership id belonging to another company resolves to
// NOT_FOUND, so an attacker cannot remove members of a tenant they can see the
// id of.
func (s *MembershipService) Remove(ctx context.Context, membershipID uuid.UUID) error {
	rc := appcontext.From(ctx)

	actorID, err := rc.RequireUser()
	if err != nil {
		return err
	}

	companyID, err := rc.RequireTenant()
	if err != nil {
		return err
	}

	return s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		membership, err := s.memberships.FindByID(ctx, membershipID, companyID)
		if err != nil {
			return err
		}

		// Last-owner protection. Removing the final owner would leave a company
		// nobody can administer and — because ownership can only be granted at
		// creation — no way to ever restore one. Enforced here rather than by a
		// database constraint because it is a business rule about a COUNT, which
		// a CHECK constraint cannot express.
		//
		// This runs inside the transaction, so the count and the delete cannot
		// interleave with a concurrent removal of the other owner.
		if membership.IsOwner() {
			owners, err := s.memberships.CountOwners(ctx, companyID)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return apperror.Conflict(
					"The last owner of a company cannot be removed").
					WithOp("tenancy.membership.Remove")
			}
		}

		if err := s.memberships.Delete(ctx, membershipID, companyID); err != nil {
			return err
		}

		s.publish(ctx, entity.EventMemberRemoved, companyID, actorID, map[string]any{
			"membership_id": membershipID.String(),
			"removed_user":  membership.UserID.String(),
			"role":          string(membership.Role),
		})
		return nil
	})
}

// publish emits a domain event tagged with the current request id.
func (s *MembershipService) publish(
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
