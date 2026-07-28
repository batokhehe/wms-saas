// Package route declares the product module's URL layout.
//
// This file is the module's authorisation policy. Every route states the
// permission it requires, so "who can do this?" is answered by reading one short
// file rather than by grepping handlers for scattered checks.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/handler"
)

// RegisterV1 mounts the module under /api/v1.
//
// The middleware chain is the platform's full stack, each step depending on the
// one before:
//
//	Authenticate      → WHO the caller is
//	ResolveCompany    → WHERE they are acting
//	RequireCompany    → refuse if nowhere
//	LoadPermissions   → WHAT they may do there (one query, cached per request)
//	RequirePermission → per route
func RegisterV1(
	rg *gin.RouterGroup,
	products *handler.Handler,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) {
	scoped := rg.Group("/products")
	scoped.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(companies),
		middleware.RequireCompany(),
		middleware.LoadPermissions(permissions),
	)

	{
		scoped.GET("",
			middleware.RequirePermission(entity.PermissionRead),
			products.List)

		scoped.POST("",
			middleware.RequirePermission(entity.PermissionCreate),
			products.Create)

		scoped.GET("/:id",
			middleware.RequirePermission(entity.PermissionRead),
			products.Get)

		scoped.PUT("/:id",
			middleware.RequirePermission(entity.PermissionUpdate),
			products.Update)

		// ---------- Attributes ----------
		//
		// Category, brand, measurements, shelf life, tracking, barcodes and
		// alternate units are all routine catalogue edits, so they share
		// product.update. Each is a distinct endpoint rather than a field on the
		// update body because each maps to one intent-revealing domain method and
		// raises its own event — burying them in a general update would make "the
		// tracking method changed" indistinguishable from "someone fixed a typo".
		scoped.PATCH("/:id/category",
			middleware.RequirePermission(entity.PermissionUpdate),
			products.AssignCategory)

		scoped.PATCH("/:id/brand",
			middleware.RequirePermission(entity.PermissionUpdate),
			products.AssignBrand)

		scoped.PATCH("/:id/measurements",
			middleware.RequirePermission(entity.PermissionUpdate),
			products.SetMeasurements)

		scoped.PATCH("/:id/shelf-life",
			middleware.RequirePermission(entity.PermissionUpdate),
			products.SetShelfLife)

		// Changing the tracking method requires only product.update, but the
		// aggregate refuses the change once stock exists — so the permission
		// gates WHO may attempt it while the domain gates WHEN it is allowed.
		scoped.PATCH("/:id/tracking",
			middleware.RequirePermission(entity.PermissionUpdate),
			products.SetTracking)

		// ---------- Barcodes ----------
		scoped.POST("/:id/barcodes",
			middleware.RequirePermission(entity.PermissionUpdate),
			products.AddBarcode)

		scoped.PATCH("/:id/barcodes/primary",
			middleware.RequirePermission(entity.PermissionUpdate),
			products.SetPrimaryBarcode)

		scoped.DELETE("/:id/barcodes/:barcode",
			middleware.RequirePermission(entity.PermissionUpdate),
			products.RemoveBarcode)

		// ---------- Alternate units ----------
		scoped.POST("/:id/uoms",
			middleware.RequirePermission(entity.PermissionUpdate),
			products.AddUOM)

		scoped.DELETE("/:id/uoms/:uomId",
			middleware.RequirePermission(entity.PermissionUpdate),
			products.RemoveUOM)

		// ---------- Lifecycle ----------
		//
		// Activation and deactivation require product.activate, NOT product.update.
		// Editing an attribute is routine; declaring a product fit for operations
		// is a commissioning decision. Granting them together would let anyone who
		// can fix a typo also put a half-configured product into operation.
		scoped.PATCH("/:id/activate",
			middleware.RequirePermission(entity.PermissionActivate),
			products.Activate)

		scoped.PATCH("/:id/deactivate",
			middleware.RequirePermission(entity.PermissionActivate),
			products.Deactivate)

		// Discontinuation is terminal and irreversible, so it requires the
		// destructive product.discontinue rather than the activation permission —
		// the product-module analogue of archiving a warehouse.
		scoped.PATCH("/:id/discontinue",
			middleware.RequirePermission(entity.PermissionDiscontinue),
			products.Discontinue)
	}
}
