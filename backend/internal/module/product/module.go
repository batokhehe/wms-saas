package product

import (
	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
	"github.com/gin-gonic/gin"
)

type Module struct {
	handler     *handler.Handler
	verifier    middleware.TokenVerifier
	companies   middleware.CompanyResolver
	permissions middleware.PermissionResolver
}

func New(deps module.Dependencies, verifier middleware.TokenVerifier, companies middleware.CompanyResolver, permissions middleware.PermissionResolver, cv service.CategoryVerifier, bv service.BrandVerifier, uv service.UOMVerifier, iv service.InventoryVerifier, sv service.StockVerifier) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	svc := service.New(repo, deps.Clock, deps.IDs, deps.Tx, cv, bv, uv, iv, sv)
	return &Module{handler: handler.New(svc), verifier: verifier, companies: companies, permissions: permissions}
}
func (m *Module) Name() string { return "product" }
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier, m.companies, m.permissions)
}
