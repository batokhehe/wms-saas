package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// decode runs fn inside a request and returns the status and parsed envelope.
func decode(t *testing.T, fn gin.HandlerFunc) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	fn(c)

	var body map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("response is not valid JSON: %v (%s)", err, rec.Body.String())
		}
	}

	return rec.Code, body
}

// TestSuccessEnvelope pins the exact success shape. If this test has to change,
// every Flutter client has to change too — which is the point of pinning it.
func TestSuccessEnvelope(t *testing.T) {
	status, body := decode(t, func(c *gin.Context) {
		OK(c, "Resource retrieved successfully", map[string]any{"id": "abc"})
	})

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if body["success"] != true {
		t.Errorf("success = %v, want true", body["success"])
	}
	if body["message"] != "Resource retrieved successfully" {
		t.Errorf("message = %v", body["message"])
	}
	if _, ok := body["data"]; !ok {
		t.Error("data is missing")
	}
	if _, ok := body["error"]; ok {
		t.Error("error must be absent on a success response")
	}

	// meta.timestamp is always present so a client can detect clock skew.
	meta, ok := body["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta is missing")
	}
	if _, ok := meta["timestamp"]; !ok {
		t.Error("meta.timestamp is missing")
	}
}

// TestErrorEnvelope pins the exact failure shape.
func TestErrorEnvelope(t *testing.T) {
	status, body := decode(t, func(c *gin.Context) {
		Error(c, apperror.NotFound("Product not found"))
	})

	if status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", status, http.StatusNotFound)
	}
	if body["success"] != false {
		t.Errorf("success = %v, want false", body["success"])
	}
	if body["message"] != "Product not found" {
		t.Errorf("message = %v", body["message"])
	}
	if _, ok := body["data"]; ok {
		t.Error("data must be absent on an error response")
	}

	errBody, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("error object is missing")
	}
	if errBody["code"] != "NOT_FOUND" {
		t.Errorf("error.code = %v, want NOT_FOUND", errBody["code"])
	}
}

// TestUnknownErrorIsOpaque is the leak guard: an error that did not come from
// apperror must never expose its message to the client.
func TestUnknownErrorIsOpaque(t *testing.T) {
	leaky := errors.New(`pq: duplicate key value violates unique constraint "idx_products_company_sku"`)

	status, body := decode(t, func(c *gin.Context) { Error(c, leaky) })

	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", status, http.StatusInternalServerError)
	}
	if msg, _ := body["message"].(string); msg != "An unexpected error occurred" {
		t.Errorf("message = %q, want the generic message", msg)
	}
	if body["message"] == leaky.Error() {
		t.Error("the raw driver message reached the client")
	}
}

// TestInternalErrorSuppressesDetails proves diagnostic context attached to a 5xx
// stays in the logs and does not travel to the client.
func TestInternalErrorSuppressesDetails(t *testing.T) {
	_, body := decode(t, func(c *gin.Context) {
		Error(c, apperror.Internal("Something failed").
			WithDetails(map[string]any{"internal_host": "db-primary.internal"}))
	})

	errBody, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("error object is missing")
	}
	if _, present := errBody["details"]; present {
		t.Errorf("details leaked on a 5xx: %v", errBody["details"])
	}
}

// TestClientErrorKeepsDetails is the counterpart: 4xx details are the client's
// own validation feedback and must survive.
func TestClientErrorKeepsDetails(t *testing.T) {
	_, body := decode(t, func(c *gin.Context) {
		Error(c, apperror.NewValidation(apperror.FieldError{
			Field: "sku", Rule: "required", Message: "sku is required",
		}))
	})

	errBody, _ := body["error"].(map[string]any)
	if errBody["code"] != "VALIDATION_ERROR" {
		t.Errorf("error.code = %v, want VALIDATION_ERROR", errBody["code"])
	}
	if _, present := errBody["details"]; !present {
		t.Error("details missing on a 4xx; the client cannot map the failure to a field")
	}
}

// TestListEmitsPaginationMeta checks the envelope carries paging under
// meta.pagination. The arithmetic itself is tested in the pagination package.
func TestListEmitsPaginationMeta(t *testing.T) {
	req := pagination.Request{Page: 2, Limit: 25}

	_, body := decode(t, func(c *gin.Context) {
		List(c, "Resources retrieved successfully", []string{"a"}, pagination.NewMetadata(req, 137))
	})

	meta, ok := body["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta is missing")
	}

	paging, ok := meta["pagination"].(map[string]any)
	if !ok {
		t.Fatal("meta.pagination is missing on a list response")
	}
	if paging["total"] != float64(137) {
		t.Errorf("meta.pagination.total = %v, want 137", paging["total"])
	}
	if paging["total_pages"] != float64(6) {
		t.Errorf("meta.pagination.total_pages = %v, want 6", paging["total_pages"])
	}
}

// TestNonListResponseHasNoPagination guards against paging metadata leaking
// onto single-resource responses, where it would be meaningless.
func TestNonListResponseHasNoPagination(t *testing.T) {
	_, body := decode(t, func(c *gin.Context) {
		OK(c, "Resource retrieved successfully", map[string]any{"id": "abc"})
	})

	meta, _ := body["meta"].(map[string]any)
	if _, present := meta["pagination"]; present {
		t.Error("meta.pagination is present on a non-list response")
	}
}
