// Package purchaseorder implements the Purchase Order planning domain.
//
// It is built to the same conventions as supplier, product and inventory:
//
//   - entity.PurchaseOrder is an AGGREGATE ROOT with no exported fields and no
//     setters; PARTIALLY_RECEIVED and COMPLETED are DERIVED from its lines rather
//     than set by a caller.
//   - The service ORCHESTRATES: verify references, call one domain method,
//     persist, publish after commit. It holds no business rules.
//   - Domain events are raised by the AGGREGATE.
//   - A separate persistence model absorbs GORM.
//
// # Position in the inbound chain
//
//	PurchaseOrder -> ASN -> GoodsReceipt -> QualityInspection -> Putaway
//
// The aggregate exposes CanGenerateASN() and IsOpenForReceipt() as the gates the
// downstream modules consult. The ASN module does not exist yet; those predicates
// are the prepared seam it will use, declared here so the rule lives with the
// aggregate that owns it.
package purchaseorder

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Module is the purchase-order vertical slice.
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

// New constructs the module and its internal dependency graph. The three
// platform collaborators arrive as interfaces owned by the MIDDLEWARE package, so
// this module imports none of auth, tenancy or RBAC.
func New(
	deps module.Dependencies,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
	suppliers service.SupplierVerifier,
	warehouses service.WarehouseVerifier,
	products service.ProductVerifier,
) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	events := service.NewLogEventPublisher(deps.Logger)
	svc := service.New(repo, suppliers, warehouses, products,
		deps.Clock, deps.IDs, deps.Tx, events)

	return &Module{
		handler:     handler.New(svc),
		service:     svc,
		verifier:    verifier,
		companies:   companies,
		permissions: permissions,
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "purchaseorder" }

// Service exposes the application service to the composition root, so the Goods
// Receipt flow can book receipts against an order without going over HTTP.
func (m *Module) Service() *service.Service { return m.service }

// RegisterV1 mounts the module under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier, m.companies, m.permissions)
}
