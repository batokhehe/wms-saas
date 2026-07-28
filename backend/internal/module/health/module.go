// Package health implements the liveness and readiness probes.
//
// It follows the standard module layout (see docs/ModuleConvention.md), minus
// repository/, entity/, mapper/ and validator/: health owns no persistence and
// accepts no input, so those layers would be empty files. A module omits a
// layer only when it genuinely has nothing to put in it.
package health

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/module/health/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/health/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/health/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Module is the health vertical slice.
type Module struct {
	service *service.Service
	handler *handler.Handler
}

// Compile-time assertions. If a required method is renamed or its signature
// drifts, the build fails here rather than the module silently vanishing from
// the router at runtime.
var (
	_ module.Module        = (*Module)(nil)
	_ module.RootRegistrar = (*Module)(nil)
	_ module.V1Registrar   = (*Module)(nil)
)

// New constructs the module and its internal dependency graph.
//
// Health takes no module.Dependencies because it uses none: its checkers are
// registered by bootstrap after infrastructure connects. Every other module
// will take Dependencies as its single constructor argument.
func New(serviceName, version, env string) *Module {
	svc := service.New(serviceName, version, env)

	return &Module{
		service: svc,
		handler: handler.New(svc),
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "health" }

// Service exposes the health service so bootstrap can register infrastructure
// checkers once Postgres and Redis are connected.
func (m *Module) Service() *service.Service { return m.service }

// RegisterRoot mounts /health, /health/live and /health/ready.
func (m *Module) RegisterRoot(rg *gin.RouterGroup) {
	route.RegisterRoot(rg, m.handler)
}

// RegisterV1 mounts the same probes under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler)
}
