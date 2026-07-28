# Repository Convention

A repository is the only place a module talks to the database. This document
covers how to build one on top of the shared generic base.

---

## 1. Compose the base, do not copy it

```go
type productRepository struct {
    *base.Base[entity.Product, *entity.Product]
}

func New(db *gorm.DB, ids port.IDGenerator) Repository {
    return &productRepository{
        Base: base.New[entity.Product, *entity.Product](db, ids, "product.repository"),
    }
}
```

`repository.Base` provides Create, CreateMany, Update, UpdateFields, Delete,
HardDelete, FindByID, FindOne, FindAll, FindMany, Count, Exists and ExistsBy.
A module repository adds only what is specific to its domain.

Composition means that when the base gains a capability — or a bug is fixed in
error translation — every module gets it without edits. Copy-pasted CRUD
diverges within a month.

### Why two type parameters

`Base[T, PT]` looks redundant but is a genuine Go limitation. The base needs the
**value** type to build `[]T` slices and the **pointer** type to call `SetID`,
whose receiver must be a pointer. One type parameter can express one or the
other, not both.

It also enforces the embedding rule: the constraint requires
`entity.Identifiable`, which only `entity.BaseEntity` satisfies. An entity that
forgot to embed it will not compile.

---

## 2. The interface is GORM-free

```go
type Repository interface {
    Create(ctx context.Context, product *entity.Product) error
    FindByID(ctx context.Context, companyID, id uuid.UUID) (*entity.Product, error)
    FindAll(ctx context.Context, companyID uuid.UUID, q dto.ListQuery) (pagination.Page[entity.Product], error)
    ExistsBySKU(ctx context.Context, companyID uuid.UUID, sku string) (bool, error)
}
```

No `*gorm.DB`, no `Scope`, no query builder in any signature. `Scope` exposes
`*gorm.DB` and is therefore usable **only inside a repository package** — a
service cannot compose queries, and does not need to.

Declare the interface next to its implementation. Its consumer is the service in
the same module, and having it here is what lets that service be tested against
a fake with no database.

---

## 3. Tenant scoping is mandatory and explicit

**Every method of a tenant-owned repository takes `companyID`.**

```go
func (r *productRepository) FindByID(
    ctx context.Context, companyID, id uuid.UUID,
) (*entity.Product, error) {
    return r.Base.FindByID(ctx, id, forTenant(companyID))
}

func forTenant(companyID uuid.UUID) base.Scope {
    return base.ForCompany(companyID)
}
```

Making the tenant a required parameter means forgetting it does not compile.
That is the entire defence: a missing tenant filter is not a bug that returns too
many rows, it is one company reading another company's stock.

The base does **not** apply the filter automatically, because not every table is
tenant-owned. Naming the helper `forTenant` rather than inlining
`base.Where("company_id = ?", id)` means a reviewer auditing this file is
scanning for one identifier, not for a `Where` clause that is easy to skim past.

A row belonging to another tenant produces `NOT_FOUND`, never `FORBIDDEN`.
Distinguishing the two would confirm that another company's record exists.

---

## 4. Scopes

| Scope                        | Purpose                                       |
| ---------------------------- | --------------------------------------------- |
| `ForCompany(id)`             | Tenant filter. Required on tenant-owned tables.|
| `ByID(id)`                   | Primary key filter.                            |
| `Search(term, columns...)`   | Case-insensitive partial match.                |
| `Where(query, args...)`      | One-off condition. Prefer a named scope.       |
| `Preload(assoc)`             | Eager-load an association.                     |
| `Lock()`                     | `FOR UPDATE` row lock.                         |
| `WithDeleted()`              | Include soft-deleted rows.                     |
| `OnlyDeleted()`              | Only soft-deleted rows.                        |

`Search` parameterises the term but interpolates the **column list**, so columns
must be compile-time constants — never client input. It escapes LIKE
metacharacters, so a search for `50%` is not a wildcard matching every row, and
it wraps the OR-ed conditions in parentheses: without them
`company_id = ? AND a ILIKE ? OR b ILIKE ?` leaks every row matching `b`, across
all tenants.

`Lock()` is required for read-modify-write on stock levels. Without it two
concurrent picks both read quantity 10, both subtract 6, and both write 4 —
leaving negative physical stock behind a correct-looking number. It is only
valid inside a transaction.

---

## 5. Pagination

```go
func (r *productRepository) FindAll(
    ctx context.Context, companyID uuid.UUID, query dto.ListQuery,
) (pagination.Page[entity.Product], error) {
    scopes := []base.Scope{forTenant(companyID)}

    if query.HasSearch() {
        scopes = append(scopes, base.Search(query.Search, dto.SearchColumns()...))
    }

    return r.Base.FindAll(ctx, query.Request, scopes...)
}
```

`FindAll` **refuses** a `pagination.Request` that has not been through `Apply`.
That check is a security control, not tidiness:

- `ORDER BY` cannot be parameterised by any SQL driver, so the column name is
  interpolated. `Apply` is what constrains it to the endpoint's allow-list.
- An unordered query lets PostgreSQL return rows in any order between calls, so
  page 2 can repeat or skip rows from page 1.

`Apply` is called in the **service**, because the sort allow-list is a rule about
what that endpoint permits. See `APIConvention.md`.

The count runs before limit/offset so the metadata describes the whole result
set. When the count is zero the second query is skipped entirely — an empty list
endpoint costs one round trip, not two.

---

## 6. Transactions

Repositories are transaction-aware with no plumbing. Every query resolves its
handle through `transaction.DB(ctx, r.db)`, which returns the transaction on the
context if there is one and the pool handle otherwise.

The consequence: the same repository method works standalone and inside a
transaction, and the **caller** decides which by choosing the context it passes.

```go
err := s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
    if err := s.repo.Create(ctx, &product); err != nil {   // enrolled
        return err
    }
    return s.stock.Initialise(ctx, product.ID)             // same transaction
})
```

**Never call `db.Begin()` in a module.** A hand-rolled transaction must get four
things right at every call site — commit on success, roll back on error, roll
back on panic, never do either twice — and the failure mode is a connection
leaked from the pool under exactly the conditions where the system is already
under stress.

Nested `RunInTransaction` calls join the outer transaction through a SAVEPOINT.
PostgreSQL has no nested transactions; opening a second connection inside an open
one would deadlock against its locks, and the symptom is a request that hangs
until the pool times out.

`Begin`/`Commit`/`Rollback` exist for the rare unit of work that cannot be
expressed as a single function. Prefer `RunInTransaction`; it is impossible to
leak.

---

## 7. Error translation

Every error return goes through `postgres.TranslateError`. The base already does
this for its own methods; custom queries must do it too:

```go
err := r.DB(ctx).Raw(query, args...).Scan(&rows).Error
return postgres.TranslateError(err, "product.repository.SearchByBarcode")
```

Without it a unique-constraint violation surfaces as a 500 carrying the
constraint name, exposing the schema to whoever triggered it. With it, the same
failure is a clean 409 `CONFLICT`. See `ErrorConvention.md`.

`UpdateFields` and `Delete` check `RowsAffected` and return `NOT_FOUND` when
nothing matched. Without that check, updating another tenant's row — or an
already-deleted one — succeeds silently and the API returns 200 for an operation
that changed nothing.

---

## 8. Writing a query the base does not cover

Use `r.DB(ctx)` so the query stays transaction-aware:

```go
func (r *productRepository) LowStock(
    ctx context.Context, companyID uuid.UUID, threshold int,
) ([]entity.Product, error) {
    var products []entity.Product

    err := r.DB(ctx).
        Joins("JOIN stock_levels sl ON sl.product_id = products.id").
        Where("products.company_id = ?", companyID).
        Where("sl.quantity < ?", threshold).
        Find(&products).Error

    return products, postgres.TranslateError(err, "product.repository.LowStock")
}
```

Prefer composing base methods with scopes where possible — they carry the tenant
filter, soft-delete handling and error translation for free.

---

## 9. Performance

- **Always `Preload` associations rendered by a list endpoint.** Lazy-loading
  inside a loop is the N+1 problem, and it is the most common cause of an
  endpoint that is fast in development and unusable in production.
- **`Exists` over `Count`** when the answer is a boolean. `ExistsBy` issues
  `SELECT 1 ... LIMIT 1`, so PostgreSQL stops at the first match instead of
  scanning every qualifying row to produce a number the caller discards.
- **`CreateMany` for bulk work.** 10,000 individual inserts is 10,000 round
  trips; batches of 200 is 50. The batch is bounded because PostgreSQL caps a
  statement at 65535 bind parameters.
- **`FindMany` only for bounded sets** — the statuses of one order, the bins in
  one aisle. Anything a tenant can grow without limit uses `FindAll`.

---

## 10. Checklist

- [ ] Composes `repository.Base` rather than reimplementing CRUD.
- [ ] Interface exposes no GORM type.
- [ ] Every tenant-owned method takes `companyID`.
- [ ] Custom queries use `r.DB(ctx)` and `postgres.TranslateError`.
- [ ] List methods return `pagination.Page[T]`.
- [ ] Associations used by list endpoints are preloaded.
- [ ] No `db.Begin()`; atomic work uses `transaction.Manager`.
