// Package customer implements the Customer master-data domain — the structural
// sibling of Supplier, built to identical conventions:
//
//   - entity.Customer is an AGGREGATE ROOT with no exported fields and no setters.
//   - The service ORCHESTRATES: verify the code is unique, call one domain
//     method, persist, publish after commit. It holds no business rules.
//   - Domain events are raised by the AGGREGATE.
//   - A separate persistence model absorbs GORM.
//
// The DeletionGuard in service/guards.go is a PREPARED extension point for the
// future Sales Order sprint — declared with a permissive default, consumed by no
// operation, because this sprint has no delete behaviour.
package customer

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Module is the customer vertical slice.
type Module struct {
	handler *handler.Handler

	verifier    middleware.TokenVerifier
	companies   middleware.CompanyResolver
	permissions middleware.PermissionResolver
}

// Compile-time assertions. If a contract method drifts, the build fails here.
var (
	_ module.Module      = (*Module)(nil)
	_ module.V1Registrar = (*Module)(nil)
)

// New constructs the module and its internal dependency graph.
func New(
	deps module.Dependencies,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	events := service.NewLogEventPublisher(deps.Logger)
	svc := service.New(repo, deps.Clock, deps.IDs, deps.Tx, events)

	return &Module{
		handler:     handler.New(svc),
		verifier:    verifier,
		companies:   companies,
		permissions: permissions,
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "customer" }

// RegisterV1 mounts the module under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier, m.companies, m.permissions)
}
