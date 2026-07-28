package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/service"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// These are HTTP-boundary tests: they drive the real handler and service over a
// real gin router with in-memory fakes, asserting status codes and the response
// envelope. They prove binding, routing and error-to-status mapping, which the
// service unit tests do not exercise.

// ---------- fakes ----------

type fakeRepo struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*entity.Supplier
}

var _ repository.Repository = (*fakeRepo)(nil)

func (r *fakeRepo) Save(_ context.Context, s *entity.Supplier) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.byID {
		if e.CompanyID() == s.CompanyID() && strings.EqualFold(e.Code().String(), s.Code().String()) {
			return apperror.Conflict("duplicate")
		}
	}
	r.byID[s.ID()] = s
	return nil
}

func (r *fakeRepo) Update(_ context.Context, s *entity.Supplier) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[s.ID()] = s
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, id, companyID uuid.UUID) (*entity.Supplier, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[id]
	if !ok || s.CompanyID() != companyID {
		return nil, apperror.NotFound("not found")
	}
	return s, nil
}

func (r *fakeRepo) FindByCode(context.Context, uuid.UUID, string) (*entity.Supplier, error) {
	return nil, apperror.NotFound("not found")
}

func (r *fakeRepo) List(_ context.Context, companyID uuid.UUID, filter repository.ListFilter) (pagination.Page[*entity.Supplier], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*entity.Supplier
	for _, s := range r.byID {
		if s.CompanyID() == companyID {
			out = append(out, s)
		}
	}
	return pagination.NewPage(out, filter.Paging, int64(len(out))), nil
}

func (r *fakeRepo) ExistsByCode(_ context.Context, companyID uuid.UUID, code string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.byID {
		if s.CompanyID() == companyID && strings.EqualFold(s.Code().String(), code) {
			return true, nil
		}
	}
	return false, nil
}

// passTx runs the unit of work without a real database.
type passTx struct{}

func (passTx) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type nopPublisher struct{}

func (nopPublisher) Publish(context.Context, entity.Event) {}

// ---------- harness ----------

func newRouter(t *testing.T) (*gin.Engine, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := &fakeRepo{byID: map[uuid.UUID]*entity.Supplier{}}
	svc := service.New(repo, adapterclock.NewFakeAt("2026-07-31T10:00:00Z"), adapterid.NewSequential(), passTx{}, nopPublisher{})
	h := New(svc)

	company := uuid.New()
	user := uuid.New()

	engine := gin.New()
	// Stand in for the auth + company middleware: inject a resolved request
	// context so the service can identify the tenant and actor.
	engine.Use(func(c *gin.Context) {
		rc := appcontext.New("test", zap.NewNop())
		rc.WithTenant(nil, &user, "")
		rc.WithCompany(company, uuid.New(), "OWNER")
		appcontext.SetGin(c, rc)
		c.Next()
	})

	g := engine.Group("/api/v1/suppliers")
	g.POST("", h.Create)
	g.GET("", h.List)
	g.GET("/:id", h.Get)
	g.PUT("/:id", h.Update)
	g.PATCH("/:id/activate", h.Activate)
	g.PATCH("/:id/deactivate", h.Deactivate)

	return engine, company
}

func do(t *testing.T, engine *gin.Engine, method, path, body string) (*httptest.ResponseRecorder, envelope) {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	var env envelope
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("unmarshalling response: %v (body=%s)", err, rec.Body.String())
		}
	}
	return rec, env
}

type envelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func (e envelope) supplier(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(e.Data, &m); err != nil {
		t.Fatalf("unmarshalling supplier: %v", err)
	}
	return m
}

// ---------- tests ----------

func TestCreateReturns201(t *testing.T) {
	engine, _ := newRouter(t)
	rec, env := do(t, engine, http.MethodPost, "/api/v1/suppliers", `{"code":"sup-01","name":"Acme Traders"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	if !env.Success {
		t.Error("envelope success = false")
	}
	if got := env.supplier(t)["code"]; got != "SUP-01" {
		t.Errorf("code = %v, want SUP-01", got)
	}
}

func TestCreateValidationErrorReturns422(t *testing.T) {
	engine, _ := newRouter(t)
	// Missing name.
	rec, env := do(t, engine, http.MethodPost, "/api/v1/suppliers", `{"code":"SUP-1"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if env.Error == nil || env.Error.Code != string(apperror.CodeValidation) {
		t.Errorf("error code = %+v, want VALIDATION_ERROR", env.Error)
	}
}

func TestGetUnknownReturns404(t *testing.T) {
	engine, _ := newRouter(t)
	rec, env := do(t, engine, http.MethodGet, "/api/v1/suppliers/"+uuid.New().String(), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if env.Error == nil || env.Error.Code != string(apperror.CodeNotFound) {
		t.Errorf("error code = %+v, want NOT_FOUND", env.Error)
	}
}

func TestGetInvalidUUIDReturns422(t *testing.T) {
	engine, _ := newRouter(t)
	rec, _ := do(t, engine, http.MethodGet, "/api/v1/suppliers/not-a-uuid", "")
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want a 4xx for a malformed id", rec.Code)
	}
}

func TestDuplicateCreateReturns409(t *testing.T) {
	engine, _ := newRouter(t)
	do(t, engine, http.MethodPost, "/api/v1/suppliers", `{"code":"SUP-1","name":"First"}`)
	rec, env := do(t, engine, http.MethodPost, "/api/v1/suppliers", `{"code":"sup-1","name":"Second"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if env.Error == nil || env.Error.Code != string(apperror.CodeConflict) {
		t.Errorf("error code = %+v, want CONFLICT", env.Error)
	}
}

func TestLifecycleAndListOverHTTP(t *testing.T) {
	engine, _ := newRouter(t)
	_, created := do(t, engine, http.MethodPost, "/api/v1/suppliers", `{"code":"SUP-1","name":"Acme"}`)
	id := created.supplier(t)["id"].(string)

	rec, env := do(t, engine, http.MethodPatch, "/api/v1/suppliers/"+id+"/deactivate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("deactivate status = %d, want 200", rec.Code)
	}
	if env.supplier(t)["status"] != "INACTIVE" {
		t.Errorf("status = %v, want INACTIVE", env.supplier(t)["status"])
	}

	rec, listEnv := do(t, engine, http.MethodGet, "/api/v1/suppliers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	var items []any
	_ = json.Unmarshal(listEnv.Data, &items)
	if len(items) != 1 {
		t.Errorf("list returned %d items, want 1", len(items))
	}
}
