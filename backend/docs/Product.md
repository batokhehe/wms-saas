# Product Domain

The Product module is the catalogue: it owns what an article *is* — its identity,
its units of measure, its barcodes, its physical profile and its lifecycle — but
not how much of it is in stock. Stock is the Inventory aggregate, referenced by
id in one direction only.

The domain model (aggregate, value objects, child collections, events) was
reviewed and frozen in a prior sprint. This document records the model **and**
every architectural decision in the infrastructure built around it: persistence,
transport, wiring, and authorisation. The only changes made inside the frozen
`entity` package are the two additive persistence seams in §3, without which the
aggregate could not be stored or loaded at all.

---

## 1. Aggregate overview

`entity.Product` is an **aggregate root** with no exported fields and no setters.
Its status reaches `ACTIVE` only through `Activate()`, its tracking method changes
only through `SetTracking()`, and it always carries exactly one primary barcode
when it has any. These are guarantees, not hopes: no caller can reach the fields
that would break them, and the persistence layer is subject to the same
encapsulation as every other caller.

The aggregate boundary is the whole product **plus** its two child collections
(barcodes and alternate units). They are loaded and saved together, in one
transaction, and share a single optimistic-lock version on the parent row.
Categories, brands, units of measure and inventory are **other** aggregates,
referenced by id only.

Module layout (mirrors `warehouse` and `location`):

```
entity/      aggregate, value objects, child collections, events, permission codes
repository/  GORM models + translation + queries  (the only package importing gorm)
service/     orchestration, specifications, verifiers, event publisher
dto/         transport contracts
mapper/      aggregate → DTO
handler/     HTTP binding
route/       URL layout + per-route permissions
module.go    vertical-slice assembly
```

---

## 2. Domain model

| Field | Type | Notes |
|---|---|---|
| `id` | `uuid.UUID` | repository-assigned |
| `companyID` | `uuid.UUID` | tenant owner |
| `version` | `uint64` | optimistic-lock token, persistence-owned |
| `sku` | `SKU` | unique per company |
| `name` | `ProductName` | unique per company |
| `description` | `string` | free text |
| `categoryID` | `*uuid.UUID` | optional, another aggregate |
| `brandID` | `*uuid.UUID` | optional, another aggregate |
| `baseUOMID` | `uuid.UUID` | required base unit, factor 1 |
| `status` | `Status` | DRAFT / ACTIVE / DISCONTINUED |
| `tracking` | `TrackingMethod` | NONE / LOT / SERIAL |
| `shelfLife` | `ShelfLife` | defined vs undefined |
| `weight` | `*Weight` | optional |
| `dimension` | `*Dimension` | optional |
| `volume` | `*Volume` | optional |
| `barcodes` | `[]ProductBarcode` | child collection |
| `uoms` | `[]ProductUOM` | child collection |
| `createdBy/updatedBy` | `uuid.UUID` | audit |
| `createdAt/updatedAt` | `time.Time` | audit |

**Factory vs. reconstitution.** `NewProduct` builds a product in DRAFT, tracking
NONE, with the base unit provisioned at factor 1, and raises `product.created`.
`Reconstitute` restores stored state *without* raising events and re-validates the
whole persisted aggregate — loading a row is not a business event.

---

## 3. Child entities

The aggregate owns two child collections. Each element is an immutable value with
unexported fields, exposed through getters.

**`ProductBarcode`** — `{ barcode Barcode, primary bool }`
- Exactly one is primary whenever the collection is non-empty.
- The first barcode added is forced primary regardless of the flag.
- Adding a new primary demotes the previous; removing the primary is refused while
  others remain.

**`ProductUOM`** — `{ uomID uuid.UUID, factor ConversionFactor }`
- The base unit is always present with factor `1` and cannot be removed.
- Every alternate unit appears at most once, with a positive exact factor.

### The two persistence seams (only changes to the frozen `entity` package)

The frozen model could not be persisted as delivered; both gaps were genuine bugs
closed with additive, invariant-neutral code.

1. **`ReconstituteBarcode` / `ReconstituteUOM`.** `Reconstitute` takes
   `[]ProductBarcode` / `[]ProductUOM`, but those types have unexported fields and
   no exported constructor — so no repository (which must live outside `entity`, §8)
   could build them. The public factory was dead code the moment persistence was
   attempted. These constructors wrap the struct literal and do no validation of
   their own; `Reconstitute.validate()` remains the single place that vets an
   assembled aggregate. They are not a mutation path — `AddBarcode`/`AddUOM` still
   enforce every rule on a live aggregate.

2. **Read accessors.** `toModel` must read every stored field, but the aggregate
   exposed no getters for `description`, `baseUOMID`, `categoryID`, `brandID`, or the
   four audit columns — they were writable only. The added getters are pure reads;
   `CategoryID`/`BrandID` return copies so a caller cannot mutate the aggregate's
   pointer.

---

## 4. Value Objects

All are immutable, constructor-validated, value-receiver types. No `float64`
anywhere: every decimal is an exact `math/big.Rat` rendered as its canonical
rational string.

| Value Object | Rule | Canonical form |
|---|---|---|
| `SKU` | 1–64 chars, trimmed, upper-cased | `"SKU-1"` |
| `ProductName` | 2–255 chars, trimmed | as entered |
| `Barcode` | 1–64 chars, trimmed | as entered |
| `Decimal` | any valid rational; rejects NaN/Inf/garbage | `"1/3"`, `"333/1000"` |
| `ConversionFactor` | positive `Decimal` | exact rational |
| `Length` | positive, centimetres | exact rational |
| `Weight` | positive, kilograms | exact rational |
| `Volume` | positive, cubic metres | exact rational |
| `Dimension` | width, height, length all positive | composed of three `Length` |
| `ShelfLife` | days ≥ 0; distinguishes **defined** from **undefined** | `{days, defined}` |
| `TrackingMethod` | `NONE` \| `LOT` \| `SERIAL` | enum |
| `Status` | `DRAFT` \| `ACTIVE` \| `DISCONTINUED` | enum |

**Canonical units are a platform contract:** length is centimetres, mass
kilograms, volume cubic metres. A future UOM module converts input to these before
Product is called. **A defined zero-day shelf life** (a product that expires on
manufacture) is deliberately distinct from an undefined one.

---

## 5. State transitions

```
          Activate                     Discontinue
  DRAFT ─────────────▶ ACTIVE ──────────────────────▶ DISCONTINUED  (terminal)
    ▲                    │                                  ▲
    └──── Deactivate ────┘                                  │
    └───────────────────────── Discontinue ────────────────┘
```

| From | Operation | To | Notes |
|---|---|---|---|
| DRAFT | Activate | ACTIVE | idempotent if already ACTIVE |
| ACTIVE | Deactivate | DRAFT | idempotent if already DRAFT |
| DRAFT / ACTIVE | Discontinue | DISCONTINUED | second call → `CONFLICT` |
| DISCONTINUED | Activate / Deactivate | — | `CONFLICT` (terminal) |

`DISCONTINUED` is terminal — there is no path out, which is why the schema models
retirement as a status rather than a soft delete. Tracking-method changes are
independent of lifecycle but are refused while inventory exists (§7 InventoryProvider).

---

## 6. Domain events

Events are raised by the **aggregate**, not the service. The service publishes what
it pulls from `PullEvents()` (which clears on read), so an event exists because a
transition happened. `LogEventPublisher` writes them to the structured audit log;
each attribute is prefixed `event_` to avoid colliding with the request logger's
fields.

| Event | Raised by | Key attributes |
|---|---|---|
| `product.created` | NewProduct | sku, name, base_uom_id, status, tracking |
| `product.renamed` | Rename | old, new |
| `product.activated` | Activate | from, to |
| `product.deactivated` | Deactivate | from, to |
| `product.discontinued` | Discontinue | from, to |
| `product.barcode_added` | AddBarcode | barcode, primary |
| `product.barcode_removed` | RemoveBarcode | barcode, primary |
| `product.primary_barcode_changed` | SetPrimaryBarcode | old, new |
| `product.uom_added` | AddUOM | uom_id, factor |
| `product.uom_removed` | RemoveUOM | uom_id, factor |
| `product.tracking_method_changed` | SetTracking | old, new |
| `product.measurements_changed` | SetMeasurements | has_weight, has_dimension, has_volume |
| `product.shelf_life_changed` | SetShelfLife | old_defined, old_days, new_defined, new_days |
| `product.category_assigned` | AssignCategory | old, new |
| `product.brand_assigned` | AssignBrand | old, new |

---

## 7. Repository responsibilities

The repository is the only package importing GORM. It translates between the
aggregate and three persistence models (parent + two child tables) and owns:

- **Save** — insert parent, then children, in one transaction.
- **Update** — `UpdateOptimistic` the parent FIRST (version check), then
  delete-and-reinsert the children. A lost version check returns before any child
  row is touched, so a losing write never partially mutates the children.
- **FindByID / FindBySKU** — load parent, batch-load children, reconstitute.
- **List** — page the parents, then batch-load all children for the page in one
  query pair keyed by `product_id IN (…)` (no N+1).
- **Specification queries** — `ExistsBySKU`, `ExistsByName(Excluding)`,
  `ExistsByBarcode(Excluding)`, backing §8.

The repository holds its own `transaction.Manager`; when the service is already in
a transaction, the manager joins it via `SAVEPOINT` rather than opening a second,
so a product and its children are one atomic unit either way. Children are
**hard**-deleted on replace (they carry no soft-delete column — the aggregate and
its events are the audit trail).

---

## 8. Specifications

`UniqueSKU`, `UniqueProductName` and `UniqueBarcode` are first-class rule objects
over the repository, each returning a typed `CONFLICT`. They produce the clear,
early message for the common case; a database `UNIQUE` index is the race-proof
backstop for two requests that pass the check concurrently and then both insert.
`UniqueProductName` and `UniqueBarcode` take an exclude-id so renaming a product to
its own name — or re-persisting its own barcode — is not a false conflict.

| Specification | Repository query | DB backstop |
|---|---|---|
| UniqueSKU | ExistsBySKU | `ux_products_company_sku` |
| UniqueProductName | ExistsByName(Excluding) | `ux_products_company_name` |
| UniqueBarcode | ExistsByBarcode(Excluding) | `ux_product_barcodes_company_barcode` |

---

## 9. Extension points (verifiers)

Four interfaces the module declares and bootstrap injects. Each answers a question
about **another aggregate** that the product cannot see, and each defaults to a
**named** permissive type (never `nil`, so an unwired verifier is not mistaken for
a permissive one):

| Interface | Question | Default | Real implementer |
|---|---|---|---|
| `CategoryVerifier` | does this category exist? | `AcceptAnyCategory` | Category sprint |
| `BrandVerifier` | does this brand exist? | `AcceptAnyBrand` | Brand sprint |
| `UOMVerifier` | does this unit exist? | `AcceptAnyUOM` | UOM sprint |
| `InventoryProvider` | is there stock for this product? | `NoInventory` (reports false) | Inventory sprint |

`InventoryProvider` is the fact behind the tracking rule: `SetTracking` refuses to
change the method while stock exists, but that fact is Inventory's to answer — the
service fetches it and passes it *into* the domain method, so the rule stays in the
aggregate. When a real module ships, its adapter is wired in `bootstrap` and no
product file changes.

---

## 10. REST API

Mounted at `/api/v1/products`. Every route runs
`Authenticate → ResolveCompany → RequireCompany → LoadPermissions → RequirePermission`.

| Method | Path | Permission | Operation |
|---|---|---|---|
| GET | `/products` | product.read | list (paged, filter by status/tracking, search sku/name) |
| POST | `/products` | product.create | create (DRAFT) |
| GET | `/products/:id` | product.read | get |
| PUT | `/products/:id` | product.update | rename / description |
| PATCH | `/products/:id/category` | product.update | assign / clear category |
| PATCH | `/products/:id/brand` | product.update | assign / clear brand |
| PATCH | `/products/:id/measurements` | product.update | set physical profile |
| PATCH | `/products/:id/shelf-life` | product.update | set / clear shelf life |
| PATCH | `/products/:id/tracking` | product.update | set tracking method |
| POST | `/products/:id/barcodes` | product.update | add barcode |
| PATCH | `/products/:id/barcodes/primary` | product.update | set primary barcode |
| DELETE | `/products/:id/barcodes/:barcode` | product.update | remove barcode |
| POST | `/products/:id/uoms` | product.update | add alternate unit |
| DELETE | `/products/:id/uoms/:uomId` | product.update | remove alternate unit |
| PATCH | `/products/:id/activate` | product.activate | DRAFT → ACTIVE |
| PATCH | `/products/:id/deactivate` | product.activate | ACTIVE → DRAFT |
| PATCH | `/products/:id/discontinue` | product.discontinue | → DISCONTINUED (terminal) |

All responses use the platform envelope `{success, message, data, meta}`. Decimal
values (weight, volume, dimensions, conversion factor) cross the wire as **strings**,
not JSON numbers, to preserve exactness.

---

## 11. RBAC permissions

Five codes, declared in `entity/permission.go`, enforced in `route/route.go`,
catalogued in `rbac/entity`, seeded by migration `20260728100001`.

| Code | Guards |
|---|---|
| `product.read` | viewing |
| `product.create` | registering |
| `product.update` | all attribute edits, barcodes, alternate units, tracking |
| `product.activate` | DRAFT ↔ ACTIVE |
| `product.discontinue` | permanent retirement (terminal) |

**Default grants:** OWNER all five; ADMIN `read/create/update/activate`; STAFF
`read`. `product.discontinue` is withheld from ADMIN because retiring an article is
an ownership decision, mirroring `warehouse.delete`.

The codes are declared twice (product module + RBAC catalogue) because
`ModuleConvention §6` forbids importing another module's `entity`. The coupling is
guarded by a `_test`-package drift test that fails the suite if the two lists — or
the seed migration — diverge. The seed migration also **backfills** existing
companies' system roles, because the RBAC provisioner never repairs an existing
role.

---

## 12. Database schema

Migration `20260728100000`. Three tables.

**`products`** — parent row.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | |
| company_id | UUID NOT NULL | FK companies ON DELETE CASCADE |
| sku | CITEXT NOT NULL | |
| name | CITEXT NOT NULL | |
| description | TEXT NOT NULL DEFAULT '' | |
| category_id / brand_id | UUID NULL | no FK yet (future aggregates) |
| base_uom_id | UUID NOT NULL | no FK yet |
| status | VARCHAR(16) NOT NULL DEFAULT 'DRAFT' | CHECK DRAFT/ACTIVE/DISCONTINUED |
| tracking | VARCHAR(16) NOT NULL DEFAULT 'NONE' | CHECK NONE/LOT/SERIAL |
| shelf_life_days | INTEGER NULL | NULL = undefined; CHECK ≥ 0 |
| weight_kg, volume_m3, dim_width/height/length_cm | TEXT NULL | exact rational strings |
| version | BIGINT NOT NULL DEFAULT 1 | optimistic lock |
| created_by / updated_by | UUID NOT NULL | FK users |
| created_at / updated_at | TIMESTAMPTZ NOT NULL | |
| deleted_at | TIMESTAMPTZ NULL | soft-delete marker (unused today) |

Indexes: `ux_products_company_sku`, `ux_products_company_name` (partial on
`deleted_at IS NULL`), `idx_products_company_status`.

**`product_barcodes`** — id, product_id (FK CASCADE), company_id (denormalised for
scoping), barcode CITEXT, is_primary, timestamps.
Index: `ux_product_barcodes_company_barcode` (UNIQUE — the scanner-correctness
guarantee), `idx_product_barcodes_product`.

**`product_uoms`** — id, product_id (FK CASCADE), company_id, uom_id, factor TEXT,
is_base, timestamps.
Index: `ux_product_uoms_product_uom` (UNIQUE), `idx_product_uoms_product`.

**Why TEXT, not NUMERIC**, for measurements and factor: the domain stores exact
rationals and forbids `float64`; `NUMERIC(p,s)` would round `"1/3"` and every
quantity derived from it would be wrong. **Why child tables, not JSONB**: only a
real UNIQUE index gives barcode uniqueness a race-proof guarantee, which a JSONB
array cannot carry.

---

## 13. Optimistic locking

`Version` is owned entirely by the persistence layer. The aggregate exposes it
read-only; no business method changes it (the invariant suite asserts this). The
repository advances it through a conditional `UPDATE … WHERE id = ? AND version = ?`.
Zero rows affected ⇒ another writer won ⇒ `ErrConcurrentModification`, which the
service translates into a `409 CONFLICT` carrying the current version as the retry
token. The whole aggregate (parent + children) shares one version, so a concurrent
edit to any part is detected.

---

## 14. Concurrency guarantees

- **Read-modify-write is atomic.** Every mutation loads, mutates and persists inside
  one transaction, so two concurrent updates cannot interleave.
- **Exactly one writer wins a race.** Proven by
  `TestConcurrentUpdateAllowsExactlyOneWriter`: two goroutines race a stale write;
  one succeeds, one gets `ErrConcurrentModification`, and the row lands at version 2.
- **Children never partially mutate.** The version check precedes any child write; a
  losing writer touches no child row.
- **Uniqueness survives races.** The specification check is advisory; the database
  UNIQUE indexes are the backstop when two requests pass the check concurrently.

---

## 15. Multi-tenant guarantees

- Every repository method takes a `companyID` and applies `ForCompany` — forgetting
  it does not compile.
- A product in another company is `NOT_FOUND`, never `FORBIDDEN` (a 403 would confirm
  it exists).
- Child tables carry a denormalised `company_id`, so the barcode uniqueness index and
  `ExistsByBarcode` are company-scoped without a join.
- The service adds a defence-in-depth `companyID` check after every load.
- The same SKU or barcode is legal across two companies but never within one.
  Integration tests prove read, list and existence checks are all isolated.

---

## 16. Validation rules

Enforced in three layers, outermost first:

1. **DTO binding** (`dto/product.go`): `required`, length bounds (`sku` 1–64,
   `name` 2–255, `barcode` 1–64), enum `oneof` for status/tracking/method,
   `min=0` for shelf-life days, UUID format for path/body ids. Dimension requires all
   three sides together.
2. **Value-object constructors** (`entity/value_objects.go`): the canonical rules in
   §4 — positivity, unit canonicalisation, NaN/Inf rejection, shelf-life
   defined/undefined.
3. **Aggregate invariants** (`entity/product.go`): exactly one primary barcode, base
   UOM present with factor 1, unique barcodes/units within a product, no tracking
   change while inventory exists, terminal DISCONTINUED, no partial mutation on error.

Cross-aggregate existence (category, brand, unit) is checked by **verifiers** (§9);
cross-row uniqueness (SKU, name, barcode) by **specifications** (§8).

---

## 17. Error catalogue

All errors are `apperror.*`, carrying a machine code the client branches on. The
message is client-safe; the cause is logged, never serialised.

| Code | HTTP | Raised when |
|---|---|---|
| `VALIDATION_ERROR` | 422 | invalid VO input (bad decimal, non-positive measurement, negative shelf life, malformed uuid), missing base unit, unknown category/brand/unit (rejecting verifier) |
| `CONFLICT` | 409 | duplicate SKU / name / barcode; discontinuing an already-discontinued product; changing tracking while inventory exists; concurrent modification |
| `NOT_FOUND` | 404 | product not in this company; removing an unknown barcode / unit |
| `FORBIDDEN` | 403 | no company context; defence-in-depth cross-tenant write |
| `UNAUTHORIZED` | 401 | no authenticated principal |
| `INTERNAL_ERROR` | 500 | unexpected persistence/driver failure (cause logged, generic message returned) |

---

## 18. Known gaps

1. **No foreign keys on `category_id`, `brand_id`, `base_uom_id`** — those aggregates
   do not exist yet; the guarantee is application-level via the permissive verifiers.
   A product can currently reference a base unit that does not exist. Closed when the
   UOM/Category/Brand sprints wire real verifiers — no product file changes.
2. **Child collections are unbounded** — nothing caps how many barcodes or units a
   product may hold.
3. **`ChangeDescription` raises no event** — the one mutator without an audit event,
   inherited from the frozen model.
4. **Events are `map[string]any`** — fact completeness is enforced by tests, not the
   compiler; the model's existing trade-off.
