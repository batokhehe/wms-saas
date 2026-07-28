package id

import (
	"testing"

	"github.com/google/uuid"
)

func TestUUIDGeneratesDistinctIDs(t *testing.T) {
	gen := NewUUID()

	seen := make(map[uuid.UUID]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		got := gen.NewID()

		if got == uuid.Nil {
			t.Fatal("NewID() returned the nil UUID")
		}
		if _, duplicate := seen[got]; duplicate {
			t.Fatalf("NewID() returned a duplicate: %s", got)
		}
		seen[got] = struct{}{}
	}
}

// TestSequentialIsPredictable is the property the whole abstraction exists for:
// a test can state the exact identifier its code will produce.
func TestSequentialIsPredictable(t *testing.T) {
	gen := NewSequential()

	want := []string{
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003",
	}

	for i, expected := range want {
		if got := gen.NewID().String(); got != expected {
			t.Errorf("call %d: NewID() = %s, want %s", i+1, got, expected)
		}
	}
}

// TestSequentialProducesValidV4 matters because a fake emitting malformed UUIDs
// would fail `binding:"uuid4"` validation and make tests fail for reasons
// unrelated to what they assert.
func TestSequentialProducesValidV4(t *testing.T) {
	got := NewSequential().NewID()

	parsed, err := uuid.Parse(got.String())
	if err != nil {
		t.Fatalf("NewID() produced an unparseable UUID %q: %v", got, err)
	}
	if parsed.Version() != 4 {
		t.Errorf("version = %d, want 4", parsed.Version())
	}
	if parsed.Variant() != uuid.RFC4122 {
		t.Errorf("variant = %v, want RFC4122", parsed.Variant())
	}
}

// TestSequentialPeekDoesNotConsume lets a test declare its expectation before
// exercising the code under test.
func TestSequentialPeekDoesNotConsume(t *testing.T) {
	gen := NewSequential()

	predicted := gen.Peek(1)

	if got := gen.NewID(); got != predicted {
		t.Errorf("NewID() = %s, want the peeked %s", got, predicted)
	}
}
