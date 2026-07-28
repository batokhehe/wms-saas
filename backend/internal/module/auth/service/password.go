package service

import (
	"golang.org/x/crypto/bcrypt"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// PasswordHasher hashes and verifies passwords.
//
// It is an interface so the service can be tested without paying bcrypt's cost
// on every case — a table-driven test of twenty login scenarios at cost 12
// takes five seconds of pure key stretching, which is enough friction that
// people stop running the tests.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Verify reports whether password matches hash. It returns an error only
	// for a malformed hash, never for a wrong password: "wrong" is a normal
	// outcome and is reported as false.
	Verify(hash, password string) bool
}

// BcryptHasher implements PasswordHasher using bcrypt.
//
// bcrypt rather than SHA-256 or PBKDF2 because passwords are LOW-entropy
// secrets: the defence is making each guess expensive. bcrypt is deliberately
// slow and memory-hard enough to resist GPU parallelism, and its cost factor is
// tunable upward as hardware improves without invalidating existing hashes —
// the cost is embedded in each hash, so old and new coexist.
//
// Argon2id would be a defensible modern alternative. bcrypt is chosen for its
// maturity, its presence in the Go extended standard library, and the fact that
// its only tuning knob is hard to misconfigure. Argon2's three interacting
// parameters are easy to get wrong in a way that looks fine.
type BcryptHasher struct {
	cost int
}

var _ PasswordHasher = (*BcryptHasher)(nil)

// NewBcryptHasher builds a hasher at the configured cost.
//
// The cost is validated at config load (see config.PasswordConfig.validate), so
// an out-of-range value fails the process at boot rather than here.
func NewBcryptHasher(cost int) *BcryptHasher {
	return &BcryptHasher{cost: cost}
}

// Hash derives a password hash.
//
// The error is deliberately opaque: bcrypt fails only on an invalid cost or a
// password over 72 bytes, and both are caught earlier — cost at boot, length in
// the validator. Reaching here means a bug, so the client gets a generic 500
// and the detail goes to the log.
func (h *BcryptHasher) Hash(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", apperror.Internal("Could not process the password").
			WithOp("auth.password.Hash").
			WithCause(err)
	}

	return string(hashed), nil
}

// Verify reports whether password matches hash.
//
// bcrypt.CompareHashAndPassword hashes the candidate with the salt and cost
// embedded in the stored hash, then compares in constant time — so this leaks
// nothing about how much of the password was correct.
//
// The comparison's duration still depends on the stored hash's cost factor,
// which is why the service compares against a dummy hash when no user exists;
// see authenticate() in service.go.
func (h *BcryptHasher) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
