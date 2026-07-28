package module

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// v1Only serves only /api/v1. It stands for the common case: a module written
// today that must keep working untouched when v2 arrives.
type v1Only struct{ name string }

func (m *v1Only) Name() string { return m.name }
func (m *v1Only) RegisterV1(rg *gin.RouterGroup) {
	rg.GET("/thing", func(c *gin.Context) { c.String(http.StatusOK, "v1") })
}

// bothVersions opts into v2 by adding one method, changing nothing else.
type bothVersions struct{ name string }

func (m *bothVersions) Name() string { return m.name }
func (m *bothVersions) RegisterV1(rg *gin.RouterGroup) {
	rg.GET("/thing", func(c *gin.Context) { c.String(http.StatusOK, "v1") })
}
func (m *bothVersions) RegisterV2(rg *gin.RouterGroup) {
	rg.GET("/thing", func(c *gin.Context) { c.String(http.StatusOK, "v2") })
}

// rootOnly stands for infrastructure modules such as health.
type rootOnly struct{ name string }

func (m *rootOnly) Name() string { return m.name }
func (m *rootOnly) RegisterRoot(rg *gin.RouterGroup) {
	rg.GET("/probe", func(c *gin.Context) { c.String(http.StatusOK, "root") })
}

// noRoutes implements Module but no registrar — the wiring mistake Validate
// exists to catch.
type noRoutes struct{}

func (noRoutes) Name() string { return "broken" }

func newTestRegistry(modules ...Module) *Registry {
	return NewRegistry(zap.NewNop()).Register(modules...)
}

func mount(t *testing.T, versions []APIVersion, modules ...Module) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// SupportedVersions is package-level state; restore it so tests do not leak
	// into one another.
	original := SupportedVersions
	SupportedVersions = versions
	t.Cleanup(func() { SupportedVersions = original })

	engine := gin.New()
	newTestRegistry(modules...).Mount(engine)
	return engine
}

func get(t *testing.T, engine *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestMountsUnderVersionPrefix(t *testing.T) {
	engine := mount(t, []APIVersion{V1}, &v1Only{name: "alpha"})

	if rec := get(t, engine, "/api/v1/thing"); rec.Code != http.StatusOK {
		t.Errorf("GET /api/v1/thing = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestV1OnlyModuleIgnoredByV2 is the core guarantee of the versioning design:
// enabling v2 must not expose a v1-only module under v2, and must not require
// editing it.
func TestV1OnlyModuleIgnoredByV2(t *testing.T) {
	engine := mount(t, []APIVersion{V1, V2}, &v1Only{name: "alpha"})

	if rec := get(t, engine, "/api/v1/thing"); rec.Code != http.StatusOK {
		t.Errorf("GET /api/v1/thing = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec := get(t, engine, "/api/v2/thing"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/v2/thing = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestModuleServesBothVersions proves a module opts into v2 by adding one
// method, and that the two versions are independently routed.
func TestModuleServesBothVersions(t *testing.T) {
	engine := mount(t, []APIVersion{V1, V2}, &bothVersions{name: "beta"})

	for path, want := range map[string]string{
		"/api/v1/thing": "v1",
		"/api/v2/thing": "v2",
	} {
		rec := get(t, engine, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, http.StatusOK)
			continue
		}
		if got := rec.Body.String(); got != want {
			t.Errorf("GET %s body = %q, want %q", path, got, want)
		}
	}
}

func TestRootRoutesAreUnversioned(t *testing.T) {
	engine := mount(t, []APIVersion{V1}, &rootOnly{name: "health"})

	if rec := get(t, engine, "/probe"); rec.Code != http.StatusOK {
		t.Errorf("GET /probe = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modules []Module
		wantErr bool
	}{
		{"valid", []Module{&v1Only{name: "alpha"}, &rootOnly{name: "health"}}, false},
		{"duplicate name", []Module{&v1Only{name: "alpha"}, &v1Only{name: "alpha"}}, true},
		{"empty name", []Module{&v1Only{name: ""}}, true},
		{"no route registrar", []Module{noRoutes{}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newTestRegistry(tt.modules...).Validate()

			if tt.wantErr && err == nil {
				t.Error("Validate() = nil, want an error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestAPIVersionPrefix(t *testing.T) {
	if got, want := V1.Prefix(), "/api/v1"; got != want {
		t.Errorf("V1.Prefix() = %q, want %q", got, want)
	}
}
