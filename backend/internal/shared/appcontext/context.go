// Package appcontext carries per-request identity and correlation data through
// every layer of the application.
//
// Without this, request-scoped values get threaded through function signatures
// one parameter at a time — and the day tenancy is added, every service method
// in the codebase changes shape. RequestContext is the extension point: adding
// a field here does not alter a single existing signature.
package appcontext

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ginKey is the key under which the RequestContext is stored in *gin.Context.
// It is exported because middleware in another package sets it.
const ginKey = "app.request_context"

// GinKey returns the *gin.Context key holding the RequestContext.
func GinKey() string { return ginKey }

// ctxKey is an unexported type so no other package can collide with our
// context.Context key. Using a string here would risk a silent overwrite by any
// library that happens to use the same literal.
type ctxKey struct{}

// RequestContext is the identity and correlation data of a single request.
//
// CompanyID is the multi-tenant discriminator. It is a pointer and is nil until
// authentication lands, but every layer is already written against it — so
// enabling tenancy later is a change to the middleware that populates it, not a
// change to the repositories that read it.
type RequestContext struct {
	// RequestID correlates every log line and response for this request.
	RequestID string

	// CompanyID is the tenant that owns this request. Nil until auth exists.
	CompanyID *uuid.UUID

	// UserID is the authenticated principal. Nil until auth exists.
	UserID *uuid.UUID

	// Role is the principal's role WITHIN the active company. It is a property
	// of the membership, not of the user: the same person can be OWNER of their
	// own company and STAFF at a client's, so this changes when the active
	// company changes.
	//
	// Populated but not yet enforced — RBAC is the next sprint.
	Role string

	// MembershipID identifies the membership that granted the active company
	// context. Nil until a company is resolved.
	//
	// It is carried alongside CompanyID rather than being re-derived because
	// authorisation checks, audit records and "remove this member" all need to
	// name the specific grant, not just the tenant. Re-querying it in each of
	// those places would mean the same lookup three times per request.
	MembershipID *uuid.UUID

	// Logger is pre-tagged with the fields above, so anything logged through it
	// is automatically attributable to this request, tenant and user.
	Logger *zap.Logger

	// StartedAt is when the request entered the server, for latency accounting.
	StartedAt time.Time

	// ClientIP and UserAgent support audit trails and abuse investigation.
	ClientIP  string
	UserAgent string
}

// New builds a RequestContext and derives its tagged logger.
func New(requestID string, base *zap.Logger) *RequestContext {
	rc := &RequestContext{
		RequestID: requestID,
		StartedAt: time.Now(),
	}

	rc.Logger = base.With(zap.String("request_id", requestID))

	return rc
}

// WithTenant attaches tenant and principal identity, refreshing the logger so
// later log lines carry it. Authentication middleware will call this; nothing
// else should.
func (rc *RequestContext) WithTenant(companyID, userID *uuid.UUID, role string) {
	rc.CompanyID = companyID
	rc.UserID = userID
	rc.Role = role

	fields := make([]zap.Field, 0, 3)
	if companyID != nil {
		fields = append(fields, zap.String("company_id", companyID.String()))
	}
	if userID != nil {
		fields = append(fields, zap.String("user_id", userID.String()))
	}
	if role != "" {
		fields = append(fields, zap.String("role", role))
	}

	if len(fields) > 0 {
		rc.Logger = rc.Logger.With(fields...)
	}
}

// WithCompany attaches the active tenant resolved from a membership.
//
// This is deliberately separate from WithTenant rather than an extension of it.
// The two are called by different middleware at different points for different
// reasons: authentication establishes WHO the caller is and knows nothing about
// companies, while company resolution establishes WHERE they are acting and
// runs only after authentication has succeeded.
//
// Keeping them apart is what allowed multi-tenancy to be added without touching
// a line of the authentication middleware.
func (rc *RequestContext) WithCompany(companyID, membershipID uuid.UUID, role string) {
	rc.CompanyID = &companyID
	rc.MembershipID = &membershipID
	rc.Role = role

	rc.Logger = rc.Logger.With(
		zap.String("company_id", companyID.String()),
		zap.String("membership_id", membershipID.String()),
		zap.String("role", role),
	)
}

// IsAuthenticated reports whether a principal is attached.
func (rc *RequestContext) IsAuthenticated() bool {
	return rc != nil && rc.UserID != nil
}

// HasTenant reports whether a company is attached. Repositories use this to
// decide whether a tenant filter can be applied.
func (rc *RequestContext) HasTenant() bool {
	return rc != nil && rc.CompanyID != nil
}

// CompanyIDOrNil returns the tenant id as a string, or "" when absent. It keeps
// nil-checking out of logging and query-building call sites.
func (rc *RequestContext) CompanyIDOrNil() string {
	if !rc.HasTenant() {
		return ""
	}
	return rc.CompanyID.String()
}

// Elapsed returns how long the request has been running.
func (rc *RequestContext) Elapsed() time.Duration {
	return time.Since(rc.StartedAt)
}

// Into stores rc in a standard context.Context.
//
// Services and repositories receive context.Context, never *gin.Context. That
// is what keeps the business layers free of any HTTP dependency and callable
// from the Asynq worker, a CLI command or a test with equal ease.
func Into(ctx context.Context, rc *RequestContext) context.Context {
	return context.WithValue(ctx, ctxKey{}, rc)
}

// From retrieves the RequestContext from ctx.
//
// It never returns nil: a caller running outside an HTTP request (a background
// job, a migration, a test) gets a usable fallback rather than a panic.
func From(ctx context.Context) *RequestContext {
	if ctx != nil {
		if rc, ok := ctx.Value(ctxKey{}).(*RequestContext); ok && rc != nil {
			return rc
		}
	}

	return &RequestContext{
		RequestID: "",
		Logger:    zap.NewNop(),
		StartedAt: time.Now(),
	}
}

// Logger returns the request-scoped logger from ctx, or a no-op logger.
func Logger(ctx context.Context) *zap.Logger {
	return From(ctx).Logger
}

// RequestID returns the correlation id from ctx, or "" outside a request.
func RequestID(ctx context.Context) string {
	return From(ctx).RequestID
}

// CompanyID returns the tenant id from ctx, or nil.
func CompanyID(ctx context.Context) *uuid.UUID {
	return From(ctx).CompanyID
}

// RequireTenant returns the active company id, or an error when no company
// context has been resolved.
//
// Every tenant-scoped service method starts here. Returning an error rather
// than a nil pointer means a caller cannot proceed with an unscoped query by
// forgetting a nil check — the tenant filter is not optional, so neither is
// obtaining it.
func (rc *RequestContext) RequireTenant() (uuid.UUID, error) {
	if !rc.HasTenant() {
		return uuid.Nil, apperror.Forbidden("A company context is required").
			WithOp("appcontext.RequireTenant")
	}
	return *rc.CompanyID, nil
}

// RequireUser returns the authenticated principal, or an error.
func (rc *RequestContext) RequireUser() (uuid.UUID, error) {
	if !rc.IsAuthenticated() {
		return uuid.Nil, apperror.Unauthorized("Authentication is required").
			WithOp("appcontext.RequireUser")
	}
	return *rc.UserID, nil
}
