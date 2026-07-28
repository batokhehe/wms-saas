package service

import (
	"context"

	"github.com/google/uuid"
)

// UserDirectory resolves a person by email address.
//
// It is declared HERE, in the consumer, rather than imported from the auth
// module — the consumer-side interface pattern required by ModuleConvention §6.
// Bootstrap injects an adapter over the identity module's user repository.
//
// This is what keeps the tenancy module from depending on identity internals.
// The interface is deliberately as narrow as the need: inviting a colleague
// requires knowing whether an account exists and what its id is, and nothing
// else. A wider interface — returning the whole user, or offering a search —
// would let tenancy browse identity data and would turn every future change to
// the user entity into a change here.
type UserDirectory interface {
	// FindIDByEmail returns the account id for an address.
	//
	// It returns apperror.ErrNotFound when no account exists. The invite flow
	// converts that into a deliberately vague message rather than surfacing it:
	// telling an inviter "no such account" turns the endpoint into an
	// enumeration oracle for anyone holding a single valid membership.
	FindIDByEmail(ctx context.Context, email string) (uuid.UUID, error)
}

// UserDirectoryFunc adapts a plain function into a UserDirectory, for tests.
type UserDirectoryFunc func(ctx context.Context, email string) (uuid.UUID, error)

// FindIDByEmail implements UserDirectory.
func (f UserDirectoryFunc) FindIDByEmail(ctx context.Context, email string) (uuid.UUID, error) {
	return f(ctx, email)
}
