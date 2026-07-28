# Storage Location Domain

A storage location is a physical place inside a warehouse where inventory can
exist: a bin, a rack level, a floor position, a receiving dock.

This is the second DDD aggregate, and the first with a cross-aggregate
reference. This document explains what that means concretely.

---

## 1. Why it is a separate aggregate from Warehouse

A location belongs to exactly one warehouse, which is the shape people normally
model as a child collection. That would be wrong here.

A large distribution centre has **tens of thousands of locations**. Loading a
warehouse "with its locations" would be unusable — and, worse, making them one
aggregate would mean locking the entire warehouse to change one bin's capacity,
serialising every operation on the site behind a relabelled shelf.

So they are two aggregates, and the reference is **by id in one direction only**.
`StorageLocation` holds a `WarehouseID`; it never holds a `*Warehouse`.

That is the rule for aggregate boundaries: *what must be transactionally
consistent together?* A warehouse's address and a bin's capacity have no such
requirement, so they belong in different aggregates.

### How the cross-aggregate check works

"Does this warehouse exist?" is asked through `service.WarehouseVerifier`, an
interface this module declares and `bootstrap` implements over the warehouse
repository. Only the **answer** crosses the boundary — an error or nil, never a
`*entity.Warehouse`.

A location aggregate that loaded a warehouse aggregate would collapse the
boundary that lets each be modified independently. Verified live: creating a
location in an unknown warehouse returns 404.

---

## 2. The aggregate

`entity.StorageLocation` has **no exported fields and no setters**. Every state
change goes through a method naming a business intent:

```go
l.Lock(reason, actor, now)
l.Unlock(actor, now)
l.AssignBarcode(barcode, actor, now)
l.ChangeCapacity(capacity, usage, actor, now)
l.EnableMixedSKU(actor, now)
l.DisableMixedSKU(distinctSKUs, actor, now)
```

"A LOCKED location never accepts stock" is only a guarantee if there is no way
to reach ACTIVE except through `Unlock()`.

GORM cannot map the type — it reflects over exported fields — so
`repository/model.go` holds a separate persistence model plus a hand-written
translation. `TestAggregateSurvivesRoundTrip` asserts every field survives,
because that translation is exactly where a forgotten field causes silent data
loss.

### Why it starts ACTIVE, unlike Warehouse

A warehouse starts in DRAFT because it must be *commissioned* — it needs an
address, a contact and zones before anyone can ship from it. A location has no
such prerequisite: once its coordinate exists, the physical shelf exists.

Forcing a DRAFT step would mean importing a 20,000-location rack layout and then
activating all 20,000, which is ceremony with no invariant behind it. A location
that is not yet usable is expressed with INACTIVE.

---

## 3. Value objects

### Coordinate

Zone / Aisle / Rack / Level / Bin as **one** value object, not five fields.

The parts are meaningless apart — "Rack 3" identifies nothing without an aisle —
so independent fields would permit half-states no operator could act on. The
value object also enforces **contiguity**: a location may have zone+aisle+rack,
but not zone+rack with the aisle missing, which describes no physical place and
would sort nonsensically in a pick path.

Only zone is required. A floor stack or a receiving dock genuinely has a zone
and nothing else.

The coordinate is **denormalised into columns** rather than kept only inside the
code string, because pick routes sort on aisle then rack then level — and
parsing a string in an `ORDER BY` cannot be indexed.

### LocationCode

The operator-facing identifier, **derived from the coordinate by default**:
`A-01-02-03`. That correspondence is the point — someone reading the label knows
where to walk with nothing to look up. An explicit code overrides it, which is
what a dock labelled `DOCK-1` needs.

### Barcode

Optional and **case-preserving**, unlike codes and coordinates. A barcode is a
machine token reproduced exactly by a scanner; upper-casing would mean a label
printed with a lowercase check character no longer matches.

Stored as SQL NULL when absent, because the unique index is partial on
`barcode IS NOT NULL` and empty strings would collide where NULLs do not.

### Capacity

Weight, volume and pallet positions, all optional and independent — a pallet
rack is constrained by positions, a shelf by weight, a bulk floor area by volume.

**Absent is not zero.** An unmeasured bin accepts stock; a zero-capacity bin
accepts none.

Quantities are `*big.Rat` in the domain and `NUMERIC(14,3)` in the column,
**never float64**. A WMS adds and subtracts these thousands of times a day, and
binary floating point accumulates error on every operation — the discrepancy
surfaces as a capacity check that passes when it should fail. Verified live:
`1234.567` and `0.001` survive a database round trip exactly.

The request DTO carries them as **strings** for the same reason: JSON numbers are
IEEE 754 doubles in every mainstream parser, so a float field would round before
the server ever validated.

---

## 4. Lifecycle

```
                    ┌────────────────────────────┐
                    ▼                            │
   [new] ──────► ACTIVE ──Deactivate──► INACTIVE─┘
                 │  ▲                        │
                 │  └────────Activate────────┘
                 │  ▲
   StartMaintenance  │Activate
                 ▼  │
             MAINTENANCE
                 │
                 │ Lock(reason)          ┌── Lock(reason) from any live status
                 ▼                       ▼
              LOCKED ◄────────────────────
                 │
                 │ Unlock  (the ONLY exit)
                 ▼
              ACTIVE

   Any live status ──Archive──► archived (deleted_at set, terminal)
```

| From | Operation | To | Notes |
| --- | --- | --- | --- |
| INACTIVE, MAINTENANCE | `Activate` | ACTIVE | Idempotent from ACTIVE. |
| ACTIVE, MAINTENANCE | `Deactivate` | INACTIVE | |
| ACTIVE, INACTIVE | `StartMaintenance` | MAINTENANCE | |
| any live | `Lock(reason)` | LOCKED | Reason required. |
| LOCKED | `Unlock` | ACTIVE | The only exit from LOCKED. |
| LOCKED | anything else | ✗ | **Refused** — see below. |
| any live | `Archive` | archived | Terminal, soft. |
| archived | anything | ✗ | Immutable. |

### LOCKED has exactly one exit

`Activate`, `Deactivate` and `StartMaintenance` all refuse on a LOCKED location.

A lock is an operational hold *with a reason* — damaged racking, a spill, a stock
discrepancy. Letting a routine reactivation clear it would silently discard that
reason, and the person lifting a hold is usually not the person who imposed it.
Unlocking must be a deliberate "the problem is resolved".

Confirmed live: all three return 409 on a locked location.

### Receiving and picking differ

| Status | `CanReceiveInventory` | `CanPickInventory` |
| --- | :---: | :---: |
| ACTIVE | ✅ | ✅ |
| INACTIVE | ✗ | ✗ |
| LOCKED | ✗ | ✗ |
| **MAINTENANCE** | **✗** | **✅** |

A MAINTENANCE location may still be picked **from**. Work is scheduled on a rack
precisely so its remaining stock can be drained before it starts, and blocking
picks would strand that stock. A LOCKED location refuses both — a lock means
"nobody touches this".

This is why the API reports `can_receive` and `can_pick` separately rather than
letting a client derive them from `status`: a client computing them would get
this case wrong.

---

## 5. Business rules

**Location code is unique within a WAREHOUSE**, not within a company. Aisle
numbering restarts at every building, so two sites both having `A-01-01-01` is
normal — company-wide uniqueness would force operators to prefix every label
with a site code they can already see.

**Barcode is unique within a COMPANY**, not within a warehouse. A scanner reads
a label with no idea which site it is standing in, and a duplicate across two
warehouses would make the scan ambiguous exactly when it matters.

Both verified in SQL and over HTTP: the same code is accepted in a second
warehouse, the same barcode is rejected across warehouses and accepted across
companies.

**Capacity cannot be reduced below current usage.** A bin holding 400 kg cannot
be re-declared as a 300 kg bin — the stock is physically there, and the system
would immediately report an overflow it cannot resolve. See §6.

`allow_overflow` does **not** exempt a reduction. Overflow permits putaway to
exceed a limit in the moment; it does not make an already-exceeded limit
acceptable to declare.

**Mixed-SKU can only be disabled when at most one SKU is stored.** Narrowing a
rule while it is already violated would leave the location permanently
non-compliant with no way for the system to say so. Enabling widens and needs no
check at all — which is why the provider is consulted only on the disable path.

**A location is never hard-deleted.** Archiving is soft; future stock movements
will reference it forever, and erasing the row would orphan the history of what
was stored where.

---

## 6. Capacity and the "pass the fact in" pattern

This is the most important design decision in the module.

```go
// The service fetches the fact.
usage, err := s.capacity.CurrentUsage(ctx, companyID, locationID)
// The aggregate applies the rule.
err = location.ChangeCapacity(capacity, usage, actorID, now)
```

The aggregate **cannot** see how full a location is — stock is another aggregate,
and one that loaded another would collapse the consistency boundary. But the
rule is unmistakably domain logic and belongs nowhere else.

So the **rule stays in the domain and only the FACT comes from outside**. The
service gathers, the aggregate decides.

Two consequences worth naming:

- The rule is testable with **no infrastructure**. The aggregate's tests supply
  usage directly; there is no fake repository, no database, no HTTP.
- The Inventory sprint changes the *provider*, not the *rule*. Behaviour is
  substituted without a line of the domain changing —
  `TestCapacityBlockedByCurrentUsage` proves it by injecting a stub that reports
  400 kg stored.

The usage is fetched **inside the transaction**, so the check and the write are
consistent — reading it outside would leave a window in which stock arrives
between them.

The same pattern applies to `DisableMixedSKU`, which takes a `distinctSKUs`
count.

---

## 7. Extension points

Five interfaces are declared and **none is implemented** by this sprint.

| Interface | Answers | Implemented by | Default |
| --- | --- | --- | --- |
| `CurrentCapacityProvider` | How full is this location? | Inventory | `EmptyCapacity` |
| `InventoryProvider` | How many SKUs? Is it empty? | Inventory | `EmptyInventory` |
| `ReceivingGuard` | May stock be put away here? | Receiving | `AllowAllReceiving` |
| `PickingGuard` | May stock be taken from here? | Picking | `AllowAllPicking` |
| `CycleCountGuard` | May it be counted? | Cycle Count | `AllowAllCycleCount` |

Every default is a **named type, never nil**. A nil guard would make "no guard
configured" and "the guard permits it" indistinguishable, so a wiring mistake
would silently disable a safety check.

`EmptyCapacity` and `EmptyInventory` report nothing stored, which is *truthful*
rather than permissive: with no stock module, nothing is stored anywhere.

### Why receiving, picking and counting are three interfaces

They have genuinely different rules, and merging them would erase distinctions
that matter:

- A **MAINTENANCE** location may be picked from but not received into. One
  `CanUse` would either strand stock or allow putaway into a rack about to be
  dismantled.
- A location is often **LOCKED in order to count it**, so a count guard reusing
  the receiving rules would refuse exactly when a count is most needed.
  `CanCount` therefore does not consult the availability predicates at all.

### Composition

`CanReceive` composes the two halves — the aggregate answers the local question,
the guard the cross-aggregate one:

```go
if !location.CanReceiveInventory() { return conflict }   // local
return s.receiving.CanReceive(ctx, companyID, locationID) // cross-aggregate
```

The aggregate is asked **first**: there is no point asking a downstream module
about a location nobody may touch.
`TestCanReceiveComposesAggregateAndGuard` asserts the guard is not even reached
for a LOCKED location.

---

## 8. What this sprint closed in Warehouse

The warehouse sprint declared `ZoneVerifier` for its default receiving, shipping
and staging zone ids, and shipped `AcceptAnyZone` because no zone concept
existed. `docs/Warehouse.md` §7 recorded the gap: *"the referential guarantee is
application-level today"*.

Storage locations **are** those zones — a receiving dock is a location — so
`bootstrap/location_adapters.go` implements the interface. Assigning a warehouse
a zone id that is not a live location in the same company now returns 422 with a
clear message instead of being silently accepted.

**The warehouse module was not modified.** It declared the interface; this is an
implementation of it. That is the extension point working exactly as designed,
and it is the first time one of this codebase's extension points has been
closed by a later sprint.

Verified live: a real location id is accepted, a random UUID is rejected, and a
warehouse can now be activated using a location as its zone.

### The remaining gap

The warehouse module's interface passes only a company and a zone id, so the
adapter **cannot** tell whether the location belongs to the warehouse being
configured. A company could point warehouse A's receiving zone at a location in
warehouse B.

Widening the interface means editing the warehouse module, which this sprint was
told not to do. The narrower check is still a large improvement over accepting
any UUID. Closing it fully is a one-line signature change to
`warehouse/service.ZoneVerifier` whenever that module is next open.

---

## 9. Future Inventory integration

When Inventory lands:

1. It implements `CurrentCapacityProvider` — summing on-hand weight, volume and
   pallet positions per location.
2. It implements `InventoryProvider` — `DistinctSKUs` and `IsEmpty`.
3. Bootstrap swaps the two `Empty*` defaults for them.
4. **No file in this module changes.**

Points worth knowing before writing it:

- `DistinctSKUs` returning a **negative** value means "unknown", which the
  aggregate treats as permissive. That is what keeps `DisableMixedSKU` usable
  before the real provider exists.
- `IsEmpty` backs the archive check and should stop at the first row rather than
  counting — it is an existence question, not an aggregate one.
- `CurrentUsage` is called inside the caller's transaction, so it must use the
  transactional handle (`transaction.DB(ctx, fallback)`) or it will read a
  stale snapshot.

Stock itself will be a **third aggregate** referencing `location_id`, not a field
on this one. The same reasoning as §1: stock levels change thousands of times a
day, and coupling them to a location's configuration would serialise every
movement behind a relabelled shelf.

---

## 10. Authorisation

| Route | Permission |
| --- | --- |
| `GET /locations`, `/locations/:id`, `/locations/barcode/:barcode` | `location.read` |
| `POST /locations` | `location.create` |
| `PUT /locations/:id`, `PATCH /:id/capacity`, `/:id/barcode` | `location.update` |
| `PATCH /:id/activate`, `/:id/deactivate`, `/:id/maintenance` | `location.update` |
| `PATCH /:id/lock`, `/:id/unlock`, `DELETE /:id` | `location.lock` |

Defaults: OWNER and **ADMIN** all four, STAFF read only.

ADMIN gets `location.lock`, which differs from `warehouse.delete` — withheld from
ADMIN. Locking a bin because a rack is damaged is a day-to-day decision made by
the person running the floor; archiving an entire site is not. Withholding it
would leave a damaged rack pickable until an owner is available.

Archiving is grouped with locking rather than update because it removes a place
from the layout permanently.

The permission constants are declared in this module and guarded against the
RBAC catalogue by a `_test`-package drift test, for the reason set out in
`Warehouse.md` §8.

---

## 11. Tenant isolation

Two levels of scoping, both mandatory parameters so neither can be forgotten:

- **Company** on every method — `RepositoryConvention` §3.
- **Warehouse** on methods answering a question about a site, because a location
  code is unique within a warehouse rather than within a company.

A location in another company returns **404, never 403** — a 403 would confirm it
exists. Verified over HTTP for read, lock, archive and create-in-another-tenant's
warehouse, and in SQL for reads, lists, barcode lookups and existence checks.

Barcode lookups are company-scoped: scanning another tenant's label resolves to
nothing.

---

## 12. Known gaps

**No optimistic locking.** Still outstanding from the warehouse sprint, and more
pressing here: two concurrent capacity changes could each read 400 kg usage and
both succeed. A `version` column checked on save is the fix, and it is a
platform-level change worth doing once rather than per-module.

**Coordinates cannot be changed.** A location IS its physical place — moving a
bin to a different aisle is retiring one location and creating another, and
pretending otherwise would silently invalidate every historical stock movement
that referenced it. There is no rename or re-coordinate endpoint, deliberately.

**No bulk-create endpoint.** `SaveMany` exists in the repository and is tested,
but no route exposes it. A rack layout import is a real need and deserves its own
request shape with per-row error reporting, which is more than this sprint's
scope.

**Zone is a free string, not an entity.** `Zone` is a coordinate segment
("A", "COLD", "RECV"), not a modelled concept with its own attributes. If zones
acquire behaviour — temperature ranges, access restrictions — they become an
aggregate and this field becomes a reference.

**`picking_priority` has no policy.** It is stored, indexed and sortable, but
nothing computes a pick path yet. That belongs to the Picking sprint.
