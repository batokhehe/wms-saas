# Customer Domain

Customer is MASTER DATA: the parties a company sells to. It is the **structural
sibling of Supplier** — identical architecture, layering, testing strategy, RBAC
shape, optimistic locking and tenant isolation, with the aggregate name, code
prefix, permission namespace, table, and future-extension target adapted. It
belongs to exactly one company (tenant) and is a flat, single-row aggregate.

For the shared design rationale see `Supplier.md`; this documents only what
differs.

---

## 1. Aggregate

`entity.Customer` is an aggregate root with no exported fields and no setters;
its code and name can never be blank, and its status changes only through
Activate/Deactivate.

| Field | Notes |
|---|---|
| ID, CompanyID | identity + tenant |
| CustomerCode | value object, unique per company, upper-cased |
| CustomerName | required, 2–255 chars |
| Email, Phone, TaxNumber | optional value objects (zero value = unset) |
| Address | one `Address` value object (street/city/province/country/postal code) |
| Status | `ACTIVE` / `INACTIVE` |
| Version | optimistic-lock token, persistence-owned |
| CreatedBy/At, UpdatedBy/At | audit |

**Value objects:** `CustomerCode`, `Email`, `Phone`, `TaxNumber`, `Address`.
**Behaviours:** `Create` (→ ACTIVE, version 1), `Update` (replaces name + contact
+ address; code immutable), `Activate`, `Deactivate` (both idempotent).
**Events:** `customer.created`, `customer.updated`, `customer.activated`,
`customer.deactivated`.

---

## 2. Invariants

- **CustomerCode is required and unique per company** — non-emptiness is the
  aggregate's; uniqueness is the `UniqueCustomerCode` specification, backed by
  `ux_customers_company_code`.
- **CustomerName is required** (2–255 chars).
- **Status is ACTIVE or INACTIVE** — a DB CHECK backs the value object.
- **A customer cannot be deleted while referenced by a sales order** — a
  cross-aggregate rule referencing a module that does not exist yet. Its
  extension point (`service.DeletionGuard` + `AllowAllDeletion`) is **prepared**
  but consumed by no operation: this sprint has no delete behaviour (a customer
  is retired by Deactivate). The future Sales Order sprint implements `CanDelete`
  and adds a delete operation that consults it.

---

## 3. REST API

Mounted at `/api/v1/customers`, behind the full auth/company/permission chain.

| Method | Path | Permission | Operation |
|---|---|---|---|
| GET | `/customers` | customer.read | list (filter by status, search code/name) |
| POST | `/customers` | customer.create | register (→ ACTIVE) |
| GET | `/customers/:id` | customer.read | get |
| PUT | `/customers/:id` | customer.update | replace mutable attributes |
| PATCH | `/customers/:id/activate` | customer.activate | → ACTIVE |
| PATCH | `/customers/:id/deactivate` | customer.activate | → INACTIVE |

---

## 4. Permissions

Four codes, catalogued in `rbac/entity`, seeded by migration `20260801100001`.

| Code | OWNER | ADMIN | STAFF |
|---|:-:|:-:|:-:|
| customer.read | ✓ | ✓ | ✓ |
| customer.create | ✓ | ✓ | — |
| customer.update | ✓ | ✓ | — |
| customer.activate | ✓ | ✓ | — |

`customer.activate` is separate from `customer.update`: deactivating a customer
stops every new sales order to them. STAFF gets read only. The seed migration
backfills existing companies' system roles; a `_test`-package drift guard fails
the build if the module's codes and the RBAC catalogue diverge.

---

## 5. Persistence

Migration `20260801100000`. One `customers` table (code CITEXT, name, nullable
contact columns, five denormalised address columns, status, version, audit,
deleted_at). Indexes `ux_customers_company_code` (partial unique) and
`idx_customers_company_status`. FK `company_id → companies` (CASCADE) and
`created_by/updated_by → users`. Repository: Save, Update (optimistic), FindByID,
FindByCode, List, ExistsByCode — every method tenant-scoped.

---

## 6. Guarantees & error catalogue

Transaction, concurrency (optimistic-lock → 409), multi-tenant isolation
(`NOT_FOUND` cross-tenant, `BelongsTo` defence-in-depth), and validation layering
are identical to Supplier — see `Supplier.md` §8–§10.

| Code | HTTP | Raised when |
|---|---|---|
| `VALIDATION_ERROR` | 422 | bad code/name/email/phone/address; malformed id |
| `CONFLICT` | 409 | duplicate code; concurrent modification |
| `NOT_FOUND` | 404 | customer not in this company |
| `FORBIDDEN` | 403 | no company context; defence-in-depth cross-tenant write |
| `UNAUTHORIZED` | 401 | no authenticated principal |
| `INTERNAL_ERROR` | 500 | unexpected persistence failure |

---

## 7. Tests

Aggregate unit tests, repository integration tests (live PG), service unit tests,
and handler HTTP-boundary tests — the same coverage matrix as Supplier.

---

## 8. Database schema and validation

The `customers` table stores aggregate identity (`id`, `company_id`), immutable
business code and name, nullable contact/tax fields, denormalised address
fields, lifecycle status, optimistic-lock version, audit metadata, and the
standard soft-delete column. `ux_customers_company_code` is a partial unique
index, so a code is unique only within one live company; all repository queries
also scope by company before returning or updating a row.

Validation is layered: JSON binding rejects malformed transport input; value
objects normalise and validate their own values; the aggregate requires a code
and a 2â€“255-character name; and the service's uniqueness specification enforces
the tenant-scoped code rule. Email, phone, tax number, and address are optional
but value-object validated when supplied. Updates require the current version;
a stale version is a `CONFLICT`.

## 9. Known gaps

1. **DeletionGuard is prepared, not consumed** — no delete operation this sprint;
   the seam awaits the Sales Order module.
2. **Address is not structured beyond free text + admin fields** — length bounds
   only, no country-code or postal-format normalisation.
