# Membership

A membership joins one user to one company with a role. It is the only link
between identity and tenancy, and it is where "who may act where, and as what"
is answered.

---

## 1. The model

```go
type Membership struct {
    entity.BaseEntity

    CompanyID uuid.UUID
    UserID    uuid.UUID
    Role      Role              // OWNER | ADMIN | STAFF
    Status    MembershipStatus  // ACTIVE | PENDING | SUSPENDED
    JoinedAt  *time.Time
    InvitedBy *uuid.UUID
}
```

One membership per user per company, enforced by a partial unique index on
`(company_id, user_id) WHERE deleted_at IS NULL`.

That constraint is what makes company resolution deterministic. Without it a
user could hold two memberships in one company with different roles, and "what
is this user's role here?" would have no single answer.

Partial, so a removed member can be re-invited later —
`TestMembershipReusableAfterSoftDelete`.

---

## 2. Status

| Status | Grants access | Meaning |
| --- | --- | --- |
| `ACTIVE` | **yes** | A working member. |
| `PENDING` | no | An issued but unaccepted invitation. |
| `SUSPENDED` | no | Revoked access, record retained. |

`CanAccess()` returns true for ACTIVE only. It is the single most important
predicate in the tenancy model, and every access path funnels through it:

- `FindActiveByUserAndCompany` — the middleware's per-request resolution.
- `accessibleTo(userID)` — the companies reachability subquery.
- `ListActiveByUser` — the switcher menu.

### PENDING reserves a seat; it does not grant one

The row exists so the invitation is durable across a restart and the seat is
reserved, but it authorises nothing. This is what stops an unaccepted invite
being used as an access grant.

Proven at both layers: `TestInvitedMemberCannotAccessYet` (service) and
`TestPendingMembershipGrantsNothing` (real SQL), plus live confirmation that an
invited user cannot switch into the company and does not see it in their menu.

### SUSPENDED retains history

Revoking access without deleting the record keeps everything that person did
attributable. Deleting the membership would orphan the audit trail.

---

## 3. Role

| Role | Intent |
| --- | --- |
| `OWNER` | Created the company. Cannot be removed while the last one. |
| `ADMIN` | Manages members and settings. |
| `STAFF` | Day-to-day warehouse operations. |

**Role is stored and resolved but enforced nowhere.** RBAC is the next sprint.
Today any ACTIVE member can perform every operation in the module. The column
and the `RequestContext.Role` field exist now so that turning on permission
checks is a change to an authorisation layer rather than a migration against a
populated table.

Role belongs to the **relationship**. The same person is OWNER of their own
company and STAFF at a client's; switching companies changes their role, which
`TestSwitchToCompanyIAmAMemberOf` asserts and the live stack confirms.

---

## 4. Invitation

```
POST /api/v1/memberships/invite
{ "email": "colleague@acme.test", "role": "STAFF" }
```

Creates a membership with `Status: PENDING`, `JoinedAt: nil`,
`InvitedBy: <caller>`.

### Invitees are named by email, not by user id

Two reasons. The inviter knows their colleague's address, not an internal UUID —
and an API demanding the id would force a user-search endpoint, which is an
account enumeration oracle. It also keeps the tenancy module from browsing
identity data: it asks "who has this address?" through a one-method interface
and receives one id or nothing.

That interface (`service.UserDirectory`) is declared in the tenancy module and
implemented by an adapter in `bootstrap`, so **tenancy never imports auth** and
auth was not modified to serve it.

### The company is never a parameter

The target comes from `RequestContext`, so a client cannot invite someone into a
company it has no active membership in.

### Failures are deliberately vague

Both "no such account" and "already a member" return the same message:

> That address cannot be invited. It may already be a member, or no account
> exists.

Distinguishing them would turn the endpoint into an account enumeration oracle
for anyone holding a single valid membership — they could probe arbitrary
addresses for registration. Asserted by `TestInviteHidesWhetherAccountExists`
and confirmed live.

### OWNER cannot be invited

Ownership is a **transfer**, not an invitation. A company has exactly one
founding owner; creating a second by invitation would make "who can delete this
company?" ambiguous with no way to resolve it. The DTO tag permits OWNER so the
rejection is a specific message rather than a generic "must be one of".

### No acceptance flow exists yet

Nothing can move a PENDING membership to ACTIVE — there is no accept endpoint
and no email delivery. An invited person is correctly locked out and also has no
way in. This is a deliberate scope boundary for this sprint, and the next one
needs to close it.

---

## 5. Removal

```
DELETE /api/v1/memberships/:id
```

Tenant-scoped: a membership id belonging to another company resolves to 404, so
knowing an id is not enough to act on it
(`TestCannotRemoveAnotherCompanysMember`, and the same at SQL level).

### Last-owner protection

Removing the final ACTIVE owner is rejected with **409 CONFLICT**. It would
leave a company nobody can administer — and because ownership can only be
granted at creation, no way to ever restore one.

Enforced in the service rather than by a database constraint, because it is a
rule about a `COUNT` that a `CHECK` cannot express. It runs **inside the
transaction**, so the count and the delete cannot interleave with a concurrent
removal of the other owner.

A PENDING owner does not count toward the total — they cannot administer
anything yet. `TestCountOwners` covers that.

Removal is a soft delete, so the history of who belonged to the company survives.

---

## 6. Listing

| Endpoint | Scope | Answers |
| --- | --- | --- |
| `GET /memberships` | company | "Who is in this company?" |
| `GET /memberships/mine` | user | "Where can I work?" |

`/memberships` requires an active company and is filtered by it. `/memberships/mine`
is deliberately **not** behind `RequireCompany`: it is what a user with no active
company calls to discover which ones they can switch to, so requiring a company
context would make it unreachable for exactly the callers who need it.

`/mine` is unpaginated by design — the result is bounded by construction (a
person belongs to a handful of companies, not thousands) and it backs a menu
that must render in one request. It returns the company inline so a client does
not make one request per membership, and returns an empty slice rather than
`null` so a client never has to handle both.

It excludes PENDING and SUSPENDED memberships (`TestMineExcludesPending`).

---

## 7. Audit events

| Event | When |
| --- | --- |
| `tenancy.member.invited` | An invitation is created. |
| `tenancy.member.removed` | A membership is revoked. |

Each carries `event_company_id`, `event_actor_id`, the membership id and the
role — **never** an email address. Audit records are forwarded to systems with
different access controls than the database, and the identifiers resolve to a
person when genuinely needed, which keeps the audit stream out of GDPR scope.
Verified by scanning the running service's logs.

`ActorID` is distinct from any subject in the attributes: "admin removed staff
member" needs both, and conflating them makes the trail unusable for the
questions it exists to answer.

---

## 8. Future RBAC integration

The next sprint adds permissions. The seams are already in place:

- `RequestContext.Role` is populated on every request.
- `RequestContext.MembershipID` names the specific grant, so a permission
  override attached to one membership has something to hang off.
- Routes are already split into tenant-optional and tenant-required groups, so a
  `middleware.RequireRole(...)` slots into the second group without touching
  handlers.

Expected shape:

```go
scoped.Use(middleware.RequireCompany())
scoped.DELETE("/companies/:id", middleware.RequireRole(entity.RoleOwner), companies.Delete)
```

Nothing in the entity, repository or service layer needs to change to support
that — which is the point of storing role now and enforcing it later.

When it lands, the operations that currently have no authorisation check should
be gated at minimum as: company update/delete → OWNER; member invite/remove →
OWNER or ADMIN; everything else → any ACTIVE member.
