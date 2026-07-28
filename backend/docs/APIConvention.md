# API Convention

## 1. The response envelope

**Every endpoint returns exactly one shape. No exceptions, no custom JSON.**

Success:

```json
{
  "success": true,
  "message": "Resource created successfully",
  "data": { },
  "meta": {
    "request_id": "3f1a9c2e-...",
    "timestamp": "2026-07-22T07:11:22Z"
  }
}
```

Failure:

```json
{
  "success": false,
  "message": "Product not found",
  "error": {
    "code": "NOT_FOUND",
    "details": null
  },
  "meta": {
    "request_id": "3f1a9c2e-...",
    "timestamp": "2026-07-22T07:11:22Z"
  }
}
```

*Why:* the Flutter client writes one deserialiser and one error interceptor
instead of special-casing every endpoint, and a change to the response format
happens in one file.

`meta.request_id` is always present. A user can quote it in a support ticket and
an operator can find the exact request in the logs.

**The only permitted exception is `204 No Content`,** because HTTP forbids a body
on a 204.

---

## 2. Writing responses

Handlers call the `response` package and never `c.JSON`:

```go
response.OK(c, "Products retrieved successfully", data)
response.Created(c, "Product created successfully", data)
response.Accepted(c, "Import queued", data)     // work handed to the queue
response.List(c, "Products retrieved successfully", data, pagination)
response.NoContent(c)
response.Error(c, err)
```

Every one funnels through a single private `write` function, which is what
guarantees the envelope invariant holds.

Messages are sentence case, human-readable, and describe what happened. They are
**not** a substitute for `error.code` — clients branch on the code, not the
message, so messages can be reworded freely.

---

## 3. Pagination

List endpoints use `response.List` with `response.NewPagination`:

```json
"meta": {
  "request_id": "...",
  "timestamp": "...",
  "pagination": {
    "page": 2,
    "per_page": 25,
    "total": 137,
    "total_pages": 6,
    "has_next": true,
    "has_prev": true
  }
}
```

`NewPagination` derives `total_pages`, `has_next` and `has_prev`, so no module
gets the arithmetic subtly wrong.

Query parameters are `page` and `per_page`. **`per_page` must be capped** by a
binding tag (`max=100`): without a maximum, `per_page=1000000` turns one HTTP
request into a full table scan.

---

## 4. Versioning

All business endpoints live under `/api/v1`. Health probes are additionally
mounted at the unversioned root.

```
GET  /api/v1/products
POST /api/v1/products
GET  /health/ready          ← orchestrators, unversioned
```

A module declares its version by implementing `V1Registrar` / `V2Registrar`. It
never writes `/api/v1` in its own route file — the registry supplies the prefix.

### When to create v2

Only for a **breaking** change. These are not breaking and go into v1:

- adding a new endpoint
- adding an optional request field
- adding a response field

These are breaking and require v2:

- removing or renaming a response field
- making an optional request field required
- changing a field's type or semantics
- changing an error code for an existing condition

*Why the bar is high:* every version is maintained until its clients are gone,
and a mobile app that users decline to update can pin you to a version for
years.

---

## 5. Resource naming

- Collections are **plural nouns**: `/products`, `/warehouses`, `/stock-movements`.
- Multi-word paths use **kebab-case**.
- JSON fields use **snake_case**.
- No verbs in paths. `POST /products` creates; `/create-product` does not exist.
- Actions that are genuinely not CRUD become a sub-resource:
  `POST /stock-movements/:id/confirm`.

| Method   | Semantics                                 |
| -------- | ----------------------------------------- |
| `GET`    | Read. Never mutates.                       |
| `POST`   | Create, or a non-idempotent action.        |
| `PUT`    | Full replace.                              |
| `PATCH`  | Partial update.                            |
| `DELETE` | Remove (soft delete in this system).       |

---

## 6. Status codes

| Code | Used for                                     |
| ---- | -------------------------------------------- |
| 200  | Successful read, update or delete.             |
| 201  | Resource created.                              |
| 202  | Accepted for background processing.            |
| 204  | Success with no body.                          |
| 4xx  | Caller's fault. See `ErrorConvention.md`.      |
| 5xx  | Our fault.                                     |

Handlers never choose a status directly for errors — the `apperror` constructor
carries it.

---

## 7. Headers

| Header           | Direction | Purpose                                     |
| ---------------- | --------- | ------------------------------------------- |
| `X-Request-ID`   | both      | Correlation id. Echoed on every response.    |
| `Authorization`  | request   | `Bearer <jwt>` once auth exists.             |
| `X-Tenant-ID`    | request   | Reserved; allowed through CORS.              |

An inbound `X-Request-ID` is honoured **only if it is a well-formed UUID**.
Otherwise a new one is minted. *Why:* an unvalidated header would let a client
inject arbitrary text into every log line for its request.

---

## 8. Requests

- Request bodies are JSON. Validation is declared as binding tags on the DTO.
- Handlers bind with `validator.BindJSON` / `BindQuery` / `BindURI`, which
  return correctly classified errors — a malformed body is a 400, a rule
  violation is a 422.
- Path parameters that are IDs bind into a struct with `binding:"required,uuid"`,
  so a malformed id is rejected before it reaches the service.
- PATCH bodies use pointer fields, so "omitted" and "set to empty" differ.

---

## 9. Multi-tenancy

**The tenant is never a request parameter.** It comes from
`appcontext.RequestContext.CompanyID`, populated by authentication middleware.

An endpoint accepting `?company_id=` would be a cross-tenant data leak with a
query string as the exploit.

A resource belonging to another company returns `404`, not `403`: a 403 would
confirm the resource exists.

---

## 10. Security headers

Applied to every response by `middleware.SecurityHeaders`:

```
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: no-referrer
Content-Security-Policy: default-src 'none'; frame-ancestors 'none'
Strict-Transport-Security: max-age=31536000  (production only)
```

The API serves JSON only, so the CSP can be maximally restrictive. HSTS is
production-only because setting it in development would pin `localhost` to HTTPS
in the developer's browser.

CORS is allow-list based and echoes a single origin rather than `*`, because
browsers reject wildcard origins on credentialed requests. `*` is rejected
outright when `APP_ENV=production`.
