# Authentication

The auth module is the **identity foundation** of the platform, not a login
endpoint. This document explains how it works and why each decision was made.

---

## 1. Identity is independent of Company

This is the single most important design decision in the module, and it is
deliberate rather than a gap awaiting Sprint 2.

- `entity.User` has **no** `CompanyID`, and never will.
- Access tokens carry **no** company claim.
- No repository method takes a `companyID`.
- `RequestContext.CompanyID` stays `nil` after authentication.

### Why

**A person can belong to several companies.** A 3PL operator legitimately works
for multiple clients; a logistics manager oversees two subsidiaries. Binding
identity to a tenant would force one account per company, which means duplicate
credentials, duplicate password resets, and no single account to lock when that
person leaves the business.

**Authentication has to work before a company context exists.** You log in, and
*then* select or are assigned a company. A tenant-scoped login is a chicken-and-egg
problem.

**Tokens would need reissuing on every company switch.** With the company in the
token, switching context means a new token; without it, switching is a request
header or a separate short-lived scoped token.

### How Sprint 2 attaches without changing this module

A `memberships` table joins users to companies. A second middleware resolves the
active company and calls `rc.WithTenant(companyID, userID, role)` — the same
method the auth middleware already calls with a nil company.

Every service and repository in the codebase already reads `CompanyID` from the
request context, so nothing below the middleware layer changes.

---

## 2. Token model

Two tokens, with different properties, because one token cannot be both fast to
verify and revocable.

| | Access token | Refresh token |
| --- | --- | --- |
| Format | JWT (HS256) | 256 bits of random, base64url |
| Storage | none — stateless | SHA-256 digest in `refresh_tokens` |
| Lifetime | 15 minutes | 7 days |
| Verified by | signature check, no I/O | database lookup |
| Revocable | **no** | yes |
| Sent on | every API request | refresh and logout only |

The access token is stateless so that authenticating a request costs no database
round trip. The price is that it **cannot be revoked**, which is exactly why its
TTL is short: 15 minutes is the window during which a stolen one remains useful.
Production configuration caps it at one hour.

The refresh token is the revocation point. It is stored server-side, so logout,
rotation and reuse detection all work.

### Access token claims

```json
{
  "iss": "wms-saas",
  "sub": "<user uuid>",
  "aud": ["wms-saas-api"],
  "exp": 1784713607,
  "nbf": 1784712707,
  "iat": 1784712707,
  "jti": "<uuid>",
  "typ": "access"
}
```

**Nothing else.** No email, no name, no role, no company. A JWT is signed but
**not encrypted**: every claim is readable by anyone holding the token, and the
token lives in client storage, proxy logs and browser history. Personal data in
a token is personal data in all of those places.

`typ` matters: without it a refresh token would verify as an access token, so
stealing one would yield an API credential rather than merely a session.

`jti` is present so a future deny-list can revoke one specific access token
without invalidating every session.

### Why HS256 rather than RS256

There is one issuer and one verifier, both in this process. Asymmetric signing
would add key distribution and rotation complexity for no benefit. If a separate
service ever needs to *verify* tokens without being able to *mint* them, that is
the point to switch — and it is a change confined to `service/token.go`.

---

## 3. Flows

### Register

```
validate → hash password → create user → issue access token
        → generate + store refresh token → return both
```

The whole thing is one transaction. Without it, a failure between creating the
user and storing the session would leave an account whose caller believes
registration failed — and whose email is now taken, so their retry fails with a
conflict they cannot resolve.

### Login

```
find user → verify password → check status → issue access token
         → generate + store refresh token → record last_login_at
```

Note what login does **not** do: revoke the caller's other sessions. A warehouse
operator legitimately holds sessions on a handheld scanner, a desktop and a
phone simultaneously. Sessions end on logout, rotation, expiry, or eviction at
the concurrent-session cap.

Status is checked **after** the password, deliberately. Checking first would let
anyone discover which accounts are locked without knowing a password.

### Refresh (rotation)

```
hash presented token → look up → reuse check → expiry check
                    → re-check account status → revoke old → issue new pair
```

**Refresh tokens are single-use.** Each refresh revokes the presented token and
issues a new one. That is what makes theft *detectable*: the legitimate client
and an attacker cannot both use the same token, so a second presentation proves
one of them is not the owner.

Account status is re-checked on every refresh, not just at login. An account
locked five minutes ago must not keep minting access tokens for the rest of the
refresh token's week-long life.

### Reuse detection

Presenting an already-revoked refresh token triggers **revocation of every
session for that user**, including the legitimate client's current one.

That is aggressive on purpose. Once a token has been used twice, the system
cannot tell which holder is the real user, and the safe assumption is that the
session family is compromised. The legitimate user is forced to log in again;
the attacker loses everything.

Two implementation details that are easy to get wrong and were both caught in
testing:

1. **The revocation runs in its own transaction.** The request it belongs to is
   about to fail with a 401, and doing the revocation inside that request's
   transaction rolls it back at the moment it matters most. The first
   implementation had exactly this bug: the attacker's stolen session survived.
2. **The rotation itself uses a conditional update.** `RevokeIfLive` is an
   atomic `UPDATE ... WHERE revoked_at IS NULL` and reports whether it performed
   the transition. Without it, two concurrent refreshes with the same token
   could both pass an `IsRevoked()` check and both mint a session. A lost race
   is indistinguishable from theft and is treated as reuse.

### Logout

```
hash token → find → revoke (or revoke all sessions)
```

Idempotent, and never reports failure for an unknown or already-revoked token. A
logout that errors leaves the client unsure whether it is signed out, and the
usual response is to retry — while a 404 would also confirm to an attacker which
stolen tokens are still live.

**The access token stays valid until it expires.** That is inherent to stateless
tokens. It is why the access TTL is short, and why a true "kill this session
now" capability would need an access-token deny-list in Redis — see Security.md.

---

## 4. Password handling

**bcrypt**, cost configurable, default and production floor 12.

bcrypt rather than SHA-256 or PBKDF2 because passwords are **low-entropy**
secrets: the defence is making each guess expensive. bcrypt is deliberately
slow, resists GPU parallelism reasonably well, and embeds its cost in each hash
— so the factor can be raised later without invalidating existing hashes.

Argon2id would be a defensible modern alternative. bcrypt was chosen for its
maturity, its presence in the Go extended standard library, and the fact that
its single tuning knob is hard to misconfigure; Argon2's three interacting
parameters are easy to get wrong in a way that looks fine.

### Complexity policy

Minimum 8 **bytes**, maximum 72 **bytes**, with at least one uppercase letter,
one lowercase letter, one digit and one special character.

The 72-byte cap is not stylistic: **bcrypt ignores everything past 72 bytes.**
Without the cap, a user with a 100-character passphrase would silently have the
last 28 characters ignored, and any longer password sharing that prefix would
authenticate. Length is measured in bytes because that is what bcrypt counts —
20 emoji is 20 runes but 80 bytes.

All rules are checked together and **every** violation is reported. A form that
rejects a password once for "needs a digit" and again for "needs a symbol"
trains people to pick the weakest thing that finally passes.

---

## 5. Refresh token storage

Stored as a **SHA-256 hex digest**, never as the value handed to the client. A
database leak yields no usable session.

### Why SHA-256 here but bcrypt for passwords

This looks inconsistent and is not:

- bcrypt's slowness defends *guessable* secrets. A refresh token is 256 bits of
  cryptographic randomness — there is no guessing attack to slow down.
- The refresh path must look the token up **by** its hash. bcrypt embeds a
  random per-hash salt, so the digest is not reproducible from the input; finding
  a match would mean scanning every row and bcrypt-comparing each one. SHA-256
  is deterministic, so the lookup is a single indexed equality.

Using bcrypt for refresh tokens would make the hot path O(n) in sessions with no
security benefit.

---

## 6. Middleware and the module boundary

`middleware.Authenticate` extracts the bearer token, verifies it, and injects the
principal into `RequestContext`.

It does **not** import the auth module. Instead `middleware` declares the
interface it needs:

```go
type TokenVerifier interface {
    VerifyAccessToken(raw string) (Principal, error)
}
```

Bootstrap injects the auth module's verifier, which satisfies it. This is the
consumer-side interface pattern required by `ModuleConvention.md` §6, and it
matters here more than anywhere else: if `middleware` imported `auth`, every
module using authentication would transitively depend on auth's internals, and
the "modules never import each other" rule would be broken by the one dependency
every module eventually needs.

Only the `Authorization` header is accepted — never a query parameter. URLs land
in access logs, proxy logs, browser history and `Referer` headers.

---

## 7. Account status

| Status | Can log in | Meaning |
| --- | --- | --- |
| `ACTIVE` | yes | Normal. |
| `INACTIVE` | no | Deactivated — offboarded, unfinished invitation. |
| `LOCKED` | no | Security hold — failed logins, suspected compromise. |

`INACTIVE` and `LOCKED` are distinct because the remedy differs: unlocking is a
security decision, reactivating is an administrative one. Collapsing them into a
single `disabled` flag loses that.

Nothing currently sets `LOCKED` automatically — there is no failed-login counter
yet. See Security.md.

---

## 8. Domain events

Published, not handled. Five events:

`auth.user.registered`, `auth.user.logged_in`, `auth.user.logged_out`,
`auth.password.changed`, `auth.refresh_token.rotated`

They carry a user id, a timestamp, the request id and non-sensitive attributes —
**never** a token, a hash, a password or an email. Audit records are forwarded to
systems with different access controls than the database; email is a personal
identifier and is resolvable from the user id when actually needed.

### Why the structured log rather than the Asynq queue

The queue is the obvious choice and is the wrong one *today*. Asynq fails a task
whose type has no registered handler, retries it, and archives it — so with no
consumer implemented, every login would generate a failed task and an operator
would open a dashboard full of red. Publishing into a broker with no subscriber
manufactures alert noise, not an audit trail.

The structured log is a real audit sink: lines already ship to a central store,
already carry `request_id`, `service` and `env`, and are already queryable. They
are tagged `component=audit` so they can be routed to their own index and
retained on a different schedule.

`EventPublisher` is an interface. When Sprint 2 adds handlers, a
`QueueEventPublisher` backed by `port.Queue` is one new type and one line in
`module.go`; no service code changes.

`Publish` returns no error, deliberately: an audit write must never fail the
business operation that produced it.

---

## 9. Configuration

| Key | Default | Notes |
| --- | --- | --- |
| `AUTH_JWT_SECRET` | **none** | ≥32 chars. Placeholders rejected at boot. |
| `AUTH_JWT_ISSUER` | `wms-saas` | Checked on every verification. |
| `AUTH_JWT_AUDIENCE` | `wms-saas-api` | Checked on every verification. |
| `AUTH_JWT_ACCESS_TOKEN_TTL` | `15m` | Capped at `1h` in production. |
| `AUTH_JWT_REFRESH_TOKEN_TTL` | `168h` | Must exceed the access TTL. |
| `AUTH_JWT_CLOCK_SKEW` | `30s` | Leeway on `exp`/`nbf`. |
| `AUTH_JWT_MAX_SESSIONS_PER_USER` | `10` | 0 = unlimited. |
| `AUTH_PASSWORD_BCRYPT_COST` | `12` | Minimum 12 in production. |

The signing secret has **no default**, exactly like `DATABASE_PASSWORD`. A
credential must never have a fallback: a deploy that forgets to set it has to
fail loudly rather than quietly sign tokens with a value that is in the
repository. Known placeholders (`secret`, `changeme`, …) are rejected by name.

`cmd/migrate` loads config with `config.WithoutAuth()`. The migration runner
touches only the database, and requiring it to carry a signing secret would mean
either a placeholder credential in the deployment manifest or granting a
schema-migration job access to the real one.

---

## 10. What is deliberately not here

| Not implemented | Why |
| --- | --- |
| Company / membership | Sprint 2. Section 1 explains the independence. |
| RBAC | Needs membership; `Principal.Role` is the seam. |
| Email verification | Field and predicate exist; no flow gates on it yet. |
| Password reset | Needs email delivery. |
| Failed-login lockout | `LOCKED` exists; nothing sets it. See Security.md. |
| Rate limiting | See Security.md — this is the top gap. |
| Session listing / revoke-one | Data is captured (device, IP, UA); no endpoint. |
| Expired-token cleanup job | `DeleteExpiredBefore` exists; nothing calls it. |
