// Package tenancy implements the multi-tenancy foundation: companies,
// memberships and company context.
//
// # Why Company and Membership are one module
//
// They look like two features and are one. The company service must create the
// OWNER membership in the same transaction as the company itself, and
// ModuleConvention §6 forbids a module from importing another module's
// repository. Splitting them would therefore force either a cross-module
// repository import (breaking the rule) or a non-atomic onboarding flow
// (leaving orphaned tenants nobody can reach).
//
// # Why User has no CompanyID
//
// A person belongs to many companies — a 3PL operator works for several
// clients. Role is a property of the RELATIONSHIP, not the person: the same
// human can be OWNER of their own company and STAFF at a client's. Both facts
// live in Membership, which is the only link between users and companies.
// See docs/MultiTenancy.md.
package tenancy

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Module is the tenancy vertical slice.
type Module struct {
	companies   *handler.CompanyHandler
	memberships *handler.MembershipHandler
	resolver    *service.ContextResolver
	verifier    middleware.TokenVerifier
}

// Compile-time assertions. If a contract method drifts, the build fails here
// rather than the module silently vanishing from the router.
var (
	_ module.Module      = (*Module)(nil)
	_ module.V1Registrar = (*Module)(nil)
)

// New constructs the module and its internal dependency graph.
//
// Manual DI, readable top to bottom: repositories, then services, then
// handlers. Two collaborators arrive from outside:
//
//   - verifier: the auth module's token verifier, as middleware.TokenVerifier.
//   - directory: an adapter over the identity module's user lookup, as
//     service.UserDirectory.
//
// Both are interfaces owned by the CONSUMER, so this module never imports auth.
func New(
	deps module.Dependencies,
	verifier middleware.TokenVerifier,
	directory service.UserDirectory,
) *Module {
	companies := repository.NewCompanyRepository(deps.DB, deps.IDs)
	memberships := repository.NewMembershipRepository(deps.DB, deps.IDs)

	events := service.NewLogEventPublisher(deps.Logger)

	companySvc := service.NewCompanyService(
		companies, memberships, deps.Clock, deps.Tx, events)
	membershipSvc := service.NewMembershipService(
		memberships, companies, directory, deps.Clock, deps.Tx, events)

	return &Module{
		companies:   handler.NewCompanyHandler(companySvc),
		memberships: handler.NewMembershipHandler(membershipSvc),
		resolver:    service.NewContextResolver(memberships, companies),
		verifier:    verifier,
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "tenancy" }

// Resolver exposes the company-context resolver so bootstrap can hand it to
// other modules' tenant-scoped routes.
//
// It returns middleware.CompanyResolver — an interface owned by the consumer,
// not by this module. Future modules therefore depend on the middleware
// package, not on tenancy, which keeps the no-cross-module-imports rule intact
// for the one dependency every tenant-scoped module needs.
func (m *Module) Resolver() middleware.CompanyResolver { return m.resolver }

// RegisterV1 mounts the module under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.companies, m.memberships, m.verifier, m.resolver)
}
