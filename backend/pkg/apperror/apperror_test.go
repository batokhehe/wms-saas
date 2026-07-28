package apperror

import (
	"errors"
	"net/http"
	"testing"
)

// TestIsComparesByCode covers the behaviour that lets callers use sentinels
// without caring about the message or cause attached at the failure site.
func TestIsComparesByCode(t *testing.T) {
	err := NotFound("Product not found").
		WithOp("product.service.Get").
		WithCause(errors.New("sql: no rows in result set"))

	if !errors.Is(err, ErrNotFound) {
		t.Error("errors.Is(err, ErrNotFound) = false, want true")
	}
	if errors.Is(err, ErrConflict) {
		t.Error("errors.Is(err, ErrConflict) = true, want false")
	}
}

// TestWithersDoNotMutate is the regression guard for the sentinel-corruption
// bug: if the fluent methods mutated in place, one handler enriching
// ErrNotFound would change it for every goroutine in the process.
func TestWithersDoNotMutate(t *testing.T) {
	original := ErrNotFound.Message

	_ = ErrNotFound.WithMessage("Product not found").
		WithOp("product.service.Get").
		WithDetails(map[string]any{"id": "abc"})

	if ErrNotFound.Message != original {
		t.Errorf("the shared sentinel was mutated: Message = %q, want %q",
			ErrNotFound.Message, original)
	}
	if ErrNotFound.Details != nil {
		t.Errorf("the shared sentinel gained Details: %v", ErrNotFound.Details)
	}
}

func TestUnwrapReachesCause(t *testing.T) {
	cause := errors.New("connection refused")
	err := Internal("boom").WithCause(cause)

	if !errors.Is(err, cause) {
		t.Error("errors.Is(err, cause) = false, want true")
	}
}

// TestFromWrapsUnknownErrors covers the choke point that prevents a raw driver
// message from reaching a client.
func TestFromWrapsUnknownErrors(t *testing.T) {
	raw := errors.New(`pq: relation "products" does not exist`)

	got := From(raw)

	if got.Code != CodeInternal {
		t.Errorf("Code = %q, want %q", got.Code, CodeInternal)
	}
	if got.Message == raw.Error() {
		t.Error("the raw message became the client-facing message")
	}
	if !errors.Is(got, raw) {
		t.Error("the original error was not preserved as the cause")
	}
}

func TestFromPreservesClassification(t *testing.T) {
	original := Conflict("SKU already exists")

	if got := From(original); got.Code != CodeConflict {
		t.Errorf("Code = %q, want %q", got.Code, CodeConflict)
	}
}

func TestFromNilIsNil(t *testing.T) {
	if got := From(nil); got != nil {
		t.Errorf("From(nil) = %v, want nil", got)
	}
}

// TestWrapKeepsOutermostOp checks that wrapping at each layer boundary does not
// overwrite the operation recorded closest to the actual failure.
func TestWrapKeepsOutermostOp(t *testing.T) {
	inner := NotFound("not found").WithOp("product.repository.FindByID")

	wrapped := Wrap(inner, "product.service.Get")

	if wrapped.Op != "product.repository.FindByID" {
		t.Errorf("Op = %q, want the original operation to be preserved", wrapped.Op)
	}
}

func TestIsInternal(t *testing.T) {
	tests := []struct {
		err  *Error
		want bool
	}{
		{BadRequest("x"), false},
		{NotFound("x"), false},
		{Validation("x"), false},
		{RateLimited("x"), false},
		{Internal("x"), true},
		{Unavailable("x"), true},
		{Timeout("x"), true},
	}

	for _, tt := range tests {
		t.Run(string(tt.err.Code), func(t *testing.T) {
			if got := tt.err.IsInternal(); got != tt.want {
				t.Errorf("IsInternal() = %t, want %t", got, tt.want)
			}
		})
	}
}

// TestCodeStatusPairing guards the invariant that a constructor can never pair a
// code with the wrong status.
func TestCodeStatusPairing(t *testing.T) {
	tests := []struct {
		err    *Error
		code   Code
		status int
	}{
		{BadRequest("x"), CodeBadRequest, http.StatusBadRequest},
		{Unauthorized("x"), CodeUnauthorized, http.StatusUnauthorized},
		{Forbidden("x"), CodeForbidden, http.StatusForbidden},
		{NotFound("x"), CodeNotFound, http.StatusNotFound},
		{Conflict("x"), CodeConflict, http.StatusConflict},
		{Validation("x"), CodeValidation, http.StatusUnprocessableEntity},
		{Internal("x"), CodeInternal, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			if tt.err.Code != tt.code {
				t.Errorf("Code = %q, want %q", tt.err.Code, tt.code)
			}
			if tt.err.Status != tt.status {
				t.Errorf("Status = %d, want %d", tt.err.Status, tt.status)
			}
		})
	}
}

func TestLogFieldsIncludeDiagnostics(t *testing.T) {
	err := Conflict("SKU exists").
		WithOp("product.service.Create").
		WithCause(errors.New("unique violation")).
		WithDetails(map[string]any{"sku": "ABC-1"})

	keys := make(map[string]bool)
	for _, f := range err.LogFields() {
		keys[f.Key] = true
	}

	for _, want := range []string{"error_code", "error_status", "error_message", "error_op", "error_details", "error"} {
		if !keys[want] {
			t.Errorf("LogFields() is missing %q", want)
		}
	}
}
