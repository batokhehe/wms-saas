// Package warehouse implements the Warehouse domain.
//
// It is the first BUSINESS domain in the system, and the first built with
// Domain-Driven Design rather than as a CRUD module. The difference is
// concrete:
//
//   - entity.Warehouse is an AGGREGATE ROOT with no exported fields and no
//     setters. Its status can only reach ACTIVE through Activate(), which is
//     what makes "an active warehouse always has an address, a contact and a
//     zone" a guarantee rather than a hope.
//   - The service ORCHESTRATES: load, call one domain method, persist, publish.
//     It contains no status comparisons and no readiness rules.
//   - Domain events are raised by the AGGREGATE, so an event exists because a
//     transition happened rather than because a service remembered to log one.
//   - A separate persistence model absorbs GORM, because an ORM cannot map
//     unexported fields — and exporting them would delete the encapsulation the
//     whole design rests on.
//
// Extension points for future modules are interfaces this module declares and
// bootstrap injects: service.DeletionGuard for the Inventory sprint and
// service.ZoneVerifier for the Location sprint. Neither is implemented here.
package warehouse

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Module is the warehouse vertical slice.
type Module struct {
	handler *handler.Handler

	verifier    middleware.TokenVerifier
	companies   middleware.CompanyResolver
	permissions middleware.PermissionResolver
}

// Compile-time assertions. If a contract method drifts, the build fails here
// rather than the module silently vanishing from the router.
var (
	_ module.Module      = (*Module)(nil)
	_ module.V1Registrar = (*Module)(nil)
)

// New constructs the module and its internal dependency graph.
//
// Three collaborators arrive from outside — auth's token verifier, tenancy's
// company resolver and RBAC's permission resolver — all as interfaces owned by
// the MIDDLEWARE package. This module therefore imports none of those three
// modules, which is what keeps ModuleConvention §6 intact for the dependencies
// every business module now needs.
//
// The two guards default to their permissive implementations. They are passed
// as parameters rather than constructed here so that the Inventory and Location
// sprints replace them in bootstrap without editing this file.
func New(
	deps module.Dependencies,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
	deletion service.DeletionGuard,
	zones service.ZoneVerifier,
) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	events := service.NewLogEventPublisher(deps.Logger)

	svc := service.New(repo, deletion, zones, deps.Clock, deps.IDs, deps.Tx, events)

	return &Module{
		handler:     handler.New(svc),
		verifier:    verifier,
		companies:   companies,
		permissions: permissions,
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "warehouse" }

// RegisterV1 mounts the module under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier, m.companies, m.permissions)
}
