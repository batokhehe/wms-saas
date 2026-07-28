# Coding Standard

Effective Go and the Go Code Review Comments are the baseline. This document
covers only what is specific to this codebase or where we deliberately differ.

---

## 1. Non-negotiables

```bash
make fmt && make vet && make test
```

- `gofmt -s` formatted. Not negotiable, not configurable.
- `go vet` clean.
- Tests pass. CI additionally runs `make test-race`.

---

## 2. Naming

| Thing            | Convention              | Example                       |
| ---------------- | ----------------------- | ----------------------------- |
| Packages         | lowercase, singular     | `product`, not `products`      |
| Files            | snake_case              | `redis_cache.go`               |
| Types/functions  | PascalCase / camelCase  | `StockMovement`, `findByID`    |
| Interfaces       | capability, not `IFoo`  | `Storage`, `Checker`           |
| Constructors     | `New`, or `NewX`        | `repository.New(db)`           |
| Test functions   | `TestThing_Condition`   | `TestReady_OneDepDown`         |

Package names are never `util`, `common`, `helpers` or `base`. A package named
for what it *is* rather than what it *does* becomes a dumping ground. If you
cannot name it, the code belongs somewhere that already exists.

Do not stutter: `product.Service`, not `product.ProductService`.

---

## 3. Errors

Full rules in `ErrorConvention.md`. The short version:

```go
// Return typed errors
return apperror.NotFound("Product not found").WithOp("product.service.Get")

// Wrap with context when crossing a layer
return apperror.Wrap(err, "product.service.Create")

// Compare with errors.Is, never string matching
if errors.Is(err, apperror.ErrConflict) { ... }
```

Never ignore an error silently. If it is genuinely safe to ignore, say why:

```go
// A cache write failure must not fail the request: the data is already
// correct, the cache is only an optimisation.
_ = port.SetJSON(ctx, c, key, value, ttl)
```

---

## 4. Ambient dependencies

Three things must never be called directly in a module, because each one makes
behaviour depend on something other than the function's inputs:

| Never                    | Use instead                        | Why                                        |
| ------------------------ | ---------------------------------- | ------------------------------------------ |
| `time.Now()`             | `port.Clock`                        | Time-dependent rules become untestable.     |
| `uuid.New()`             | `port.IDGenerator` (via repository) | Tests cannot predict a random id.           |
| `db.Begin()`             | `transaction.Manager`               | Hand-rolled transactions leak connections.  |

```go
// WRONG
if order.CreatedAt.Add(30 * time.Minute).Before(time.Now()) { ... }

// RIGHT
if order.CreatedAt.Add(30 * time.Minute).Before(s.clock.Now()) { ... }
```

Test doubles ship in the production packages so any module's tests can import
them:

```go
c := clock.NewFakeAt("2026-07-22T10:00:00Z")  // or clock.NewFake(someTime)
c.Advance(31 * time.Minute)                   // step over the expiry boundary

gen := id.NewSequential()
want := gen.Peek(1)                           // the id the next Create will use
```

The exception is row audit timestamps. `CreatedAt` and `UpdatedAt` are managed by
GORM, not the Clock — see `EntityConvention.md` §4 for why fighting the ORM there
is not worth it.

---

## 5. Context

- `context.Context` is the **first parameter**, always named `ctx`.
- Never store a `context.Context` in a struct.
- Never pass `*gin.Context` below the handler layer. Use `appcontext.Context(c)`.
- Never use `context.Background()` inside a request path — it discards
  cancellation, so a disconnected client leaves work running.

Exception: shutdown paths deliberately use `context.Background()` because the
request context is already cancelled, and reusing it would abort every cleanup
step immediately.

---

## 6. Logging

```go
log := appcontext.Logger(ctx)
log.Info("stock reserved", zap.String("sku", sku), zap.Int("qty", qty))
```

- Always the context logger, never a package-level global. The context logger
  carries request id, tenant and user.
- Structured fields, never string interpolation. `zap.String("sku", sku)` is
  queryable; `fmt.Sprintf("sku=%s", sku)` is not.
- Never log secrets, tokens, passwords or full request bodies.
- **Log an error once**, at the layer that converts it into a response.

| Level   | Use for                                          |
| ------- | ------------------------------------------------ |
| `Debug` | Development detail, 4xx client errors.            |
| `Info`  | Normal lifecycle and business events.             |
| `Warn`  | Recovered or degraded, no action needed yet.      |
| `Error` | A fault that needs a human. 5xx.                  |

`Fatal` and `Panic` are forbidden outside `main` — they skip deferred functions
and therefore defeat graceful shutdown.

---

## 7. Comments

Comment **why**, not **what**. The code already says what.

```go
// WRONG — restates the code
// Set the max open connections
sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)

// RIGHT — explains a decision that is not visible
// KEYS is O(n) over the whole keyspace and blocks the single-threaded Redis
// server for the duration; on a production dataset that is an outage.
keys, cursor, err := c.client.Scan(ctx, cursor, pattern, scanBatchSize).Result()
```

Every exported identifier has a doc comment starting with its name. Every
package has a package comment explaining its role and its boundary.

Do not write comments addressed to a reviewer ("changed this to fix the bug",
"as discussed"). They are noise the moment the PR merges.

---

## 8. Structs and interfaces

- Accept interfaces, return concrete types.
- Declare an interface **where it is consumed**, not where it is implemented.
  `repository.Repository` is declared next to its implementation only because
  its consumer — the service — is in the same module.
- Keep interfaces small. `port.Queue` has one method.
- Assert compliance at compile time:

```go
var _ port.Cache = (*RedisCache)(nil)
```

- Functional options for anything with more than three optional parameters
  (`port.WithPriority`, `port.WithDelay`), so adding an option later does not
  break existing call sites.

---

## 9. Concurrency

- The race detector runs in CI. Do not merge around it.
- A goroutine started in a request must respect `ctx` cancellation.
- Never start an unbounded goroutine per request; use the queue.
- Guard shared mutable state with a mutex, or do not share it. The health
  service is safe for concurrent use because it is immutable after
  construction.

---

## 10. Tests

- Table-driven where there is more than one case.
- Service-layer tests must need **no database and no Redis**. If a test needs
  infrastructure to run, the layering is wrong.
- Name what is being asserted: `TestReady_OneDependencyDown`.
- Test behaviour, not implementation. A test that breaks on a rename without a
  behaviour change is a liability.
- Use `t.Setenv` rather than `os.Setenv` — it restores automatically and marks
  the test as non-parallel.
- Failure messages state got and want:

```go
t.Errorf("Ready().Status = %q, want %q", got, want)
```

---

## 11. Imports

Three groups, separated by blank lines, `gofmt`-ordered within each:

```go
import (
    "context"
    "fmt"

    "github.com/gin-gonic/gin"
    "go.uber.org/zap"

    "github.com/batokhehe/wms-saas/backend/internal/shared/port"
)
```

Alias only to resolve a genuine collision, and make the alias descriptive:

```go
adaptercache "github.com/.../internal/shared/adapter/cache"
infraredis   "github.com/.../internal/shared/infra/redis"
```

Never dot-import. Never blank-import except for a driver registration, with a
comment saying what it registers.

---

## 12. Forbidden

| Do not                             | Because                                            |
| ---------------------------------- | -------------------------------------------------- |
| `panic()` in library code           | Return an error. Panic is for unrecoverable init.   |
| `log.Fatal` outside `main`          | Skips defers, defeats graceful shutdown.            |
| Global mutable state                | Untestable, racy.                                   |
| `c.JSON` in a handler               | Breaks the one-envelope rule.                       |
| `gorm` outside `repository/`        | Collapses the layering.                             |
| `gin` in `service/`                 | Makes business logic untestable and un-reusable.    |
| `AutoMigrate`                       | See `MigrationGuide.md`.                            |
| `time.Now()` in a module            | Use `port.Clock`; time-dependent rules stay testable.|
| `uuid.New()` in a module            | The repository assigns ids from `port.IDGenerator`.  |
| `db.Begin()` in a module            | Use `transaction.Manager`; hand-rolled tx leak.      |
| Entity returned from a handler      | Return a DTO; entities are not an API contract.      |
| Unpaginated list of a growable table| Use `FindAll`; it degrades every week otherwise.     |
| Vendor SDKs in a module             | Defeats the port abstraction.                       |
| Secrets in code or committed `.env` | Obvious.                                            |
| `interface{}` where a type fits     | Use generics or a concrete type.                    |

---

## 13. Pull requests

- One logical change.
- `make fmt vet test` before pushing.
- New module? Work through the checklist in `ModuleConvention.md`.
- Schema change? Up **and** down migration, both tested.
- New endpoint? Confirm the envelope matches `APIConvention.md`.
- Explain **why** in the description. The diff already shows what.
