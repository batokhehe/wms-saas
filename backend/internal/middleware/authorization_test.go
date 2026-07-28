package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// stubPermissionResolver returns a fixed answer, so these tests exercise the
// middleware's own behaviour rather than the RBAC module's resolution logic
// (which has its own tests).
type stubPermissionResolver struct {
	codes []string
	err   error

	gotCompanyID uuid.UUID
	gotRole      string
	calls        int
}

func (s *stubPermissionResolver) Resolve(
	_ context.Context, companyID uuid.UUID, roleName string,
) ([]string, error) {
	s.calls++
	s.gotCompanyID = companyID
	s.gotRole = roleName
	return s.codes, s.err
}

// runAuth drives a request through the authorisation chain.
func runAuth(
	t *testing.T,
	resolver PermissionResolver,
	companyID *uuid.UUID,
	role string,
	guards ...gin.HandlerFunc,
) (*httptest.ResponseRecorder, []string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	engine := gin.New()

	var seen []string

	// Stand in for RequestContext + Authenticate + ResolveCompany.
	engine.Use(func(c *gin.Context) {
		rc := appcontext.New("test-request", zap.NewNop())
		userID := uuid.New()
		rc.WithTenant(nil, &userID, "")
		if companyID != nil {
			rc.WithCompany(*companyID, uuid.New(), role)
		}
		appcontext.SetGin(c, rc)
		c.Next()
	})

	engine.Use(LoadPermissions(resolver))
	for _, guard := range guards {
		engine.Use(guard)
	}

	engine.GET("/probe", func(c *gin.Context) {
		seen = Permissions(c)
		c.String(http.StatusOK, "ok")
	})

	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))

	return rec, seen
}

func TestLoadPermissionsResolvesOnce(t *testing.T) {
	companyID := uuid.New()
	resolver := &stubPermissionResolver{codes: []string{"role.read", "company.read"}}

	rec, seen := runAuth(t, resolver, &companyID, "ADMIN",
		RequirePermission("role.read"),
		RequirePermission("company.read"),
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Two guards, one query. Without the per-request cache each guard would hit
	// the database independently.
	if resolver.calls != 1 {
		t.Errorf("resolver called %d times, want 1", resolver.calls)
	}
	if resolver.gotCompanyID != companyID {
		t.Errorf("resolver got company %s, want %s", resolver.gotCompanyID, companyID)
	}
	if resolver.gotRole != "ADMIN" {
		t.Errorf("resolver got role %q, want ADMIN", resolver.gotRole)
	}
	if len(seen) != 2 {
		t.Errorf("Permissions() returned %v, want two codes", seen)
	}
}

func TestRequirePermissionAllows(t *testing.T) {
	companyID := uuid.New()
	resolver := &stubPermissionResolver{codes: []string{"role.create"}}

	rec, _ := runAuth(t, resolver, &companyID, "OWNER", RequirePermission("role.create"))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRequirePermissionDenies(t *testing.T) {
	companyID := uuid.New()
	resolver := &stubPermissionResolver{codes: []string{"role.read"}}

	rec, _ := runAuth(t, resolver, &companyID, "STAFF", RequirePermission("role.create"))

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

	// The response names the missing permission so a user knows what to ask for.
	errBody, _ := body["error"].(map[string]any)
	details, _ := errBody["details"].(map[string]any)
	if details["required_permission"] != "role.create" {
		t.Errorf("details = %v, want the missing permission named", details)
	}
}

// TestRequirePermissionIsConjunctive: a route declaring two permissions
// requires BOTH. A disjunctive reading would quietly widen access.
func TestRequirePermissionIsConjunctive(t *testing.T) {
	companyID := uuid.New()
	resolver := &stubPermissionResolver{codes: []string{"role.read"}}

	rec, _ := runAuth(t, resolver, &companyID, "STAFF",
		RequirePermission("role.read", "role.create"))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 — holding one of two permissions is not enough", rec.Code)
	}
}

// TestRequirePermissionWithNoCodesDenies: an empty requirement almost always
// means a constant was mistyped or a slice came out empty, and failing open
// there would silently unguard a route.
func TestRequirePermissionWithNoCodesDenies(t *testing.T) {
	companyID := uuid.New()
	resolver := &stubPermissionResolver{codes: []string{"role.read"}}

	rec, _ := runAuth(t, resolver, &companyID, "OWNER", RequirePermission())

	if rec.Code == http.StatusOK {
		t.Error("a route declaring no permission allowed the request")
	}
}

// TestNoTenantMeansNoPermissions: authorisation is evaluated WITHIN a company,
// so a caller with no active company holds nothing.
func TestNoTenantMeansNoPermissions(t *testing.T) {
	resolver := &stubPermissionResolver{codes: []string{"role.read"}}

	rec, seen := runAuth(t, resolver, nil, "", RequirePermission("role.read"))

	if resolver.calls != 0 {
		t.Error("the resolver ran without a company context")
	}
	if len(seen) != 0 {
		t.Errorf("Permissions() = %v, want none", seen)
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// TestResolutionFailureIsNotADenial distinguishes an outage from a decision.
// Presenting an unreachable database as "you are not allowed" would send an
// operator hunting for a permissions bug that does not exist.
func TestResolutionFailureIsNotADenial(t *testing.T) {
	companyID := uuid.New()
	resolver := &stubPermissionResolver{err: errors.New("database unreachable")}

	rec, _ := runAuth(t, resolver, &companyID, "OWNER", RequirePermission("role.read"))

	if rec.Code == http.StatusForbidden {
		t.Error("an infrastructure failure was reported as a 403")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// TestMissingLoadPermissionsFailsClosed: if the middleware is not wired, every
// guarded route must deny rather than allow.
func TestMissingLoadPermissionsFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		rc := appcontext.New("test", zap.NewNop())
		userID, companyID := uuid.New(), uuid.New()
		rc.WithTenant(nil, &userID, "")
		rc.WithCompany(companyID, uuid.New(), "OWNER")
		appcontext.SetGin(c, rc)
		c.Next()
	})

	// LoadPermissions deliberately omitted.
	engine.GET("/probe", RequirePermission("role.read"), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 — an unwired chain must fail closed", rec.Code)
	}
}

func TestRequireAnyPermission(t *testing.T) {
	companyID := uuid.New()
	resolver := &stubPermissionResolver{codes: []string{"role.read"}}

	rec, _ := runAuth(t, resolver, &companyID, "STAFF",
		RequireAnyPermission("role.create", "role.read"))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 — one of the codes is held", rec.Code)
	}

	rec, _ = runAuth(t, resolver, &companyID, "STAFF",
		RequireAnyPermission("role.create", "role.delete"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 — none of the codes is held", rec.Code)
	}
}

func TestHasPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	engine := gin.New()
	companyID := uuid.New()

	engine.Use(func(c *gin.Context) {
		rc := appcontext.New("test", zap.NewNop())
		userID := uuid.New()
		rc.WithTenant(nil, &userID, "")
		rc.WithCompany(companyID, uuid.New(), "ADMIN")
		appcontext.SetGin(c, rc)
		c.Next()
	})
	engine.Use(LoadPermissions(&stubPermissionResolver{codes: []string{"company.read"}}))

	var held, absent bool
	engine.GET("/probe", func(c *gin.Context) {
		held = HasPermission(c, "company.read")
		absent = HasPermission(c, "company.delete")
		c.String(http.StatusOK, "ok")
	})

	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))

	if !held {
		t.Error("HasPermission(company.read) = false, want true")
	}
	if absent {
		t.Error("HasPermission(company.delete) = true, want false")
	}
}
