# Inventory Persistence (Sprint 7A)

This document covers the persistence layer that makes the Inventory aggregate
round-trippable through PostgreSQL: the schema, the translation layer, the
repository, and the guarantees each provides. The aggregate itself (behaviours,
invariants, value objects, events) was delivered in Sprint 7 and is unchanged —
it already exposed every getter persistence needs, so no aggregate code was
modified.

Inventory is a **single-row aggregate**: no child collections, so one table and
one translation model, persisted atomically with an optimistic-lock version.

---

## 1. Schema

Migration `20260729100000_create_inventories_table`. One table, `inventories`.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | repository-assigned |
| company_id | UUID NOT NULL | FK `companies` ON DELETE CASCADE |
| warehouse_id | UUID NOT NULL | FK `warehouses` ON DELETE RESTRICT |
| location_id | UUID NOT NULL | FK `storage_locations` ON DELETE RESTRICT |
| product_id | UUID NOT NULL | FK `products` ON DELETE RESTRICT |
| tracking_type | VARCHAR(16) NOT NULL | CHECK `NONE`/`LOT`/`SERIAL` |
| lot_number | CITEXT NULL | present only for LOT |
| serial_number | CITEXT NULL | present only for SERIAL |
| on_hand | BIGINT NOT NULL DEFAULT 0 | physical count |
| reserved | BIGINT NOT NULL DEFAULT 0 | promised count |
| status | VARCHAR(16) NOT NULL DEFAULT 'ACTIVE' | CHECK `ACTIVE`/`LOCKED` |
| version | BIGINT NOT NULL DEFAULT 1 | optimistic-lock token |
| created_by / updated_by | UUID NOT NULL | FK `users` |
| created_at / updated_at | TIMESTAMPTZ NOT NULL | audit |
| deleted_at | TIMESTAMPTZ NULL | soft-delete marker (unused; GORM convention) |

`available` is **not stored** — it is `on_hand − reserved`, derived by the
aggregate. Quantities are `BIGINT`, not `NUMERIC`: they are discrete counts, and
integer arithmetic is exact and overflow-checked in the aggregate.

**FKs are RESTRICT** on warehouse/location/product: a warehouse, location or
product that still has stock against it must not be hard-deleted out from under
its inventory — the same rule `storage_locations` applies to its warehouse.

### CHECK constraints (database backstops for the aggregate's invariants)

- `inventories_quantities_check` — `on_hand >= 0 AND reserved >= 0 AND on_hand >= reserved`
- `inventories_tracking_check` — tracking_type ∈ {NONE, LOT, SERIAL}
- `inventories_status_check` — status ∈ {ACTIVE, LOCKED}
- `inventories_tracking_presence_check` — the lot/serial presence rules per
  tracking type, plus `SERIAL ⇒ on_hand <= 1`

The aggregate is the primary guard; these are the backstop for anything that
reaches the table another way.

---

## 2. Indexes and constraints

### Uniqueness — one authoritative record per addressable position

Each is **partial** on its tracking type (and on `deleted_at IS NULL`), so the
three rules never collide:

| Index | Columns | Predicate |
|---|---|---|
| `ux_inventories_none_position` | (company_id, product_id, location_id) | tracking = NONE |
| `ux_inventories_lot_position` | (company_id, product_id, location_id, lot_number) | tracking = LOT |
| `ux_inventories_serial` | (serial_number) | tracking = SERIAL |

The serial index is on the **bare serial column** — a serial is a globally unique
physical unit (an IMEI, a VIN), so the same serial cannot exist twice *anywhere*,
not merely once per company. This is enforced across tenants.

### Lookup indexes (all partial on `deleted_at IS NULL`, company_id leading)

`(company_id, warehouse_id)` · `(company_id, product_id)` ·
`(company_id, location_id)` · `(company_id, product_id, location_id)` (the hot
path behind `FindByProductLocation`) · `(serial_number)` · `(lot_number)`.

---

## 3. Translation layer

`repository/model.go`. `inventoryModel` embeds `shared/entity.BaseEntity`
(id/version/timestamps/soft-delete) and carries the columns above. It exists
because `entity.Inventory` has unexported fields GORM cannot reflect over;
exporting them would delete the encapsulation the aggregate rests on.

- **`toModel(*entity.Inventory) *inventoryModel`** — reads through the aggregate's
  getters (the only access anyone has). Nullable `lot_number` / `serial_number`
  come from `HasLot()`/`HasSerial()`; counts from `OnHand().Value()` /
  `Reserved().Value()`.
- **`toDomain(*inventoryModel) *entity.Inventory`** — calls `entity.Reconstitute`,
  **not** the factory, so loading a row raises no `InventoryCreated` event.
  Value-object construction errors are discarded (the DB already enforced the
  constraints); `Reconstitute` still rejects a structurally invalid aggregate
  (reserved above on-hand, a serial without a number).

The model is unexported and never leaves the repository package.

---

## 4. Repository methods and flow

`repository/repository_impl.go` implements the interface declared in
`repository/repository.go` (Sprint 7).

| Method | Flow |
|---|---|
| `Save` | create-or-update in one transaction (see §5) |
| `FindByID` | tenant-scoped `FindByID` → `toDomain` |
| `FindByProductLocation` | tenant + product + location, `FindMany` → all rows (1 for NONE, many for LOT/SERIAL) |
| `FindByLot` | tenant + product + location + lot, `FindOne` |
| `FindBySerial` | tenant + product + serial, `FindOne` |
| `List` | tenant + optional warehouse/location/product/tracking/status filters, paginated |
| `Exists` | tenant + product + location existence check (backs `InventoryExists`) |

The repository holds no business rules — it translates and queries. It composes
the generic `shared/repository.Base` over the persistence model (held as an
unexported field, not embedded, so base CRUD is not promoted onto it).

---

## 5. Optimistic locking

The repository owns `Version`. A single `Save` covers both create and update
because the aggregate's contract exposes one write method:

1. Inside a transaction, check whether a row with this id already exists
   (tenant-scoped).
2. **Not found** → `Create` (version starts at 1).
3. **Found** → `UpdateOptimistic`, which issues
   `UPDATE … WHERE id = ? AND version = ?` and advances the version **only** on a
   successful match. Zero rows affected means another writer already moved the
   row → `ErrConcurrentModification`, surfaced as `CONFLICT`.

`TestConcurrentSaveAllowsExactlyOneWriter` proves the database — not an
in-process lock — arbitrates: two goroutines race a stale write, one succeeds,
one gets the conflict, and the row lands at version 2.

---

## 6. Transaction guarantees

`Save` wraps its check-and-write in the shared `transaction.Manager`. Outside a
caller transaction it opens its own; inside one it joins via a `SAVEPOINT` rather
than opening a second (PostgreSQL has no nested transactions). Either way the
write is atomic — there are no partial writes, and a rolled-back outer
transaction leaves no row, proven by `TestSaveRollsBack`.

---

## 7. Multi-tenant isolation

Every method takes a `companyID` and applies `ForCompany` — forgetting it does
not compile. A record in another company is `NOT_FOUND`, never `FORBIDDEN` (a 403
would confirm it exists). Even a globally-unique serial cannot be resolved by the
wrong tenant, because `FindBySerial` is still company-scoped
(`TestReadIsTenantIsolated`).

---

## 8. Integration tests

`repository/repository_integration_test.go` (`//go:build integration`), run
against live PostgreSQL with `make test-integration`. 14 tests, all passing:

| Test | Proves |
|---|---|
| `TestRoundTripNone` | every field of a NONE record (incl. reserved/available) survives; no events on load |
| `TestRoundTripLotAndSerial` | lot and serial survive; serial on-hand is 1 |
| `TestSaveCreatesThenUpdates` | Save creates, then updates with a version bump to 2 |
| `TestConcurrentSaveAllowsExactlyOneWriter` | optimistic lock: exactly one writer wins |
| `TestFindByProductLocation` | returns every record for a product+location |
| `TestFindByLotAndSerial` | resolves by lot/serial; NOT_FOUND for unknown |
| `TestExists` | false before, true after |
| `TestListFiltersAndIsolates` | status filter + tenant exclusion |
| `TestReadIsTenantIsolated` | cross-tenant FindByID/FindBySerial refused |
| `TestNoneUniqueness` | duplicate NONE position → CONFLICT |
| `TestLotUniqueness` | duplicate lot → CONFLICT; distinct lot allowed |
| `TestSerialUniquenessIsGlobal` | duplicate serial rejected within AND across companies |
| `TestSaveRollsBack` | a rolled-back transaction leaves no row |
| `TestForeignKeyRejectsUnknownCompany` | FK integrity |

The fixture seeds the full FK chain (company → warehouse → location → product)
per tenant, because `inventories` foreign-keys all four.
