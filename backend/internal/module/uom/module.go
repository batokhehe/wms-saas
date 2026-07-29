package uom

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

type Module struct {
	handler  *handler.Handler
	verifier middleware.TokenVerifier
}

var (
	_ module.Module      = (*Module)(nil)
	_ module.V1Registrar = (*Module)(nil)
)

func New(deps module.Dependencies, verifier middleware.TokenVerifier) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	svc := service.New(repo, deps.Clock, deps.IDs, deps.Tx)

	return &Module{
		handler:  handler.New(svc),
		verifier: verifier,
	}
}

func (m *Module) Name() string { return "uom" }

func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier)
}
