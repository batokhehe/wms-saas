package repository

import (
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Scope is a composable query fragment.
//
// It is the mechanism by which a module's repository narrows a base query
// without the base repository knowing anything about the module's domain.
//
// Scope exposes *gorm.DB, which means it can only be used from inside a
// repository package — the one layer permitted to see GORM. Services compose
// behaviour by calling repository methods, never by passing Scopes.
type Scope func(*gorm.DB) *gorm.DB

// apply folds scopes over a query in order.
func apply(query *gorm.DB, scopes []Scope) *gorm.DB {
	for _, scope := range scopes {
		if scope != nil {
			query = scope(query)
		}
	}
	return query
}

// ForCompany restricts a query to one tenant.
//
// This is the single most important scope in the system. Every query against a
// tenant-owned table must carry it: a missing tenant filter is not a bug that
// returns too many rows, it is one company reading another company's stock.
//
// It is not applied automatically by the base repository, because not every
// table is tenant-owned. Module repositories are responsible for applying it,
// which is why RepositoryConvention.md requires companyID to be a mandatory
// parameter of every module repository method — making it impossible to forget
// without failing to compile.
func ForCompany(companyID uuid.UUID) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("company_id = ?", companyID)
	}
}

// ByID restricts a query to a single primary key.
func ByID(id uuid.UUID) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("id = ?", id)
	}
}

// Search applies a case-insensitive partial match across columns.
//
// Column names come from the calling repository as compile-time constants,
// never from client input — the search *term* is parameterised, but the column
// list is interpolated and would be an injection vector otherwise.
//
// ILIKE '%term%' cannot use a B-tree index. It is acceptable at the scale of an
// admin list; a full-text or trigram index is the answer when a table grows
// past a few hundred thousand rows.
func Search(term string, columns ...string) Scope {
	return func(db *gorm.DB) *gorm.DB {
		term = strings.TrimSpace(term)
		if term == "" || len(columns) == 0 {
			return db
		}

		// Escape the LIKE metacharacters so a search for "50%" does not become
		// a wildcard that matches every row.
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(term)
		pattern := "%" + escaped + "%"

		conditions := make([]string, 0, len(columns))
		args := make([]any, 0, len(columns))

		for _, column := range columns {
			conditions = append(conditions, column+" ILIKE ?")
			args = append(args, pattern)
		}

		// Wrapped in parentheses so OR-ing the search terms cannot widen an
		// outer AND — without them, `company_id = ? AND a ILIKE ? OR b ILIKE ?`
		// leaks every row matching b, across all tenants.
		return db.Where("("+strings.Join(conditions, " OR ")+")", args...)
	}
}

// WithDeleted includes soft-deleted rows.
//
// GORM excludes them by default. Opting in must be explicit and rare: a restore
// flow, an audit view, an admin purge job. See docs/SoftDeleteConvention.md.
func WithDeleted() Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Unscoped()
	}
}

// OnlyDeleted returns exclusively soft-deleted rows, for a "recycle bin" view.
func OnlyDeleted() Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Unscoped().Where("deleted_at IS NOT NULL")
	}
}

// Where is an escape hatch for a one-off condition.
//
// Prefer a named scope: `ForCompany(id)` states intent at the call site, while
// an inline Where("company_id = ?", id) is invisible to a reviewer scanning for
// missing tenant filters.
func Where(query string, args ...any) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(query, args...)
	}
}

// Preload eager-loads an association.
//
// Use it whenever a list result renders related data. Loading associations
// lazily inside a loop is the N+1 query problem, and it is the single most
// common cause of a list endpoint that is fast in development and unusable in
// production.
func Preload(association string, args ...any) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Preload(association, args...)
	}
}

// Lock takes FOR UPDATE row locks.
//
// Required for read-modify-write on stock levels: without it, two concurrent
// picks can both read quantity 10, both subtract 6, and both write 4 — leaving
// the warehouse with negative physical stock and a correct-looking number.
//
// Only valid inside a transaction; the lock is released at commit.
func Lock() Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Clauses(lockForUpdate())
	}
}
