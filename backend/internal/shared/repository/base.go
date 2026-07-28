// Package repository provides the generic base every module repository
// composes.
//
// It removes the seven CRUD methods that would otherwise be copy-pasted — and
// subtly diverge — across every module, and it centralises three things that
// are easy to get wrong exactly once instead of once per module: identifier
// assignment, error translation, and transaction enrolment.
//
// GORM does not leak past this package. Base returns entities and typed
// application errors; only Scope exposes a *gorm.DB, and Scopes are usable only
// from inside a repository package.
package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/batokhehe/wms-saas/backend/internal/shared/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/infra/postgres"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Entity constrains the pointer form of a base repository's type parameter.
//
// The two-parameter shape (T, PT) is the standard Go workaround for a genuine
// language limitation: methods needed by the repository — SetID in particular —
// have pointer receivers, but the repository must also be able to allocate a
// fresh T with `new`. A single parameter can express one or the other, not
// both. Declaring `Base[Product, *Product]` gives us the entity type for slices
// and the pointer type for mutation.
//
// It also enforces the embedding rule at compile time: a type that does not
// embed entity.BaseEntity cannot satisfy Identifiable, so it cannot be used
// with Base at all.
type Entity[T any] interface {
	*T
	entity.Identifiable
}

// Base is a generic CRUD repository for a single entity type.
//
// Module repositories embed it and add domain queries:
//
//	type productRepository struct {
//	    *repository.Base[entity.Product, *entity.Product]
//	}
type Base[T any, PT Entity[T]] struct {
	db  *gorm.DB
	ids port.IDGenerator
	// op prefixes error operations, e.g. "product.repository". It makes a
	// translated error name the module it came from without every method
	// repeating the string.
	op string
}

// New builds a base repository.
//
//	repository.New[entity.Product, *entity.Product](deps.DB, deps.IDs, "product.repository")
func New[T any, PT Entity[T]](db *gorm.DB, ids port.IDGenerator, op string) *Base[T, PT] {
	return &Base[T, PT]{db: db, ids: ids, op: op}
}

// DB exposes the underlying handle to the embedding repository, resolved
// against any transaction on ctx.
//
// It is how a module repository writes a query the base does not cover, while
// still enrolling in the caller's transaction. It returns *gorm.DB and is
// therefore usable only from a repository package.
func (r *Base[T, PT]) DB(ctx context.Context) *gorm.DB {
	return transaction.DB(ctx, r.db)
}

// query returns a scoped, transaction-aware query for T.
func (r *Base[T, PT]) query(ctx context.Context, scopes []Scope) *gorm.DB {
	var model T
	return apply(r.DB(ctx).Model(PT(&model)), scopes)
}

func (r *Base[T, PT]) opName(method string) string { return r.op + "." + method }

// Create inserts a new entity.
//
// It assigns an identifier from the injected generator when the entity does not
// already have one. This is what makes "never call uuid.New() in a module"
// enforceable rather than aspirational: the module does not have to remember,
// because the repository does it.
//
// A caller may still set the ID beforehand — for an idempotent create driven by
// a client-supplied key — and it is preserved.
func (r *Base[T, PT]) Create(ctx context.Context, e PT) error {
	if e == nil {
		return apperror.Internal("Cannot create a nil entity").WithOp(r.opName("Create"))
	}

	if !e.IsPersisted() {
		e.SetID(r.ids.NewID())
	}

	err := r.DB(ctx).Create(e).Error
	return postgres.TranslateError(err, r.opName("Create"))
}

// CreateMany inserts entities in batches.
//
// Batching matters at import scale: 10,000 individual INSERTs is 10,000 network
// round trips, while batches of 200 is 50. The batch size is bounded because
// PostgreSQL caps a statement at 65535 bind parameters, and one oversized batch
// fails at the protocol level rather than gracefully.
func (r *Base[T, PT]) CreateMany(ctx context.Context, entities []T, batchSize int) error {
	if len(entities) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	for i := range entities {
		e := PT(&entities[i])
		if !e.IsPersisted() {
			e.SetID(r.ids.NewID())
		}
	}

	err := r.DB(ctx).CreateInBatches(entities, batchSize).Error
	return postgres.TranslateError(err, r.opName("CreateMany"))
}

// Update persists every field of an existing entity.
//
// It refuses an entity with no identifier: GORM's Save would interpret that as
// an insert, silently creating a duplicate row instead of updating.
func (r *Base[T, PT]) Update(ctx context.Context, e PT) error {
	if e == nil {
		return apperror.Internal("Cannot update a nil entity").WithOp(r.opName("Update"))
	}
	if !e.IsPersisted() {
		return apperror.Internal("Cannot update an entity without an ID").
			WithOp(r.opName("Update"))
	}

	err := r.DB(ctx).Save(e).Error
	return postgres.TranslateError(err, r.opName("Update"))
}

// ErrConcurrentModification reports that another writer changed a row after
// the caller read it. It is deliberately a sentinel so services can add the
// current version without inspecting SQL or driver errors.
var ErrConcurrentModification = errors.New("concurrent modification")

// UpdateOptimistic persists an aggregate only when its version still matches.
// The single UPDATE is the concurrency boundary: no SELECT FOR UPDATE and no
// in-process mutex can protect writes made by another API instance.
func (r *Base[T, PT]) UpdateOptimistic(ctx context.Context, e PT) error {
	if e == nil {
		return apperror.Internal("Cannot update a nil entity").WithOp(r.opName("UpdateOptimistic"))
	}
	if !e.IsPersisted() {
		return apperror.Internal("Cannot update an entity without an ID").WithOp(r.opName("UpdateOptimistic"))
	}
	if e.GetVersion() == 0 {
		return apperror.Internal("Cannot update an entity without a version").WithOp(r.opName("UpdateOptimistic"))
	}

	// Advance only the persistence model. The aggregate remains immutable with
	// respect to Version; a later read observes the new token.
	expectedVersion := e.GetVersion()
	nextVersion := expectedVersion + 1
	e.SetVersion(nextVersion)
	result := r.DB(ctx).Model(e).Where("id = ? AND version = ?", e.GetID(), expectedVersion).
		Select("*").Updates(e)
	if result.Error != nil {
		return postgres.TranslateError(result.Error, r.opName("UpdateOptimistic"))
	}
	if result.RowsAffected == 0 {
		return apperror.Conflict("The resource was changed by another request").
			WithCause(ErrConcurrentModification).WithOp(r.opName("UpdateOptimistic"))
	}
	return nil
}

// UpdateFields applies a partial update and reports NOT_FOUND when nothing
// matched.
//
// The RowsAffected check is the point. Without it, updating a row belonging to
// another tenant — or an already-deleted one — succeeds silently, and the API
// returns 200 for an operation that changed nothing.
func (r *Base[T, PT]) UpdateFields(
	ctx context.Context,
	id uuid.UUID,
	fields map[string]any,
	scopes ...Scope,
) error {
	if len(fields) == 0 {
		return nil
	}

	result := r.query(ctx, append(scopes, ByID(id))).Updates(fields)
	if result.Error != nil {
		return postgres.TranslateError(result.Error, r.opName("UpdateFields"))
	}
	if result.RowsAffected == 0 {
		return apperror.NotFound("The requested resource was not found").
			WithOp(r.opName("UpdateFields"))
	}

	return nil
}

// Delete soft-deletes an entity by id.
//
// GORM sets deleted_at rather than issuing a DELETE, because entity.BaseEntity
// carries a gorm.DeletedAt. See docs/SoftDeleteConvention.md.
//
// Scopes must include the tenant filter, so a delete cannot cross tenants.
func (r *Base[T, PT]) Delete(ctx context.Context, id uuid.UUID, scopes ...Scope) error {
	var model T

	result := r.query(ctx, append(scopes, ByID(id))).Delete(PT(&model))
	if result.Error != nil {
		return postgres.TranslateError(result.Error, r.opName("Delete"))
	}
	if result.RowsAffected == 0 {
		// Distinguishing "did not exist" from "not yours" would leak the
		// existence of another tenant's data, so both produce NOT_FOUND.
		return apperror.NotFound("The requested resource was not found").
			WithOp(r.opName("Delete"))
	}

	return nil
}

// HardDelete permanently removes rows.
//
// It is not exposed to HTTP anywhere and must only be reached from an
// administrative purge job. Ordinary deletion is soft; see
// docs/SoftDeleteConvention.md for when a hard delete is legitimate (GDPR
// erasure, retention expiry).
func (r *Base[T, PT]) HardDelete(ctx context.Context, id uuid.UUID, scopes ...Scope) error {
	var model T

	result := r.query(ctx, append(scopes, ByID(id))).Unscoped().Delete(PT(&model))
	if result.Error != nil {
		return postgres.TranslateError(result.Error, r.opName("HardDelete"))
	}
	if result.RowsAffected == 0 {
		return apperror.NotFound("The requested resource was not found").
			WithOp(r.opName("HardDelete"))
	}

	return nil
}

// FindByID returns one entity, or a NOT_FOUND error.
func (r *Base[T, PT]) FindByID(ctx context.Context, id uuid.UUID, scopes ...Scope) (PT, error) {
	var found T

	err := r.query(ctx, append(scopes, ByID(id))).First(PT(&found)).Error
	if err != nil {
		return nil, postgres.TranslateError(err, r.opName("FindByID"))
	}

	return PT(&found), nil
}

// FindOne returns the first entity matching the scopes, or NOT_FOUND.
func (r *Base[T, PT]) FindOne(ctx context.Context, scopes ...Scope) (PT, error) {
	var found T

	err := r.query(ctx, scopes).First(PT(&found)).Error
	if err != nil {
		return nil, postgres.TranslateError(err, r.opName("FindOne"))
	}

	return PT(&found), nil
}

// FindAll returns a page of entities together with its metadata.
//
// The count runs before limit/offset, so the metadata describes the whole
// result set rather than the current page.
//
// It refuses a pagination.Request that has not been through Apply. That check
// is a security control, not tidiness: OrderClause interpolates a column name
// into SQL, and Apply is what constrains that name to the endpoint's
// allow-list. An unapplied request would also produce an unordered query, where
// PostgreSQL may return rows in any order between calls — so page 2 can repeat
// or skip rows from page 1.
func (r *Base[T, PT]) FindAll(
	ctx context.Context,
	page pagination.Request,
	scopes ...Scope,
) (pagination.Page[T], error) {
	if !page.Applied() {
		return pagination.Page[T]{}, apperror.Internal("Pagination was not validated").
			WithOp(r.opName("FindAll")).
			WithCause(errors.New("pagination.Request.Apply was not called before FindAll"))
	}

	var total int64
	if err := r.query(ctx, scopes).Count(&total).Error; err != nil {
		return pagination.Page[T]{}, postgres.TranslateError(err, r.opName("FindAll.Count"))
	}

	// Short-circuit: with no matches there is nothing to select, and skipping
	// the second query keeps an empty list endpoint to a single round trip.
	if total == 0 {
		return pagination.NewPage([]T{}, page, 0), nil
	}

	var items []T
	err := r.query(ctx, scopes).
		Order(page.OrderClause()).
		Limit(page.Limit).
		Offset(page.Offset()).
		Find(&items).Error
	if err != nil {
		return pagination.Page[T]{}, postgres.TranslateError(err, r.opName("FindAll"))
	}

	return pagination.NewPage(items, page, total), nil
}

// FindMany returns every entity matching the scopes, without pagination.
//
// Use it only where the result set is bounded by construction — the statuses of
// one order, the bins in one aisle. For anything a tenant can grow without
// limit, use FindAll: an unpaginated query over a large table is a slow request
// that gets slower every week until it times out.
func (r *Base[T, PT]) FindMany(ctx context.Context, scopes ...Scope) ([]T, error) {
	var items []T

	if err := r.query(ctx, scopes).Find(&items).Error; err != nil {
		return nil, postgres.TranslateError(err, r.opName("FindMany"))
	}

	return items, nil
}

// Count returns how many entities match the scopes.
func (r *Base[T, PT]) Count(ctx context.Context, scopes ...Scope) (int64, error) {
	var total int64

	if err := r.query(ctx, scopes).Count(&total).Error; err != nil {
		return 0, postgres.TranslateError(err, r.opName("Count"))
	}

	return total, nil
}

// Exists reports whether an entity with the given id matches the scopes.
//
// It uses SELECT 1 ... LIMIT 1 rather than Count, so PostgreSQL stops at the
// first match instead of scanning every qualifying row to produce a number the
// caller discards.
func (r *Base[T, PT]) Exists(ctx context.Context, id uuid.UUID, scopes ...Scope) (bool, error) {
	return r.ExistsBy(ctx, append(scopes, ByID(id))...)
}

// ExistsBy reports whether any entity matches the scopes.
func (r *Base[T, PT]) ExistsBy(ctx context.Context, scopes ...Scope) (bool, error) {
	// GORM has no first-class EXISTS helper. Selecting the literal 1 with a
	// LIMIT is the equivalent: when no row matches, Scan reports no error and
	// leaves the destination at zero.
	var found int64

	err := r.query(ctx, scopes).
		Select("1").
		Limit(1).
		Scan(&found).Error
	if err != nil {
		return false, postgres.TranslateError(err, r.opName("ExistsBy"))
	}

	return found == 1, nil
}

const defaultBatchSize = 200

// lockForUpdate builds the FOR UPDATE clause used by the Lock scope.
func lockForUpdate() clause.Locking {
	return clause.Locking{Strength: "UPDATE"}
}
