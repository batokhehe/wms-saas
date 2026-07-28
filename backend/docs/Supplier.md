# Supplier Domain

Supplier is MASTER DATA: the vendors a company buys from. It belongs to exactly
one company (tenant) and is a flat, single-row aggregate — no child collections.
This module is built to the same conventions as Warehouse, Product and Inventory;
it introduces no new patterns.

---

## 1. Aggregate

`entity.Supplier` is an aggregate root with no exported fields and no setters. Its
code and name can never be blank, and its status changes only through
Activate/Deactivate.

| Field | Notes |
|---|---|
| ID, CompanyID | identity + tenant |
| Code | `SupplierCode` value object, unique per company, upper-cased |
| Name | required, 2–255 chars |
| Email, Phone, TaxNumber | optional value objects (zero value = unset) |
| Address | one `Address` value object bundling street/city/province/country/postal code |
| Status | `ACTIVE` / `INACTIVE` |
| Version | optimistic-lock token, persistence-owned |
| CreatedBy/At, UpdatedBy/At | audit |

**Value objects:** `SupplierCode`, `Email`, `Phone`, `TaxNumber`, `Address`. Each
validates itself at construction; the optional ones have a zero value meaning
"unset", so a supplier can be onboarded before contact details are known.

**Behaviours:** `Create` (factory → ACTIVE, version 1), `Update` (replaces name +
contact + address; the code is immutable), `Activate`, `Deactivate` (both
idempotent — a no-op emits no event).

---

## 2. Invariants

- **SupplierCode is required and unique per company.** Non-emptiness is the
  aggregate's; uniqueness is a set rule the service enforces via the
  `UniqueSupplierCode` specification, backed by the DB unique index
  `ux_suppliers_company_code`.
- **SupplierName is required** (2–255 chars) — enforced by the aggregate on both
  create and update.
- **Status is ACTIVE or INACTIVE** — a DB CHECK backs the value object.
- **A supplier cannot be deleted while referenced by a purchase order** — a
  cross-aggregate rule referencing a module that does not exist yet. Its
  extension point (`service.DeletionGuard` + `AllowAllDeletion`) is **prepared**
  but consumed by no operation: this sprint has no delete behaviour (a supplier
  is retired by Deactivate). The future Purchase Order sprint implements
  `CanDelete` and adds a delete operation that consults it.

---

## 3. Domain events

Raised by the aggregate, published by the service after commit:
`supplier.created`, `supplier.updated`, `supplier.activated`,
`supplier.deactivated`. `LogEventPublisher` writes them to the audit log with
every field prefixed `event_`.

---

## 4. Application flow

```
resolve tenant + actor → BEGIN tx
   → (create) run UniqueSupplierCode → factory → Save
   → (mutate) load (tenant-scoped) → one domain method → Update (optimistic)
→ COMMIT → publish events
```

Reads (Get, List) resolve the tenant, query, and map — no transaction, no events.

---

## 5. REST API

Mounted at `/api/v1/suppliers`. Every route runs
`Authenticate → ResolveCompany → RequireCompany → LoadPermissions → RequirePermission`.

| Method | Path | Permission | Operation |
|---|---|---|---|
| GET | `/suppliers` | supplier.read | list (filter by status, search code/name) |
| POST | `/suppliers` | supplier.create | register (→ ACTIVE) |
| GET | `/suppliers/:id` | supplier.read | get |
| PUT | `/suppliers/:id` | supplier.update | replace mutable attributes |
| PATCH | `/suppliers/:id/activate` | supplier.activate | → ACTIVE |
| PATCH | `/suppliers/:id/deactivate` | supplier.activate | → INACTIVE |

Update is a full replacement of the editable fields (the code excepted), because
the postal address is one composite value object and a partial update of its
parts is ambiguous.

---

## 6. Permissions

Four codes (`entity/permission.go`), catalogued in `rbac/entity`, seeded by
migration `20260731100001`.

| Code | Guards | OWNER | ADMIN | STAFF |
|---|---|:-:|:-:|:-:|
| supplier.read | viewing | ✓ | ✓ | ✓ |
| supplier.create | register | ✓ | ✓ | — |
| supplier.update | edit attributes | ✓ | ✓ | — |
| supplier.activate | activate / deactivate | ✓ | ✓ | — |

`supplier.activate` is separate from `supplier.update`: editing a phone number is
routine, while deactivating a supplier stops every new purchase order to them.
STAFF gets read only — curating master data is not their job. The seed migration
backfills existing companies' system roles; a `_test`-package drift guard fails
the build if the module's codes and the RBAC catalogue diverge.

---

## 7. Persistence

Migration `20260731100000`. One `suppliers` table: code CITEXT, name, nullable
contact columns (email CITEXT, phone, tax_number), the five denormalised address
columns, status, version, audit, deleted_at. Indexes:
`ux_suppliers_company_code` (partial unique) and `idx_suppliers_company_status`.
FK `company_id → companies` (CASCADE) and `created_by/updated_by → users`. The
`Address` value object maps to five columns; unset contact fields are stored as
NULL and reconstitute as zero value objects.

Repository (`repository.Repository`): Save, Update (optimistic), FindByID,
FindByCode, List, ExistsByCode — every method tenant-scoped, translating to the
aggregate before anything leaves the boundary.

---

## 8. Transaction & concurrency guarantees

Writes run inside one transaction; events publish only after commit, so a
rolled-back change emits nothing. `Update` uses a conditional
`UPDATE … WHERE id = ? AND version = ?`; a lost race yields
`ErrConcurrentModification`, which the service turns into a `409` carrying the
current version. Proven by `TestConcurrentUpdateAllowsExactlyOneWriter`.

---

## 9. Multi-tenant guarantees

Every query is `company_id`-scoped — forgetting it does not compile. A supplier
in another company is `NOT_FOUND`, never `FORBIDDEN`. The service adds a
defence-in-depth `BelongsTo` check after every load. The same code is legal
across two companies but not within one.

---

## 10. Validation & error catalogue

Validation layers: DTO binding (required, length, `email`, `oneof`, uuid) →
value-object constructors → aggregate invariants.

| Code | HTTP | Raised when |
|---|---|---|
| `VALIDATION_ERROR` | 422 | bad code/name/email/phone/address; malformed id |
| `CONFLICT` | 409 | duplicate code; concurrent modification |
| `NOT_FOUND` | 404 | supplier not in this company |
| `FORBIDDEN` | 403 | no company context; defence-in-depth cross-tenant write |
| `UNAUTHORIZED` | 401 | no authenticated principal |
| `INTERNAL_ERROR` | 500 | unexpected persistence failure |

---

## 11. Tests

- **Aggregate unit tests** — factory, reconstitution, update, activate/deactivate
  idempotency, version immutability, value-object validation.
- **Repository integration tests** (live PG) — round trip (incl. NULL contact),
  optimistic locking, tenant isolation, duplicate-code conflict, status CHECK, FK,
  filtering, rollback, permission seed.
- **Service unit tests** — create/update/lifecycle, uniqueness, rollback, auth,
  tenant isolation, concurrent-modification translation.
- **Handler tests** — HTTP boundary: 201/422/404/409 status mapping, envelope
  shape, lifecycle + list over a real gin router.

---

## 12. Known gaps

1. **DeletionGuard is prepared, not consumed** — there is no delete operation
   this sprint; the seam awaits the Purchase Order module.
2. **Address is not structured beyond free text + admin fields** — no country
   code normalisation or postal-code format validation, only length bounds.
