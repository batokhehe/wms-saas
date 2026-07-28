// Package pagination provides the one paging contract every list endpoint uses.
//
// It exists so that `page`, `limit`, `search`, `sort` and `order` mean the same
// thing on every endpoint, and so that the total-pages arithmetic is written
// once rather than approximated per module.
//
// It also carries the SQL-injection defence for sorting. `ORDER BY` cannot be
// parameterised by any driver, so the column name is interpolated into the
// query — which means an unvalidated `sort` parameter is a direct injection
// vector. Request.Apply resolves the client's value against a per-endpoint
// allow-list, and Request.OrderClause refuses to render anything that has not
// been through it.
package pagination

import (
	"sort"
	"strings"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Sort direction values accepted from clients.
const (
	OrderAsc  = "asc"
	OrderDesc = "desc"
)

// Defaults applied when Options leaves a field unset.
const (
	DefaultPage         = 1
	DefaultLimit        = 25
	DefaultMaxLimit     = 100
	DefaultOrder        = OrderDesc
	DefaultSearchLength = 255
)

// Request is the query string of a list endpoint.
//
// Bind it with validator.BindQuery, then call Apply before use. The binding
// tags catch structurally invalid input; Apply enforces the semantic rules that
// depend on the endpoint.
type Request struct {
	Page   int    `form:"page"   json:"page"   binding:"omitempty,min=1"`
	Limit  int    `form:"limit"  json:"limit"  binding:"omitempty,min=1"`
	Search string `form:"search" json:"search" binding:"omitempty,max=255"`
	Sort   string `form:"sort"   json:"sort"   binding:"omitempty,max=64"`
	Order  string `form:"order"  json:"order"  binding:"omitempty,oneof=asc desc ASC DESC"`

	// column is the resolved, allow-listed database column. It is unexported so
	// no caller can populate it directly and bypass validation.
	column string
	// applied records that Apply has run. OrderClause and Offset refuse to work
	// without it, which turns "forgot to validate" from a vulnerability into a
	// loud error.
	applied bool
}

// Options describes the paging rules of one endpoint.
type Options struct {
	// DefaultLimit is used when the client sends no limit. Zero means 25.
	DefaultLimit int
	// MaxLimit caps the page size. Zero means 100.
	//
	// A cap is mandatory, not advisory: without one, `?limit=1000000` turns a
	// single HTTP request into a full table scan and a multi-megabyte response.
	MaxLimit int
	// DefaultSort is the API sort key used when the client sends none. It must
	// be a key of AllowedSorts.
	DefaultSort string
	// DefaultOrder is "asc" or "desc". Zero means "desc".
	DefaultOrder string
	// AllowedSorts maps the API-facing sort key to its database column:
	//
	//	map[string]string{"name": "products.name", "created_at": "products.created_at"}
	//
	// The indirection matters. Clients sort by a stable public name, while the
	// column can be renamed or qualified with a table alias without breaking
	// them — and only names in this map can ever reach the SQL.
	AllowedSorts map[string]string
}

// Apply validates and normalises the request against an endpoint's rules.
//
// It returns a VALIDATION_ERROR with per-field details, so an unsupported sort
// key produces the same shaped response as any other bad input rather than a
// 500 from a malformed query.
func (r *Request) Apply(opts Options) error {
	opts = opts.withDefaults()

	if r.Page < 1 {
		r.Page = DefaultPage
	}

	switch {
	case r.Limit < 1:
		r.Limit = opts.DefaultLimit
	case r.Limit > opts.MaxLimit:
		// Clamping rather than rejecting: a client asking for too much gets the
		// maximum, and the pagination metadata tells it what it actually got.
		// Rejecting would break naive clients for no protective benefit.
		r.Limit = opts.MaxLimit
	}

	r.Search = strings.TrimSpace(r.Search)

	order := strings.ToLower(strings.TrimSpace(r.Order))
	if order != OrderAsc && order != OrderDesc {
		order = opts.DefaultOrder
	}
	r.Order = order

	sortKey := strings.TrimSpace(r.Sort)
	if sortKey == "" {
		sortKey = opts.DefaultSort
	}

	column, ok := opts.AllowedSorts[sortKey]
	if !ok {
		return apperror.NewValidation(apperror.FieldError{
			Field:   "sort",
			Rule:    "oneof",
			Message: "sort must be one of: " + strings.Join(sortedKeys(opts.AllowedSorts), ", "),
		}).WithOp("pagination.Request.Apply")
	}

	r.Sort = sortKey
	r.column = column
	r.applied = true

	return nil
}

// withDefaults fills unset option fields.
func (o Options) withDefaults() Options {
	if o.DefaultLimit <= 0 {
		o.DefaultLimit = DefaultLimit
	}
	if o.MaxLimit <= 0 {
		o.MaxLimit = DefaultMaxLimit
	}
	if o.DefaultOrder != OrderAsc && o.DefaultOrder != OrderDesc {
		o.DefaultOrder = DefaultOrder
	}
	if o.AllowedSorts == nil {
		o.AllowedSorts = map[string]string{}
	}

	// An endpoint that declared no default sort but does allow sorting gets a
	// deterministic one anyway. Paging over an unordered result set is a real
	// bug: PostgreSQL may return rows in any order between queries, so page 2
	// can repeat or skip rows from page 1.
	if _, ok := o.AllowedSorts[o.DefaultSort]; !ok {
		if keys := sortedKeys(o.AllowedSorts); len(keys) > 0 {
			o.DefaultSort = keys[0]
		}
	}

	return o
}

// Applied reports whether Apply has run. The base repository checks this.
func (r Request) Applied() bool { return r.applied }

// Offset converts the page number into a SQL offset.
func (r Request) Offset() int {
	if r.Page < 1 {
		return 0
	}
	return (r.Page - 1) * r.Limit
}

// OrderClause renders the validated `ORDER BY` expression, e.g.
// "products.created_at DESC".
//
// It returns "" when Apply has not run. The base repository treats that as an
// error rather than issuing an unordered query, so a forgotten Apply cannot
// silently produce unstable pagination — or reach SQL with an unchecked column.
func (r Request) OrderClause() string {
	if !r.applied || r.column == "" {
		return ""
	}
	return r.column + " " + strings.ToUpper(r.Order)
}

// SortColumn returns the resolved database column, or "" before Apply.
func (r Request) SortColumn() string { return r.column }

// HasSearch reports whether a non-empty search term was supplied.
func (r Request) HasSearch() bool { return r.Search != "" }

// Metadata is the paging block returned in the response envelope's `meta`.
type Metadata struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// NewMetadata derives the computed fields from a request and a total count.
func NewMetadata(req Request, total int64) Metadata {
	page, limit := req.Page, req.Limit
	if page < 1 {
		page = DefaultPage
	}
	if limit < 1 {
		limit = DefaultLimit
	}

	// Ceiling division. Plain integer division would report 5 pages for 126
	// rows at 25 per page, hiding the final row from every client.
	totalPages := int((total + int64(limit) - 1) / int64(limit))

	return Metadata{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
		HasPrev:    page > 1 && total > 0,
	}
}

// Page couples a slice of results with its metadata, so a service returns one
// value instead of a (slice, total) pair every caller has to reassemble.
type Page[T any] struct {
	Items []T
	Meta  Metadata
}

// NewPage builds a Page, normalising a nil slice to an empty one so the JSON
// encoder emits [] rather than null. A client forced to handle both will
// eventually crash on one.
func NewPage[T any](items []T, req Request, total int64) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{Items: items, Meta: NewMetadata(req, total)}
}

// MapPage converts a Page of one type into a Page of another, preserving the
// metadata. It is the bridge from a page of entities to a page of DTOs.
func MapPage[In, Out any](in Page[In], convert func(In) Out) Page[Out] {
	items := make([]Out, 0, len(in.Items))
	for _, item := range in.Items {
		items = append(items, convert(item))
	}
	return Page[Out]{Items: items, Meta: in.Meta}
}

// sortedKeys returns map keys in a stable order, so error messages listing the
// permitted sort fields do not shuffle between requests.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
