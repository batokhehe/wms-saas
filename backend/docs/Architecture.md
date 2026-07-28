# Architecture

The backend is a **Modular Clean Architecture** monolith: vertical feature
modules, with dependencies inside each module pointing inward toward the domain.

It is a monolith on purpose. A WMS is transactionally dense — receiving stock,
allocating it and shipping it must be atomic — and distributing those writes
across services buys operational cost before the product has customers. The
module boundaries are drawn so a module can be lifted out later if it needs to
be.

---

## 1. Layout

```
backend/
├── cmd/
│   ├── api/                HTTP entrypoint. Thin: flags, config, hand off.
│   └── migrate/            Schema migration CLI (separate binary).
├── internal/
│   ├── bootstrap/          Manual DI container, router, server, lifecycle.
│   ├── config/             Viper loader, typed structs, validation.
│   ├── middleware/         Cross-cutting HTTP concerns.
│   ├── module/             One package per feature (vertical slice).
│   │   ├── auth/           Identity: users, sessions, JWT.
│   │   ├── health/         Liveness and readiness probes.
│   │   ├── rbac/           Roles, permissions, authorisation.
│   │   ├── location/       Storage locations (DDD aggregate root).
│   │   ├── tenancy/        Companies, memberships, company context.
│   │   ├── warehouse/      First business domain (DDD aggregate root).
│   │   └── template/       The scaffold every new module copies.
│   └── shared/
│       ├── adapter/        Port implementations (cache, queue, storage, clock, id).
│       ├── appcontext/     RequestContext: identity + correlation.
│       ├── entity/         BaseEntity embedded by every domain entity.
│       ├── infra/          Connection lifecycle (postgres, redis, asynq).
│       ├── migration/      golang-migrate runner.
│       ├── module/         Module contract + version registry.
│       ├── pagination/     Paging contract + sort allow-listing.
│       ├── port/           Technology-agnostic interfaces.
│       ├── repository/     Generic base repository + query scopes.
│       ├── response/       The single JSON envelope.
│       ├── transaction/    Atomic units of work.
│       └── validator/      Binding and validation.
├── pkg/
│   ├── apperror/           Error taxonomy.
│   └── logger/             Zap construction and context propagation.
├── migrations/             Versioned SQL. Source of truth for schema.
└── docs/                   These documents.
```

`internal/` is enforced by the Go toolchain: nothing outside this module can
import it. `pkg/` is the deliberate opposite — code that would still make sense
in a different service.

**Dependency direction is one-way: `internal/` may import `pkg/`; `pkg/` never
imports `internal/`.** This is why `response` lives in `internal/shared` rather
than in `pkg`: it depends on `appcontext`, which is application-specific.

---

## 2. The three shared layers

### `port/` — what modules are allowed to depend on

`port` declares interfaces only: `Cache`, `Queue`, `Storage`, `Clock`,
`IDGenerator`. A business module imports `port` and receives an interface. It
never imports `go-redis`, `asynq`, `minio` or any other vendor SDK.

This is enforced structurally, not by policy. `module.Dependencies` exposes
`Cache port.Cache`, not `*redis.Client` — a module physically cannot reach the
Redis client through the value it is handed.

`Clock` and `IDGenerator` look trivial enough to skip, which is exactly why they
are ports. A service calling `time.Now()` or `uuid.New()` directly has behaviour
that is not determined by its inputs, so a test cannot pin the result — and the
only reliable way to prevent that is to never give the service the option.

### `infra/` — connection lifecycle

`infra/postgres`, `infra/redis`, `infra/asynq` own dialling, health checking and
closing. They produce a client and manage its life, and are wired only by
bootstrap.

### `adapter/` — port implementations

`adapter/cache/redis_cache.go` implements `port.Cache` on top of the Redis
client. `adapter/queue/asynq_queue.go` implements `port.Queue` on top of Asynq.

Swapping a technology means adding a sibling file in `adapter/` and changing one
line in `bootstrap/container.go`:

```go
c.Cache   = adaptercache.NewRedisCache(rdb.Client, cfg.App.Name)
c.Queue   = adapterqueue.NewAsynqQueue(asynqClient.Client)
c.Storage = adapterstorage.NewUnavailable()
```

Zero business code changes. That is the entire point of the split.

**The one deliberate exception is `*gorm.DB`,** which is handed to repositories
directly. Wrapping GORM in a generic repository interface would cost more than it
buys — you end up reimplementing a query builder badly. The rule enforced by
convention instead: `gorm` may only be imported inside a module's `repository/`
package. See `ModuleConvention.md`.

A second, much narrower exception exists in `shared/entity`, which imports
`gorm.DeletedAt` — a data type, not behaviour — so that soft deletion is applied
automatically to every query. `EntityConvention.md` explains the trade.

### `shared/` persistence layers

Three packages sit between modules and the database, and none of them leaks GORM
past its own boundary:

- **`entity`** — `BaseEntity` (id, timestamps, soft-delete marker). Every
  persisted entity embeds it, and `repository.Base` will not compile against a
  type that does not.
- **`repository`** — a generic `Base[T, PT]` supplying the CRUD every module
  would otherwise copy-paste and subtly diverge on, plus composable `Scope`s.
  `Scope` exposes `*gorm.DB` and is therefore usable only from inside a
  repository package.
- **`transaction`** — `Manager.RunInTransaction` is the sanctioned way to make
  work atomic. Its interface carries no GORM type, so a service depends on it
  without depending on the persistence technology. Repositories resolve their
  handle through `transaction.DB(ctx, fallback)`, which is what makes the same
  method work standalone and inside a transaction with no plumbing.

---

## 3. Layering inside a module

| Directory     | Responsibility                                     | May import                |
| ------------- | -------------------------------------------------- | ------------------------- |
| `entity/`     | Domain types and domain behaviour.                  | nothing from this project |
| `dto/`        | API request/response contracts.                     | entity                    |
| `mapper/`     | entity ⇄ dto conversion.                            | entity, dto               |
| `repository/` | Persistence. **The only place `gorm` may appear.**  | entity, dto, infra        |
| `service/`    | Business rules. **No gin, no gorm.**                | repository, mapper, port  |
| `validator/`  | Rules that binding tags cannot express.             | dto                       |
| `handler/`    | HTTP. **No business logic, no `c.JSON`.**           | service, dto, response    |
| `route/`      | URL layout and route-level middleware.              | handler                   |
| `module.go`   | Wires the slice; implements the module contract.    | all of the above          |

The two rules that carry the weight:

- **A service never imports Gin.** It is therefore callable from an HTTP handler,
  an Asynq worker, a CLI command or a unit test with no server running.
- **A handler never imports GORM.** Business logic cannot hide in the transport
  layer if the transport layer cannot reach the database.

---

## 4. API versioning

Versioning is expressed as **optional interfaces**, not as a routing table.

```go
type V1Registrar interface { RegisterV1(rg *gin.RouterGroup) }
type V2Registrar interface { RegisterV2(rg *gin.RouterGroup) }
```

`module.Registry.Mount` iterates `SupportedVersions` and type-asserts each module
against that version's registrar. Consequences:

- A module serving only v1 implements `RegisterV1` and is **never touched again**
  when v2 arrives.
- A module needing a breaking change adds `RegisterV2` alongside `RegisterV1` and
  serves both simultaneously.
- Introducing `/api/v2` is a two-line change: add `V2` to `SupportedVersions` and
  its case to `mountModule`.

The alternative — a single `RegisterRoutes(group)` method — would force every
module to be edited the day a second version appeared, because each would have to
decide internally which version it was being mounted under.

`bootstrap/router.go` contains no module name and no version string. Both live in
the registry.

---

## 5. Request context

`appcontext.RequestContext` carries per-request identity through every layer:

```go
type RequestContext struct {
    RequestID string
    CompanyID *uuid.UUID   // tenant; nil until auth exists
    UserID    *uuid.UUID   // principal; nil until auth exists
    Role      string
    Logger    *zap.Logger  // pre-tagged with all of the above
    StartedAt time.Time
    ClientIP  string
    UserAgent string
}
```

It is stored in both `*gin.Context` and the request's `context.Context`, so
services and repositories retrieve it from a plain `context.Context` and never
see a `*gin.Context`.

**Why this matters for a multi-company SaaS:** the tenant is read from the
context, never from a request field. A client has no way to influence which
company its query is scoped to. Repository methods take `companyID` as a required
argument, so a developer cannot forget to scope a query — it will not compile.

`CompanyID` is nil today and every layer is already written against it. Enabling
tenancy is a change to the middleware that populates it, not to the repositories
that read it.

---

## 6. Request lifecycle

```
request
  → RequestContext    mint/validate X-Request-ID, build tagged logger
  → Recovery          panic becomes a standard error envelope
  → ErrorHandler      safety net for unwritten c.Error()
  → AccessLog         one structured line per request
  → SecurityHeaders
  → CORS
  → route handler → service → repository → PostgreSQL
  → response envelope
```

Order is load-bearing and documented inline in `bootstrap/router.go`.
`RequestContext` must run first: everything after it, including `Recovery`, reads
the logger it creates.

---

## 7. Dependency injection

DI is manual and lives entirely in `bootstrap/container.go`. No Wire, no Fx, no
reflection.

The trade-off is deliberate. A generated or reflective container saves a few
lines of wiring but moves construction order into a tool's mental model. When
something fails at 3 a.m., "what is constructed before what" should be answerable
by reading one file top to bottom. `NewContainer` is that file.

Order: logger → Postgres → Redis → Asynq → adapters → modules. Each
infrastructure component registers a closer the moment it succeeds; shutdown
walks that list in reverse.

`Registry.Validate()` runs at boot and fails the process if a module registers no
routes at all — almost always a forgotten interface method, which would otherwise
surface as a silently missing endpoint in production.

---

## 8. Health probes

| Endpoint        | Checks            | Purpose                               |
| --------------- | ----------------- | ------------------------------------- |
| `/health/live`  | nothing external  | Is the process alive? Restart if not. |
| `/health/ready` | PostgreSQL, Redis | Should traffic be routed here?        |

Liveness deliberately checks no dependencies. If it did, a database outage would
cause the orchestrator to restart every replica simultaneously, converting a
recoverable incident into a total one.

Readiness probes dependencies concurrently under a 2-second per-check budget, so
a hung dependency cannot hold the endpoint open.

Both are mounted at the unversioned root **and** under `/api/v1`, because
orchestrators are configured once and must not be asked to follow API versioning.

A failing readiness check returns a 503 in the standard error envelope. The
per-component breakdown is written to the logs but suppressed from the response,
because those messages contain internal hostnames and connection strings.

---

## 9. Graceful shutdown

On SIGINT or SIGTERM:

1. The HTTP server stops accepting new connections and drains in-flight requests,
   bounded by `HTTP_SHUTDOWN_TIMEOUT`.
2. Only then are Asynq, Redis and PostgreSQL closed, in reverse construction
   order.

Closing the database before draining would fail every request currently being
served — the exact outcome graceful shutdown exists to avoid.

---

## 10. Configuration

Precedence: real environment variables > `.env` file > built-in defaults.

Every key is registered with a default in `config/defaults.go`. This is required,
not merely convenient: `viper.AutomaticEnv` only resolves keys it already knows
about when `Unmarshal` walks the struct.

`AllowEmptyEnv(true)` is set so an explicitly blank variable is honoured rather
than silently falling back to a default — which is why `DATABASE_PASSWORD` has
**no** default. A production deploy that forgets to set it must fail loudly
instead of quietly authenticating with a well-known development credential.

`Config.validate()` rejects bad configuration at boot and applies stricter rules
under `APP_ENV=production`: no blank database password, no `sslmode=disable`, no
wildcard CORS.

---

## 11. Identity, tenancy and authorisation

The `auth` module is the identity foundation, and it is **deliberately
independent of Company**: `entity.User` carries no `CompanyID`, access tokens
carry no company claim, and no auth repository method takes one.

A person can belong to several companies — a 3PL operator works for multiple
clients — so binding identity to a tenant would mean duplicate accounts and no
single account to lock when that person leaves. Authentication also has to work
before a company context exists.

That independence is now realised rather than merely planned. The `tenancy`
module adds companies and memberships, and `middleware.ResolveCompany` populates
`CompanyID`, `MembershipID` and `Role` on the request context — **without a
single change to the auth module**. Because every service and repository already
read `CompanyID` from the context, nothing below the middleware layer changed
either.

The two middlewares answer different questions and are deliberately separate:
`Authenticate` establishes WHO the caller is, `ResolveCompany` establishes WHERE
they are acting. Correspondingly `RequestContext` grew a `WithCompany` method
alongside the existing `WithTenant` rather than absorbing it.

Neither middleware imports the module that implements it. `middleware` declares
the `TokenVerifier` and `CompanyResolver` interfaces it needs, and bootstrap
injects the implementations — the consumer-side interface pattern from
`ModuleConvention.md` §6. Without it, every module needing authentication or a
tenant would transitively depend on auth's and tenancy's internals.

The same pattern joins the feature modules: tenancy must resolve an invitee by
email, so it declares a one-method `UserDirectory` interface and bootstrap
adapts auth's user repository to it in `bootstrap/directory.go`. Tenancy never
imports auth, and auth gained no accessor to serve it.

Authorisation completes the chain. The `rbac` module adds roles and permissions,
and `middleware.LoadPermissions` resolves what a caller may do in the active
company — again without changing tenancy or auth. It joins to membership by role
NAME rather than by a foreign key, which is what let it be added without
altering the memberships table:

	Authenticate     → WHO the caller is
	ResolveCompany   → WHERE they are acting
	RequireCompany   → refuse if nowhere
	LoadPermissions  → WHAT they may do there
	RequirePermission → per route

Each step reads what the previous one injected, so the order is load-bearing.
Every step fails closed: a miss at any point yields an empty permission set and
a 403, never a permissive default.

See `Authentication.md`, `MultiTenancy.md`, `Membership.md`, `RBAC.md` and
`PermissionMatrix.md`.

---

## 12. Business domains

`warehouse` is the first, and it is modelled with Domain-Driven Design rather
than as a CRUD module. Two structural differences from the platform modules are
worth knowing before writing the next one:

**The aggregate has no exported fields.** `entity.Warehouse` exposes getters and
intent-revealing methods (`Activate`, `Suspend`, `ChangeContact`) and nothing
else. "An ACTIVE warehouse always has an address, a contact and a zone" is only
a guarantee if there is no way to reach ACTIVE except through `Activate()`.

**It therefore does NOT embed `shared/entity.BaseEntity`.** That convention
governs persistence types, and an aggregate is not one — GORM maps by reflecting
over exported fields, so `repository/model.go` holds a separate model that does
embed it, plus a hand-written translation. This is the standard shape for DDD
over an ORM and is the one documented departure from `EntityConvention`.

Domain events are raised by the aggregate rather than the service, so an event
exists because a transition happened rather than because a service remembered to
record one.

Cross-aggregate rules live behind interfaces the domain declares and bootstrap
injects — `DeletionGuard` for the Inventory sprint, `ZoneVerifier` for Location.
Neither required editing the warehouse module, and `ZoneVerifier` has since been
implemented by exactly that route.

`location` is the second, and the first with a CROSS-AGGREGATE REFERENCE. Three
patterns it establishes for the domains that follow:

**Aggregates reference each other by ID, never by object.** A location holds a
`WarehouseID`, not a `*Warehouse`. "Does this warehouse exist?" is asked through
an interface the location module declares and bootstrap implements over the
warehouse repository — so only the ANSWER crosses the boundary. Loading one
aggregate from another would collapse the consistency boundary that lets each be
modified independently.

**A rule whose data lives elsewhere still belongs in the domain.** "Capacity may
not be reduced below current usage" cannot be evaluated by the aggregate alone,
because stock is another aggregate. The service fetches the usage and PASSES IT
IN; the aggregate applies the rule. The rule stays in the domain, only the fact
comes from outside — and the rule stays testable with no infrastructure at all.

**Extension points are named types, never nil.** A nil guard makes "no guard
configured" and "the guard permits it" indistinguishable, so a wiring mistake
would silently disable a safety check.

The two modules now reference each other in BOTH directions without either
importing the other: see `bootstrap/location_adapters.go`.

See `Warehouse.md` and `StorageLocation.md`.

---

## 13. Processes

| Binary        | Purpose                                    |
| ------------- | ------------------------------------------ |
| `cmd/api`     | HTTP server.                                |
| `cmd/migrate` | Schema migrations. Runs before the API.     |

`cmd/migrate` loads configuration with `config.WithoutAuth()`: the migration
runner touches only the database, and requiring it to carry a JWT signing secret
would mean either a placeholder credential in the deployment manifest or handing
a schema-migration job the real one. Each binary declares the configuration it
actually needs.

---

## 14. Further reading

| Document                    | Covers                                          |
| --------------------------- | ----------------------------------------------- |
| `API.md`                    | Endpoint reference.                              |
| `Authentication.md`         | Identity model, tokens, auth flows.              |
| `MultiTenancy.md`           | Tenant isolation, company context, switching.    |
| `Membership.md`             | Membership model, roles, invitations.            |
| `RBAC.md`                   | Permission resolution, role model, protections.  |
| `PermissionMatrix.md`       | The catalogue and per-role default grants.       |
| `Warehouse.md`              | Aggregate root, lifecycle, extension points.     |
| `StorageLocation.md`        | Locations, capacity, cross-aggregate references. |
| `Security.md`               | Threat coverage and known gaps.                  |
| `ModuleConvention.md`       | Module structure and layer rules.                |
| `EntityConvention.md`       | BaseEntity, identifiers, timestamps, columns.    |
| `RepositoryConvention.md`   | Base repository, scopes, tenancy, transactions.  |
| `SoftDeleteConvention.md`   | Why soft delete, restore and purge strategies.   |
| `ErrorConvention.md`        | Error taxonomy and translation.                  |
| `APIConvention.md`          | Envelope, versioning, pagination, headers.       |
| `MigrationGuide.md`         | Versioned SQL, safe schema changes.              |
| `CodingStandard.md`         | Naming, logging, testing, forbidden patterns.    |

They are separate binaries and separate container images. The API image ships no
binary capable of altering the schema, and migrations run as a Compose
dependency (`service_completed_successfully`) or a Kubernetes init container —
neither of which should require starting an HTTP server.
