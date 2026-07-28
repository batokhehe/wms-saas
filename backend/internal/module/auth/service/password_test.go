package service

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Cost 4 (bcrypt.MinCost) throughout: these tests exercise logic, not the work
// factor. At the production cost of 12 this file would take ~10 seconds, which
// is enough friction that people stop running it.
const testCost = bcrypt.MinCost

func TestHashProducesVerifiableHash(t *testing.T) {
	h := NewBcryptHasher(testCost)

	hash, err := h.Hash("Str0ng!Passw0rd")
	if err != nil {
		t.Fatalf("Hash() = %v, want nil", err)
	}

	if !h.Verify(hash, "Str0ng!Passw0rd") {
		t.Error("Verify() = false for the password that produced the hash")
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	h := NewBcryptHasher(testCost)

	hash, _ := h.Hash("Str0ng!Passw0rd")

	for _, wrong := range []string{
		"str0ng!passw0rd",  // case differs
		"Str0ng!Passw0r",   // truncated
		"Str0ng!Passw0rd ", // trailing space
		"",
	} {
		if h.Verify(hash, wrong) {
			t.Errorf("Verify() = true for wrong password %q", wrong)
		}
	}
}

// TestHashIsSalted is the property that stops a leaked table being cracked
// wholesale: identical passwords must not produce identical hashes, so an
// attacker cannot group users by hash or use a precomputed rainbow table.
func TestHashIsSalted(t *testing.T) {
	h := NewBcryptHasher(testCost)

	first, _ := h.Hash("Str0ng!Passw0rd")
	second, _ := h.Hash("Str0ng!Passw0rd")

	if first == second {
		t.Error("two hashes of the same password are identical; the salt is not being applied")
	}
	if !h.Verify(first, "Str0ng!Passw0rd") || !h.Verify(second, "Str0ng!Passw0rd") {
		t.Error("both hashes must still verify against the original password")
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	h := NewBcryptHasher(testCost)

	for _, bad := range []string{
		"",
		"not-a-hash",
		"$2a$12$tooshort",
		// A raw password stored where a hash belongs — the failure mode the
		// VARCHAR(60) column width is meant to catch at the database level.
		"Str0ng!Passw0rd",
	} {
		if h.Verify(bad, "Str0ng!Passw0rd") {
			t.Errorf("Verify() = true for malformed hash %q", bad)
		}
	}
}

// TestHashRejectsOverlongPassword documents bcrypt's hard 72-byte limit, which
// is why validator.MaxPasswordLength exists. Without that validation a user
// with a 100-character passphrase would silently have the last 28 characters
// ignored.
func TestHashRejectsOverlongPassword(t *testing.T) {
	h := NewBcryptHasher(testCost)

	if _, err := h.Hash(strings.Repeat("a", 73)); err == nil {
		t.Error("Hash() accepted a 73-byte password; bcrypt cannot hash beyond 72 bytes")
	}
}

// TestCostIsEmbeddedInHash proves the cost factor can be raised later without
// invalidating existing hashes: each hash carries the cost it was made with, so
// old and new coexist.
func TestCostIsEmbeddedInHash(t *testing.T) {
	hash, err := NewBcryptHasher(5).Hash("Str0ng!Passw0rd")
	if err != nil {
		t.Fatalf("Hash() = %v", err)
	}

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("Cost() = %v", err)
	}
	if cost != 5 {
		t.Errorf("embedded cost = %d, want 5", cost)
	}

	// A hasher configured at a different cost still verifies the old hash.
	if !NewBcryptHasher(6).Verify(hash, "Str0ng!Passw0rd") {
		t.Error("a hasher at a different cost could not verify an existing hash")
	}
}

// TestDummyHashIsValid guards the timing-attack defence in authenticate(). If
// the constant were malformed, bcrypt would fail fast instead of burning the
// same time as a real verification, reopening the enumeration oracle.
func TestDummyHashIsValid(t *testing.T) {
	if cost, err := bcrypt.Cost([]byte(dummyHash)); err != nil {
		t.Fatalf("dummyHash is not a valid bcrypt hash: %v", err)
	} else if cost < 10 {
		t.Errorf("dummyHash cost = %d; too low to mimic a real verification", cost)
	}

	// It must never match anything a caller could submit.
	if NewBcryptHasher(testCost).Verify(dummyHash, "") {
		t.Error("dummyHash verified against the empty password")
	}
}
