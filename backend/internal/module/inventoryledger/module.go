// Package inventoryledger implements the append-only inventory ledger.
//
// It records every stock transition and NEVER owns stock: InventoryPosition
// remains the only source of truth for quantities, and every balance stored here
// is a snapshot of what that position reported at the moment of the movement.
//
// Immutability is structural, not procedural: the aggregate has no setters, the
// repository exposes no Update or Delete, the HTTP surface is read-only, and a
// database trigger rejects UPDATE and DELETE outright.
//
// The integration seam in service/interfaces.go is interfaces ONLY — no event bus
// is implemented, and the wiring decision belongs to the composition root.
package inventoryledger

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Module is the inventory-ledger vertical slice.
type Module struct {
	handler *handler.Handler
	service *service.Service

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
func New(
	deps module.Dependencies,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	svc := service.New(repo, deps.Clock, deps.IDs, deps.Tx)

	return &Module{
		handler:     handler.New(svc),
		service:     svc,
		verifier:    verifier,
		companies:   companies,
		permissions: permissions,
	}
}

// Subscriber exposes the ledger as an InventoryEventSubscriber, so the
// composition root can connect it to a publisher without reaching into the
// module's private graph.
func (m *Module) Subscriber() service.InventoryEventSubscriber { return m.service }

// Name identifies the module.
func (m *Module) Name() string { return "inventoryledger" }

// RegisterV1 mounts the module under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier, m.companies, m.permissions)
}
