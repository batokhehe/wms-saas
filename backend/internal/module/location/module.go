// Package location implements the Storage Location domain.
//
// A storage location is a physical place inside a warehouse where inventory can
// exist: a bin, a rack level, a floor position, a receiving dock.
//
// # Why it is a separate aggregate from Warehouse
//
// A location belongs to exactly one warehouse, which is the shape people
// normally model as a child collection. That would be wrong: a large
// distribution centre has tens of thousands of locations, so loading a
// warehouse "with its locations" would be unusable — and making them one
// aggregate would mean locking the whole site to change one bin's capacity.
//
// So they are two aggregates and the reference is BY ID in one direction only.
// Whether the warehouse exists is asked through service.WarehouseVerifier, not
// by loading it.
//
// # Extension points
//
// Five interfaces are declared and none is implemented here:
//
//	CurrentCapacityProvider → Inventory   (how full is this location?)
//	InventoryProvider       → Inventory   (how many SKUs? is it empty?)
//	ReceivingGuard          → Receiving   (may stock be put away here?)
//	PickingGuard            → Picking     (may stock be taken from here?)
//	CycleCountGuard         → Cycle Count (may it be counted?)
//
// Each has a named permissive default so an unwired guard cannot be mistaken
// for a permissive one, and bootstrap swaps in the real implementation when the
// module that owns it exists. No file in this package changes when that happens.
package location

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Module is the location vertical slice.
type Module struct {
	handler *handler.Handler
	repo    repository.Repository

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

// Config bundles the module's collaborators.
//
// A struct rather than a long positional signature: with three platform
// interfaces and six domain extension points, positional arguments of similar
// types are a place where a caller silently transposes two guards and the
// compiler cannot help.
type Config struct {
	Verifier    middleware.TokenVerifier
	Companies   middleware.CompanyResolver
	Permissions middleware.PermissionResolver

	Warehouses service.WarehouseVerifier
	Capacity   service.CurrentCapacityProvider
	Inventory  service.InventoryProvider
	Receiving  service.ReceivingGuard
	Picking    service.PickingGuard
	Counting   service.CycleCountGuard
}

// New constructs the module and its internal dependency graph.
//
// The three platform collaborators are interfaces owned by the MIDDLEWARE
// package, so this module imports neither auth, tenancy nor rbac. The six
// domain collaborators are interfaces this module declares itself.
func New(deps module.Dependencies, cfg Config) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	events := service.NewLogEventPublisher(deps.Logger)

	svc := service.New(service.Dependencies{
		Repo:       repo,
		Warehouses: cfg.Warehouses,
		Capacity:   cfg.Capacity,
		Inventory:  cfg.Inventory,
		Receiving:  cfg.Receiving,
		Picking:    cfg.Picking,
		Counting:   cfg.Counting,
		Clock:      deps.Clock,
		IDs:        deps.IDs,
		Tx:         deps.Tx,
		Events:     events,
	})

	return &Module{
		handler:     handler.New(svc),
		repo:        repo,
		verifier:    cfg.Verifier,
		companies:   cfg.Companies,
		permissions: cfg.Permissions,
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "location" }

// Repository exposes the persistence contract so bootstrap can adapt it to
// another module's extension point.
//
// Specifically: the warehouse module declares a ZoneVerifier for validating its
// default receiving / shipping / staging zone ids, and those zones ARE storage
// locations. Bootstrap adapts this repository to that interface — see
// bootstrap/zone_verifier.go — which closes a gap the warehouse sprint
// documented as open, without either module importing the other.
func (m *Module) Repository() repository.Repository { return m.repo }

// RegisterV1 mounts the module under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier, m.companies, m.permissions)
}
