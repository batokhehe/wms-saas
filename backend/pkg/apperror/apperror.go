// Package apperror is the single error vocabulary of the service.
//
// Three rules make it work:
//
//  1. Every layer returns *Error, or an error that wraps one. Business code
//     never returns a bare errors.New to a caller that will transport it.
//  2. Message is client-safe and Cause is not. The transport layer serialises
//     the first and logs the second, so a driver error can never leak to a
//     tenant.
//  3. Code and Status are chosen together by the constructors, so a handler
//     cannot pair a NOT_FOUND code with a 200 status.
package apperror

import (
	"errors"
	"fmt"
	"net/http"

	"go.uber.org/zap"
)

// Code is a stable, machine-readable error identifier.
//
// These are part of the public API contract: the Flutter client branches on
// them, so a released code may never change meaning. Add new codes rather than
// repurposing existing ones.
type Code string

const (
	CodeBadRequest       Code = "BAD_REQUEST"
	CodeValidation       Code = "VALIDATION_ERROR"
	CodeUnauthorized     Code = "UNAUTHORIZED"
	CodeForbidden        Code = "FORBIDDEN"
	CodeNotFound         Code = "NOT_FOUND"
	CodeMethodNotAllowed Code = "METHOD_NOT_ALLOWED"
	CodeConflict         Code = "CONFLICT"
	CodeUnprocessable    Code = "UNPROCESSABLE_ENTITY"
	CodeRateLimited      Code = "TOO_MANY_REQUESTS"
	CodeInternal         Code = "INTERNAL_ERROR"
	CodeUnavailable      Code = "SERVICE_UNAVAILABLE"
	CodeTimeout          Code = "TIMEOUT"
)

// Error is a domain error carrying everything the transport and logging layers
// need to handle it without inspecting the domain that produced it.
type Error struct {
	// Code is the machine-readable identifier sent to the client.
	Code Code
	// Message is human-readable and safe to expose to the caller.
	Message string
	// Status is the HTTP status the transport layer emits.
	Status int
	// Details carries structured, client-safe context: field-level validation
	// failures, the conflicting identifier, the retry-after hint.
	Details any
	// Op names the operation that failed ("product.service.Create"). It is
	// logged, never serialised, and makes a stack of wrapped errors readable.
	Op string
	// cause is the underlying error. Logged, never serialised.
	cause error
}

// Error implements the error interface. The rendering includes the cause, so an
// *Error passed to a plain logger still carries full diagnostic context.
func (e *Error) Error() string {
	var b []byte

	if e.Op != "" {
		b = append(b, e.Op...)
		b = append(b, ": "...)
	}

	b = append(b, e.Code...)
	b = append(b, ": "...)
	b = append(b, e.Message...)

	if e.cause != nil {
		b = append(b, ": "...)
		b = append(b, e.cause.Error()...)
	}

	return string(b)
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.cause }

// Is makes errors.Is compare by code, so callers can write
// errors.Is(err, apperror.ErrNotFound) regardless of message or cause.
func (e *Error) Is(target error) bool {
	var other *Error
	if !errors.As(target, &other) {
		return false
	}
	return e.Code == other.Code
}

// IsInternal reports whether this error represents a server-side fault, which
// is what decides log level and whether details are suppressed.
func (e *Error) IsInternal() bool { return e.Status >= http.StatusInternalServerError }

// The methods below return a copy rather than mutating in place. Sentinel
// errors are package-level values; mutating one would corrupt it for every
// other goroutine that shares it.

// WithCause attaches the underlying error for logging.
func (e *Error) WithCause(err error) *Error {
	clone := *e
	clone.cause = err
	return &clone
}

// WithDetails attaches structured, client-safe context.
func (e *Error) WithDetails(details any) *Error {
	clone := *e
	clone.Details = details
	return &clone
}

// WithMessage overrides the human-readable message.
func (e *Error) WithMessage(message string) *Error {
	clone := *e
	clone.Message = message
	return &clone
}

// WithOp records the failing operation, e.g. "inventory.repo.FindByID".
func (e *Error) WithOp(op string) *Error {
	clone := *e
	clone.Op = op
	return &clone
}

// WithMessagef overrides the message using a format string.
func (e *Error) WithMessagef(format string, args ...any) *Error {
	return e.WithMessage(fmt.Sprintf(format, args...))
}

// LogFields renders the error for zap.
//
// This is the logger integration point: every layer logs errors the same way,
// so a single query over `error_code` finds every occurrence of a failure class
// across the whole service.
func (e *Error) LogFields() []zap.Field {
	fields := []zap.Field{
		zap.String("error_code", string(e.Code)),
		zap.Int("error_status", e.Status),
		zap.String("error_message", e.Message),
	}

	if e.Op != "" {
		fields = append(fields, zap.String("error_op", e.Op))
	}
	if e.Details != nil {
		fields = append(fields, zap.Any("error_details", e.Details))
	}
	if e.cause != nil {
		fields = append(fields, zap.Error(e.cause))
	}

	return fields
}

// New builds an Error from its parts. Prefer the named constructors below;
// this exists for codes that need a non-standard status.
func New(code Code, status int, message string) *Error {
	return &Error{Code: code, Status: status, Message: message}
}

// Named constructors. Each pins the code/status pair so the two can never drift.

func BadRequest(message string) *Error {
	return New(CodeBadRequest, http.StatusBadRequest, message)
}

func Validation(message string) *Error {
	return New(CodeValidation, http.StatusUnprocessableEntity, message)
}

func Unauthorized(message string) *Error {
	return New(CodeUnauthorized, http.StatusUnauthorized, message)
}

func Forbidden(message string) *Error {
	return New(CodeForbidden, http.StatusForbidden, message)
}

func NotFound(message string) *Error {
	return New(CodeNotFound, http.StatusNotFound, message)
}

func MethodNotAllowed(message string) *Error {
	return New(CodeMethodNotAllowed, http.StatusMethodNotAllowed, message)
}

func Conflict(message string) *Error {
	return New(CodeConflict, http.StatusConflict, message)
}

func Unprocessable(message string) *Error {
	return New(CodeUnprocessable, http.StatusUnprocessableEntity, message)
}

func RateLimited(message string) *Error {
	return New(CodeRateLimited, http.StatusTooManyRequests, message)
}

func Internal(message string) *Error {
	return New(CodeInternal, http.StatusInternalServerError, message)
}

func Unavailable(message string) *Error {
	return New(CodeUnavailable, http.StatusServiceUnavailable, message)
}

func Timeout(message string) *Error {
	return New(CodeTimeout, http.StatusGatewayTimeout, message)
}

// Sentinels for errors.Is comparison. Because Is compares by code, these match
// any error of the same class regardless of its message.
var (
	ErrNotFound     = NotFound("not found")
	ErrUnauthorized = Unauthorized("unauthorized")
	ErrForbidden    = Forbidden("forbidden")
	ErrConflict     = Conflict("conflict")
	ErrValidation   = Validation("validation failed")
	ErrInternal     = Internal("internal error")
)

// From normalises any error into an *Error.
//
// Errors that did not originate here become opaque internal errors: the
// original is preserved as the cause for logging, but the client sees only a
// generic message. This is the single choke point that prevents a raw SQL or
// driver string from ever reaching a tenant.
func From(err error) *Error {
	if err == nil {
		return nil
	}

	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}

	return Internal("An unexpected error occurred").WithCause(err)
}

// Wrap converts err into an *Error and tags it with op, preserving the original
// classification if there is one. Use it at layer boundaries to build a
// readable trail without reclassifying the failure.
func Wrap(err error, op string) *Error {
	if err == nil {
		return nil
	}

	appErr := From(err)
	if appErr.Op == "" {
		return appErr.WithOp(op)
	}

	return appErr
}
