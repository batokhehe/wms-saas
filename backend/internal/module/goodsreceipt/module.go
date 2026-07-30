// Package goodsreceipt implements the Goods Receipt inbound domain.
//
// # Position in the inbound chain
//
//	PurchaseOrder -> ASN -> GoodsReceipt -> QualityInspection -> Putaway
//
// A receipt is the document that says "this arrived". It holds no stock: posting
// happens on Receive, which books every line into the Inventory module inside the
// receipt's own transaction, so a receipt marked RECEIVED always has stock behind
// it and a failed posting rolls the document back.
//
// The DocumentReference value object is where the chain hangs from. A receipt may
// reference a PURCHASE_ORDER, an ASN, or nothing at all. Today the reference is
// stored and validated but does not update the referenced document — see
// docs/Inbound.md for the sequencing.
package goodsreceipt

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Config carries the module's collaborators, so New does not grow a positional
// argument list nobody can read at the call site.
type Config struct {
	Verifier    middleware.TokenVerifier
	Companies   middleware.CompanyResolver
	Permissions middleware.PermissionResolver

	Warehouses service.WarehouseVerifier
	Locations  service.LocationVerifier
	Products   service.ProductVerifier
	Stock      service.StockPoster

	// Orders receives the arrival against the purchase order a receipt
	// references, inside the receipt's own transaction. Nil means the REFUSING
	// default: a receipt against an order cannot be posted without it.
	Orders service.PurchaseOrderReceiver
}

// Module is the goods-receipt vertical slice.
type Module struct {
	handler *handler.Handler
	service *service.Service

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
func New(deps module.Dependencies, cfg Config) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	events := service.NewLogEventPublisher(deps.Logger)
	svc := service.New(repo, cfg.Warehouses, cfg.Locations, cfg.Products, cfg.Stock,
		cfg.Orders, deps.Clock, deps.IDs, deps.Tx, events)

	return &Module{
		handler:     handler.New(svc),
		service:     svc,
		verifier:    cfg.Verifier,
		companies:   cfg.Companies,
		permissions: cfg.Permissions,
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "goodsreceipt" }

// Service exposes the application service to the composition root, for the
// downstream inbound modules that act on a received document.
func (m *Module) Service() *service.Service { return m.service }

// RegisterV1 mounts the module under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier, m.companies, m.permissions)
}
