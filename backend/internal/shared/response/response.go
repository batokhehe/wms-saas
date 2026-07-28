// Package response defines the one and only JSON envelope this API emits.
//
// The contract is absolute: no handler may write its own JSON structure, and
// no handler may call c.JSON directly. A single shape means the Flutter client
// writes one deserialiser and one error interceptor instead of special-casing
// every endpoint, and it means response format changes happen in one file.
//
// Success:
//
//	{"success": true,  "message": "...", "data": {...}, "meta": {...}}
//
// Failure:
//
//	{"success": false, "message": "...", "error": {"code": "...", "details": ...}}
package response

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Envelope is the response body. Exactly one of Data or Error is populated.
type Envelope struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Error   *Body  `json:"error,omitempty"`
	Meta    *Meta  `json:"meta,omitempty"`
}

// Body is the error object. It carries only what is safe to expose: the
// machine-readable code and client-facing details. The wrapped cause stays in
// the logs.
type Body struct {
	Code    string `json:"code"`
	Details any    `json:"details,omitempty"`
}

// Meta carries envelope metadata. RequestID is always present so a user can
// quote one value in a support ticket and an operator can find the exact
// request; Pagination appears only on list endpoints.
//
// The paging shape lives in internal/shared/pagination rather than here,
// because the same struct describes both the response block and the arithmetic
// the repository layer performs. Defining it twice would let the two drift.
type Meta struct {
	RequestID  string               `json:"request_id,omitempty"`
	Timestamp  time.Time            `json:"timestamp"`
	Pagination *pagination.Metadata `json:"pagination,omitempty"`
}

// ---------- Success responses ----------

// OK writes 200 with data.
func OK(c *gin.Context, message string, data any) {
	write(c, http.StatusOK, Envelope{Success: true, Message: message, Data: data})
}

// Created writes 201 with data.
func Created(c *gin.Context, message string, data any) {
	write(c, http.StatusCreated, Envelope{Success: true, Message: message, Data: data})
}

// Accepted writes 202, for work handed to the queue rather than done inline.
func Accepted(c *gin.Context, message string, data any) {
	write(c, http.StatusAccepted, Envelope{Success: true, Message: message, Data: data})
}

// NoContent writes 204 with no body. It is the one legitimate exception to the
// envelope rule, because HTTP forbids a body on a 204.
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// List writes 200 with data and pagination metadata.
func List(c *gin.Context, message string, data any, meta pagination.Metadata) {
	envelopeMeta := newMeta(c)
	envelopeMeta.Pagination = &meta

	write(c, http.StatusOK, Envelope{
		Success: true,
		Message: message,
		Data:    data,
		Meta:    envelopeMeta,
	})
}

// Page writes 200 for a pagination.Page, which is what a service returns from a
// list use case. It keeps handlers from unpacking Items and Meta by hand.
func Page[T any](c *gin.Context, message string, page pagination.Page[T]) {
	List(c, message, page.Items, page.Meta)
}

// ---------- Error responses ----------

// Error writes the failure envelope for err and logs it.
//
// This is the only error exit a handler needs. It classifies the error, picks
// the status, decides what is safe to expose, and logs at the right level —
// so none of that logic is duplicated across handlers.
func Error(c *gin.Context, err error) {
	appErr := apperror.From(err)
	if appErr == nil {
		return
	}

	log := appcontext.FromGin(c).Logger

	// 5xx is our fault and pages someone; 4xx is the caller's and is only worth
	// a debug line. Logging client errors at warn or above is how alert fatigue
	// starts.
	if appErr.IsInternal() {
		log.Error("request failed", appErr.LogFields()...)
	} else {
		log.Debug("request rejected", appErr.LogFields()...)
	}

	body := &Body{Code: string(appErr.Code)}

	// Details on an internal error may contain diagnostic data that was never
	// vetted for a tenant's eyes, so they are dropped on the way out. They
	// remain in the log line above.
	if !appErr.IsInternal() {
		body.Details = appErr.Details
	}

	// Record the error on the Gin context so the access-log middleware can see
	// the outcome without re-deriving it.
	_ = c.Error(appErr) //nolint:errcheck // Gin returns the *gin.Error it stored.
	c.Abort()

	write(c, appErr.Status, Envelope{
		Success: false,
		Message: appErr.Message,
		Error:   body,
	})
}

// ---------- internals ----------

// write is the single point where a response body is serialised. Every exported
// helper funnels through it, which is what guarantees the envelope invariant.
func write(c *gin.Context, status int, env Envelope) {
	if env.Meta == nil {
		env.Meta = newMeta(c)
	}
	c.JSON(status, env)
}

func newMeta(c *gin.Context) *Meta {
	return &Meta{
		RequestID: appcontext.FromGin(c).RequestID,
		Timestamp: time.Now().UTC(),
	}
}
