package category

import (
	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
	"github.com/gin-gonic/gin"
)

type Module struct {
	handler     *handler.Handler
	verifier    middleware.TokenVerifier
	companies   middleware.CompanyResolver
	permissions middleware.PermissionResolver
}

func New(deps module.Dependencies, verifier middleware.TokenVerifier, companies middleware.CompanyResolver, permissions middleware.PermissionResolver) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	svc := service.New(repo, deps.Clock, deps.IDs, deps.Tx)
	return &Module{handler: handler.New(svc), verifier: verifier, companies: companies, permissions: permissions}
}
func (m *Module) Name() string { return "category" }
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier, m.companies, m.permissions)
}
