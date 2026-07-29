// Package route declares the inventory-ledger module's URL layout.
//
// This file is the module's authorisation policy. Every route is a read and every
// one requires the same single capability — the ledger offers no operation that
// changes anything, so there is nothing else to gate.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/handler"
)

// RegisterV1 mounts the module under /api/v1.
func RegisterV1(
	rg *gin.RouterGroup,
	ledger *handler.Handler,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) {
	scoped := rg.Group("/inventory-ledger")
	scoped.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(companies),
		middleware.RequireCompany(),
		middleware.LoadPermissions(permissions),
	)

	{
		scoped.GET("",
			middleware.RequirePermission(entity.PermissionRead),
			ledger.List)

		// Registered BEFORE /:id so "position" is matched as a literal segment
		// rather than being captured as an entry id.
		scoped.GET("/position/:positionId",
			middleware.RequirePermission(entity.PermissionRead),
			ledger.ListByPosition)

		scoped.GET("/:id",
			middleware.RequirePermission(entity.PermissionRead),
			ledger.Get)
	}
}
