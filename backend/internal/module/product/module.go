// Package product implements the Product domain.
//
// It is a Domain-Driven Design module, built the same way as warehouse and
// location:
//
//   - entity.Product is an AGGREGATE ROOT with no exported fields and no
//     setters. Its status reaches ACTIVE only through Activate(), its tracking
//     method changes only through SetTracking(), and it always has exactly one
//     primary barcode — guarantees rather than hopes, because no caller can
//     reach the fields that would break them.
//   - The service ORCHESTRATES: load, gather cross-aggregate facts, call one
//     domain method, persist, publish. It contains no status comparisons and no
//     readiness rules.
//   - Domain events are raised by the AGGREGATE, so an event exists because a
//     transition happened rather than because a service remembered to log one.
//   - A separate persistence model (and two child-table models) absorb GORM,
//     because an ORM cannot map unexported fields — and exporting them would
//     delete the encapsulation the whole design rests on. See
//     repository/model.go and docs/Product.md §4.
//
// Extension points for future modules are interfaces this module declares and
// bootstrap injects: CategoryVerifier, BrandVerifier and UOMVerifier for the
// taxonomy/units sprints, and InventoryProvider for the Inventory sprint. None
// is implemented here.
package product

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Module is the product vertical slice.
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
// modules, which keeps ModuleConvention §6 intact.
//
// The four verifiers default to their permissive implementations. They are
// passed as parameters rather than constructed here so the Category, Brand, UOM
// and Inventory sprints replace them in bootstrap without editing this file.
func New(
	deps module.Dependencies,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
	categories service.CategoryVerifier,
	brands service.BrandVerifier,
	uoms service.UOMVerifier,
	inventory service.InventoryProvider,
) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	events := service.NewLogEventPublisher(deps.Logger)

	svc := service.New(
		repo, categories, brands, uoms, inventory,
		deps.Clock, deps.IDs, deps.Tx, events,
	)

	return &Module{
		handler:     handler.New(svc),
		verifier:    verifier,
		companies:   companies,
		permissions: permissions,
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "product" }

// RegisterV1 mounts the module under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier, m.companies, m.permissions)
}
