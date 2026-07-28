// Package module defines the contract every feature module implements.
//
// This is the seam that makes the architecture modular. Bootstrap knows only
// these interfaces, so adding a feature means writing a module and appending it
// to one slice — never editing the router, the server or another module.
package module

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
)

// Dependencies is the shared infrastructure handed to every module constructor.
//
// Note what is here and what is not: modules receive port interfaces, never
// *redis.Client or *asynq.Client. A module physically cannot reach a vendor SDK
// through this struct, which is what makes the abstraction real rather than
// merely advisory.
//
// DB is the exception and is deliberate: GORM is the persistence contract, and
// wrapping it in a generic repository interface would cost far more than it
// buys. The rule enforced by convention instead is that *gorm.DB may only be
// touched inside a module's repository package.
type Dependencies struct {
	DB      *gorm.DB
	Cache   port.Cache
	Queue   port.Queue
	Storage port.Storage
	Logger  *zap.Logger

	// Clock and IDs replace direct calls to time.Now() and uuid.New(). Handing
	// them to every module means a service's behaviour is fully determined by
	// its inputs, so a test can pin both time and identifiers.
	Clock port.Clock
	IDs   port.IDGenerator

	// Tx runs a unit of work atomically. Modules use it instead of db.Begin();
	// its interface exposes no GORM type, so a service can depend on it without
	// depending on the persistence technology.
	Tx transaction.Manager
}

// Module is one vertical slice of the product. Every module implements this.
//
// Note that Module itself declares no routing method. Routing is expressed
// through the per-version interfaces below, which is what allows a v2 to be
// introduced without touching modules that only serve v1.
type Module interface {
	// Name identifies the module in logs and diagnostics. It must match the
	// package directory name.
	Name() string
}

// ---------- API versioning ----------
//
// Each API version is an optional interface. The router type-asserts against
// them, so:
//
//   - a module serving only v1 implements V1Registrar and is untouched forever;
//   - a module that later needs v2 adds RegisterV2 and nothing else changes;
//   - introducing /api/v2 means adding one interface here and one group in the
//     router — zero edits to any existing module.
//
// The alternative (a single RegisterRoutes method) would force every module to
// be recompiled and rewritten the day a second version appears.

// V1Registrar mounts routes under /api/v1.
type V1Registrar interface {
	RegisterV1(rg *gin.RouterGroup)
}

// V2Registrar mounts routes under /api/v2.
//
// No module implements this yet. It exists so the versioning mechanism is
// proven and documented before the first breaking change, rather than being
// improvised under deadline pressure.
type V2Registrar interface {
	RegisterV2(rg *gin.RouterGroup)
}

// RootRegistrar mounts unversioned routes at the server root.
//
// It is reserved for infrastructure endpoints — health probes, metrics —
// because orchestrators and load balancers are configured once and must not be
// asked to follow API versioning. Business modules must not implement it.
type RootRegistrar interface {
	RegisterRoot(rg *gin.RouterGroup)
}

// ---------- Optional capabilities ----------

// TaskRegistrar is implemented by modules that process background jobs. The
// worker binary type-asserts against it, so a module with no jobs simply does
// not implement it.
type TaskRegistrar interface {
	RegisterTasks(mux *asynq.ServeMux)
}

// Migrator is implemented by modules needing programmatic schema setup in tests
// or local development. SQL files under migrations/ remain the source of truth
// for every deployed environment; see docs/MigrationGuide.md.
type Migrator interface {
	Migrate(ctx context.Context, db *gorm.DB) error
}

// HealthReporter is implemented by modules owning an external dependency that
// belongs in the readiness probe.
type HealthReporter interface {
	CheckHealth(ctx context.Context) error
}
