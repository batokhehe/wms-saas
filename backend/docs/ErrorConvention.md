# Error Convention

All errors flow through `pkg/apperror`. Three rules make it work:

1. **Every layer returns `*apperror.Error`, or an error wrapping one.** Business
   code never returns a bare `errors.New` to a caller that will transport it.
2. **`Message` is client-safe; `cause` is not.** The transport layer serialises
   the first and logs the second, so a driver error can never leak to a tenant.
3. **`Code` and `Status` are chosen together** by the named constructors, so a
   handler cannot pair a `NOT_FOUND` code with a 200 status.

---

## 1. The type

```go
type Error struct {
    Code    Code    // machine-readable, sent to the client
    Message string  // human-readable, safe to expose
    Status  int     // HTTP status the transport layer emits
    Details any     // structured client-safe context
    Op      string  // failing operation, logged only
    cause   error   // wrapped underlying error, logged only
}
```

`Op` and `cause` never reach the client. `Details` reaches the client for 4xx
and is **suppressed for 5xx**, because internal-error details have not been
vetted for a tenant's eyes.

---

## 2. Codes

| Code                 | Status | When                                              |
| -------------------- | ------ | ------------------------------------------------- |
| `BAD_REQUEST`        | 400    | Malformed body, unparseable input.                 |
| `UNAUTHORIZED`       | 401    | Missing or invalid credentials.                    |
| `FORBIDDEN`          | 403    | Authenticated but not permitted.                   |
| `NOT_FOUND`          | 404    | Resource does not exist (or not in this tenant).   |
| `METHOD_NOT_ALLOWED` | 405    | Wrong HTTP verb for the route.                     |
| `CONFLICT`           | 409    | Uniqueness or FK violation, concurrent update.     |
| `VALIDATION_ERROR`   | 422    | Well-formed but breaks a rule.                     |
| `UNPROCESSABLE_ENTITY` | 422  | Semantically invalid but not field-level.          |
| `TOO_MANY_REQUESTS`  | 429    | Rate limit exceeded.                               |
| `INTERNAL_ERROR`     | 500    | Unexpected server fault.                           |
| `SERVICE_UNAVAILABLE`| 503    | A dependency is down.                              |
| `TIMEOUT`            | 504    | An upstream operation timed out.                   |

**Codes are part of the public API contract.** The Flutter client branches on
them. A released code may never change meaning — add a new one instead.

`404` for a resource that exists in another tenant is deliberate: `403` would
confirm the resource exists, which is an information leak across companies.

---

## 3. Creating errors

```go
apperror.NotFound("Product not found")
apperror.Conflict("A product with this SKU already exists")
apperror.Forbidden("You do not have access to this warehouse")
```

Enrich with the fluent methods. Each returns a **copy** — sentinels are
package-level values, and mutating one would corrupt it for every goroutine that
shares it:

```go
return apperror.Conflict("A resource with this name already exists").
    WithOp("product.service.Create").
    WithDetails(map[string]any{"sku": req.SKU}).
    WithCause(err)
```

| Method            | Purpose                                        |
| ----------------- | ---------------------------------------------- |
| `WithCause(err)`  | Attach the underlying error. **Logged only.**   |
| `WithDetails(v)`  | Client-safe structured context.                 |
| `WithOp(s)`       | Failing operation, `"module.layer.Method"`.     |
| `WithMessage(s)`  | Override the human-readable message.            |

---

## 4. Comparing errors

`Error.Is` compares by **code**, so sentinels match regardless of message:

```go
if errors.Is(err, apperror.ErrNotFound) {
    // any NOT_FOUND, whatever its message or cause
}
```

Available sentinels: `ErrNotFound`, `ErrUnauthorized`, `ErrForbidden`,
`ErrConflict`, `ErrValidation`, `ErrInternal`.

---

## 5. Database errors

Repositories never return a raw GORM error:

```go
err := r.db.WithContext(ctx).Create(resource).Error
return postgres.TranslateError(err, "product.repository.Create")
```

`TranslateError` maps:

| GORM / driver error       | Becomes                       |
| ------------------------- | ----------------------------- |
| `ErrRecordNotFound`       | `NOT_FOUND` (404)              |
| `ErrDuplicatedKey`        | `CONFLICT` (409)               |
| `ErrForeignKeyViolated`   | `CONFLICT` (409)               |
| `context.Canceled`        | `TIMEOUT` (499, client hung up)|
| `context.DeadlineExceeded`| `TIMEOUT` (504)                |
| anything else             | `INTERNAL_ERROR` (500)         |

An error already classified upstream keeps its classification, so translating
twice is harmless.

`context.Canceled` maps to 499 rather than 500 deliberately: the caller
disconnected, nothing is broken, and logging it as a server fault would generate
false alerts.

---

## 6. Validation errors

```go
if err := validator.BindJSON(c, &req); err != nil {
    response.Error(c, err)
    return
}
```

`apperror.FromValidator` converts go-playground failures into a 422 with
per-field details:

```json
{
  "success": false,
  "message": "The request contains invalid fields",
  "error": {
    "code": "VALIDATION_ERROR",
    "details": {
      "fields": [
        { "field": "sku",      "rule": "required", "message": "sku is required" },
        { "field": "quantity", "rule": "gte",      "message": "quantity must be greater than or equal to 0" }
      ]
    }
  }
}
```

Field names are the **JSON** names, not Go struct names — a client that sent
`sku_code` must not be told that `SKUCode` is invalid, a name appearing nowhere
in its request.

A `validator.InvalidValidationError` (nil or non-struct target) is a programming
bug, not user input, and becomes a 500 rather than a 422.

---

## 7. Logging

`response.Error` logs every error exactly once, at the right level:

```go
if appErr.IsInternal() {
    log.Error("request failed", appErr.LogFields()...)   // 5xx: pages someone
} else {
    log.Debug("request rejected", appErr.LogFields()...) // 4xx: caller's fault
}
```

*Why the split:* logging client errors at warn or above is how alert fatigue
starts. A tenant sending a malformed body is not an incident.

`LogFields()` emits `error_code`, `error_status`, `error_message`, `error_op`,
`error_details` and the wrapped cause — so one query over `error_code` finds
every occurrence of a failure class across the whole service.

**Do not log an error and also return it.** The layer that converts it to a
response logs it. Logging at every layer produces the same failure five times
with five different stack positions.

---

## 8. Panics

`middleware.Recovery` converts a panic into `INTERNAL_ERROR` through
`response.Error`, so a bug produces exactly the same envelope as any other 500.
The panic value becomes the wrapped cause: it reaches the logs, never the
client.

A severed connection (broken pipe, connection reset) is logged at warn and
aborted without a response — the socket is already gone, and it is not an
incident.

---

## 9. Anti-patterns

```go
// WRONG: leaks the driver message to the client
return fmt.Errorf("failed to insert product: %w", err)

// WRONG: code and status can drift apart
c.JSON(500, gin.H{"error": "not found"})

// WRONG: mutates a shared sentinel
apperror.ErrNotFound.Message = "product missing"

// WRONG: logged here and again by response.Error
log.Error("create failed", zap.Error(err))
return err

// RIGHT
return apperror.NotFound("Product not found").
    WithOp("product.service.Get").
    WithCause(err)
```
