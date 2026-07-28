# Role-Based Access Control

How the system decides whether an action is allowed, and why the model is shaped
the way it is.

---

## 1. The model

```
memberships.role  (a NAME: 'OWNER', 'ADMIN', 'STAFF')
        │
        │  joined by (company_id, name)
        ▼
      roles  ──────►  role_permissions  ──────►  permissions
   (per company)         (the grant)          (global catalogue)
```

| Table | Scope | Mutable at runtime |
| --- | --- | --- |
| `permissions` | **global** | no — seeded by migration |
| `roles` | per company | yes |
| `role_permissions` | per company (via role) | yes |

### Why permissions are global

A permission names a capability the *software* has — "delete a company",
"invite a member". That set is determined by what the code can do, not by what
any customer bought, so it is identical for every tenant.

Per-tenant permission rows would mean N copies of the same immutable list,
drifting the moment one tenant's seed ran and another's did not. Worse, every
check would have to ask "does THIS tenant know about this capability?" before it
could ask "is this allowed?".

What each company controls is the **mapping** from its roles to those
permissions, which is `role_permissions`.

### Why roles are per company

Two companies may grant completely different permissions to the role they both
call "ADMIN", and neither can see the other's definition. Verified live: after
Acme revoked `membership.invite` from its ADMIN, Globex's ADMIN kept it.

---

## 2. Integrating without changing Membership

This sprint could not modify the tenancy module, and did not.

`memberships.role` already stores a role NAME, and `middleware.ResolveCompany`
already puts it on `RequestContext.Role`. RBAC resolves `(company_id,
role_name)` against the `roles` table. No foreign key was added to
`memberships`, no column changed, and tenancy is unaware RBAC exists.

An integration test (`TestMembershipTableIsUnchanged`) asserts `memberships` has
no `role_id` column and still has the `role` column resolution depends on.

### The cost of joining by name

A role rename would orphan every membership naming it — those members would
silently resolve to no permissions, with no error anywhere. So **no rename is
exposed for any role**, system or custom. `entity.Role.CanRename` returns false
unconditionally and `UpdateRoleRequest` has no name field.

If renaming becomes a requirement, the correct fix is to add `role_id` to
`memberships` with a foreign key and migrate the existing name values — a change
to the tenancy module, which is why it was not done here.

---

## 3. Permission resolution

`middleware.LoadPermissions` runs once per request, after `ResolveCompany`:

```
Authenticate     → WHO
ResolveCompany   → WHERE (company + role name)
RequireCompany   → refuse if nowhere
LoadPermissions  → WHAT  (one query, cached on the request)
RequirePermission → per route
```

Running it once matters: a route guarded by two permissions would otherwise hit
the database twice. Verified — two guards, one resolver call.

### It fails closed, everywhere

| Situation | Result |
| --- | --- |
| No company context | empty set → 403 |
| Unknown role name | empty set → 403 |
| Role with no grants | empty set → 403 |
| `LoadPermissions` not wired | empty set → 403 |
| Route declares no permission | 500 (a wiring bug, not an allow) |
| **Resolver returns an error** | **500, not 403** |

The last row is a deliberate distinction. A resolution *miss* is an
authorisation decision; a resolution *failure* is an outage. Presenting an
unreachable database as "you are not allowed" sends an operator hunting for a
permissions bug that does not exist.

### `RequirePermission` is conjunctive

A route declaring two codes requires **both**. A reviewer scanning a route table
reads "needs role.create and role.assign_permissions" as a conjunction, and a
disjunctive reading would quietly widen access. `RequireAnyPermission` exists
separately for the cases that genuinely need it, so the semantics are visible at
the declaration.

Declaring **no** codes denies the request. An empty requirement almost always
means a constant was mistyped or a slice came out empty, and failing open there
would silently unguard a route.

---

## 4. Role inheritance

**There is none, and that is deliberate.**

OWNER does not "extend" ADMIN, and ADMIN does not "extend" STAFF. Each role
holds an explicit, independent set of grants. The apparent nesting in the
default matrix is a coincidence of the seed values, not a mechanism.

Hierarchical roles are appealing until an administrator wants a role that is
"ADMIN but without member removal". With inheritance that needs a negative
grant — an exception to an inherited rule — and negative grants make the
question "why can this person do X?" require walking a tree and applying
precedence rules. Flat sets answer it with one lookup.

The cost is duplication in the seed data. That cost is paid once, at
provisioning, in `entity.DefaultPermissions`.

---

## 5. Provisioning

Companies get their three system roles **lazily**, on first use:

- `RoleService.List` provisions, so the roles screen is never empty.
- `Evaluator.findRole` provisions on a miss and retries once.

### Why not seed them when a company is created

That would require editing the tenancy module (forbidden this sprint) and would
make tenancy depend on RBAC — inverting the dependency, since authorisation
should know about tenancy and not the reverse.

Lazy provisioning also covers every company created *before* this sprint with no
backfill migration. The evaluator's retry is what makes it invisible: without
it, an existing company's OWNER would be denied everything on their first
request and succeed on their second — the kind of intermittent authorisation
failure that is impossible to diagnose from a bug report.

### It never repairs an edited role

If a company has an ADMIN role, provisioning leaves it alone — even if it is
missing a permission the defaults would grant. Silently re-adding a permission
an administrator deliberately revoked would be a security regression disguised
as a fix. `TestProvisionDoesNotRepairEditedRoles` asserts this.

---

## 6. Protections

| Rule | Enforced by | Failure |
| --- | --- | --- |
| Only OWNER may create/delete roles | `role.create` / `role.delete` granted only to OWNER | 403 |
| System roles cannot be deleted | `entity.Role.CanDelete` | 409 |
| System permissions cannot be modified | no write method exists | — |
| OWNER's permissions cannot be changed | `RoleService.SetPermissions` | 409 |
| No role may be renamed | no name field in the update DTO | — |
| Cannot create a role named OWNER/ADMIN/STAFF | validator | 422 |

### "Only OWNER" is expressed as data, not a name check

`role.create` and `role.delete` are granted only to the OWNER system role, and
the route requires the permission. There is no `if role == "OWNER"` anywhere.

That is the right shape for RBAC — the point of a permission system is that
authority is *granted*, not hard-wired. A name comparison would make the role
table decorative.

The guarantee still holds in practice because the OWNER role's own grants are
immutable, and ADMIN is not seeded with `role.assign_permissions` — so nobody
can grant themselves `role.create` without already holding the ability to
change grants.

### Why OWNER cannot be weakened

Ownership is the recovery path for every other authorisation mistake: if an
administrator strips ADMIN of something, the owner puts it back. Allowing OWNER
itself to be reduced would let a company lock itself out of its own account with
no way back short of database surgery — and the caller doing it may not realise
until the next time they need the permission they just removed.

### Immutability is structural, not a check

`PermissionRepository` exposes only three read methods, and
`permissionRepository` deliberately does **not** embed the generic base
repository — embedding would promote `Create`, `Update` and `Delete` onto the
concrete type.

That mattered: the first implementation *did* embed it, and an integration test
(`TestPermissionsAreImmutable`) caught the leak. The documented guarantee and
the code now agree.

---

## 7. Tenant isolation

Every role query is company-scoped through `base.ForCompany`, applied in a named
`forTenant` helper so a reviewer auditing for missing filters looks for one
identifier rather than an inline `Where` clause.

Reading a role from another company returns **404**, never 403 — a 403 would
confirm it exists.

`role_permissions` carries no `company_id`. The tenant is implied by `role_id`,
and duplicating it would create a second source of truth that could disagree
with the role's own company — a denormalisation whose only possible outcome is a
grant pointing at the wrong tenant.

Verified live and against real SQL: reading, updating, deleting and
**escalating permissions on** another company's role all fail, and the target
role is left untouched.

---

## 8. Future ABAC compatibility

Attribute-based access control asks "may this user do X *to this specific
object*?" rather than "may this user do X?". The seams are in place:

**The permission check is already a function call, not a data lookup.**
`RequirePermission(code)` sits at the route. An attribute-aware guard —
`RequireOwnership("warehouse")`, say — is a sibling middleware that runs after
it, without changing anything that exists.

**`RequestContext` already carries what a policy needs.** `UserID`, `CompanyID`,
`MembershipID` and `Role` are all resolved before any handler runs, so a policy
engine has its subject and environment without additional queries.

**`entity.Set` is the natural input to a policy.** ABAC does not replace RBAC in
practice — it refines it. The usual shape is "hold the permission AND satisfy
the constraint", so the coarse RBAC check stays as the fast path and the
attribute check narrows it.

**Codes are hierarchical strings.** `company.update` already implies a resource
type and an action. Extending to `company.update.own` or a scope qualifier needs
no schema change.

What would need to change: `role_permissions` would gain a nullable `conditions`
JSONB column, and `LoadPermissions` would return conditions alongside codes.
Neither alters the tables or the middleware ordering that exist today.

---

## 9. Known gaps

**Permission checks are not applied to tenancy routes.** This is the significant
one. `middleware.RequirePermission` is built, tested and enforcing on every RBAC
route — but `/companies/*` and `/memberships/*` are still unguarded, because
adding the middleware means editing `tenancy/route/route.go`, which this sprint
was told not to modify.

The permission codes for those operations exist and are granted correctly
(`company.update`, `membership.invite`, …). Wiring is one line per route; see
`PermissionMatrix.md` §4 for the exact change.

**No per-request permission caching across requests.** `LoadPermissions` runs
one query per request. That is one extra round trip on every authorised call.
`port.Cache` is wired and the natural key is `(company_id, role_name)`, but
cache invalidation on a permission change is a correctness risk that deserves
its own sprint rather than being bolted on.

**No ownership transfer.** OWNER is still granted only at company creation.
Unchanged from the previous sprint, and now more visible because OWNER's grants
are immutable.

**Custom roles cannot be assigned to members.** A company can create a role, but
`memberships.role` is constrained by a CHECK to OWNER/ADMIN/STAFF — relaxing it
means altering the memberships table. Custom roles therefore resolve correctly
but nobody can hold one yet.
