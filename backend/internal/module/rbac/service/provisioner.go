// Package service holds the RBAC module's business rules.
//
// LAYER RULE: no gin, no gorm, no http, no SQL.
package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Provisioner creates a company's system roles on first use.
//
// # Why provisioning is lazy rather than triggered by company creation
//
// The obvious design is to seed OWNER/ADMIN/STAFF inside the tenancy module's
// company-creation transaction. That would require editing that module, which
// this sprint must not do — and it would also make tenancy depend on RBAC,
// inverting the dependency (authorisation should know about tenancy, not the
// reverse).
//
// Lazy provisioning avoids both. It is called at the start of any RBAC
// operation and by the evaluator on a cache miss, so a company acquires its
// roles the first time anything asks about them. Every company created before
// this sprint is covered by the same path, with no backfill migration.
//
// The cost is one extra existence check on the first RBAC call per company. The
// benefit is that RBAC is entirely additive: no other module changed.
type Provisioner struct {
	roles       repository.RoleRepository
	permissions repository.PermissionRepository
	grants      repository.RolePermissionRepository
	tx          transaction.Manager
	clock       port.Clock
}

// NewProvisioner builds the provisioner.
func NewProvisioner(
	roles repository.RoleRepository,
	permissions repository.PermissionRepository,
	grants repository.RolePermissionRepository,
	tx transaction.Manager,
	clock port.Clock,
) *Provisioner {
	return &Provisioner{
		roles:       roles,
		permissions: permissions,
		grants:      grants,
		tx:          tx,
		clock:       clock,
	}
}

// EnsureSystemRoles provisions OWNER, ADMIN and STAFF for a company if absent.
//
// Idempotent and safe to call on every request. It is a no-op after the first
// call, costing one indexed count; the unique index on (company_id, name) is
// the real guarantee if two requests race, and a losing racer's CONFLICT is
// swallowed because the other transaction did the work.
//
// It deliberately does NOT repair a partially-provisioned company by adding
// missing permissions to an existing role. Once a role exists, its grants are
// the company's to control — silently re-adding a permission an administrator
// deliberately revoked would be a security regression disguised as a fix.
func (p *Provisioner) EnsureSystemRoles(ctx context.Context, companyID uuid.UUID) error {
	existing, err := p.roles.ListAll(ctx, companyID)
	if err != nil {
		return err
	}

	present := make(map[string]struct{}, len(existing))
	for i := range existing {
		present[entity.NormalizeRoleName(existing[i].Name)] = struct{}{}
	}

	missing := make([]string, 0, 3)
	for _, name := range entity.SystemRoleNames() {
		if _, ok := present[name]; !ok {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	err = p.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		for _, name := range missing {
			if err := p.provisionRole(ctx, companyID, name); err != nil {
				return err
			}
		}
		return nil
	})

	// A concurrent request provisioned the same company first. Its rows are
	// correct and identical to the ones this call would have written, so the
	// conflict is the expected outcome of a race rather than a failure.
	if err != nil && errors.Is(err, apperror.ErrConflict) {
		return nil
	}

	return err
}

// provisionRole creates one system role with its default permissions.
func (p *Provisioner) provisionRole(
	ctx context.Context, companyID uuid.UUID, name string,
) error {
	role := entity.Role{
		CompanyID:   companyID,
		Name:        name,
		Description: entity.DefaultDescription(name),
		IsSystem:    true,
	}

	if err := p.roles.Create(ctx, &role); err != nil {
		return err
	}

	defaults := entity.DefaultPermissions(name)
	if len(defaults) == 0 {
		return nil
	}

	permissions, err := p.permissions.FindByCodes(ctx, defaults)
	if err != nil {
		return err
	}

	// A default code with no catalogue row means entity.PermissionCatalogue and
	// the seed migration have drifted. Failing loudly beats provisioning a role
	// that silently lacks permissions its documentation promises.
	if len(permissions) != len(defaults) {
		return apperror.Internal("The permission catalogue is incomplete").
			WithOp("rbac.provisioner.provisionRole").
			WithCause(errors.New(
				"seeded permissions do not cover entity.DefaultPermissions(" + name + ")"))
	}

	ids := make([]uuid.UUID, 0, len(permissions))
	for i := range permissions {
		ids = append(ids, permissions[i].ID)
	}

	_, err = p.grants.Grant(ctx, role.ID, ids)
	return err
}
