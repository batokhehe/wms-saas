// Package transaction provides the sanctioned way to run work atomically.
//
// Business modules must never call db.Begin() themselves. A hand-rolled
// transaction has to get four things right at every call site — commit on
// success, roll back on error, roll back on panic, and never do either twice —
// and the failure mode of getting it wrong is a connection leaked from the pool
// under exactly the conditions where the system is already struggling.
//
// RunInTransaction gets those four things right once.
package transaction

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// ctxKey is unexported so no other package can inject a transaction into a
// context, accidentally or otherwise.
type ctxKey struct{}

// Manager runs work inside a database transaction.
//
// The interface is deliberately free of any GORM type: a service depends on
// Manager and remains ignorant of the persistence technology, exactly as it
// does with port.Cache and port.Queue.
type Manager interface {
	// RunInTransaction executes fn inside a transaction. The context passed to
	// fn carries the transaction, so any repository called with it enrolls
	// automatically.
	//
	// The transaction commits when fn returns nil and rolls back when it
	// returns an error or panics. The error from fn is returned unchanged, so
	// the caller still sees its own typed error rather than a wrapper.
	RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// GormManager implements Manager on top of GORM.
type GormManager struct {
	db *gorm.DB
}

var _ Manager = (*GormManager)(nil)

// NewGormManager builds the manager.
func NewGormManager(db *gorm.DB) *GormManager {
	return &GormManager{db: db}
}

// RunInTransaction executes fn atomically.
//
// If ctx already carries a transaction, fn joins it through a SAVEPOINT rather
// than opening a second one. This matters because PostgreSQL has no nested
// transactions: opening a second connection inside an open transaction would
// deadlock against the first one's locks, and the symptom is a request that
// hangs until the pool times out.
//
// With a savepoint, an inner failure rolls back only the inner work and the
// outer transaction decides what to do — which is the behaviour a caller
// composing two services naturally expects.
func (m *GormManager) RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if existing, ok := From(ctx); ok {
		return existing.Transaction(func(tx *gorm.DB) error {
			return fn(Into(ctx, tx))
		})
	}

	// gorm.Transaction handles commit, rollback and panic-rollback. It also
	// re-panics after rolling back, so a panic still reaches middleware.Recovery
	// instead of being swallowed into a generic error.
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(Into(ctx, tx))
	})
}

// Begin opens an explicit transaction.
//
// This is an escape hatch for the rare case where the unit of work cannot be
// expressed as a single function — a multi-step import driven by a stream, for
// example. Prefer RunInTransaction; it is impossible to leak.
//
// The caller MUST ensure exactly one of Commit or Rollback runs, on every path
// including panics:
//
//	tx, err := manager.Begin(ctx)
//	if err != nil {
//	    return err
//	}
//	defer tx.Rollback() // no-op once committed
//
//	if err := doWork(tx.Context()); err != nil {
//	    return err
//	}
//	return tx.Commit()
func (m *GormManager) Begin(ctx context.Context) (*Transaction, error) {
	if _, ok := From(ctx); ok {
		return nil, errors.New("transaction: Begin called inside an existing transaction; use RunInTransaction")
	}

	tx := m.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("transaction: begin: %w", tx.Error)
	}

	return &Transaction{tx: tx, ctx: Into(ctx, tx)}, nil
}

// Transaction is an explicitly managed transaction returned by Begin.
type Transaction struct {
	tx   *gorm.DB
	ctx  context.Context
	done bool
}

// Context returns a context carrying this transaction. Repositories called with
// it enroll automatically.
func (t *Transaction) Context() context.Context { return t.ctx }

// Commit finalises the transaction. Calling it twice is an error.
func (t *Transaction) Commit() error {
	if t.done {
		return errors.New("transaction: already committed or rolled back")
	}
	t.done = true

	if err := t.tx.Commit().Error; err != nil {
		return fmt.Errorf("transaction: commit: %w", err)
	}
	return nil
}

// Rollback aborts the transaction.
//
// It is a no-op after Commit, which is what makes `defer tx.Rollback()` safe to
// write unconditionally — the pattern that stops a transaction leaking on an
// early return.
func (t *Transaction) Rollback() error {
	if t.done {
		return nil
	}
	t.done = true

	if err := t.tx.Rollback().Error; err != nil {
		return fmt.Errorf("transaction: rollback: %w", err)
	}
	return nil
}

// ---------- context plumbing ----------
//
// These three functions are the only reason `gorm` appears in a signature here.
// They are called by repositories, which are already permitted to see GORM;
// services use Manager and never touch them.

// Into returns a context carrying tx.
func Into(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, ctxKey{}, tx)
}

// From extracts a transaction from ctx, if one is in progress.
func From(ctx context.Context) (*gorm.DB, bool) {
	if ctx == nil {
		return nil, false
	}
	tx, ok := ctx.Value(ctxKey{}).(*gorm.DB)
	return tx, ok && tx != nil
}

// DB returns the transaction from ctx, or fallback when none is in progress.
//
// Every repository query starts here. It is what makes a repository
// transaction-aware without any explicit plumbing: the same method works inside
// and outside a transaction, and the caller decides which by choosing the
// context it passes.
func DB(ctx context.Context, fallback *gorm.DB) *gorm.DB {
	if tx, ok := From(ctx); ok {
		return tx
	}
	return fallback.WithContext(ctx)
}

// InTransaction reports whether ctx is inside a transaction. Useful for
// asserting a precondition in code that must not run standalone.
func InTransaction(ctx context.Context) bool {
	_, ok := From(ctx)
	return ok
}
