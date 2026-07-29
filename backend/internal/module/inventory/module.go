// Package inventory implements the Inventory domain.
//
// It is a Domain-Driven Design module built to the same conventions as
// warehouse, location and product:
//
//   - entity.Inventory is an AGGREGATE ROOT with no exported fields and no
//     setters. It owns every stock transition; nothing outside it mutates a
//     quantity, so "on-hand is never below reserved" and "a serial is always one
//     unit" are guarantees, not hopes.
//   - The service ORCHESTRATES: load, gather cross-aggregate facts, call one
//     domain method, persist, publish. It holds no business rules.
//   - Domain events are raised by the AGGREGATE and published AFTER commit, so an
//     event exists because a transition persisted.
//   - A separate persistence model absorbs GORM (see repository/model.go).
//
// Extension points are interfaces this module declares and bootstrap injects:
// ProductProvider, WarehouseProvider and LocationProvider verify the references a
// new position points at; ReservationProvider is the seam for the future
// Reservation aggregate. See ../port.
package inventory

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Module is the inventory vertical slice.
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

// Config carries the collaborators the module needs, so the constructor's
// signature does not grow unwieldy as extension points accumulate.
type Config struct {
	Verifier    middleware.TokenVerifier
	Companies   middleware.CompanyResolver
	Permissions middleware.PermissionResolver

	Products   service.ProductVerifier
	Warehouses service.WarehouseVerifier
	Locations  service.LocationVerifier

	// Policy is the per-company stock policy seam. Nil means the conservative
	// DefaultStockPolicy.
	Policy service.StockPolicyProvider
}

// New constructs the module and its internal dependency graph.
//
// The three platform collaborators arrive as interfaces owned by the MIDDLEWARE
// package, so this module imports none of auth, tenancy or RBAC. The four
// providers are passed in rather than constructed here, so bootstrap can wire
// real adapters (or permissive defaults) without editing this file.
func New(deps module.Dependencies, cfg Config) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	events := service.NewLogEventPublisher(deps.Logger)

	svc := service.New(
		repo,
		cfg.Products, cfg.Warehouses, cfg.Locations, cfg.Policy,
		deps.Clock, deps.IDs, deps.Tx, events,
	)

	return &Module{
		handler:     handler.New(svc),
		verifier:    cfg.Verifier,
		companies:   cfg.Companies,
		permissions: cfg.Permissions,
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "inventory" }

// RegisterV1 mounts the module under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier, m.companies, m.permissions)
}
