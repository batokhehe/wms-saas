// Package template is the reference scaffold every feature module copies.
//
// It contains NO business logic. It exists to fix the shape of a module — the
// layers, their dependency direction, the naming, the wiring — so that ten
// modules written by different people over two years still look like one
// codebase.
//
// It is a real, compiling package rather than a documentation snippet on
// purpose: a template that does not compile rots silently, and by the time
// someone copies it, it no longer matches the interfaces it claims to satisfy.
// This one breaks the build the day it drifts.
//
// To create a module:
//
//  1. Copy internal/module/template to internal/module/<name>.
//  2. Rename the package declarations and the Resource type.
//  3. Delete the layers you genuinely do not need (see health, which has no
//     repository because it owns no data).
//  4. Register it in bootstrap.buildRegistry.
//
// See docs/ModuleConvention.md for the full rules.
package template

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/module/template/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/template/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/template/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/template/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// Module is the vertical slice.
type Module struct {
	handler *handler.Handler
}

// Compile-time assertions. If a contract method is renamed or its signature
// drifts, the build fails here — rather than the module silently vanishing from
// the router at runtime.
var (
	_ module.Module      = (*Module)(nil)
	_ module.V1Registrar = (*Module)(nil)
)

// New constructs the module and its internal dependency graph.
//
// This is manual dependency injection in miniature: repository, then service,
// then handler, each receiving exactly what it needs. The graph is readable top
// to bottom and requires no framework to understand.
//
// The signature — one Dependencies argument — is identical for every module, so
// registering a new one in bootstrap is always the same single line.
func New(deps module.Dependencies) *Module {
	repo := repository.New(deps.DB, deps.IDs)
	svc := service.New(repo, deps.Cache, deps.Queue, deps.Clock, deps.Tx)

	return &Module{
		handler: handler.New(svc),
	}
}

// Name identifies the module. It must match the package directory name.
func (m *Module) Name() string { return "template" }

// RegisterV1 mounts the module under /api/v1.
//
// A module serving only v1 implements only this method. When /api/v2 is
// introduced, this module is untouched unless it actually needs to change — at
// which point it adds RegisterV2 alongside, and serves both.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler)
}
