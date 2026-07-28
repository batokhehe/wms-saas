# Module Convention

Every feature module follows exactly this structure. `internal/module/template`
is the canonical scaffold — copy it, do not write a module from memory.

The template is a real, compiling package rather than a snippet in this
document. A documentation-only template rots silently; by the time someone
copies it, it no longer satisfies the interfaces it claims to. The template
breaks the build the day it drifts.

---

## 1. Structure

```
internal/module/<name>/
├── entity/         Domain types. Depends on nothing.
├── dto/            API contracts (requests, responses, queries).
├── mapper/         entity ⇄ dto conversion.
├── repository/     Persistence. The ONLY place gorm may be imported.
├── service/        Business rules. No gin, no gorm.
├── validator/      Rules that binding tags cannot express.
├── handler/        HTTP. No business logic, no c.JSON.
├── route/          URL layout and route-level middleware.
└── module.go       Wires the slice; implements the module contract.
```

Directory names are fixed. Package name equals directory name. Module name
(returned by `Name()`) equals the module directory name.

**Omitting a layer** is allowed only when it would genuinely be empty. `health`
has no `repository/`, `entity/`, `mapper/` or `validator/` because it owns no
data and accepts no input. "I'll add it later" is not a reason to omit;
"this module has nothing to put in it" is.

---

## 2. Creating a module

```bash
cp -r internal/module/template internal/module/product
```

1. Rename every `package template` → `package product` (the sub-packages keep
   their names: `entity`, `dto`, …).
2. Rename the `Resource` type to the real aggregate.
3. Delete layers that genuinely do not apply.
4. Register it in `bootstrap/container.go`:

```go
c.Registry = module.NewRegistry(c.Logger).Register(
    c.Health,
    product.New(deps),   // ← the only wiring change
)
```

Nothing else changes. The router iterates the registry; it has no knowledge of
any specific module.

---

## 3. Layer rules

These are not style preferences. Each one prevents a specific failure.

### `entity/` — imports nothing from this project

Domain types and domain behaviour. GORM struct tags are tolerated (inert
metadata, not a behavioural dependency), but no `gorm` import, no `gin`, no
`context`.

Domain predicates belong here as methods:

```go
func (r *Resource) IsDeleted() bool                    { return r.DeletedAt != nil }
func (r *Resource) BelongsTo(companyID uuid.UUID) bool { return r.CompanyID == companyID }
```

*Why:* scattering `r.DeletedAt != nil` across services means one inverted check
becomes a bug nobody notices.

### `dto/` — never reuse an entity as a DTO

DTOs change when the API contract changes; entities change when the domain does.
They change for different reasons and therefore must be different types.

*Why:* returning entities directly means every internal field rename is a
breaking API change, and every new internal column silently leaks to clients.

Use pointer fields for PATCH bodies so "omitted" and "set to empty" are
distinguishable.

### `mapper/` — conversion lives here and nowhere else

Handlers and services do not build DTOs inline.

*Why:* when a field must be hidden from API output, there is exactly one place
to change and one place to review when asking "can this field leak?".

Return `make([]T, 0, n)` rather than a nil slice, so JSON emits `[]` and not
`null`. A client that must handle both will eventually crash on one.

### `repository/` — the only place `gorm` appears

Every method takes `companyID` as a required argument, and every query starts
from a tenant-scoped base:

```go
func (r *gormRepository) scoped(ctx context.Context, companyID uuid.UUID) *gorm.DB {
    return r.db.WithContext(ctx).
        Model(&entity.Resource{}).
        Where("company_id = ?", companyID).
        Where("deleted_at IS NULL")
}
```

*Why:* a missing tenant filter is not a bug that returns too many rows — it is a
cross-tenant data leak. Making the tenant a required parameter means forgetting
it does not compile.

Every error return goes through `postgres.TranslateError`:

```go
return postgres.TranslateError(err, "product.repository.Create")
```

*Why:* without it, a unique-constraint violation surfaces as a 500 carrying the
constraint name, exposing the schema to whoever triggered it. With it, the same
failure is a 409 `CONFLICT`.

The repository **interface** is declared in this package, next to its
implementation, because its consumer is the service in the same module — and
because the service must be testable against a fake with no database.

### `service/` — no gin, no gorm, no http

Takes `context.Context`, returns domain/DTO types and errors.

Every tenant-scoped use case starts by resolving the tenant from the context:

```go
companyID, err := tenant(ctx)
if err != nil {
    return dto.Response{}, err
}
```

*Why:* reading the tenant from the context rather than from a request field
makes cross-tenant access impossible to request — the client cannot influence
it. Until auth exists this returns 401, which is the correct placeholder: it
fails closed.

Business invariants are enforced here, not in the handler and not in the
repository. This is the layer that owns "what is allowed".

### `validator/` — only what tags cannot express

Struct-tag validation lives on the DTO. This package is for cross-field
constraints, domain formats, and checks like "must not be only whitespace"
(which `required` does not catch).

Rules needing database access do **not** belong here — "does this SKU already
exist" is a business invariant and lives in the service, where it can be
enforced inside the same transaction as the write.

### `handler/` — four things only

Bind, call service, shape response, return:

```go
func (h *Handler) Create(c *gin.Context) {
    var req dto.CreateRequest
    if err := validator.BindJSON(c, &req); err != nil {
        response.Error(c, err)
        return
    }

    result, err := h.service.Create(appcontext.Context(c), req)
    if err != nil {
        response.Error(c, err)
        return
    }

    response.Created(c, "Resource created successfully", result)
}
```

Every handler is these five statements. *Why:* the uniformity means a reviewer
can tell at a glance when a handler is doing something it should not.

Never call `c.JSON` directly. See `APIConvention.md`.

### `route/` — the module's whole surface in one file

*Why:* a reviewer can audit every endpoint and its middleware — which is where
authorisation will be applied — without reading a single handler body.

Never hard-code `/api/v1` here. The registry supplies the prefix; a module that
embedded its version would break the moment v2 arrived.

### `module.go` — wiring and contract

```go
func New(deps module.Dependencies) *Module {
    repo := repository.New(deps.DB)
    svc  := service.New(repo, deps.Cache, deps.Queue)
    return &Module{handler: handler.New(svc)}
}
```

Manual DI in miniature: each layer receives exactly what it needs, readable top
to bottom, no framework required to understand it.

Always declare compile-time assertions:

```go
var (
    _ module.Module      = (*Module)(nil)
    _ module.V1Registrar = (*Module)(nil)
)
```

*Why:* if a contract method is renamed or its signature drifts, the build fails
here rather than the module silently vanishing from the router at runtime.

---

## 4. Dependencies

Modules receive one argument:

```go
type Dependencies struct {
    DB      *gorm.DB
    Cache   port.Cache
    Queue   port.Queue
    Storage port.Storage
    Logger  *zap.Logger
    Clock   port.Clock
    IDs     port.IDGenerator
    Tx      transaction.Manager
}
```

Note what is absent: `*redis.Client`, `*asynq.Client`. A module cannot reach a
vendor SDK through this struct, which makes the abstraction enforceable rather
than merely advisory.

`Clock`, `IDs` and `Tx` replace `time.Now()`, `uuid.New()` and `db.Begin()`.
Handing them to every module means a service's behaviour is fully determined by
its inputs, so a test can pin both time and identifiers. They are wired in
`module.go`:

```go
func New(deps module.Dependencies) *Module {
    repo := repository.New(deps.DB, deps.IDs)
    svc  := service.New(repo, deps.Cache, deps.Queue, deps.Clock, deps.Tx)
    return &Module{handler: handler.New(svc)}
}
```

The uniform signature is also why registering a module is always the same single
line.

---

## 5. Optional capabilities

Implement only what applies:

| Interface        | Purpose                                          |
| ---------------- | ------------------------------------------------ |
| `V1Registrar`    | Serve `/api/v1`. Nearly every module.             |
| `V2Registrar`    | Serve `/api/v2`. Only after a breaking change.    |
| `RootRegistrar`  | Unversioned root routes. **Infrastructure only.** |
| `TaskRegistrar`  | Process Asynq background jobs.                    |
| `Migrator`       | Programmatic schema setup for tests only.         |
| `HealthReporter` | Contribute to the readiness probe.                |

---

## 6. Module-to-module communication

Modules **must not** import each other's `repository/` or `entity/` packages.

When module A needs something from module B, B exposes a narrow interface that A
declares in its own package (consumer-side interface), and bootstrap wires B's
service in as the implementation.

*Why:* direct cross-module imports produce a dependency graph nobody can reason
about, and they are what makes a "modular" monolith stop being modular. A
consumer-side interface keeps A testable and makes the coupling visible in one
place.

---

## 7. Checklist

- [ ] Directory names match the template exactly.
- [ ] `Name()` matches the directory name.
- [ ] `gorm` imported only in `repository/`.
- [ ] `gin` not imported in `service/`.
- [ ] Every entity embeds `entity.BaseEntity` (`EntityConvention.md`).
- [ ] Repository composes `repository.Base` (`RepositoryConvention.md`).
- [ ] Every repository method takes `companyID`.
- [ ] Every repository error goes through `postgres.TranslateError`.
- [ ] No `time.Now()`, `uuid.New()` or `db.Begin()` anywhere in the module.
- [ ] List endpoints embed `pagination.Request` and declare `AllowedSorts`.
- [ ] Multi-step writes run inside `tx.RunInTransaction`.
- [ ] Every handler exit goes through `response.*`; no `c.JSON`.
- [ ] Entities are not returned from handlers; DTOs are.
- [ ] Compile-time interface assertions present in `module.go`.
- [ ] Registered in `bootstrap/container.go`.
- [ ] Service has unit tests that need no database.
