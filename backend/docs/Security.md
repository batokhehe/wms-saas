# Security

What the system defends against, how, and — importantly — what it does not
defend against yet.

---

## 1. Credential storage

| Secret | Stored as | Why |
| --- | --- | --- |
| Password | bcrypt, cost ≥12 | Low-entropy: make each guess expensive. |
| Refresh token | SHA-256 hex | High-entropy: only needs to be unrecoverable. |
| Access token | not stored | Stateless by design. |
| JWT signing secret | env var, ≥32 chars | Never in code or the repository. |

The password/refresh split looks inconsistent and is not. bcrypt is slow to
defend *guessable* secrets; a refresh token is 256 bits of randomness with no
guessing attack to slow down, and the refresh path must look it up **by** its
hash — bcrypt's per-hash salt makes the digest unreproducible, so matching would
mean scanning every row. See Authentication.md §5.

**A leaked database yields no usable session and no usable password.** The
attacker holds bcrypt hashes and SHA-256 digests of values they cannot invert.

The `password_hash` column is `VARCHAR(60)` — exactly bcrypt's output length — so
a bug that writes a raw password there fails on length rather than storing it.

---

## 2. What is never logged

Verified by scanning the running service's logs during end-to-end testing:

- Passwords, in any form.
- Raw refresh tokens, or their hashes.
- Access tokens.
- Password hashes.

The raw refresh token is hashed on entry to `Refresh` and `Logout` and the raw
value is never used again, so it cannot reach a log line or an error message.

Domain events carry a user id, a timestamp and non-sensitive attributes — never
a credential, and not the email either. Audit records are forwarded to systems
with different access controls than the database; the email is resolvable from
the user id when it is genuinely needed, which keeps the audit stream out of
GDPR scope.

`dto.UserResponse` has **no field** capable of holding a password hash. The
guarantee is structural, not procedural.

---

## 3. Account enumeration

An unknown email and a wrong password produce the **identical** response:
`401 UNAUTHORIZED` / `"Invalid email or password"`.

Matching the message is not enough — the *timing* has to match too. A real
verification costs ~185ms of bcrypt; returning early for an unknown address
would take microseconds, and that gap is trivially measurable over the network.
When no account matches, the service compares the submitted password against a
constant dummy bcrypt hash so both paths burn the same time.

Measured on the running service:

```
0.182 0.183 0.190 0.186 0.189   unknown email
0.185 0.184 0.186 0.182 0.182   wrong password
```

The dummy hash is covered by a test asserting it is well-formed and its cost is
realistic — if it were malformed, bcrypt would fail fast and reopen the oracle.

`403 FORBIDDEN` for a locked account is only reachable by someone who has
already proved they know the password, so it reveals nothing to an attacker.

---

## 4. Token security

**Algorithm pinning.** The keyfunc rejects any non-HMAC signing method and
`WithValidMethods` restricts to HS256. This blocks the classic JWT attacks:
`"alg":"none"` and swapping HS256 for RS256 so a public key is treated as an
HMAC secret. Covered by a test that builds a real `alg=none` token.

**Issuer and audience are verified**, not merely stamped. They stop a token
minted by a sibling service — sharing the secret through a copy-paste of the
deployment config — from being accepted here.

**Expiry is required.** `WithExpirationRequired` means a bug that omits `exp`
fails verification rather than issuing a permanent credential.

**Token type is checked.** `typ: "access"` is enforced, so a stolen refresh token
cannot be presented as an API credential.

**Verification errors are generic.** Expired, bad signature, wrong audience and
malformed all return the same message. Distinguishing them tells an attacker
probing the endpoint which part of a forged token to fix next.

**Secrets are validated at boot.** Under 32 characters is rejected (HMAC-SHA256
tops out at a 256-bit key; shorter is brute-forceable offline from a single
captured token), as are known placeholders like `secret` and `changeme`.

**Bearer header only.** Never a query parameter — URLs land in access logs, proxy
logs, browser history and `Referer` headers.

---

## 5. Session security

**Rotation.** Every refresh revokes the presented token and issues a new one, so
a refresh token is single-use. This makes theft *detectable*: two holders cannot
both use one token.

**Reuse detection.** Presenting an already-rotated token revokes every session
for that user. Aggressive by design — once a token has been used twice, the
system cannot tell which holder is real.

Two subtleties, both found in testing rather than by inspection:

1. The revocation runs in **its own transaction**. The request is about to fail
   with a 401, and the first implementation did the revocation inside that
   request's transaction — so the rollback undid it and the attacker's stolen
   session survived. This is the kind of bug that reads as correct and is only
   caught by exercising the real path.
2. Rotation uses an **atomic conditional update** (`UPDATE ... WHERE revoked_at
   IS NULL`) and checks `RowsAffected`. Without it, two concurrent refreshes
   could both pass an `IsRevoked()` check and both mint a session. A lost race is
   indistinguishable from theft and is treated as reuse.

**Status re-checked on refresh.** An account locked five minutes ago stops
minting access tokens immediately, rather than at the end of the refresh token's
week-long life.

**Concurrent session cap** (default 10). At the cap the oldest session is
evicted rather than the login refused — refusing someone with ten legitimate
devices is a support ticket.

**Provenance captured** per session: device, IP (via `ClientIP()`, which honours
the trusted-proxy configuration — a raw `X-Forwarded-For` would be
client-controlled and would let an attacker forge their own audit trail) and
user agent. All untrusted, display-only, never used for authorisation.

---

## 6. Multi-tenant isolation

Authentication is deliberately company-independent (Authentication.md §1), so
there is no tenant boundary to enforce *within* this module.

The mechanism it must not break — and does not — is that every other module
reads `CompanyID` from `RequestContext` and never from a request field. The auth
middleware sets `CompanyID` to `nil`; Sprint 2 adds a second middleware that
populates it after resolving membership. No service or repository changes.

---

## 7. Transport and input

- Security headers on every response: `nosniff`, `X-Frame-Options: DENY`,
  `Referrer-Policy: no-referrer`, a maximally restrictive CSP, and HSTS in
  production.
- CORS is allow-list based and echoes one origin; `*` is rejected outright when
  `APP_ENV=production`.
- No trusted proxies by default — a deployment behind a load balancer must
  configure them explicitly, otherwise `X-Forwarded-For` is spoofable.
- All persistence goes through parameterised queries. The one place a value is
  interpolated into SQL is `ORDER BY`, which no driver can parameterise, and
  that is constrained by a per-endpoint allow-list (`pagination.Options`). The
  base repository refuses a pagination request that has not been validated.
- Request bodies are bound and validated before reaching a service; client
  strings are truncated to their column widths so a 10 KB User-Agent cannot fail
  an insert.
- An inbound `X-Request-ID` is honoured only if it parses as a UUID — otherwise
  a client could inject arbitrary text into every log line for its request.

---

## 8. Known gaps

Listed plainly because an undocumented gap is worse than an open one.

### High priority

**No rate limiting.** This is the top gap. `/auth/login` and `/auth/register` are
the only unauthenticated write endpoints in the system, which makes them the
natural target for credential stuffing. bcrypt at cost 12 also makes login a CPU
amplifier: a few hundred concurrent login attempts can saturate the service.
Redis and a `port.Cache` with an atomic `Increment` are already wired, so the
sliding-window counter has somewhere to live.

**No failed-login lockout.** `StatusLocked` exists and nothing sets it. There is
no attempt counter, so an attacker gets unlimited guesses against a single
account. Needs care: a naive implementation lets an attacker lock any account by
failing logins against it, so lockout should be per-IP-and-account, or use a
progressive delay rather than a hard lock.

**Access tokens cannot be revoked before expiry.** Inherent to stateless tokens.
Logout revokes the refresh token, but the access token stays valid for up to 15
minutes. A true "kill this session now" needs a `jti` deny-list in Redis checked
by the middleware — `jti` is already in every token for exactly this.

### Medium priority

**No expired-token cleanup.** `DeleteExpiredBefore` exists and nothing calls it,
so `refresh_tokens` grows without bound. Needs an Asynq job — which needs the
worker binary that does not exist yet.

**No password reset or email verification.** Both need email delivery. The
`email_verified_at` field and `IsEmailVerified()` predicate exist so that gating
on verification later is a change to `CanAuthenticate()`, not a migration on a
populated table.

**No secret rotation.** Changing `AUTH_JWT_SECRET` invalidates every access token
immediately. Zero-downtime rotation needs the verifier to accept a previous
secret during an overlap window.

**Audit events are log-only.** Durable enough for an audit trail, but not
queryable as structured records and not tamper-evident. Sprint 2's queue-backed
publisher plus an `audit_log` table is the path.

### Accepted

**Single signing secret for all tenants.** Correct while identity is
company-independent. Revisit only if per-tenant token isolation becomes a
requirement.

**bcrypt over Argon2id.** Deliberate; see Authentication.md §4.

**No MFA.** Out of scope for this sprint. The `users` table has room, and TOTP
would be additive.

---

## 9. Operational checklist before production

- [ ] `AUTH_JWT_SECRET` generated per environment (`openssl rand -base64 48`),
      stored in a secrets manager, never in the repository.
- [ ] `AUTH_PASSWORD_BCRYPT_COST` ≥12 and benchmarked on production hardware.
- [ ] `AUTH_JWT_ACCESS_TOKEN_TTL` ≤1h (validation enforces this).
- [ ] `DATABASE_SSL_MODE` not `disable` (validation enforces this).
- [ ] `HTTP_ALLOWED_ORIGINS` an explicit list, not `*` (validation enforces this).
- [ ] TLS terminated in front of the service; trusted proxies configured.
- [ ] Rate limiting in place on `/auth/*` — see §8.
- [ ] Alerting on the `refresh token reuse detected` warn line, which is a
      strong theft signal.
- [ ] Audit log retention decided; `component=audit` routed to its own index.
