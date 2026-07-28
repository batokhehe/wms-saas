# Inventory Domain

Inventory is the current state of one Product inside one Storage Location. It is
not a stock ledger — it **owns every stock transition**, and nothing outside the
aggregate mutates a quantity. This document covers the full module: the aggregate
it wraps, the application layer built in Sprint 7B (service, transport, wiring,
authorisation), and how a request flows through it.

The aggregate (Sprint 7) and its persistence (Sprint 7A) are documented in
`InventoryPersistence.md`; this focuses on the application layer.

---

## 1. Aggregate summary

`entity.Inventory` is an aggregate root with no exported fields and no setters.

- **Identity/references:** id, company, warehouse, location, product.
- **Tracking:** `NONE` (a fungible pool), `LOT` (one record per batch), `SERIAL`
  (one record per unit, on-hand always exactly 1).
- **Quantities:** on-hand and reserved (non-negative integers); available =
  on-hand − reserved, derived.
- **Status:** `ACTIVE ↔ LOCKED`; LOCKED freezes every movement.
- **Invariants:** on-hand ≥ 0, reserved ≥ 0, on-hand ≥ reserved, serial ⇒ on-hand
  = 1, lot/serial present per tracking type. Every behaviour upholds these; the
  service never re-checks them.

Behaviours: `Increase`, `Decrease`, `Reserve`, `ReleaseReservation`, `Adjust`,
`TransferOut`, `TransferIn`, `CompleteCycleCount`, `Lock`, `Unlock`.

---

## 2. Application flow

Every write follows one shape:

```
resolve tenant + actor  (from RequestContext, never from the body)
  → BEGIN transaction
      → load aggregate (tenant-scoped) OR verify references + run factory
      → gather cross-aggregate facts (providers, specifications)
      → call ONE aggregate behaviour
      → repo.Save  (create or optimistic update)
  → COMMIT
  → publish the events the aggregate recorded
```

Reads (`GetInventory`, `ListInventory`) resolve the tenant, query the repository,
and map to DTOs — no transaction, no events.

---

## 3. REST API

Mounted at `/api/v1/inventories`. Every route runs
`Authenticate → ResolveCompany → RequireCompany → LoadPermissions → RequirePermission`.

| Method | Path | Permission | Operation |
|---|---|---|---|
| GET | `/inventories` | inventory.read | list (filter by warehouse/location/product/tracking/status) |
| POST | `/inventories` | inventory.create | open a stock position |
| GET | `/inventories/:id` | inventory.read | get |
| POST | `/inventories/:id/increase` | inventory.update | add stock |
| POST | `/inventories/:id/decrease` | inventory.update | remove stock |
| POST | `/inventories/:id/reserve` | inventory.reserve | reserve available |
| POST | `/inventories/:id/release` | inventory.reserve | release a reservation |
| POST | `/inventories/:id/transfer-out` | inventory.transfer | move stock out |
| POST | `/inventories/:id/transfer-in` | inventory.transfer | move stock in |
| POST | `/inventories/:id/adjust` | inventory.adjust | absolute manual correction |
| POST | `/inventories/:id/cycle-count` | inventory.cyclecount | reconcile to a physical count |
| POST | `/inventories/:id/lock` | inventory.lock | freeze |
| POST | `/inventories/:id/unlock` | inventory.lock | unfreeze |

Movement bodies are `{ "quantity": <positive int> }`. Adjust is
`{ "quantity": <int ≥ 0>, "reason": "…" }`; cycle-count is `{ "counted": <int ≥ 0> }`.
All responses use the platform envelope; the response carries on-hand, reserved
and the **aggregate-computed** available.

---

## 4. Permissions

Eight codes (`entity/permission.go`), catalogued in `rbac/entity`, seeded by
migration `20260730100000`.

| Code | Guards | OWNER | ADMIN | STAFF |
|---|---|:-:|:-:|:-:|
| inventory.read | viewing | ✓ | ✓ | ✓ |
| inventory.create | open a position | ✓ | ✓ | — |
| inventory.update | increase / decrease | ✓ | ✓ | ✓ |
| inventory.reserve | reserve / release | ✓ | ✓ | ✓ |
| inventory.transfer | transfer in / out | ✓ | ✓ | ✓ |
| inventory.cyclecount | cycle count | ✓ | ✓ | ✓ |
| inventory.lock | lock / unlock | ✓ | ✓ | — |
| inventory.adjust | manual absolute correction | ✓ | — | — |

`inventory.adjust` is withheld from ADMIN — a manual override with no physical
count behind it is a governance decision, like `warehouse.delete`. STAFF gets the
operational stock actions the earlier sprints deferred to Inventory. The seed
migration also **backfills** existing companies' system roles, because the RBAC
provisioner never repairs an existing role. A `_test`-package drift guard fails
the build if the module's codes and the RBAC catalogue diverge.

---

## 5. Service responsibilities

The service orchestrates and holds **no business rules**:

- **Validation** — parse request strings into value objects (`Quantity`,
  `LotNumber`, `SerialNumber`, `InventoryQuantity`); the VO constructors reject
  bad input. It never checks an aggregate invariant.
- **Repository coordination** — load, save, list.
- **Transaction boundaries** — every write runs inside one transaction.
- **Specification execution** — at create, the tracking-appropriate uniqueness
  rule (`InventoryExists` for NONE, `UniqueLot`, `UniqueSerial`).
- **Provider verification** — at create, that the product, warehouse and location
  exist and relate correctly (the cross-aggregate references the aggregate cannot
  check itself).
- **Event publication** — drains and publishes the aggregate's events after commit.

A `mutate` helper factors the load → verify → one-call → save shape so the
transaction boundary and event publication are identical for every operation.

---

## 6. Publisher

Events are raised by the **aggregate** and pulled by the service **after the
transaction commits**, so an event is only ever published for a change that
persisted. `LogEventPublisher` writes them to the structured audit log (prefixing
every field with `event_`), preferring the request-scoped logger so each line is
attributable to its request. There is no broker subscriber yet; the log is the
durable trail.

---

## 7. Transaction flow

`Save` is create-or-update behind one method: inside a transaction it checks
whether the row exists (tenant-scoped), then `Create`s or `UpdateOptimistic`s.
The version check is a conditional `UPDATE … WHERE id = ? AND version = ?`; a lost
race yields `ErrConcurrentModification`, which the service turns into a `409`
carrying the current version. Nothing is published until the transaction commits,
and a rolled-back transaction leaves no row and emits no event.

---

## 8. Event lifecycle

```
behaviour called → aggregate validates invariant → mutates → records Event
  → repo.Save persists → transaction commits → service.publish → PullEvents (clears)
  → EventPublisher.Publish
```

Catalogue: `inventory.created`, `increased`, `decreased`, `reserved`, `released`,
`adjusted`, `transferred` (OUT/IN), `locked`, `unlocked`, `cycle_count_completed`.
Each carries immutable facts plus the resulting on-hand/reserved/available.

---

## 9. Repository interaction

The service depends only on the `repository.Repository` interface (Save,
FindByID, FindByProductLocation, FindByLot, FindBySerial, List, Exists) and never
sees a persistence model. The specifications wrap the same interface. Tenant
scoping, optimistic locking and translation all live behind it — see
`InventoryPersistence.md`.

---

## 10. Validation rules

Three layers, outermost first:

1. **DTO binding** — required fields, `oneof` for tracking/status, `gt=0` for
   movement amounts, `gte=0` for counts, uuid format for ids, length caps on lot
   (64) and serial (128).
2. **Value-object constructors** — non-negative/positive quantities, overflow
   guard, lot/serial non-empty.
3. **Aggregate invariants** — the quantity and tracking rules in §1, plus the
   locked-state freeze and serial-fixed-at-one rules.

Cross-aggregate existence (product/warehouse/location) is checked by **providers**;
cross-record uniqueness (none/lot/serial) by **specifications** backed by DB
indexes.

---

## 11. Error catalogue

| Code | HTTP | Raised when |
|---|---|---|
| `VALIDATION_ERROR` | 422 | bad amount/count/uuid; unknown product/warehouse/location (provider); location not in the warehouse or archived |
| `CONFLICT` | 409 | duplicate position (none/lot/serial); insufficient on-hand/available; decrease below reserved; a movement on a LOCKED record; a quantity-change on a SERIAL record; lock-already-locked / unlock-when-active; concurrent modification; active external reservations block a removal |
| `NOT_FOUND` | 404 | inventory not in this company |
| `FORBIDDEN` | 403 | no company context; defence-in-depth cross-tenant write |
| `UNAUTHORIZED` | 401 | no authenticated principal |
| `INTERNAL_ERROR` | 500 | unexpected persistence failure (cause logged, generic message returned) |

---

## 12. Known gaps

1. **ReservationProvider is permissive.** `DecreaseInventory` and `TransferOut`
   consult it, but the default `NoReservations` reports none, so external
   reservations never block a removal yet. The Reservation sprint wires the real
   adapter — no inventory file changes.
2. **No hard FK from a serial to a global registry.** Serial uniqueness is
   enforced across the `inventories` table (a globally-unique partial index), not
   against an external serial catalogue that does not exist.
3. **Transfers are per-record.** `TransferOut` and `TransferIn` each act on one
   inventory record; pairing them into an atomic two-location move is a
   higher-level (Putaway/movement) concern, out of this module's scope.
