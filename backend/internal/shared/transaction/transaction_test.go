package transaction

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

// These tests cover the context plumbing, which is the part that decides
// whether a repository enrols in the caller's transaction. Behaviour that needs
// a live connection (commit, rollback, savepoints) belongs in an integration
// test against a real PostgreSQL instance.

func TestFromEmptyContext(t *testing.T) {
	if _, ok := From(context.Background()); ok {
		t.Error("From(background) reported a transaction where none exists")
	}
	if InTransaction(context.Background()) {
		t.Error("InTransaction(background) = true, want false")
	}
}

func TestFromNilContext(t *testing.T) {
	// A nil context reaches this code from tests and from background jobs that
	// forgot to plumb one. It must not panic.
	if _, ok := From(nil); ok { //nolint:staticcheck // deliberately testing the nil case
		t.Error("From(nil) reported a transaction")
	}
}

func TestIntoAndFromRoundTrip(t *testing.T) {
	tx := &gorm.DB{}

	ctx := Into(context.Background(), tx)

	got, ok := From(ctx)
	if !ok {
		t.Fatal("From() did not find the transaction that Into() stored")
	}
	if got != tx {
		t.Error("From() returned a different handle than Into() stored")
	}
	if !InTransaction(ctx) {
		t.Error("InTransaction() = false inside a transaction context")
	}
}

// TestDBPrefersTransaction is the behaviour that makes repositories
// transaction-aware without any explicit plumbing.
func TestDBPrefersTransaction(t *testing.T) {
	tx := &gorm.DB{}
	fallback := &gorm.DB{}

	if got := DB(Into(context.Background(), tx), fallback); got != tx {
		t.Error("DB() used the fallback handle while a transaction was in progress")
	}
}

// The fallback branch of DB() — the one taken outside a transaction — calls
// WithContext, which requires a handle built by gorm.Open. A hand-constructed
// *gorm.DB panics inside GORM's own Session/Statement plumbing, so a unit test
// there would assert on GORM internals rather than on our logic. That path
// belongs in an integration test against a real connection; the branch
// selection itself is covered by TestFromEmptyContext above.

// TestContextKeyIsPrivate proves no other package can inject a transaction by
// guessing a key — the reason ctxKey is an unexported struct rather than a
// string constant.
func TestContextKeyIsPrivate(t *testing.T) {
	ctx := context.WithValue(context.Background(), "transaction", &gorm.DB{}) //nolint:staticcheck,revive // deliberately using a string key

	if _, ok := From(ctx); ok {
		t.Error("From() honoured a foreign context key")
	}
}
