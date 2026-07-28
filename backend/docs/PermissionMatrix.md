# Permission Matrix

The complete permission catalogue and what each system role is provisioned with.

Source of truth is `entity.PermissionCatalogue()` plus migration
`20260724100000_create_permissions_table.up.sql`. An integration test
(`TestSeededCatalogueMatchesCode`) fails if the two ever drift — a code in one
but not the other produces either a permission that can never be granted or a
grant that can never be resolved, and neither fails anywhere else.

---

## 1. Catalogue

| Code | Module | Capability |
| --- | --- | --- |
| `company.read` | company | View the active company. |
| `company.update` | company | Change company details. |
| `company.delete` | company | Soft-delete the company. |
| `membership.read` | membership | View the member list. |
| `membership.invite` | membership | Invite a person. |
| `membership.remove` | membership | Remove a member. |
| `role.read` | role | View roles and their permissions. |
| `role.create` | role | Define a custom role. |
| `role.update` | role | Change a role's description. |
| `role.delete` | role | Delete a custom role. |
| `role.assign_permissions` | role | Change what a role grants. |
| `permission.read` | permission | View the catalogue. |

Twelve permissions across four modules. Warehouse, product and inventory codes
are deliberately **absent**: a permission for a module that cannot be exercised
is a promise the software does not keep, and it would appear in every role
editor as an option that does nothing. They get added by the sprint that
implements them.

---

## 2. Default grants

| Permission | OWNER | ADMIN | STAFF |
| --- | :---: | :---: | :---: |
| `company.read` | ✅ | ✅ | ✅ |
| `company.update` | ✅ | ✅ | — |
| `company.delete` | ✅ | — | — |
| `membership.read` | ✅ | ✅ | ✅ |
| `membership.invite` | ✅ | ✅ | — |
| `membership.remove` | ✅ | ✅ | — |
| `role.read` | ✅ | ✅ | ✅ |
| `role.create` | ✅ | — | — |
| `role.update` | ✅ | — | — |
| `role.delete` | ✅ | — | — |
| `role.assign_permissions` | ✅ | — | — |
| `permission.read` | ✅ | ✅ | ✅ |
| **Total** | **12** | **7** | **4** |

Confirmed against the running stack: `OWNER perms=12, ADMIN perms=7,
STAFF perms=4`.

These are **seed values, not policy**. Once a company exists, its rows are the
single source of truth and an administrator with `role.assign_permissions` may
edit them freely — except OWNER, which is immutable.

### Why ADMIN stops where it does

`company.delete` is withheld because destroying the tenant is an ownership
decision, not an operational one.

The four `role.*` write permissions are withheld because **an admin who can
edit roles can grant themselves anything**. Allowing it would make the
distinction between ADMIN and OWNER cosmetic — ADMIN could self-promote in one
request. This is the boundary `TestAdminIsOperationalNotStructural` pins.

### Why STAFF is read-only here

Staff perform warehouse operations, and those permissions do not exist yet.
Over the *administrative* surface they can see the company, the member list and
the role definitions, but change nothing.

---

## 3. Enforced routes

Every RBAC route declares its requirement in `route/route.go`. No handler
contains a permission check.

| Method | Path | Requires |
| --- | --- | --- |
| GET | `/api/v1/roles` | `role.read` |
| POST | `/api/v1/roles` | `role.create` |
| PUT | `/api/v1/roles/:id` | `role.update` |
| DELETE | `/api/v1/roles/:id` | `role.delete` |
| PUT | `/api/v1/roles/:id/permissions` | `role.assign_permissions` |
| GET | `/api/v1/permissions` | `permission.read` |
| GET | `/api/v1/permissions/mine` | *(none)* |

`/permissions/mine` is unguarded deliberately: a caller must always be able to
discover what they themselves may do, and requiring `permission.read` to find
out whether you hold `permission.read` is circular.

Assigning permissions is a **separate** permission from updating a role. One
changes what people can do, the other changes a label; granting them together
would mean anyone who can rename a role can also escalate it.

A denied request returns 403 naming the missing code:

```json
{
  "success": false,
  "message": "You do not have permission to perform this action",
  "error": {
    "code": "FORBIDDEN",
    "details": { "required_permission": "role.create" }
  }
}
```

Naming it is a deliberate trade: it tells a legitimate user exactly what to ask
their administrator for, and reveals nothing an attacker could not read from the
published catalogue.

---

## 4. Routes NOT yet enforced

`/companies/*` and `/memberships/*` remain **unguarded**. Their permission codes
exist and are granted correctly, but the middleware is not applied, because
doing so means editing `tenancy/route/route.go` — which this sprint was
instructed not to modify.

The intended mapping, ready to apply:

| Method | Path | Should require |
| --- | --- | --- |
| GET | `/api/v1/companies/current` | `company.read` |
| GET | `/api/v1/companies/:id` | `company.read` |
| PUT | `/api/v1/companies/:id` | `company.update` |
| DELETE | `/api/v1/companies/:id` | `company.delete` |
| GET | `/api/v1/memberships` | `membership.read` |
| POST | `/api/v1/memberships/invite` | `membership.invite` |
| DELETE | `/api/v1/memberships/:id` | `membership.remove` |

`POST /companies`, `GET /companies`, `POST /companies/switch` and
`GET /memberships/mine` stay unguarded — they are reachable before any company
context exists, so there is no role to evaluate against.

Applying it needs two changes to `tenancy/route/route.go`:

```go
// 1. add LoadPermissions to the scoped chain
scoped.Use(middleware.RequireCompany(), middleware.LoadPermissions(permissions))

// 2. declare each route's requirement
scoped.PUT("/companies/:id",
    middleware.RequirePermission("company.update"), companies.Update)
```

plus passing `c.RBAC.Resolver()` into `tenancy.New` from bootstrap. The
resolver is already exposed for exactly this.

---

## 5. Customising

```
PUT /api/v1/roles/:id/permissions
{ "permissions": ["company.read", "membership.read"] }
```

The request carries the desired **final state**, not a delta — a client
rendering a checkbox list knows what it wants, not what changed, and making it
compute the diff invites a whole class of bug. The service computes the diff
internally so the audit trail still records individual grants and revocations,
which is what lets the log answer "when did ADMIN lose `company.delete`?".

An empty array is meaningful and revokes everything. The field is `required`, so
omitting it is an error rather than being read as "revoke all" — a client bug
that sent `{}` would otherwise silently strip a role.

Idempotent: re-sending the same set emits no audit events.

Revocation is a **soft delete**. "Who revoked what, and when" is exactly the
question an access-control audit asks, and a hard delete leaves no evidence the
permission was ever held. Re-granting revives the original row rather than
inserting a second.

---

## 6. Adding a permission

1. Add the constant to `entity` and to `PermissionCatalogue()`.
2. Add the row to a **new** migration — never edit the seed migration, which has
   already run everywhere.
3. Decide which system roles get it by default in `entity.DefaultPermissions`.
   This affects only companies provisioned *after* the change; existing
   companies keep their current grants, deliberately (see `RBAC.md` §5).
4. Declare it on the route with `middleware.RequirePermission`.
5. Update this document.

`TestSeededCatalogueMatchesCode` fails if steps 1 and 2 disagree.
