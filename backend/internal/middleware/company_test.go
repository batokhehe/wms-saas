package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// stubResolver returns a fixed answer, so these tests exercise the middleware's
// own behaviour rather than the tenancy module's resolution logic (which has
// its own tests).
type stubResolver struct {
	result CompanyContext
	err    error

	gotUserID    uuid.UUID
	gotRequested uuid.UUID
	called       bool
}

func (s *stubResolver) Resolve(
	_ context.Context, userID, requestedID uuid.UUID,
) (CompanyContext, error) {
	s.called = true
	s.gotUserID = userID
	s.gotRequested = requestedID
	return s.result, s.err
}

// run drives a request through the middleware chain and reports what happened.
func run(
	t *testing.T,
	resolver CompanyResolver,
	userID *uuid.UUID,
	header string,
	extra ...gin.HandlerFunc,
) (*httptest.ResponseRecorder, *appcontext.RequestContext) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	engine := gin.New()

	var captured *appcontext.RequestContext

	// Stand in for RequestContext + Authenticate middleware.
	engine.Use(func(c *gin.Context) {
		rc := appcontext.New("test-request", zap.NewNop())
		if userID != nil {
			rc.WithTenant(nil, userID, "")
		}
		appcontext.SetGin(c, rc)
		c.Next()
	})

	engine.Use(ResolveCompany(resolver))
	for _, mw := range extra {
		engine.Use(mw)
	}

	engine.GET("/probe", func(c *gin.Context) {
		captured = appcontext.FromGin(c)
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	if header != "" {
		req.Header.Set(HeaderCompanyID, header)
	}
	engine.ServeHTTP(rec, req)

	return rec, captured
}

func TestResolveCompanyInjectsContext(t *testing.T) {
	companyID, membershipID, userID := uuid.New(), uuid.New(), uuid.New()

	resolver := &stubResolver{result: CompanyContext{
		CompanyID: companyID, MembershipID: membershipID, Role: "OWNER",
	}}

	rec, rc := run(t, resolver, &userID, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rc == nil || !rc.HasTenant() {
		t.Fatal("no company was injected into the RequestContext")
	}
	if *rc.CompanyID != companyID {
		t.Errorf("CompanyID = %s, want %s", rc.CompanyID, companyID)
	}
	if rc.MembershipID == nil || *rc.MembershipID != membershipID {
		t.Errorf("MembershipID = %v, want %s", rc.MembershipID, membershipID)
	}
	if rc.Role != "OWNER" {
		t.Errorf("Role = %q, want OWNER", rc.Role)
	}
}

// TestResolveCompanyPassesHeaderThrough proves an explicit choice reaches the
// resolver rather than being dropped.
func TestResolveCompanyPassesHeaderThrough(t *testing.T) {
	companyID, userID := uuid.New(), uuid.New()

	resolver := &stubResolver{result: CompanyContext{
		CompanyID: companyID, MembershipID: uuid.New(), Role: "STAFF",
	}}

	run(t, resolver, &userID, companyID.String())

	if resolver.gotRequested != companyID {
		t.Errorf("resolver got requested = %s, want %s", resolver.gotRequested, companyID)
	}
	if resolver.gotUserID != userID {
		t.Errorf("resolver got user = %s, want %s", resolver.gotUserID, userID)
	}
}

// TestResolveCompanySkipsUnauthenticated: there is no tenant to resolve for a
// caller with no principal, and rejecting here would duplicate Authenticate.
func TestResolveCompanySkipsUnauthenticated(t *testing.T) {
	resolver := &stubResolver{}

	rec, rc := run(t, resolver, nil, "")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if resolver.called {
		t.Error("the resolver ran for an unauthenticated request")
	}
	if rc.HasTenant() {
		t.Error("a tenant was injected for an unauthenticated request")
	}
}

// TestResolveCompanyProceedsWithoutContext: no usable membership is not a
// failure at this layer — RequireCompany is the enforcing half.
func TestResolveCompanyProceedsWithoutContext(t *testing.T) {
	userID := uuid.New()
	resolver := &stubResolver{err: ErrNoCompanyContext}

	rec, rc := run(t, resolver, &userID, "")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 — no company is not itself an error", rec.Code)
	}
	if rc.HasTenant() {
		t.Error("a tenant was injected despite ErrNoCompanyContext")
	}
}

// TestResolveCompanySurfacesForbidden: a named-but-inaccessible company must
// NOT be downgraded to no-context, which would turn an attempted cross-tenant
// access into a confusing success on the wrong tenant.
func TestResolveCompanySurfacesForbidden(t *testing.T) {
	userID, companyID := uuid.New(), uuid.New()

	resolver := &stubResolver{
		err: apperror.Forbidden("You do not have access to this company"),
	}

	rec, _ := run(t, resolver, &userID, companyID.String())

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body["success"] != false {
		t.Error("expected the standard error envelope")
	}
}

// TestResolveCompanyRejectsMalformedHeader: silently falling back to the
// default company would let a typo route a write into the wrong tenant — the
// single worst failure mode in a multi-tenant system.
func TestResolveCompanyRejectsMalformedHeader(t *testing.T) {
	userID := uuid.New()
	resolver := &stubResolver{result: CompanyContext{CompanyID: uuid.New()}}

	rec, _ := run(t, resolver, &userID, "not-a-uuid")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if resolver.called {
		t.Error("the resolver ran with a malformed header; it should be rejected first")
	}
}

func TestRequireCompanyRejectsMissingTenant(t *testing.T) {
	userID := uuid.New()
	resolver := &stubResolver{err: ErrNoCompanyContext}

	rec, _ := run(t, resolver, &userID, "", RequireCompany())

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	errBody, _ := body["error"].(map[string]any)
	if errBody["code"] != "FORBIDDEN" {
		t.Errorf("error.code = %v, want FORBIDDEN", errBody["code"])
	}

	// The message must tell the client how to fix it.
	if msg, _ := body["message"].(string); msg == "" ||
		!containsHeaderHint(msg) {
		t.Errorf("message %q does not tell the client to send %s", msg, HeaderCompanyID)
	}
}

func TestRequireCompanyAllowsResolvedTenant(t *testing.T) {
	userID := uuid.New()
	resolver := &stubResolver{result: CompanyContext{
		CompanyID: uuid.New(), MembershipID: uuid.New(), Role: "ADMIN",
	}}

	rec, _ := run(t, resolver, &userID, "", RequireCompany())

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func containsHeaderHint(msg string) bool {
	for i := 0; i+len(HeaderCompanyID) <= len(msg); i++ {
		if msg[i:i+len(HeaderCompanyID)] == HeaderCompanyID {
			return true
		}
	}
	return false
}
