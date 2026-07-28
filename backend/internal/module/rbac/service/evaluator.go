package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Evaluator resolves what a caller may do in the active company.
//
// It satisfies middleware.PermissionResolver. The interface is owned by the
// middleware package, so this type depends on middleware rather than the other
// way round — the same direction as auth's token verifier and tenancy's company
// resolver.
//
// # Resolution
//
//	membership.role (a NAME, from RequestContext.Role)
//	      +  active company id
//	          ↓
//	   roles(company_id, name)
//	          ↓
//	   role_permissions  ──►  permissions.code
//
// The role NAME is the join key. That is what lets RBAC integrate without
// altering the memberships table: no foreign key is added, and tenancy is
// unaware RBAC exists.
type Evaluator struct {
	roles       repository.RoleRepository
	permissions repository.PermissionRepository
	provisioner *Provisioner
}

var _ middleware.PermissionResolver = (*Evaluator)(nil)

// NewEvaluator builds the evaluator.
func NewEvaluator(
	roles repository.RoleRepository,
	permissions repository.PermissionRepository,
	provisioner *Provisioner,
) *Evaluator {
	return &Evaluator{roles: roles, permissions: permissions, provisioner: provisioner}
}

// Resolve returns the permission codes a role grants within a company.
//
// It fails CLOSED at every step: an unknown role, a role with no grants, or a
// company whose provisioning failed all yield an empty set, which denies every
// permission check. The alternative — falling back to a built-in default when
// the database says otherwise — would mean a company that deliberately revoked
// a permission silently regains it whenever a lookup misses.
func (e *Evaluator) Resolve(
	ctx context.Context, companyID uuid.UUID, roleName string,
) ([]string, error) {
	if roleName == "" {
		// A membership with no role name cannot be resolved. This should not
		// occur — memberships.role is NOT NULL — so it means the caller reached
		// here without a company context.
		return nil, nil
	}

	role, err := e.findRole(ctx, companyID, roleName)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, nil
	}

	byRole, err := e.permissions.CodesByRole(ctx, []uuid.UUID{role.ID})
	if err != nil {
		return nil, err
	}

	codes := byRole[role.ID]
	result := make([]string, 0, len(codes))
	for _, code := range codes {
		result = append(result, code.String())
	}

	return result, nil
}

// findRole looks a role up, provisioning the company's system roles once if the
// first attempt misses.
//
// The retry exists because provisioning is lazy: a company created before this
// sprint, or one whose first RBAC call is this very request, has no rows yet.
// Without the retry its OWNER would be denied everything on their first
// request and succeed on their second, which is exactly the kind of
// intermittent authorisation failure that is impossible to diagnose from a bug
// report.
//
// Provisioning is attempted only for SYSTEM role names. A membership naming a
// custom role that does not exist is a genuine miss, and re-provisioning would
// not create it.
func (e *Evaluator) findRole(
	ctx context.Context, companyID uuid.UUID, roleName string,
) (*entity.Role, error) {
	role, err := e.roles.FindByName(ctx, companyID, roleName)
	if err == nil {
		return role, nil
	}
	if !errors.Is(err, apperror.ErrNotFound) {
		return nil, err
	}

	if !entity.IsSystemRoleName(roleName) {
		return nil, nil
	}

	if err := e.provisioner.EnsureSystemRoles(ctx, companyID); err != nil {
		return nil, err
	}

	role, err = e.roles.FindByName(ctx, companyID, roleName)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return role, nil
}

// ResolveSet returns the caller's permissions as a Set, for in-process checks.
//
// Used by the RBAC service itself — for example to decide whether stripping a
// role would lock the caller out — where a []string would force a linear scan.
func (e *Evaluator) ResolveSet(
	ctx context.Context, companyID uuid.UUID, roleName string,
) (entity.Set, error) {
	codes, err := e.Resolve(ctx, companyID, roleName)
	if err != nil {
		return nil, err
	}

	set := make(entity.Set, len(codes))
	for _, code := range codes {
		set[entity.Code(code)] = struct{}{}
	}

	return set, nil
}
