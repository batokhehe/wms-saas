// Package rbac implements role-based access control.
//
// # How it integrates without changing Membership
//
// memberships.role already stores a role NAME ('OWNER', 'ADMIN', 'STAFF'), and
// middleware.ResolveCompany already puts that name on RequestContext.Role. This
// module resolves (company_id, role_name) → roles → role_permissions →
// permissions.
//
// No foreign key is added to memberships, no column changes, and the tenancy
// module is unaware RBAC exists. The join is by name within a company.
//
// The cost of that choice is that a role rename would orphan the memberships
// naming it, so no rename is exposed for any role — see entity.Role.CanRename
// and docs/RBAC.md.
package rbac

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Module is the RBAC vertical slice.
type Module struct {
	roles       *handler.RoleHandler
	permissions *handler.PermissionHandler
	evaluator   *service.Evaluator

	verifier  middleware.TokenVerifier
	companies middleware.CompanyResolver
}

// Compile-time assertions. If a contract method drifts, the build fails here
// rather than the module silently vanishing from the router.
var (
	_ module.Module      = (*Module)(nil)
	_ module.V1Registrar = (*Module)(nil)
)

// New constructs the module and its internal dependency graph.
//
// Manual DI, readable top to bottom: repositories, then the provisioner and
// evaluator, then services, then handlers. Two collaborators arrive from
// outside — the auth module's token verifier and the tenancy module's company
// resolver — both as interfaces owned by the middleware package, so this module
// imports neither auth nor tenancy.
func New(
	deps module.Dependencies,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
) *Module {
	roles := repository.NewRoleRepository(deps.DB, deps.IDs)
	permissions := repository.NewPermissionRepository(deps.DB, deps.IDs)
	grants := repository.NewRolePermissionRepository(deps.DB, deps.IDs)

	events := service.NewLogEventPublisher(deps.Logger)

	provisioner := service.NewProvisioner(roles, permissions, grants, deps.Tx, deps.Clock)
	evaluator := service.NewEvaluator(roles, permissions, provisioner)

	roleSvc := service.NewRoleService(
		roles, permissions, grants, provisioner, deps.Clock, deps.Tx, events)
	permissionSvc := service.NewPermissionService(permissions, evaluator)

	return &Module{
		roles:       handler.NewRoleHandler(roleSvc),
		permissions: handler.NewPermissionHandler(permissionSvc),
		evaluator:   evaluator,
		verifier:    verifier,
		companies:   companies,
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "rbac" }

// Resolver exposes the permission evaluator so bootstrap can hand it to other
// modules' guarded routes.
//
// It returns middleware.PermissionResolver — an interface owned by the
// consumer, not by this module. Future modules therefore depend on the
// middleware package, not on rbac, which keeps the no-cross-module-imports rule
// intact for the one dependency every guarded module needs.
func (m *Module) Resolver() middleware.PermissionResolver { return m.evaluator }

// RegisterV1 mounts the module under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.roles, m.permissions, m.verifier, m.companies, m.evaluator)
}
