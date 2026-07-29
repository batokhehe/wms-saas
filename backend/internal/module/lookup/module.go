package lookup

import (
	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
	"github.com/gin-gonic/gin"
)

type Module struct{ handler *handler }

func New(deps module.Dependencies, verifier middleware.TokenVerifier, companies middleware.CompanyResolver, permissions middleware.PermissionResolver) *Module {
	return &Module{handler: &handler{repo: repository{db: deps.DB}, verifier: verifier, companies: companies, permissions: permissions}}
}
func (m *Module) Name() string { return "lookup" }
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	g := rg.Group("/lookups")
	g.Use(middleware.Authenticate(m.handler.verifier), middleware.ResolveCompany(m.handler.companies), middleware.RequireCompany(), middleware.LoadPermissions(m.handler.permissions))
	g.GET("/warehouses", middleware.RequirePermission("warehouse.read"), m.handler.list("warehouses", "code", "name", "name", false))
	g.GET("/locations", middleware.RequirePermission("location.read"), m.handler.list("storage_locations", "code", "code", "code", false))
	g.GET("/products", middleware.RequirePermission("product.read"), m.handler.list("products", "sku", "name", "name", false))
	g.GET("/suppliers", middleware.RequirePermission("supplier.read"), m.handler.list("suppliers", "code", "name", "name", false))
	g.GET("/customers", middleware.RequirePermission("customer.read"), m.handler.list("customers", "code", "name", "name", false))
	g.GET("/uoms", middleware.RequirePermission("uom.read"), m.handler.list("uoms", "code", "name", "code", true))
	g.GET("/categories", middleware.RequirePermission("category.read"), m.handler.list("categories", "code", "name", "code", false))
	g.GET("/brands", middleware.RequirePermission("brand.read"), m.handler.list("brands", "code", "name", "code", false))
}
