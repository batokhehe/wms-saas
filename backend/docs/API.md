# API Reference

Endpoint reference. For the rules every endpoint follows — envelope shape,
versioning policy, pagination, headers — see `APIConvention.md`.

Base URL: `/api/v1`

---

## Conventions in one paragraph

Every response uses the same envelope: `success`, `message`, and either `data`
or `error`, plus `meta` carrying `request_id` and `timestamp`. Clients branch on
`error.code`, never on `message` — codes are contractual, messages are not.
Authenticated endpoints take `Authorization: Bearer <access_token>`.

---

## Health

Mounted at the unversioned root **and** under `/api/v1`, because orchestrators
are configured once and must not follow API versioning.

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| GET | `/health/live` | — | Liveness. Checks nothing external. |
| GET | `/health/ready` | — | Readiness. Probes PostgreSQL and Redis. |
| GET | `/health` | — | Alias for readiness. |

`/health/ready` returns 503 when a dependency is down. The per-component
breakdown is written to the logs but suppressed from the response, because those
messages contain internal hostnames.

---

## Authentication

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| POST | `/api/v1/auth/register` | — | Create an account, return tokens. |
| POST | `/api/v1/auth/login` | — | Authenticate, return tokens. |
| POST | `/api/v1/auth/refresh` | — | Rotate refresh token, reissue access. |
| POST | `/api/v1/auth/logout` | — | Revoke a session. |
| GET | `/api/v1/auth/me` | Bearer | Current user's profile. |

Refresh and logout are unauthenticated because a client reaching them has, by
definition, an expired or unwanted access token. Their credential is the refresh
token in the body.

---

### POST /api/v1/auth/register

```json
{
  "email": "ops@example.com",
  "password": "Str0ng!Passw0rd",
  "full_name": "Ops User"
}
```

`201 Created`:

```json
{
  "success": true,
  "message": "Account created successfully",
  "data": {
    "user": {
      "id": "56d45699-b7fe-42b5-837b-17d782d624cc",
      "email": "ops@example.com",
      "full_name": "Ops User",
      "status": "ACTIVE",
      "email_verified": false,
      "created_at": "2026-07-22T09:31:47Z",
      "updated_at": "2026-07-22T09:31:47Z"
    },
    "tokens": {
      "access_token": "eyJhbGciOiJIUzI1NiIs...",
      "refresh_token": "8wGtIkgDVlj...",
      "token_type": "Bearer",
      "expires_in": 899,
      "refresh_expires_at": "2026-07-29T09:31:47Z"
    }
  },
  "meta": { "request_id": "...", "timestamp": "..." }
}
```

The email is normalised to lowercase. `expires_in` is seconds — relative rather
than absolute so it is immune to client clock skew.

**Password rules:** 8–72 bytes, with an uppercase letter, a lowercase letter, a
digit and a special character. See Authentication.md §4 for why the ceiling is 72
*bytes*.

| Status | Code | When |
| --- | --- | --- |
| 422 | `VALIDATION_ERROR` | Password or field rules violated. |
| 409 | `CONFLICT` | Email already registered. |
| 400 | `BAD_REQUEST` | Malformed JSON. |

A validation failure lists **every** violated rule:

```json
{
  "success": false,
  "message": "The request contains invalid fields",
  "error": {
    "code": "VALIDATION_ERROR",
    "details": {
      "fields": [
        { "field": "password", "rule": "min",       "message": "password must be at least 8 characters" },
        { "field": "password", "rule": "uppercase", "message": "password must contain at least one uppercase letter" }
      ]
    }
  }
}
```

---

### POST /api/v1/auth/login

```json
{
  "email": "ops@example.com",
  "password": "Str0ng!Passw0rd",
  "device": "Warehouse Scanner 12"
}
```

`200 OK` — same body shape as register, with `last_login_at` populated.

`device` is an optional client-supplied label shown in the session list. It is
untrusted and display-only.

| Status | Code | When |
| --- | --- | --- |
| 401 | `UNAUTHORIZED` | Unknown email **or** wrong password. |
| 403 | `FORBIDDEN` | Account is `LOCKED` or `INACTIVE`. |

**Unknown email and wrong password return the identical error**, with matching
response latency. Distinguishing them turns the endpoint into an account
enumeration oracle. The 403 is only reachable by someone who already proved they
know the password, so it reveals nothing.

Login does **not** revoke other sessions — an operator may hold a scanner, a
desktop and a phone at once.

---

### POST /api/v1/auth/refresh

```json
{ "refresh_token": "8wGtIkgDVlj..." }
```

`200 OK` — a new access token **and a new refresh token**. The presented token
is revoked.

| Status | Code | When |
| --- | --- | --- |
| 401 | `UNAUTHORIZED` | Unknown, expired, or already-rotated token. |
| 403 | `FORBIDDEN` | Account deactivated since the token was issued. |

**Refresh tokens are single-use.** Clients must store the new one and discard the
old immediately.

> **Reuse triggers full session revocation.** Presenting an already-rotated token
> revokes *every* session for that user, including the legitimate client's
> current one, and forces a fresh login. Once a token has been used twice the
> system cannot tell which holder is real. See Authentication.md §3.

---

### POST /api/v1/auth/logout

```json
{ "refresh_token": "8wGtIkgDVlj...", "all_sessions": false }
```

`200 OK`. Set `all_sessions: true` to sign out everywhere.

**Always returns 200**, including for an unknown or already-revoked token.
Idempotent so a retry is safe, and a 404 would confirm to an attacker which
stolen tokens are still live.

The access token remains valid until it expires (≤15 minutes). Clients should
discard it locally on logout.

---

### GET /api/v1/auth/me

Requires `Authorization: Bearer <access_token>`.

`200 OK`:

```json
{
  "success": true,
  "message": "Profile retrieved successfully",
  "data": {
    "id": "56d45699-b7fe-42b5-837b-17d782d624cc",
    "email": "ops@example.com",
    "full_name": "Ops User",
    "status": "ACTIVE",
    "email_verified": false,
    "last_login_at": "2026-07-22T09:32:10Z",
    "created_at": "2026-07-22T09:31:47Z",
    "updated_at": "2026-07-22T09:32:10Z"
  },
  "meta": { "request_id": "...", "timestamp": "..." }
}
```

There is no password hash field, by construction: `dto.UserResponse` has no
field capable of holding one.

| Status | Code | When |
| --- | --- | --- |
| 401 | `UNAUTHORIZED` | Missing, malformed, expired or non-access token. |

The user is identified by the token's subject, never by a request parameter — a
client has no way to name someone else's profile.

---

## Tenancy

All endpoints require `Authorization: Bearer <access_token>`.

The active company travels in `X-Company-ID`. It is **not** in the access token
— see `MultiTenancy.md` §3 for why.

| Method | Path | Company | Purpose |
| --- | --- | --- | --- |
| POST | `/api/v1/companies` | optional | Create a company; caller becomes OWNER. |
| GET | `/api/v1/companies` | optional | Companies the caller can act in. |
| POST | `/api/v1/companies/switch` | optional | Change the active company. |
| GET | `/api/v1/memberships/mine` | optional | The caller's memberships + companies. |
| GET | `/api/v1/companies/current` | **required** | Active company + caller's role. |
| GET | `/api/v1/companies/:id` | **required** | One company. |
| PUT | `/api/v1/companies/:id` | **required** | Update a company. |
| DELETE | `/api/v1/companies/:id` | **required** | Soft-delete a company. |
| GET | `/api/v1/memberships` | **required** | Members of the active company. |
| POST | `/api/v1/memberships/invite` | **required** | Invite someone (PENDING). |
| DELETE | `/api/v1/memberships/:id` | **required** | Remove a member. |

"Company: optional" endpoints work with **no** active company — creating your
first one, listing the ones you belong to and switching between them all happen
before any tenant is active.

### Resolving the active company

1. `X-Company-ID` header, validated against the caller's ACTIVE memberships.
2. Otherwise, if the caller has exactly one ACTIVE membership, that one.
3. Otherwise none — company-required endpoints then return 403.

A caller in **two or more** companies who sends no header gets no context. The
system refuses to guess: picking one would let an operator ship stock from the
wrong client with no error raised.

A malformed `X-Company-ID` is a **400**, never a silent fallback.

---

### POST /api/v1/companies

```json
{ "code": "acme", "name": "Acme Logistics", "email": "ops@acme.test" }
```

`201 Created` — the company plus the caller's new OWNER membership:

```json
{
  "success": true,
  "message": "Company created successfully",
  "data": {
    "company": {
      "id": "9ec3d746-19d0-40a7-bd88-24ccaeccafdf",
      "code": "ACME",
      "name": "Acme Logistics",
      "status": "ACTIVE",
      "created_at": "2026-07-23T02:31:52Z",
      "updated_at": "2026-07-23T02:31:52Z"
    },
    "membership_id": "cbc968de-3b09-453f-86e3-938ff7d759e6",
    "role": "OWNER"
  }
}
```

`code` is normalised to upper case and must be 2–32 letters/digits. The company
and the OWNER membership are created in one transaction.

| Status | Code | When |
| --- | --- | --- |
| 409 | `CONFLICT` | Code already taken. |
| 422 | `VALIDATION_ERROR` | Bad code format, reserved code, blank name. |

---

### GET /api/v1/companies/current

`200 OK` — same shape as create: the active company, the caller's membership id
and their role in it.

| Status | Code | When |
| --- | --- | --- |
| 403 | `FORBIDDEN` | No active company; send `X-Company-ID`. |

---

### POST /api/v1/companies/switch

```json
{ "company_id": "d3b8fa06-af5d-419b-b315-0d48cf501082" }
```

`200 OK`, and echoes `X-Company-ID` in the response header.

This **validates** rather than issuing a new token: the client sends the
returned id in `X-Company-ID` on subsequent requests.

| Status | Code | When |
| --- | --- | --- |
| 403 | `FORBIDDEN` | Not a member, membership not ACTIVE, no such company, or the company is not ACTIVE. |

All four produce the identical message — distinguishing them would let any
authenticated user probe for the existence of other tenants.

---

### GET /api/v1/companies/:id

`200 OK` with the company.

| Status | Code | When |
| --- | --- | --- |
| 404 | `NOT_FOUND` | No ACTIVE membership in it, **or** it does not exist. |
| 422 | `VALIDATION_ERROR` | `:id` is not a UUID. |

**404, not 403.** A 403 would confirm that a company with that id exists.

---

### POST /api/v1/memberships/invite

```json
{ "email": "colleague@acme.test", "role": "STAFF" }
```

`201 Created` with a **PENDING** membership: `joined_at` is null and it grants
no access until accepted.

> No acceptance endpoint exists yet. An invited person is correctly locked out
> and currently has no way in — see `Membership.md` §4.

| Status | Code | When |
| --- | --- | --- |
| 409 | `CONFLICT` | No such account, **or** already a member. |
| 422 | `VALIDATION_ERROR` | `role` is `OWNER` (ownership is a transfer). |

The 409 message is identical for both causes, so the endpoint cannot be used to
discover which addresses are registered.

---

### DELETE /api/v1/memberships/:id

`200 OK`.

| Status | Code | When |
| --- | --- | --- |
| 404 | `NOT_FOUND` | The membership belongs to another company. |
| 409 | `CONFLICT` | It is the company's last ACTIVE owner. |

---

## RBAC

All endpoints require `Authorization: Bearer <token>` **and** an active company
(`X-Company-ID`, or an implicit single membership).

| Method | Path | Requires |
| --- | --- | --- |
| GET | `/api/v1/roles` | `role.read` |
| POST | `/api/v1/roles` | `role.create` |
| PUT | `/api/v1/roles/:id` | `role.update` |
| DELETE | `/api/v1/roles/:id` | `role.delete` |
| PUT | `/api/v1/roles/:id/permissions` | `role.assign_permissions` |
| GET | `/api/v1/permissions` | `permission.read` |
| GET | `/api/v1/permissions/mine` | *(none)* |

A denied request returns 403 naming the missing code in `error.details`.

See `PermissionMatrix.md` for the catalogue and per-role defaults.

---

### GET /api/v1/roles

`200 OK`, paginated. A company's three system roles are provisioned on first
call, so the list is never empty.

```json
{
  "success": true,
  "message": "Roles retrieved successfully",
  "data": [
    {
      "id": "…",
      "name": "ADMIN",
      "description": "Manages members and company settings.",
      "is_system": true,
      "permissions": ["company.read", "company.update", "membership.read"],
      "created_at": "2026-07-24T10:00:00Z",
      "updated_at": "2026-07-24T10:00:00Z"
    }
  ],
  "meta": { "request_id": "…", "timestamp": "…", "pagination": { } }
}
```

Permissions are inline: a list without them forces one request per role to
render a matrix.

Sortable by `name` or `created_at`. Filter with `?is_system=true|false`.

---

### POST /api/v1/roles

```json
{
  "name": "auditor",
  "description": "Read-only reviewer",
  "permissions": ["company.read", "membership.read"]
}
```

`201 Created`. The name is normalised to upper case; 2–32 letters, digits or
underscores. `permissions` is optional — a role with none grants nothing until
assigned.

| Status | Code | When |
| --- | --- | --- |
| 409 | `CONFLICT` | A role with this name already exists. |
| 422 | `VALIDATION_ERROR` | Bad name format, a reserved name (OWNER/ADMIN/STAFF), an unknown or duplicate permission code. |

Custom roles cannot yet be **assigned** to members — see `RBAC.md` §9.

---

### PUT /api/v1/roles/:id

```json
{ "description": "Updated description" }
```

`200 OK`. Only the description is mutable. **No role can be renamed**, system or
custom: memberships name roles by string with no foreign key, so a rename would
strand every member holding it.

---

### DELETE /api/v1/roles/:id

`200 OK`.

| Status | Code | When |
| --- | --- | --- |
| 404 | `NOT_FOUND` | The role belongs to another company. |
| 409 | `CONFLICT` | It is a system role. |

---

### PUT /api/v1/roles/:id/permissions

```json
{ "permissions": ["company.read", "role.read"] }
```

`200 OK` with the updated role.

Replaces the **entire** set. An empty array revokes everything; the field is
required, so omitting it is an error rather than being read as "revoke all".
Idempotent.

| Status | Code | When |
| --- | --- | --- |
| 404 | `NOT_FOUND` | The role belongs to another company. |
| 409 | `CONFLICT` | It is the OWNER role, whose permissions are immutable. |
| 422 | `VALIDATION_ERROR` | Unknown or duplicate permission code. |

---

### GET /api/v1/permissions

`200 OK` with the global catalogue, ordered by module then code. Filter with
`?module=role`.

The catalogue is seeded by migration and is immutable at runtime — there is no
create, update or delete.

---

### GET /api/v1/permissions/mine

`200 OK`:

```json
{
  "success": true,
  "data": {
    "role": "STAFF",
    "permissions": ["company.read", "membership.read", "permission.read", "role.read"]
  }
}
```

Unguarded by design: requiring `permission.read` to discover whether you hold
`permission.read` is circular.

Use it to hide buttons the user cannot press. It is a convenience, never a
security boundary — the server enforces every permission independently, and a
client ignoring this response entirely would gain nothing.

---

## Warehouses

All endpoints require `Authorization: Bearer <token>` and an active company.

| Method | Path | Permission |
| --- | --- | --- |
| GET | `/api/v1/warehouses` | `warehouse.read` |
| POST | `/api/v1/warehouses` | `warehouse.create` |
| GET | `/api/v1/warehouses/:id` | `warehouse.read` |
| PUT | `/api/v1/warehouses/:id` | `warehouse.update` |
| PATCH | `/api/v1/warehouses/:id/contact` | `warehouse.update` |
| PATCH | `/api/v1/warehouses/:id/activate` | `warehouse.activate` |
| PATCH | `/api/v1/warehouses/:id/deactivate` | `warehouse.activate` |
| PATCH | `/api/v1/warehouses/:id/suspend` | `warehouse.delete` |
| PATCH | `/api/v1/warehouses/:id/archive` | `warehouse.delete` |
| DELETE | `/api/v1/warehouses/:id` | `warehouse.delete` |

`DELETE` and `PATCH /archive` perform the **same** operation — a warehouse is
never hard-deleted. See `Warehouse.md` §5.

---

### POST /api/v1/warehouses

```json
{ "code": "wh-01", "name": "Jakarta Central", "type": "MAIN", "description": "Primary DC" }
```

`201 Created`. The warehouse is always **DRAFT** — status cannot be chosen, and
reaching ACTIVE requires the activation endpoint, which enforces readiness.

```json
{
  "success": true,
  "message": "Warehouse created successfully",
  "data": {
    "id": "…",
    "code": "WH-01",
    "name": "Jakarta Central",
    "type": "MAIN",
    "status": "DRAFT",
    "zones": {},
    "can_receive": false,
    "can_ship": false,
    "is_archived": false,
    "created_by": "…", "updated_by": "…",
    "created_at": "…", "updated_at": "…"
  }
}
```

`code` is canonicalised to upper case; 2–32 letters, digits and hyphens.
`can_receive` / `can_ship` are computed by the domain — do not derive them from
the status, or you are reimplementing a business rule.

| Status | Code | When |
| --- | --- | --- |
| 409 | `CONFLICT` | Code **or name** already used in this company. |
| 422 | `VALIDATION_ERROR` | Bad code format, short name, unknown type. |

Both code and name are unique per company: operators pick a destination by name,
so duplicates would make mis-shipping a matter of chance.

---

### PUT /api/v1/warehouses/:id

```json
{
  "name": "Jakarta Central DC",
  "address": "Jl. Sudirman 1, Jakarta",
  "receiving_zone_id": "…"
}
```

All fields optional. `code`, `status` and `type` are **not** updatable — code is
printed on physical labels, and status changes go through the lifecycle
endpoints.

| Status | Code | When |
| --- | --- | --- |
| 404 | `NOT_FOUND` | Belongs to another company. |
| 409 | `CONFLICT` | Clearing the address of an ACTIVE warehouse; name taken. |

---

### PATCH /api/v1/warehouses/:id/activate

`200 OK` with the activated warehouse.

Requires the warehouse to have a name, an address, a contact, and **at least
one** operational zone. Every unmet requirement is reported together:

```json
{
  "success": false,
  "message": "The request contains invalid fields",
  "error": {
    "code": "VALIDATION_ERROR",
    "details": { "fields": [
      { "field": "address", "rule": "required", "message": "a warehouse must have an address before it can be activated" },
      { "field": "contact", "rule": "required", "message": "a warehouse must have a contact name and phone before it can be activated" },
      { "field": "zones",   "rule": "required", "message": "a warehouse must have at least one operational zone assigned before it can be activated" }
    ]}
  }
}
```

Idempotent: activating an already-ACTIVE warehouse returns 200 and changes
nothing.

---

### PATCH /api/v1/warehouses/:id/suspend

```json
{ "reason": "failed fire inspection" }
```

`200 OK`. The reason is required — a suspension nobody can explain is one nobody
can safely lift. Reachable from any live status including DRAFT.

A SUSPENDED warehouse cannot be deactivated (409); lifting the hold is an
explicit `activate`, which re-checks readiness.

---

### PATCH /api/v1/warehouses/:id/contact

```json
{ "contact_name": "Budi", "contact_phone": "+62-811-1111" }
```

`200 OK`. A dedicated endpoint because changing the contact raises a domain
event downstream notification routing needs. Name and phone must be supplied
together. An ACTIVE warehouse cannot have its contact cleared (409).

---

### DELETE /api/v1/warehouses/:id

`200 OK`. **Soft delete** — the row is retained with `deleted_at` set, because
future stock movements will reference it. Subsequent reads return 404.

An archived warehouse is immutable: every mutation returns 409.

| Status | Code | When |
| --- | --- | --- |
| 404 | `NOT_FOUND` | Belongs to another company. |
| 409 | `CONFLICT` | Already archived, or a deletion guard refused (future: holds stock). |

---

## Storage Locations

All endpoints require `Authorization: Bearer <token>` and an active company.

| Method | Path | Permission |
| --- | --- | --- |
| GET | `/api/v1/locations` | `location.read` |
| POST | `/api/v1/locations` | `location.create` |
| GET | `/api/v1/locations/:id` | `location.read` |
| GET | `/api/v1/locations/barcode/:barcode` | `location.read` |
| PUT | `/api/v1/locations/:id` | `location.update` |
| PATCH | `/api/v1/locations/:id/capacity` | `location.update` |
| PATCH | `/api/v1/locations/:id/barcode` | `location.update` |
| PATCH | `/api/v1/locations/:id/activate` | `location.update` |
| PATCH | `/api/v1/locations/:id/deactivate` | `location.update` |
| PATCH | `/api/v1/locations/:id/maintenance` | `location.update` |
| PATCH | `/api/v1/locations/:id/lock` | `location.lock` |
| PATCH | `/api/v1/locations/:id/unlock` | `location.lock` |
| DELETE | `/api/v1/locations/:id` | `location.lock` |

---

### POST /api/v1/locations

```json
{
  "warehouse_id": "…",
  "zone": "A", "aisle": "01", "rack": "02", "level": "03",
  "barcode": "LOC-000123",
  "max_weight": "1234.567",
  "max_pallet": 4
}
```

`201 Created`. The location is always **ACTIVE** — unlike a warehouse, which
starts in DRAFT, a location needs no commissioning.

```json
{
  "data": {
    "id": "…",
    "warehouse_id": "…",
    "code": "A-01-02-03",
    "coordinate": { "zone": "A", "aisle": "01", "rack": "02", "level": "03", "depth": 4 },
    "barcode": "LOC-000123",
    "status": "ACTIVE",
    "picking_priority": 100,
    "allow_mixed_sku": false,
    "allow_overflow": false,
    "capacity": { "max_weight": "1234.567", "max_pallet": 4, "is_unlimited": false },
    "can_receive": true,
    "can_pick": true,
    "is_archived": false
  }
}
```

`code` is **derived from the coordinate** unless supplied — so `A-01-02-03` is
both the address and the label. Supply `code` explicitly for a dock labelled
`DOCK-1`.

Capacity values are **strings**, not JSON numbers: parsers represent numbers as
IEEE 754 doubles, which would round `1234.567` before the server saw it.

`can_receive` / `can_ship` are computed by the domain. Do not derive them from
`status` — the two differ (see below).

| Status | Code | When |
| --- | --- | --- |
| 404 | `NOT_FOUND` | The warehouse does not exist in this company. |
| 409 | `CONFLICT` | Code taken in this warehouse, or barcode taken in this company. |
| 422 | `VALIDATION_ERROR` | Missing zone, gapped coordinate, bad barcode or capacity. |

**Code is unique per WAREHOUSE; barcode is unique per COMPANY.** Aisle numbering
restarts at every building, but a scanner reads a label with no idea which site
it is standing in.

A coordinate may not have gaps — `zone` + `rack` with no `aisle` is rejected.

---

### GET /api/v1/locations/barcode/:barcode

`200 OK` with the single matching location. Company-scoped, not
warehouse-scoped: a scanner does not know which site it is in.

Case-sensitive — a barcode is a machine token reproduced exactly by the scanner.

---

### PATCH /api/v1/locations/:id/capacity

```json
{ "max_weight": "1000", "max_volume": "2.5", "max_pallet": 4 }
```

`200 OK`. An empty string clears a limit, meaning "not measured" — always
permitted, since it widens.

| Status | Code | When |
| --- | --- | --- |
| 409 | `CONFLICT` | The new capacity is below what is currently stored. |

**Capacity cannot be reduced below current usage**, and `allow_overflow` does
not exempt this. Until the Inventory module exists, usage is reported as empty,
so reductions always succeed.

---

### PATCH /api/v1/locations/:id/lock

```json
{ "reason": "damaged racking" }
```

`200 OK`. The reason is required — a lock nobody can explain is one nobody can
safely lift.

**LOCKED has exactly one exit: `unlock`.** `activate`, `deactivate` and
`maintenance` all return 409 on a locked location, so a routine transition
cannot silently discard the reason a hold was imposed.

`unlock` on a location that is not locked returns 409 — it almost always means
the caller targeted the wrong record.

---

### Availability

| Status | `can_receive` | `can_pick` |
| --- | :---: | :---: |
| ACTIVE | true | true |
| INACTIVE | false | false |
| LOCKED | false | false |
| **MAINTENANCE** | **false** | **true** |

A MAINTENANCE location may still be picked **from** so its stock can be drained
before work starts. This is why the two flags are reported separately.

---

### DELETE /api/v1/locations/:id

`200 OK`. **Soft delete** — the row is retained because future stock movements
will reference it. Subsequent reads return 404.

An archived location is immutable: every mutation returns 409.

| Status | Code | When |
| --- | --- | --- |
| 409 | `CONFLICT` | Already archived, or it still holds stock (future). |

---

## Error codes

| Code | Status | Meaning |
| --- | --- | --- |
| `BAD_REQUEST` | 400 | Malformed body. |
| `UNAUTHORIZED` | 401 | Missing or invalid credentials. |
| `FORBIDDEN` | 403 | Authenticated but not permitted. |
| `NOT_FOUND` | 404 | Resource does not exist. |
| `METHOD_NOT_ALLOWED` | 405 | Wrong verb. |
| `CONFLICT` | 409 | Uniqueness violation. |
| `VALIDATION_ERROR` | 422 | Field rules violated; see `error.details.fields`. |
| `TOO_MANY_REQUESTS` | 429 | Rate limited (not yet enforced). |
| `INTERNAL_ERROR` | 500 | Server fault. |
| `SERVICE_UNAVAILABLE` | 503 | Dependency down. |

`error.details` is present on 4xx and **suppressed on 5xx** — internal error
detail has not been vetted for a tenant's eyes and stays in the logs.

---

## Client integration notes

**Token storage.** Keep both tokens out of `localStorage` on web — it is
readable by any injected script. Prefer memory plus an httpOnly cookie, or the
platform keystore on mobile.

**Refresh ahead of expiry.** Use `expires_in` to schedule a refresh at roughly
80% of the lifetime rather than waiting for a 401.

**Serialise refreshes.** Two concurrent refreshes with the same token look
exactly like theft, and one of them will lose the race and trigger full session
revocation. A client with parallel in-flight requests must funnel refresh through
a single mutex and queue the others behind it.

**Persist the rotated token atomically.** If the app crashes between receiving a
new refresh token and storing it, the user is logged out. Write it before
completing the refresh.

**Correlation.** Send `X-Request-ID` as a UUID to correlate client and server
logs; anything not a valid UUID is replaced. It is echoed on every response and
in `meta.request_id`.
