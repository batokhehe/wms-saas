# Warehouse Domain

The first business domain, and the first built with Domain-Driven Design rather
than as a CRUD module. This document explains what that means concretely and why
each decision was made.

---

## 1. Warehouse is an Aggregate Root

`entity.Warehouse` has **no exported fields and no setters**. Every state change
goes through a method that names a business intent:

```go
w.Activate(actor, now)
w.Suspend(reason, actor, now)
w.ChangeContact(contact, actor, now)
w.AssignReceivingZone(zoneID, actor, now)
w.Archive(actor, now)
```

### Why encapsulation is the whole point

"An ACTIVE warehouse always has an address, a contact and at least one zone" is
only a *guarantee* if there is no way to reach ACTIVE except through
`Activate()`. Exported fields make it advisory: any caller writes
`w.Status = StatusActive` and every rule in the file becomes a suggestion.

This is why the module departs from `EntityConvention` — which says entities
embed `shared/entity.BaseEntity` and carry GORM tags. That convention governs
**persistence types**, and the aggregate is not one.

### The cost, and where it is paid

GORM maps by reflecting over exported fields, so it cannot touch the aggregate
at all. `repository/model.go` holds a separate `warehouseModel` that embeds
`BaseEntity`, carries the tags, and translates both ways.

Two alternatives were rejected:

- **Export the aggregate's fields.** Deletes the encapsulation the sprint exists
  to establish.
- **Implement `database/sql` interfaces on the aggregate.** Drags persistence
  into the innermost layer and still needs exported fields for column mapping.

The translation is hand-written, which is exactly the kind of code where a
forgotten field causes silent data loss. `TestAggregateSurvivesRoundTrip` asserts
every field survives.

### Value objects

`Code`, `Address`, `Contact` and `Zones` are value objects, not string fields.
An invalid `Code` cannot exist — `NewCode` is the only way to obtain one and it
validates.

`Contact` bundles name and phone because they are meaningless apart: a number
with nobody attached cannot be acted on, a name with no number cannot be
reached. Two independent string fields would permit exactly those half-states,
so `NewContact` rejects them.

Value objects are immutable. `Zones.Assign` returns a copy, so a caller holding
one cannot reach back into the aggregate — verified by
`TestZonesValueObjectIsImmutable`.

### Factory vs. reconstitution

| | Raises events | Validates | Used by |
| --- | --- | --- | --- |
| `NewWarehouse` | yes | yes | the service, on create |
| `Reconstitute` | **no** | no | the repository, on load |

The distinction is fundamental and easy to get wrong. Loading a row is not a
business event: if reconstitution raised `WarehouseCreated`, an audit log would
claim the warehouse was created once per page view.

`Reconstitute` also does not validate — the data came from the database, which
already enforced the constraints. Validating would mean a row that became
invalid through a migration could never be loaded, not even to be repaired.

---

## 2. Where the rules live

| Question | Answered by | Why |
| --- | --- | --- |
| Is this warehouse ready to activate? | **aggregate** | It can see everything it needs. |
| May an archived warehouse be edited? | **aggregate** | Same. |
| Is this code already taken? | **service** | A question about a SET; only the repository can see siblings. |
| Does this warehouse hold stock? | **DeletionGuard** | Another aggregate entirely. |
| Does this zone exist? | **ZoneVerifier** | Another aggregate entirely. |
| May this user do it at all? | **middleware** | Authorisation, declared per route. |

The test for whether a rule belongs in the aggregate: *can it be answered by
looking only at this object?* If yes, it goes in — and the service must not
duplicate it.

Read `service/service.go` and note what is absent: no `if status ==`, no
readiness check, no state machine. Every method is the same shape:

```
resolve tenant and actor → load aggregate → call ONE domain method
  → persist → publish what the aggregate recorded
```

---

## 3. Business rules

**Code and name are BOTH unique per company.** Name uniqueness is unusual and
deliberate: an operator picks a destination warehouse from a dropdown *by name*,
so two sites called "Jakarta Central" would make mis-shipping a matter of chance
rather than error.

Both are partial unique indexes on `deleted_at IS NULL`, so a business that
closes a site can later open a new one on the same code.

**Uniqueness is per company, not global.** Two businesses both legitimately call
their main site "WH-01" — verified live and in the integration suite.

**Only an ACTIVE warehouse may receive or ship.** Exposed as
`CanReceiveInventory()` / `CanShipInventory()` rather than a status comparison
callers perform. When "receiving requires a receiving zone" becomes true, it
changes in one place and every caller inherits it. The API returns
`can_receive` / `can_ship` for the same reason — a client deriving them from the
status would be reimplementing a business rule.

**A warehouse is never deleted.** See §5.

---

## 4. Lifecycle and state transitions

```
                    ┌──────────────────────────────┐
                    ▼                              │
   [new] ──────► DRAFT ──Activate*──► ACTIVE ──Deactivate──► INACTIVE
                   │                    │                       │
                   │                    │                       │
                   └────────┬───────────┴───────────────────────┘
                            │ Suspend(reason)
                            ▼
                        SUSPENDED
                            │
                            │ Activate*
                            ▼
                         ACTIVE

   Any live status ──Archive──► archived (deleted_at set, terminal)

   * Activate enforces the readiness rules below.
```

| From | Operation | To | Notes |
| --- | --- | --- | --- |
| DRAFT | `Activate` | ACTIVE | Readiness enforced. |
| ACTIVE | `Deactivate` | INACTIVE | Only from ACTIVE. |
| INACTIVE | `Activate` | ACTIVE | Readiness re-enforced. |
| any live | `Suspend` | SUSPENDED | Reason required. |
| SUSPENDED | `Activate` | ACTIVE | Lifting the hold. |
| SUSPENDED | `Deactivate` | ✗ | **Refused** — see below. |
| any live | `Archive` | archived | Terminal. |
| archived | anything | ✗ | Immutable. |

### Activation requirements

Activation declares a site fit to receive and ship stock, so it demands the
facts an operator actually needs:

- a **name** (always true — enforced at construction)
- an **address**, so a driver can reach it
- a **contact**, so someone can be called when a delivery goes wrong
- **at least one operational zone**, so arriving goods have somewhere to go

Every missing requirement is reported together, not one per rejection — an
operator completing a warehouse should see the whole checklist. Confirmed live:
a bare DRAFT returns 422 listing address, contact and zones in one response.

"At least one" rather than all three, because the requirement differs by type: a
TRANSIT cross-dock legitimately has no staging area, and a CONSIGNMENT site may
only ever receive. Demanding all three would make those unactivatable.

### Why SUSPENDED cannot be deactivated

Suspension is a governance hold — a failed audit, a fire inspection, a
compliance block. Deactivating it would silently lift the hold and turn it into
an operational state, discarding the reason it was imposed. Lifting a suspension
must be an explicit `Activate`, which re-checks readiness.

### Why activation is idempotent

A retried request, or two operators pressing the button at once, is not a
business failure. Returning a conflict would make clients implement compensating
logic for a no-op.

### Why an ACTIVE warehouse cannot lose its address or contact

Activation required them. Permitting their removal afterwards would leave an
operational site no driver can reach — an invariant broken through the back
door. `ChangeAddress` and `ChangeContact` refuse it on ACTIVE warehouses and
allow it on DRAFTs still being filled in.

---

## 5. Deletion: archive only

**A warehouse is never hard-deleted.** Future stock movements, receipts and
shipments will reference it forever, and erasing the row would orphan years of
operational history an audit depends on.

`DELETE /warehouses/:id` and `PATCH /warehouses/:id/archive` call the **same**
service method. There are not two behaviours — DELETE is the REST-conventional
alias clients expect, and sharing one path means they cannot diverge. Both
require `warehouse.delete`.

An archived warehouse is **immutable**: every mutation returns CONFLICT. It is a
historical record, and permitting even a description edit would mean the record
of what the site was at retirement is not stable.

### The CanDelete extension point

Two checks compose:

```go
// 1. cross-aggregate — service.DeletionGuard
if err := s.deletion.CanDelete(ctx, companyID, warehouseID); err != nil {
    return err
}
// 2. the aggregate's own invariant
if err := warehouse.Archive(actorID, now); err != nil {
    return err
}
```

`entity.Warehouse.CanArchive` answers what the aggregate can see — "am I already
archived?". It **cannot** answer "do I hold stock?", because stock is a different
aggregate and one that loaded another would collapse the boundary that makes
each independently consistent.

The guard runs **first**. Asking it second would mean archiving a warehouse and
then discovering it should not have been.

`AlwaysAllowDeletion` is in force today. The Inventory sprint implements the
interface — "return CONFLICT when on-hand quantity is non-zero" — and bootstrap
injects it. **No file in this module changes.** `TestDeletionGuardBlocksArchive`
proves that by substituting a blocking guard.

It is a named type rather than a nil check, so an unwired guard cannot be
mistaken for a permissive one.

---

## 6. Domain events

| Event | Raised by |
| --- | --- |
| `warehouse.created` | `NewWarehouse` |
| `warehouse.activated` | `Activate` |
| `warehouse.suspended` | `Suspend` (carries the reason) |
| `warehouse.archived` | `Archive` |
| `warehouse.contact_changed` | `ChangeContact` |
| `warehouse.zone_assigned` | `Assign*Zone` |

**Events are raised by the AGGREGATE, not the service.** That is the difference
between an event stream recording domain facts and one recording what a service
happened to remember to log: the aggregate knows a transition occurred because
it performed it, and it cannot forget.

`PullEvents()` clears as it reads, so a second save cannot republish. The
service only forwards what it pulls and never constructs an event itself.

Deliberate asymmetries:

- **Renames raise nothing.** Cosmetic; nobody must react.
- **Contact changes do.** The contact is who gets called when a delivery goes
  wrong, so a silent change is an operational risk that notification routing
  needs to know about.
- **No-op changes raise nothing.** Resending an unchanged value must not pollute
  the audit stream.

The contact event records the previous **name** but not the previous phone
number — an audit reader needs to know who was replaced, and a phone number is
personal data that does not belong in an event stream. Verified live: zero phone
numbers in the audit log.

---

## 7. Future Location / Zone integration

`DefaultReceivingZoneID`, `DefaultShippingZoneID` and `DefaultStagingZoneID` are
nullable UUIDs with **no foreign key**, because the zones table does not exist.
Inventing a placeholder would bake in a shape the Location sprint has not chosen.

**The referential guarantee is therefore application-level today.** That is
stated here rather than left to be discovered.

The extension point is `service.ZoneVerifier`:

```go
type ZoneVerifier interface {
    VerifyZone(ctx context.Context, companyID, zoneID uuid.UUID) error
}
```

`AcceptAnyZone` is in force. The Location sprint implements it and bootstrap
injects it — again, no warehouse file changes.
`TestZoneVerifierBlocksAssignment` proves it by substituting a rejecting
verifier.

The aggregate validates what it can (well-formed id, known kind) and delegates
existence to the verifier, because a zone is another aggregate.

### What the Location sprint should add

1. A `zones` table with `company_id` and `warehouse_id`.
2. A migration adding the three foreign keys to `warehouses`.
3. A `ZoneVerifier` implementation, injected in bootstrap.
4. Optionally, tightening activation: "a warehouse that receives must have a
   receiving zone" would become a rule inside `Activate`.

Point 4 is why `CanReceiveInventory()` is a method rather than a status
comparison — the rule can gain a condition without any caller changing.

---

## 8. Authorisation

| Route | Permission |
| --- | --- |
| `GET /warehouses`, `GET /warehouses/:id` | `warehouse.read` |
| `POST /warehouses` | `warehouse.create` |
| `PUT /warehouses/:id`, `PATCH /:id/contact` | `warehouse.update` |
| `PATCH /:id/activate`, `PATCH /:id/deactivate` | `warehouse.activate` |
| `PATCH /:id/suspend`, `PATCH /:id/archive`, `DELETE /:id` | `warehouse.delete` |

Defaults: OWNER all five, ADMIN all but `warehouse.delete`, STAFF read only.

**Activation is a separate permission from update.** Editing an address is
routine; declaring a site fit to receive and ship stock is a commissioning
decision. Granting them together would mean anyone who can fix a typo can also
put a half-configured warehouse into operation.

**Suspension requires `warehouse.delete`, not `warehouse.activate`.** It is a
governance hold, not an operational toggle — an operator who can commission a
site should not be able to place it under a compliance block.

### The permission constants are declared twice

`ModuleConvention` §6 forbids a module importing another's `entity` package, so
warehouse declares its own constants rather than importing RBAC's. The coupling
is guarded rather than hidden: `entity/permission_test.go` imports `rbac/entity`
from a `_test` package — a test-only dependency that does not appear in the
production build graph — and fails if the two lists drift.

### The migration backfills existing companies

The RBAC provisioner deliberately never repairs an existing role. Without a
backfill, every company created before this sprint would have an OWNER who
cannot manage warehouses at all. Migration `20260725100001` grants the five new
codes to existing system roles per the default matrix — a one-time correction
tied to introducing a capability, touching nothing an administrator has chosen.

---

## 9. Tenant isolation

Every repository method takes a `companyID` and applies `base.ForCompany` in a
named `forTenant` helper, so a reviewer auditing for missing filters looks for
one identifier rather than an inline `Where`.

A warehouse belonging to another company returns **404, never 403** — a 403
would confirm it exists. Verified over HTTP for read, activate and archive, and
against real SQL in the integration suite.

The service also calls `warehouse.BelongsTo(companyID)` as defence in depth.
That can only fire if the repository filter is ever broken — which is exactly
when a cross-tenant write would otherwise go unnoticed.

---

## 10. Known gaps

**No warehouse-scoped operational permissions.** STAFF can read warehouses but
the permissions for working *inside* one (receiving, picking, shipping) do not
exist, because those modules do not. They join the catalogue with the sprint
that implements them, not speculatively.

**`Type` is descriptive, not behavioural.** Nothing branches on MAIN vs
TRANSIT yet. It is modelled now because it is part of the language operators
already use, and future modules will branch on it — a TRANSIT site holds nothing
overnight, a CONSIGNMENT site holds stock the company does not own.

**No optimistic locking.** Two concurrent updates are serialised by the
transaction, but a lost update is still possible across separate requests: read,
read, write, write. A `version` column checked on save is the fix, and it is a
platform-level change worth doing once rather than per-module.

**Address is a single free-text line.** Structured addresses are seductive and
wrong at this stage — the correct decomposition differs by country, and guessing
now forces a migration when the first non-domestic customer arrives.

**No warehouse-level default currency, timezone or operating hours.** All are
plausible; none has a consumer yet.
