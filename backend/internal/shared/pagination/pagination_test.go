package pagination

import (
	"errors"
	"strings"
	"testing"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

func testOptions() Options {
	return Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "created_at",
		DefaultOrder: OrderDesc,
		AllowedSorts: map[string]string{
			"name":       "products.name",
			"created_at": "products.created_at",
		},
	}
}

func TestApplyFillsDefaults(t *testing.T) {
	var req Request

	if err := req.Apply(testOptions()); err != nil {
		t.Fatalf("Apply() = %v, want nil", err)
	}

	if req.Page != 1 {
		t.Errorf("Page = %d, want 1", req.Page)
	}
	if req.Limit != 25 {
		t.Errorf("Limit = %d, want 25", req.Limit)
	}
	if req.Sort != "created_at" {
		t.Errorf("Sort = %q, want created_at", req.Sort)
	}
	if req.Order != OrderDesc {
		t.Errorf("Order = %q, want %q", req.Order, OrderDesc)
	}
}

// TestApplyClampsLimit covers the control that stops ?limit=1000000 turning one
// request into a full table scan.
func TestApplyClampsLimit(t *testing.T) {
	req := Request{Limit: 5000}

	if err := req.Apply(testOptions()); err != nil {
		t.Fatalf("Apply() = %v, want nil", err)
	}

	if req.Limit != 100 {
		t.Errorf("Limit = %d, want it clamped to 100", req.Limit)
	}
}

// TestApplyRejectsUnknownSort is the SQL-injection guard. ORDER BY cannot be
// parameterised, so an unlisted sort key must never reach OrderClause.
func TestApplyRejectsUnknownSort(t *testing.T) {
	req := Request{Sort: "name; DROP TABLE products--"}

	err := req.Apply(testOptions())
	if err == nil {
		t.Fatal("Apply() = nil, want a validation error for an unlisted sort key")
	}

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("Apply() returned %T, want *apperror.Error", err)
	}
	if appErr.Code != apperror.CodeValidation {
		t.Errorf("Code = %q, want %q", appErr.Code, apperror.CodeValidation)
	}
	if req.OrderClause() != "" {
		t.Errorf("OrderClause() = %q after a rejected Apply, want empty", req.OrderClause())
	}
}

// TestOrderClauseUsesMappedColumn proves the API sort key is translated to the
// database column rather than being passed through.
func TestOrderClauseUsesMappedColumn(t *testing.T) {
	req := Request{Sort: "name", Order: "ASC"}

	if err := req.Apply(testOptions()); err != nil {
		t.Fatalf("Apply() = %v, want nil", err)
	}

	if got, want := req.OrderClause(), "products.name ASC"; got != want {
		t.Errorf("OrderClause() = %q, want %q", got, want)
	}
}

// TestOrderClauseEmptyBeforeApply is what the base repository relies on to
// refuse an unvalidated request.
func TestOrderClauseEmptyBeforeApply(t *testing.T) {
	req := Request{Sort: "name"}

	if req.Applied() {
		t.Error("Applied() = true before Apply()")
	}
	if got := req.OrderClause(); got != "" {
		t.Errorf("OrderClause() = %q before Apply(), want empty", got)
	}
}

func TestApplyNormalisesOrderCase(t *testing.T) {
	req := Request{Order: "DESC"}

	if err := req.Apply(testOptions()); err != nil {
		t.Fatalf("Apply() = %v, want nil", err)
	}
	if req.Order != OrderDesc {
		t.Errorf("Order = %q, want lowercase %q", req.Order, OrderDesc)
	}
}

func TestApplyTrimsSearch(t *testing.T) {
	req := Request{Search: "  widget  "}

	if err := req.Apply(testOptions()); err != nil {
		t.Fatalf("Apply() = %v, want nil", err)
	}
	if req.Search != "widget" {
		t.Errorf("Search = %q, want %q", req.Search, "widget")
	}
	if !req.HasSearch() {
		t.Error("HasSearch() = false, want true")
	}
}

// TestApplyErrorListsAllowedSorts checks the failure is actionable: a client
// must be told which values are acceptable.
func TestApplyErrorListsAllowedSorts(t *testing.T) {
	req := Request{Sort: "nope"}

	err := req.Apply(testOptions())
	if err == nil {
		t.Fatal("Apply() = nil, want an error")
	}

	var appErr *apperror.Error
	errors.As(err, &appErr)

	details, ok := appErr.Details.(apperror.ValidationDetails)
	if !ok || len(details.Fields) == 0 {
		t.Fatalf("Details = %#v, want field-level validation details", appErr.Details)
	}
	if !strings.Contains(details.Fields[0].Message, "created_at") {
		t.Errorf("message %q does not list the permitted sort keys", details.Fields[0].Message)
	}
}

func TestOffset(t *testing.T) {
	tests := []struct {
		page, limit, want int
	}{
		{1, 25, 0},
		{2, 25, 25},
		{4, 10, 30},
	}

	for _, tt := range tests {
		req := Request{Page: tt.page, Limit: tt.limit}
		if got := req.Offset(); got != tt.want {
			t.Errorf("Request{Page:%d, Limit:%d}.Offset() = %d, want %d",
				tt.page, tt.limit, got, tt.want)
		}
	}
}

func TestNewMetadata(t *testing.T) {
	tests := []struct {
		name                     string
		page, limit              int
		total                    int64
		wantPages                int
		wantHasNext, wantHasPrev bool
	}{
		{"first of several", 1, 25, 137, 6, true, false},
		{"middle", 3, 25, 137, 6, true, true},
		{"last", 6, 25, 137, 6, false, true},
		{"exactly one page", 1, 25, 25, 1, false, false},
		{"empty", 1, 25, 0, 0, false, false},
		// A partial final page must still count as a page; plain integer
		// division would report 5 here and hide the last row from every client.
		{"partial final page", 1, 25, 126, 6, true, false},
		// Past the end: no next page, and prev is still true so a client can
		// navigate back rather than getting stuck.
		{"beyond last page", 9, 25, 137, 6, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewMetadata(Request{Page: tt.page, Limit: tt.limit}, tt.total)

			if got.TotalPages != tt.wantPages {
				t.Errorf("TotalPages = %d, want %d", got.TotalPages, tt.wantPages)
			}
			if got.HasNext != tt.wantHasNext {
				t.Errorf("HasNext = %t, want %t", got.HasNext, tt.wantHasNext)
			}
			if got.HasPrev != tt.wantHasPrev {
				t.Errorf("HasPrev = %t, want %t", got.HasPrev, tt.wantHasPrev)
			}
		})
	}
}

// TestNewPageNormalisesNilSlice guards the JSON contract: a list endpoint must
// emit [] rather than null when there are no results.
func TestNewPageNormalisesNilSlice(t *testing.T) {
	page := NewPage[string](nil, Request{Page: 1, Limit: 25}, 0)

	if page.Items == nil {
		t.Error("Items is nil; it must be an empty slice so JSON emits []")
	}
	if len(page.Items) != 0 {
		t.Errorf("len(Items) = %d, want 0", len(page.Items))
	}
}

func TestMapPagePreservesMetadata(t *testing.T) {
	source := NewPage([]int{1, 2, 3}, Request{Page: 2, Limit: 3}, 10)

	mapped := MapPage(source, func(i int) string { return string(rune('a' + i)) })

	if len(mapped.Items) != 3 {
		t.Errorf("len(Items) = %d, want 3", len(mapped.Items))
	}
	if mapped.Meta != source.Meta {
		t.Errorf("Meta = %+v, want it preserved as %+v", mapped.Meta, source.Meta)
	}
}

// TestDefaultSortFallsBackToAnAllowedKey covers the misconfiguration case: an
// endpoint whose DefaultSort is not in AllowedSorts must still produce a
// deterministic ordering, because paging over an unordered result set can
// repeat or skip rows between pages.
func TestDefaultSortFallsBackToAnAllowedKey(t *testing.T) {
	opts := Options{
		AllowedSorts: map[string]string{"id": "products.id"},
		DefaultSort:  "does_not_exist",
	}

	var req Request
	if err := req.Apply(opts); err != nil {
		t.Fatalf("Apply() = %v, want nil", err)
	}

	if req.OrderClause() == "" {
		t.Error("OrderClause() is empty; a misconfigured default must still yield an ordering")
	}
}
