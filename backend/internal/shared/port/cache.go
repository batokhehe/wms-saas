// Package port declares the technology-agnostic interfaces that business
// modules are allowed to depend on.
//
// This is the dependency-inversion boundary of the system. Modules import
// `port`; they never import redis, asynq, minio or any other vendor SDK. The
// concrete adapters live in internal/shared/adapter and are wired in bootstrap,
// so swapping Redis for Memcached or Asynq for RabbitMQ touches exactly one
// file per technology and zero business code.
package port

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrCacheMiss is returned by Cache.Get when the key is absent or expired.
//
// A miss is a normal control-flow outcome, not a fault, so it is a sentinel
// callers compare with errors.Is rather than an *apperror.Error.
var ErrCacheMiss = errors.New("cache: key not found")

// Cache is a key/value store with expiry.
//
// The interface deliberately speaks in []byte rather than `any`: serialisation
// is the caller's decision, and keeping it out of the interface means an
// implementation cannot silently change the wire format of cached data.
type Cache interface {
	// Get returns the raw value for key, or ErrCacheMiss if it is not present.
	Get(ctx context.Context, key string) ([]byte, error)

	// Set stores value under key. A ttl of zero means no expiry.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes the given keys. Deleting an absent key is not an error.
	Delete(ctx context.Context, keys ...string) error

	// Exists reports whether key is present.
	Exists(ctx context.Context, key string) (bool, error)

	// Increment atomically adds delta and returns the new value. It is the
	// primitive behind rate limiting and counters, and must be atomic across
	// replicas — which is why it belongs on the interface rather than being
	// emulated with Get followed by Set.
	Increment(ctx context.Context, key string, delta int64) (int64, error)

	// Expire sets or refreshes the ttl of an existing key.
	Expire(ctx context.Context, key string, ttl time.Duration) error

	// DeleteByPrefix removes every key sharing a prefix. It backs tenant-scoped
	// invalidation ("drop everything cached for company X").
	DeleteByPrefix(ctx context.Context, prefix string) error
}

// GetJSON reads key and unmarshals it into T.
//
// Generic helpers are free functions rather than interface methods because Go
// does not permit type parameters on methods. Keeping them here means every
// implementation gets typed access for free.
func GetJSON[T any](ctx context.Context, c Cache, key string) (T, error) {
	var value T

	raw, err := c.Get(ctx, key)
	if err != nil {
		return value, err
	}

	if err := json.Unmarshal(raw, &value); err != nil {
		// Corrupt cache entries must not surface as domain errors; the caller
		// should treat this like a miss and recompute.
		return value, ErrCacheMiss
	}

	return value, nil
}

// SetJSON marshals value and stores it under key.
func SetJSON[T any](ctx context.Context, c Cache, key string, value T, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, raw, ttl)
}

// Remember returns the cached value for key, computing and storing it via load
// on a miss. It is the read-through pattern every module would otherwise
// reimplement slightly differently.
func Remember[T any](
	ctx context.Context,
	c Cache,
	key string,
	ttl time.Duration,
	load func(ctx context.Context) (T, error),
) (T, error) {
	if cached, err := GetJSON[T](ctx, c, key); err == nil {
		return cached, nil
	}

	value, err := load(ctx)
	if err != nil {
		return value, err
	}

	// A cache write failure must not fail the request: the data is already
	// correct, the cache is only an optimisation.
	_ = SetJSON(ctx, c, key, value, ttl)

	return value, nil
}
