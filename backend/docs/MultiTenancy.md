# Multi-Tenancy

How the system separates one company's data from another's, and why the model
is shaped the way it is.

---

## 1. Why User has no CompanyID

This is the decision everything else follows from, and it is permanent rather
than a stage on the way to something else.

```
users          ──┐
                 ├──  memberships  (company_id, user_id, role, status)
companies      ──┘
```

`users` has no `company_id`. `companies` has no `owner_id`. The **only** link
between them is `memberships`.

### A person belongs to several companies

A 3PL operator works for multiple clients. A logistics manager oversees two
subsidiaries. A consultant is engaged by four warehouses at once. If identity
carried a company, each of those people would need one account per company —
duplicate credentials, duplicate password resets, and no single account to lock
when they leave.

### Role is a property of the relationship, not the person

The same human can be OWNER of their own company and STAFF at a client's. That
sentence is impossible to express with a `role` column on `users`. It is
natural on `memberships`, where the role sits on the edge between the two.

The integration suite asserts exactly this: `TestUserInTwoCompanies` gives one
user OWNER at Acme and STAFF at Globex and checks both resolve correctly.

### Authentication must work before a company exists

You log in, and *then* select or are assigned a company. A tenant-scoped login
is a chicken-and-egg problem: you cannot name the company you belong to before
proving who you are.

This is why `Authentication.md` §1 states identity is company-independent, and
why this sprint added multi-tenancy **without changing a line of the auth
module**. The `TestAuthModuleIsUnaffected` integration test asserts that `users`
still has no `company_id` column.

---

## 2. The tenant boundary

`companies` is the tenant ROOT. It has no `company_id` of its own — it *is* the
tenant. Every other tenant-owned table will carry `company_id` referencing it.

That makes isolation two different rules depending on the table:

| Table | Isolation rule | Enforced by |
| --- | --- | --- |
| `companies` | Reachable only via an ACTIVE membership | `accessibleTo(userID)` scope |
| `memberships` | `company_id = ?` | `ForCompany(companyID)` scope |
| everything else (future) | `company_id = ?` | `ForCompany(companyID)` scope |

### Reachability, for companies

```sql
companies.id IN (
    SELECT m.company_id FROM memberships m
    WHERE m.user_id = ? AND m.status = 'ACTIVE' AND m.deleted_at IS NULL
)
```

Every company read takes a `userID` and applies this. A method that returned a
company without one would not compile — the same structural guarantee that
`RepositoryConvention` §3 gets from a mandatory `companyID` parameter.

It is a correlated subquery rather than a `JOIN` deliberately: a JOIN would
duplicate company rows if the membership uniqueness constraint were ever
violated, silently inflating both the page and the total count. `IN (SELECT …)`
cannot duplicate, so pagination stays correct even under data corruption.
PostgreSQL plans it as a semi-join over `idx_memberships_user_company`, so the
defensive shape costs nothing.

### NOT_FOUND, never FORBIDDEN

Reading a company you are not a member of returns **404**. A 403 would confirm
that a company with that id exists, which is an information leak across tenants.
The same rule applies to memberships: knowing a membership id is not enough to
act on it, and one belonging to another company is simply absent.

Verified over HTTP and against real SQL.

---

## 3. Company context

The active tenant is resolved per request by `middleware.ResolveCompany` and
lands in `RequestContext` as `CompanyID`, `MembershipID` and `Role`.

### It travels in a header, not in the token

```
X-Company-ID: <uuid>
```

Access tokens carry **no company claim** — `Authentication.md` §2, unchanged by
this sprint. Consequences:

- Switching companies needs no credential reissue.
- A stolen token is scoped to *no* tenant rather than permanently to one.
- The company can change between two requests made with the same token, which
  is exactly what a switcher UI needs.

The alternative — company in the token — would mean minting new credentials on
every switch and would make a leaked token a persistent grant to one specific
tenant.

### Resolution order

1. **Header wins.** Validated against the caller's ACTIVE memberships. Naming a
   company you cannot reach is a **403**, never a silent fallback.
2. **Exactly one ACTIVE membership → use it.** The overwhelmingly common case;
   requiring a header for it would be pointless friction.
3. **Otherwise → no company context.**

### Rule 3 is the important one

Auto-selecting for a multi-company user would mean a request without a header
lands in whichever tenant happened to sort first. A warehouse operator working
for two clients could then ship stock from the wrong one with no error raised.

**Ambiguity is refused, not guessed.** `TestResolveRefusesToGuessBetweenCompanies`
is the unit test; the live stack confirms a two-company user with no header gets
a 403 telling them to choose.

### Permissive resolve, enforcing require

`ResolveCompany` attaches a context when it can and proceeds when it cannot.
`RequireCompany` is the enforcing half, applied per route.

The split exists because some authenticated endpoints must work with **no**
company: creating your first one, listing the ones you belong to, and switching
between them. A single enforcing middleware would make those unreachable for
exactly the users who need them most.

A malformed `X-Company-ID` is rejected with 400 rather than ignored. Silently
falling back to the default company would let a typo route a write into the
wrong tenant — the worst failure mode in a multi-tenant system.

### Suspension takes effect immediately

Company status is re-checked on **every** request, not only at switch time.
Suspending a tenant for non-payment must take effect on their next request, not
at the end of whatever session its members happen to be in. Confirmed live: a
member's context breaks the moment the company is set to SUSPENDED.

---

## 4. Where the tenant filter is applied

| Layer | Responsibility |
| --- | --- |
| Middleware | Resolves the tenant into `RequestContext`. |
| Handler | **Never mentions CompanyID.** Binds input, calls the service. |
| Service | `rc.RequireTenant()`, then passes the id down. |
| Repository | Applies `ForCompany` / `accessibleTo`. Cannot be skipped. |

No handler filters by company — the repository rules require it and no handler
in the module does. A handler that could name a tenant would be a handler that
could name someone else's.

`RequireTenant()` returns an **error** rather than a nil pointer, so a service
cannot proceed with an unscoped query by forgetting a nil check. The filter is
not optional, so neither is obtaining it.

---

## 5. Registration flow

```
register user   (auth module — unchanged)
      ↓
POST /companies (tenancy module)
      ├─ create company
      └─ create OWNER membership, status ACTIVE
      ↓
company context available
```

Both writes are **one transaction**. Without it, a failure between them leaves a
company nobody can reach — not even its creator, since access is granted
exclusively through memberships — and permanently consumes the company code, so
the retry fails with a conflict the user cannot resolve.
`TestCreateCompanyRollsBack` and `TestOnboardingRollsBack` both assert the code
is free again after a failure.

The founder's membership is ACTIVE immediately, not PENDING: nobody invited
them, and a PENDING owner could not reach the company they just created.

> **A note on scope.** The sprint brief described this flow as a single
> "Register" sequence, while also requiring that Authentication not be modified.
> Those pull in opposite directions, so registration stays in auth exactly as it
> was and company creation is a separate call the client makes next. Merging them
> would have meant editing the auth service. If you would rather have a single
> onboarding endpoint, the right shape is a new `POST /api/v1/onboarding` in the
> tenancy module that calls both — still leaving auth untouched.

---

## 6. Company lifecycle

| Status | Can be a working context | Meaning |
| --- | --- | --- |
| `ACTIVE` | yes | Normal. |
| `INACTIVE` | no | Dormant — ended contract, lapsed trial. |
| `SUSPENDED` | no | Enforcement hold — non-payment, terms violation. |

`INACTIVE` and `SUSPENDED` are distinct because the remedy differs: lifting a
suspension is a commercial decision, reactivating is an administrative one.
Collapsing them into one `disabled` flag loses the reason, and the reason is
what a support agent needs first.

Deleting a company is a **soft delete**. Memberships are deliberately not
cascaded: the CASCADE fires only on a hard delete, and soft-deleting the members
would destroy the record of who belonged to the company — exactly the history an
audit needs after a tenant closes. They become unreachable anyway, because every
company read filters on the company's own `deleted_at`.

---

## 7. Schema notes

Company codes and membership pairs both use **partial** unique indexes
(`WHERE deleted_at IS NULL`), so a deleted company's code can be reclaimed and a
removed member can be re-invited. Both are covered by integration tests.

Every membership index leads with the column the query filters on first:
`idx_memberships_user_company` for the middleware's per-request lookup,
`idx_memberships_company` for the member list.

`memberships.invited_by` is `ON DELETE SET NULL`, not CASCADE: deleting the
person who sent an invitation must not delete the membership of the person they
invited.

---

## 8. Known gaps

Listed plainly, because an undocumented gap is worse than an open one.

**No authorisation.** Role is stored and resolved but **enforced nowhere**. Any
ACTIVE member of a company can currently update it, delete it, invite members
and remove them. RBAC is the next sprint, and `RequestContext.Role` plus
`MembershipID` are the seam it will use. Until then, treat every member as an
administrator.

**No invitation acceptance.** `POST /memberships/invite` creates a PENDING
membership, and nothing can transition it to ACTIVE — there is no accept
endpoint and no email delivery. An invited person is correctly locked out
(proven by `TestPendingMembershipGrantsNothing`), but they also have no way in.
The next sprint needs an accept flow.

**No ownership transfer.** OWNER can only be granted at company creation;
invitation explicitly rejects it, because a second owner by invitation would
make "who can delete this company?" ambiguous. Last-owner removal is blocked, so
ownership cannot be lost — but it also cannot be moved.

**No per-tenant rate limiting or quotas.** One tenant can currently exhaust
shared capacity.

**Company codes are globally unique.** `ExistsByCode` is deliberately unscoped,
so a tenant can discover that a code is taken. This leaks only what the unique
index would reveal on insert anyway, and a clean CONFLICT beats a 500 — but it
is a genuine, if narrow, cross-tenant signal.

---

## 9. Adding a tenant-scoped module

1. Give the entity a `CompanyID uuid.UUID` with `json:"-"`.
2. Lead every index with `company_id`; make unique constraints per-tenant and
   partial on `deleted_at IS NULL`.
3. Make `companyID` a required parameter of every repository method, and apply
   `base.ForCompany(companyID)` in a named `forTenant` helper.
4. Start every service method with `appcontext.From(ctx).RequireTenant()`.
5. Mount routes behind `middleware.Authenticate`, `middleware.ResolveCompany`
   and `middleware.RequireCompany`.
6. Never let a handler mention a company id.

See `RepositoryConvention.md` and `Membership.md`.
