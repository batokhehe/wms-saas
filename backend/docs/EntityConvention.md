# Entity Convention

An entity is a domain type that is persisted. This document covers what every
entity must look like and why.

---

## 1. Every entity embeds BaseEntity

```go
package entity

import (
    "github.com/google/uuid"
    sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

type Product struct {
    sharedentity.BaseEntity

    CompanyID uuid.UUID `gorm:"type:uuid;not null;index" json:"-"`
    SKU       string    `gorm:"type:citext;not null"   json:"sku"`
    Name      string    `gorm:"type:varchar(255);not null" json:"name"`
}

func (Product) TableName() string { return "products" }
```

`BaseEntity` supplies:

| Field       | Type             | Purpose                                  |
| ----------- | ---------------- | ---------------------------------------- |
| `ID`        | `uuid.UUID`      | Primary key, assigned by the repository.  |
| `CreatedAt` | `time.Time`      | Set by GORM on insert.                    |
| `UpdatedAt` | `time.Time`      | Set by GORM on every write.               |
| `DeletedAt` | `gorm.DeletedAt` | Soft-delete marker.                       |

Embedding is not optional. `repository.Base` constrains its type parameter to
`entity.Identifiable`, which only `BaseEntity` satisfies — so an entity that
does not embed it **cannot compile** against the base repository.

---

## 2. Why gorm is imported in the entity layer

The project rule is that `gorm` appears only inside a module's `repository/`
package. `BaseEntity` is the single documented exception, and it is narrow:
`gorm.DeletedAt` is a **data type**, not behaviour. No `*gorm.DB`, no queries,
no session handling ever appears in an entity.

The exception buys something concrete. `gorm.DeletedAt` makes soft deletion
automatic — GORM appends `WHERE deleted_at IS NULL` to every query against the
model without the caller asking. Hand-rolling that with a `*time.Time` means
every single query must remember the filter, and the first one that forgets
silently resurrects deleted rows. In a WMS that is deleted stock reappearing in
an availability check, and nobody notices until a customer is promised
inventory that does not exist.

One import in one shared struct is a better trade than that failure mode.

---

## 3. Identifiers

**Never call `uuid.New()` in a module.** The repository assigns the ID from the
injected `port.IDGenerator`:

```go
func (r *Base[T, PT]) Create(ctx context.Context, e PT) error {
    if !e.IsPersisted() {
        e.SetID(r.ids.NewID())
    }
    ...
}
```

Two reasons this matters:

- **Determinism.** A test using `id.NewSequential()` knows the created entity
  will be `00000000-0000-4000-8000-000000000001` and can assert on it directly,
  rather than reading the id back out of the result it is supposed to be
  verifying.
- **Substitutability.** UUIDv4 is random, which makes it a poor B-tree key:
  inserts scatter across the whole index and the page cache stops helping. If
  that becomes a problem, moving to time-ordered UUIDv7 is one line behind
  `port.IDGenerator` instead of an audit of every package.

A caller may still set the ID explicitly — for an idempotent create driven by a
client-supplied key — and the repository preserves it.

**Why not a database default?** `gen_random_uuid()` never fires, because GORM
sends the zero UUID explicitly rather than omitting the column. It is also the
wrong place: the Flutter client works offline, and a device that cannot mint its
own identifier must round-trip to the server before it can reference anything it
just created.

---

## 4. Timestamps

`CreatedAt` and `UpdatedAt` are managed by **GORM**, not by the injected Clock.

This is a decision, not an oversight. GORM overwrites `UpdatedAt` on every write
regardless of what the caller set, so a repository assigning it from a Clock
would be silently overridden — producing code that *looks* injectable but is
not. Fighting the ORM to reclaim two audit columns is not worth the complexity.

`port.Clock` is therefore for **business time**: expiry windows, scheduling,
cut-offs, SLA calculations. That is what actually needs to be deterministic in a
unit test; row audit timestamps do not.

The connection sets `NowFunc` to return UTC, so every GORM-managed timestamp is
UTC in memory as well as in the column. Without it, a test asserting on
`CreatedAt` would pass in one timezone and fail in CI.

When a test genuinely needs a fixed `CreatedAt`, set it explicitly before
`Create` — GORM preserves a non-zero value.

---

## 5. Multi-tenancy

Every tenant-owned entity carries `CompanyID`:

```go
CompanyID uuid.UUID `gorm:"type:uuid;not null;index" json:"-"`
```

`json:"-"` is deliberate. The tenant is implied by the caller's token; echoing
it back only widens what a client can learn about the system.

`BaseEntity` does **not** include `CompanyID`, because not every table is
tenant-owned — `companies` itself, system settings, and reference data are not.
Putting it in the base would force a meaningless column onto those tables and,
worse, make "is this tenant-scoped?" invisible at the declaration site.

See `RepositoryConvention.md` for how the tenant filter is enforced on queries.

---

## 6. Column types

| Use for                  | Type                    | Why                                        |
| ------------------------ | ----------------------- | ------------------------------------------ |
| Identifiers              | `uuid`                  | Offline-capable clients; no id guessing.    |
| Case-insensitive strings | `citext`                | `ABC-1` and `abc-1` must not be two rows.   |
| Timestamps               | `timestamptz`           | Warehouses span time zones.                 |
| Money / quantities       | `numeric(p,s)`          | Never float — 0.1 + 0.2 ≠ 0.3.              |
| Free text                | `text`                  | No length limit unless one is a real rule.  |
| Bounded strings          | `varchar(n)`            | When `n` is an actual business constraint.  |

`numeric` for stock quantities matters more than it looks. A WMS adds and
subtracts the same quantities thousands of times a day; binary floating point
accumulates error on every operation, and the discrepancy surfaces as a stock
count that will not reconcile.

---

## 7. Domain behaviour belongs on the entity

```go
func (p *Product) BelongsTo(companyID uuid.UUID) bool { return p.CompanyID == companyID }
func (p *Product) IsSellable() bool                   { return p.Status == StatusActive && !p.IsDeleted() }
```

Scattering `p.Status == StatusActive && p.DeletedAt.Valid == false` across five
services means one inverted check becomes a bug nobody notices. A named method
is testable in isolation and reads as the rule it encodes.

Entities must **not** contain persistence logic, HTTP concerns, or calls to
other services. If a rule needs a database lookup, it is a service concern.

---

## 8. Checklist

- [ ] Embeds `sharedentity.BaseEntity`.
- [ ] `TableName()` declared, pinning the table name.
- [ ] `CompanyID` present and `json:"-"` if tenant-owned.
- [ ] No `uuid.New()` anywhere in the module.
- [ ] No `time.Now()` — inject `port.Clock` for business time.
- [ ] `numeric` for quantities and money, never `float`.
- [ ] Migration written; `company_id` leads every index.
- [ ] Unique constraints are per-tenant and partial on `deleted_at IS NULL`.

See also: `SoftDeleteConvention.md`, `RepositoryConvention.md`,
`MigrationGuide.md`.
