package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/validator"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// RoleService implements the role-management use cases.
//
// It depends on repository interfaces and on ports, never on concrete
// infrastructure — no *gorm.DB, no time.Now().
type RoleService struct {
	roles       repository.RoleRepository
	permissions repository.PermissionRepository
	grants      repository.RolePermissionRepository
	provisioner *Provisioner
	clock       port.Clock
	tx          transaction.Manager
	events      EventPublisher
}

// NewRoleService builds the service.
func NewRoleService(
	roles repository.RoleRepository,
	permissions repository.PermissionRepository,
	grants repository.RolePermissionRepository,
	provisioner *Provisioner,
	clock port.Clock,
	tx transaction.Manager,
	events EventPublisher,
) *RoleService {
	return &RoleService{
		roles:       roles,
		permissions: permissions,
		grants:      grants,
		provisioner: provisioner,
		clock:       clock,
		tx:          tx,
		events:      events,
	}
}

// List returns the roles of the caller's active company, with permissions.
func (s *RoleService) List(
	ctx context.Context, query dto.ListRolesQuery,
) (pagination.Page[dto.RoleResponse], error) {
	companyID, err := appcontext.From(ctx).RequireTenant()
	if err != nil {
		return pagination.Page[dto.RoleResponse]{}, err
	}

	// Provisioned here so a company's first visit to the roles screen shows the
	// three system roles rather than an empty list.
	if err := s.provisioner.EnsureSystemRoles(ctx, companyID); err != nil {
		return pagination.Page[dto.RoleResponse]{}, err
	}

	if err := query.Request.Apply(dto.RoleSortOptions()); err != nil {
		return pagination.Page[dto.RoleResponse]{}, err
	}

	page, err := s.roles.List(ctx, companyID, query)
	if err != nil {
		return pagination.Page[dto.RoleResponse]{}, err
	}

	// One query for every role on the page, not one per role. See
	// PermissionRepository.CodesByRole.
	roleIDs := make([]uuid.UUID, 0, len(page.Items))
	for i := range page.Items {
		roleIDs = append(roleIDs, page.Items[i].ID)
	}

	byRole, err := s.permissions.CodesByRole(ctx, roleIDs)
	if err != nil {
		return pagination.Page[dto.RoleResponse]{}, err
	}

	return mapper.ToRolePage(page, byRole), nil
}

// Create defines a new custom role.
//
// The role and its initial grants are one transaction. Without it, a failure
// after the role row is written leaves a role with no permissions whose name is
// now taken, so the retry fails with a conflict the caller cannot resolve.
func (s *RoleService) Create(
	ctx context.Context, req dto.CreateRoleRequest,
) (dto.RoleResponse, error) {
	rc := appcontext.From(ctx)

	actorID, err := rc.RequireUser()
	if err != nil {
		return dto.RoleResponse{}, err
	}
	companyID, err := rc.RequireTenant()
	if err != nil {
		return dto.RoleResponse{}, err
	}

	if err := validator.ValidateCreateRole(req); err != nil {
		return dto.RoleResponse{}, err
	}

	var response dto.RoleResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		name := entity.NormalizeRoleName(req.Name)

		// Checked explicitly so the common case produces a clear message. The
		// unique index remains the real guarantee against a race.
		taken, err := s.roles.ExistsByName(ctx, companyID, name)
		if err != nil {
			return err
		}
		if taken {
			return apperror.Conflict("A role with this name already exists").
				WithOp("rbac.role.Create")
		}

		role := mapper.FromCreateRoleRequest(req, companyID)
		if err := s.roles.Create(ctx, &role); err != nil {
			return err
		}

		codes := toCodes(req.Permissions)
		granted, err := s.applyGrants(ctx, &role, codes)
		if err != nil {
			return err
		}

		response = mapper.ToRoleResponse(&role, granted)

		s.publish(ctx, entity.EventRoleCreated, companyID, actorID, map[string]any{
			"role_id":     role.ID.String(),
			"role_name":   role.Name,
			"permissions": mapper.SortedCodes(granted),
		})
		return nil
	})
	if err != nil {
		return dto.RoleResponse{}, err
	}

	return response, nil
}

// Update applies a partial update to a role.
//
// Only the description is mutable. Name is immutable for system AND custom
// roles, because memberships point at roles by name with no foreign key — see
// entity.Role.CanRename.
func (s *RoleService) Update(
	ctx context.Context, roleID uuid.UUID, req dto.UpdateRoleRequest,
) (dto.RoleResponse, error) {
	rc := appcontext.From(ctx)

	actorID, err := rc.RequireUser()
	if err != nil {
		return dto.RoleResponse{}, err
	}
	companyID, err := rc.RequireTenant()
	if err != nil {
		return dto.RoleResponse{}, err
	}

	if err := validator.ValidateUpdateRole(req); err != nil {
		return dto.RoleResponse{}, err
	}

	var response dto.RoleResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		role, err := s.roles.FindByID(ctx, roleID, companyID)
		if err != nil {
			return err
		}

		// Defence in depth. The repository already filtered by tenant, so this
		// can only fire if that filter is ever broken — which is exactly when a
		// cross-tenant write would otherwise go unnoticed.
		if !role.BelongsTo(companyID) {
			return apperror.Forbidden("This role belongs to another company").
				WithOp("rbac.role.Update")
		}

		mapper.ApplyUpdateRoleRequest(role, req)

		if err := s.roles.Update(ctx, role); err != nil {
			return err
		}

		codes, err := s.currentCodes(ctx, role.ID)
		if err != nil {
			return err
		}

		response = mapper.ToRoleResponse(role, codes)

		s.publish(ctx, entity.EventRoleUpdated, companyID, actorID, map[string]any{
			"role_id":   role.ID.String(),
			"role_name": role.Name,
		})
		return nil
	})
	if err != nil {
		return dto.RoleResponse{}, err
	}

	return response, nil
}

// Delete removes a custom role.
//
// System roles are protected. memberships.role names them with no foreign key,
// so deleting one would strand every member holding it: their role would
// resolve to nothing, they would silently lose all permissions, and there is no
// API to re-create a system role by hand.
func (s *RoleService) Delete(ctx context.Context, roleID uuid.UUID) error {
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
		role, err := s.roles.FindByID(ctx, roleID, companyID)
		if err != nil {
			return err
		}

		if !role.CanDelete() {
			return apperror.Conflict("System roles cannot be deleted").
				WithOp("rbac.role.Delete")
		}

		// The grants are left in place rather than cascaded. They are
		// unreachable once the role is soft-deleted (every read joins through
		// it), and retaining them means restoring a deleted role would restore
		// its permissions too — plus the audit trail of what it once granted
		// survives, which is the point of soft deletion.
		if err := s.roles.Delete(ctx, roleID, companyID); err != nil {
			return err
		}

		s.publish(ctx, entity.EventRoleDeleted, companyID, actorID, map[string]any{
			"role_id":   roleID.String(),
			"role_name": role.Name,
		})
		return nil
	})
}

// SetPermissions replaces a role's entire permission set.
//
// The request carries the desired FINAL state; the diff is computed here so the
// audit trail records individual grants and revocations rather than one opaque
// "replaced". That distinction is what makes the log answer "when did ADMIN
// lose company.delete?".
func (s *RoleService) SetPermissions(
	ctx context.Context, roleID uuid.UUID, req dto.SetRolePermissionsRequest,
) (dto.RoleResponse, error) {
	rc := appcontext.From(ctx)

	// The actor is read inside applyGrants from the same context, so it is not
	// bound here; RequireUser still runs so an unauthenticated caller is
	// rejected before any work happens.
	if _, err := rc.RequireUser(); err != nil {
		return dto.RoleResponse{}, err
	}
	companyID, err := rc.RequireTenant()
	if err != nil {
		return dto.RoleResponse{}, err
	}

	if err := validator.ValidateSetPermissions(req); err != nil {
		return dto.RoleResponse{}, err
	}

	var response dto.RoleResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		role, err := s.roles.FindByID(ctx, roleID, companyID)
		if err != nil {
			return err
		}

		// The OWNER role cannot be weakened.
		//
		// Ownership is the recovery path for every other authorisation mistake:
		// if an administrator strips ADMIN of role.assign_permissions, the owner
		// puts it back. Allowing OWNER itself to be reduced would let a company
		// lock itself out of its own account with no way back short of database
		// surgery — and the caller doing it may not realise until the next time
		// they need the permission they just removed.
		if role.IsOwnerRole() {
			return apperror.Conflict(
				"The OWNER role's permissions cannot be changed").
				WithOp("rbac.role.SetPermissions")
		}

		desired := toCodes(req.Permissions)

		granted, err := s.applyGrants(ctx, role, desired)
		if err != nil {
			return err
		}

		response = mapper.ToRoleResponse(role, granted)
		return nil
	})
	if err != nil {
		return dto.RoleResponse{}, err
	}

	return response, nil
}

// currentCodes returns the codes a role currently grants.
func (s *RoleService) currentCodes(
	ctx context.Context, roleID uuid.UUID,
) ([]entity.Code, error) {
	byRole, err := s.permissions.CodesByRole(ctx, []uuid.UUID{roleID})
	if err != nil {
		return nil, err
	}
	return byRole[roleID], nil
}

// applyGrants reconciles a role's grants toward the desired set and returns the
// result.
//
// It emits one PermissionAssigned and one PermissionRevoked event describing
// the DELTA, not the final state: an audit reader needs to know what changed,
// and a "final state" record forces them to diff two log lines by hand.
func (s *RoleService) applyGrants(
	ctx context.Context, role *entity.Role, desired []entity.Code,
) ([]entity.Code, error) {
	rc := appcontext.From(ctx)
	actorID := uuid.Nil
	if rc.UserID != nil {
		actorID = *rc.UserID
	}

	current, err := s.currentCodes(ctx, role.ID)
	if err != nil {
		return nil, err
	}

	currentSet := entity.NewSet(current...)
	desiredSet := entity.NewSet(desired...)

	var toAdd, toRemove []entity.Code
	for code := range desiredSet {
		if !currentSet.Has(code) {
			toAdd = append(toAdd, code)
		}
	}
	for code := range currentSet {
		if !desiredSet.Has(code) {
			toRemove = append(toRemove, code)
		}
	}

	if len(toAdd) > 0 {
		ids, err := s.resolveIDs(ctx, toAdd)
		if err != nil {
			return nil, err
		}
		if _, err := s.grants.Grant(ctx, role.ID, ids); err != nil {
			return nil, err
		}

		s.publish(ctx, entity.EventPermissionAssigned, role.CompanyID, actorID, map[string]any{
			"role_id":     role.ID.String(),
			"role_name":   role.Name,
			"permissions": mapper.SortedCodes(toAdd),
		})
	}

	if len(toRemove) > 0 {
		ids, err := s.resolveIDs(ctx, toRemove)
		if err != nil {
			return nil, err
		}
		if _, err := s.grants.Revoke(ctx, role.ID, ids, s.clock.Now()); err != nil {
			return nil, err
		}

		s.publish(ctx, entity.EventPermissionRevoked, role.CompanyID, actorID, map[string]any{
			"role_id":     role.ID.String(),
			"role_name":   role.Name,
			"permissions": mapper.SortedCodes(toRemove),
		})
	}

	return desired, nil
}

// resolveIDs translates permission codes into catalogue ids.
//
// A code with no row means the validator's in-code catalogue and the seeded
// table have drifted. That is an internal inconsistency rather than a client
// error — the validator already rejected genuinely unknown codes — so it is a
// 500, not a 422.
func (s *RoleService) resolveIDs(
	ctx context.Context, codes []entity.Code,
) ([]uuid.UUID, error) {
	permissions, err := s.permissions.FindByCodes(ctx, codes)
	if err != nil {
		return nil, err
	}

	if len(permissions) != len(codes) {
		return nil, apperror.Internal("The permission catalogue is incomplete").
			WithOp("rbac.role.resolveIDs").
			WithCause(errors.New("a validated permission code has no catalogue row"))
	}

	ids := make([]uuid.UUID, 0, len(permissions))
	for i := range permissions {
		ids = append(ids, permissions[i].ID)
	}
	return ids, nil
}

// toCodes normalises a client's permission strings.
func toCodes(raw []string) []entity.Code {
	codes := make([]entity.Code, 0, len(raw))
	for _, value := range raw {
		codes = append(codes, entity.NormalizeCode(value))
	}
	return codes
}

// publish emits a domain event tagged with the current request id.
func (s *RoleService) publish(
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
