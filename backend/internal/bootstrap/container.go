// Package bootstrap assembles the application.
//
// This is the only place that knows how every component is constructed and in
// what order. Dependency injection is manual and explicit: no reflection, no
// code generation, no global registry. The entire object graph can be read from
// top to bottom in this file, which is what you want at 3 a.m. when the
// question is "what is constructed before what".
package bootstrap

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/config"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand"
	"github.com/batokhehe/wms-saas/backend/internal/module/category"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer"
	"github.com/batokhehe/wms-saas/backend/internal/module/health"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory"
	inventoryservice "github.com/batokhehe/wms-saas/backend/internal/module/inventory/service"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger"
	"github.com/batokhehe/wms-saas/backend/internal/module/location"
	locationrepository "github.com/batokhehe/wms-saas/backend/internal/module/location/repository"
	locationservice "github.com/batokhehe/wms-saas/backend/internal/module/location/service"
	"github.com/batokhehe/wms-saas/backend/internal/module/lookup"
	"github.com/batokhehe/wms-saas/backend/internal/module/product"
	productrepository "github.com/batokhehe/wms-saas/backend/internal/module/product/repository"
	productservice "github.com/batokhehe/wms-saas/backend/internal/module/product/service"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac"
	"github.com/batokhehe/wms-saas/backend/internal/module/supplier"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse"
	warehouserepository "github.com/batokhehe/wms-saas/backend/internal/module/warehouse/repository"
	warehousesvc "github.com/batokhehe/wms-saas/backend/internal/module/warehouse/service"
	adaptercache "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/cache"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	adapterqueue "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/queue"
	adapterstorage "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/storage"
	infraasynq "github.com/batokhehe/wms-saas/backend/internal/shared/infra/asynq"
	infrapostgres "github.com/batokhehe/wms-saas/backend/internal/shared/infra/postgres"
	infraredis "github.com/batokhehe/wms-saas/backend/internal/shared/infra/redis"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/logger"
)

// Container holds every long-lived component of the process.
type Container struct {
	Config *config.Config
	Logger *zap.Logger

	// Infrastructure: connection lifecycle owners.
	Postgres *infrapostgres.DB
	Redis    *infraredis.Client
	Asynq    *infraasynq.Client

	// Ports: what modules are actually given. Nothing below this line exposes a
	// vendor SDK type, which is what makes the abstraction enforceable rather
	// than merely documented.
	Cache   port.Cache
	Queue   port.Queue
	Storage port.Storage
	Clock   port.Clock
	IDs     port.IDGenerator
	Tx      transaction.Manager

	Health          *health.Module
	Auth            *auth.Module
	Tenancy         *tenancy.Module
	RBAC            *rbac.Module
	Warehouse       *warehouse.Module
	Location        *location.Module
	Product         *product.Module
	Inventory       *inventory.Module
	InventoryLedger *inventoryledger.Module
	Supplier        *supplier.Module
	Customer        *customer.Module
	Category        *category.Module
	Brand           *brand.Module
	Lookup          *lookup.Module
	Registry        *module.Registry

	// closers are torn down in reverse registration order, mirroring how defer
	// unwinds, so a component is never closed before its dependents.
	closers []closer
}

// closer pairs a shutdown function with a name for logging.
type closer struct {
	name string
	fn   func() error
}

// NewContainer builds the entire object graph.
//
// Construction is fail-fast and ordered: infrastructure, then the adapters that
// wrap it, then modules. If any step fails, everything already built is
// released before the error is returned, so a failed boot leaks no connections.
func NewContainer(ctx context.Context, cfg *config.Config) (*Container, error) {
	log, err := logger.New(logger.Config{
		Level:       cfg.Log.Level,
		Format:      cfg.Log.Format,
		AppName:     cfg.App.Name,
		AppVersion:  cfg.App.Version,
		Environment: cfg.App.Env,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap: logger: %w", err)
	}

	c := &Container{Config: cfg, Logger: log}

	// Service, version and env are already stamped on every line by the root
	// logger, so repeating them here would emit duplicate JSON keys.
	log.Info("bootstrapping application")

	// ---------- Infrastructure ----------

	pg, err := infrapostgres.New(ctx, cfg.Database, log)
	if err != nil {
		return nil, c.abort(err)
	}
	c.Postgres = pg
	c.register("postgres", pg.Close)

	rdb, err := infraredis.New(ctx, cfg.Redis, log)
	if err != nil {
		return nil, c.abort(err)
	}
	c.Redis = rdb
	c.register("redis", rdb.Close)

	asynqClient, err := infraasynq.New(cfg.Redis, log)
	if err != nil {
		return nil, c.abort(err)
	}
	c.Asynq = asynqClient
	c.register("asynq", asynqClient.Close)

	// ---------- Adapters ----------
	//
	// These three lines are the entire cost of swapping a technology. Replacing
	// Redis with Memcached, or Asynq with RabbitMQ, changes the right-hand side
	// here and nothing else in the codebase.

	c.Cache = adaptercache.NewRedisCache(rdb.Client, cfg.App.Name)
	c.Queue = adapterqueue.NewAsynqQueue(asynqClient.Client)
	c.Storage = adapterstorage.NewUnavailable()

	// Ambient dependencies. They look trivial to construct inline, which is
	// exactly why they are wired here instead: a service that reaches for
	// time.Now() or uuid.New() directly cannot be tested deterministically, and
	// the only way to prevent that is to never give it the option.
	c.Clock = adapterclock.NewSystem()
	c.IDs = adapterid.NewUUID()
	c.Tx = transaction.NewGormManager(pg.DB)

	// ---------- Modules ----------

	c.Health = health.New(cfg.App.Name, cfg.App.Version, cfg.App.Env)
	c.Health.Service().Register("postgres", pg)
	c.Health.Service().Register("redis", rdb)

	if err := c.buildRegistry(); err != nil {
		return nil, c.abort(err)
	}

	log.Info("application bootstrapped", zap.Int("modules", c.Registry.Len()))

	return c, nil
}

// Dependencies is the value handed to every business module constructor.
func (c *Container) Dependencies() module.Dependencies {
	return module.Dependencies{
		DB:      c.Postgres.DB,
		Cache:   c.Cache,
		Queue:   c.Queue,
		Storage: c.Storage,
		Logger:  c.Logger,
		Clock:   c.Clock,
		IDs:     c.IDs,
		Tx:      c.Tx,
	}
}

// buildRegistry is the single place where modules are declared.
//
// Adding a feature is one line here. The router never changes, because it
// iterates the registry rather than naming modules.
func (c *Container) buildRegistry() error {
	deps := c.Dependencies()

	// Auth is constructed before the registry because other modules will need
	// its token verifier for their protected routes. They receive it as
	// middleware.TokenVerifier — an interface owned by the middleware package —
	// so no module ever imports auth directly.
	c.Auth = auth.New(deps, c.Config.Auth)

	// Tenancy receives auth's token verifier and a directory adapter over auth's
	// user lookup. Both are interfaces owned by the CONSUMER (middleware and
	// tenancy respectively), so tenancy never imports auth — the composition
	// root does the joining. See directory.go.
	c.Tenancy = tenancy.New(
		deps,
		c.Auth.Verifier(),
		newUserDirectory(c.Postgres.DB, c.IDs),
	)

	// RBAC receives auth's token verifier and tenancy's company resolver, both
	// as interfaces owned by the middleware package — so it imports neither
	// module. It is constructed after tenancy because it needs that resolver.
	c.RBAC = rbac.New(deps, c.Auth.Verifier(), c.Tenancy.Resolver())

	// Warehouse is the first business domain. It takes all three platform
	// collaborators plus its two extension-point guards.
	//
	// The guards are the permissive implementations until the modules that
	// replace them exist:
	//
	//   - DeletionGuard  → the Inventory sprint returns CONFLICT when a
	//     warehouse holds stock. Swap AlwaysAllowDeletion for that here; no
	//     warehouse file changes.
	//   - ZoneVerifier   → the Location sprint checks a zone exists and belongs
	//     to the company. Swap AcceptAnyZone for that here.
	//
	// They are named types rather than nil, so an unwired guard cannot be
	// mistaken for a permissive one.
	// Warehouse and Location reference each other in BOTH directions:
	// a location must belong to a real warehouse, and a warehouse's default
	// zones must be real locations.
	//
	// Neither module imports the other. Each declares the narrow interface it
	// needs, and the adapters in location_adapters.go implement them over the
	// other's repository. The cycle is broken by constructing standalone
	// repository instances here — they are stateless (a *gorm.DB and an id
	// generator), so a second instance costs nothing and avoids reaching into
	// either module's private graph.
	warehouseRepo := warehouserepository.New(c.Postgres.DB, c.IDs)
	locationRepo := locationrepository.New(c.Postgres.DB, c.IDs)

	// This replaces warehouse's AcceptAnyZone default and closes the gap
	// docs/Warehouse.md §7 recorded: a warehouse's default zone ids are now
	// verified to be real storage locations in the same company.
	c.Warehouse = warehouse.New(
		deps,
		c.Auth.Verifier(),
		c.Tenancy.Resolver(),
		c.RBAC.Resolver(),
		warehousesvc.NewAlwaysAllowDeletion(),
		newZoneVerifier(locationRepo),
	)

	// The five domain extension points keep their permissive defaults until the
	// modules that own them exist. Each is a named type rather than nil, so an
	// unwired guard cannot be mistaken for a permissive one:
	//
	//   Capacity / Inventory → the Inventory sprint
	//   Receiving            → the Receiving sprint
	//   Picking              → the Picking sprint
	//   Counting             → the Cycle Count sprint
	//
	// Warehouses is the exception: warehouses exist today, so a real adapter is
	// wired rather than a permissive stand-in.
	c.Location = location.New(deps, location.Config{
		Verifier:    c.Auth.Verifier(),
		Companies:   c.Tenancy.Resolver(),
		Permissions: c.RBAC.Resolver(),
		Warehouses:  newWarehouseVerifier(warehouseRepo),
		Capacity:    locationservice.NewEmptyCapacity(),
		Inventory:   locationservice.NewEmptyInventory(),
		Receiving:   locationservice.NewAllowAllReceiving(),
		Picking:     locationservice.NewAllowAllPicking(),
		Counting:    locationservice.NewAllowAllCycleCount(),
	})

	// Product is a catalogue aggregate. Its four extension points keep their
	// permissive defaults until the modules that own them exist. Each is a named
	// type rather than nil, so an unwired verifier cannot be mistaken for a
	// permissive one:
	//
	//   CategoryVerifier  → the Category sprint
	//   BrandVerifier     → the Brand sprint
	//   UOMVerifier       → the UOM sprint
	//   InventoryProvider → the Inventory sprint
	//
	// None has a real implementation today: unlike warehouse↔location, no shipped
	// module owns a category, brand, unit or stock, so there is nothing to adapt
	// yet. When one ships, its adapter is wired here and no product file changes.
	c.Product = product.New(
		deps,
		c.Auth.Verifier(),
		c.Tenancy.Resolver(),
		c.RBAC.Resolver(),
		productservice.NewAcceptAnyCategory(),
		productservice.NewAcceptAnyBrand(),
		productservice.NewAcceptAnyUOM(),
		productservice.NewNoInventory(),
		productservice.NewNoStock(),
	)

	// Inventory owns stock. Its three reference providers are wired to REAL
	// adapters over the product, warehouse and location repositories — all three
	// modules exist, so a stock position's references are verified rather than
	// trusted. ReservationProvider keeps its permissive default until the
	// Reservation sprint exists; a standalone product repository is constructed
	// here for the same reason warehouse/location are (stateless, no reach into a
	// module's private graph).
	productRepo := productrepository.New(c.Postgres.DB, c.IDs)
	c.Inventory = inventory.New(deps, inventory.Config{
		Verifier:    c.Auth.Verifier(),
		Companies:   c.Tenancy.Resolver(),
		Permissions: c.RBAC.Resolver(),
		Products:    newInventoryProductProvider(productRepo),
		Warehouses:  newInventoryWarehouseProvider(warehouseRepo),
		Locations:   newInventoryLocationProvider(locationRepo),
		Policy:      inventoryservice.NewDefaultStockPolicy(),
	})

	// Supplier is master data — a self-contained vertical slice with no
	// cross-aggregate providers to wire this sprint. Its DeletionGuard extension
	// point is prepared (service/guards.go) for the future Purchase Order sprint
	// but consumed by no operation, so there is nothing to inject.
	c.Supplier = supplier.New(deps, c.Auth.Verifier(), c.Tenancy.Resolver(), c.RBAC.Resolver())

	// Customer is master data — the structural sibling of Supplier. Its
	// DeletionGuard extension point is prepared (service/guards.go) for the future
	// Sales Order sprint but consumed by no operation, so there is nothing to
	// inject.
	c.Customer = customer.New(deps, c.Auth.Verifier(), c.Tenancy.Resolver(), c.RBAC.Resolver())
	c.Category = category.New(deps, c.Auth.Verifier(), c.Tenancy.Resolver(), c.RBAC.Resolver())
	c.Brand = brand.New(deps, c.Auth.Verifier(), c.Tenancy.Resolver(), c.RBAC.Resolver())
	c.Lookup = lookup.New(deps, c.Auth.Verifier(), c.Tenancy.Resolver(), c.RBAC.Resolver())

	// The append-only audit trail behind every stock movement. It is wired as a
	// standalone module: the Inventory module publishes through the seam in
	// inventoryledger/service/interfaces.go, and connecting the two is a
	// composition-root decision a later sprint makes without touching either.
	c.InventoryLedger = inventoryledger.New(deps, c.Auth.Verifier(), c.Tenancy.Resolver(), c.RBAC.Resolver())

	c.Registry = module.NewRegistry(c.Logger).Register(
		c.Health,
		c.Auth,
		c.Tenancy,
		c.RBAC,
		c.Warehouse,
		c.Location,
		c.Product,
		c.Inventory,
		c.InventoryLedger,
		c.Supplier,
		c.Customer,
		c.Category,
		c.Brand,
		c.Lookup,
	)

	// Catch wiring mistakes at boot rather than as a mysteriously missing
	// endpoint in production.
	if err := c.Registry.Validate(); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	return nil
}

// register adds a shutdown hook.
func (c *Container) register(name string, fn func() error) {
	c.closers = append(c.closers, closer{name: name, fn: fn})
}

// abort releases everything built so far and returns the original error. It
// keeps the failure path in NewContainer to a single line per step.
func (c *Container) abort(err error) error {
	if shutdownErr := c.shutdown(context.Background()); shutdownErr != nil && c.Logger != nil {
		c.Logger.Error("cleanup after failed boot did not complete", zap.Error(shutdownErr))
	}
	return err
}

// shutdown releases every resource in reverse order of construction.
//
// It never stops early on error: one stubborn connection must not prevent the
// rest from being released. Errors are logged and the first is returned.
func (c *Container) shutdown(ctx context.Context) error {
	var firstErr error

	for i := len(c.closers) - 1; i >= 0; i-- {
		select {
		case <-ctx.Done():
			// The shutdown budget is spent; abandon the remaining closers and
			// let process exit reclaim their file descriptors.
			if firstErr == nil {
				firstErr = ctx.Err()
			}
			return firstErr
		default:
		}

		if err := c.closers[i].fn(); err != nil {
			if c.Logger != nil {
				c.Logger.Error("failed to close resource",
					zap.String("resource", c.closers[i].name),
					zap.Error(err),
				)
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

// Close is the exported shutdown entry point used by App.
func (c *Container) Close(ctx context.Context) error {
	return c.shutdown(ctx)
}
