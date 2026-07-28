// Package cache adapts Redis to the port.Cache interface.
//
// This package is the only place in the codebase, outside of connection
// bootstrap, that may import go-redis. Replacing Redis means writing a sibling
// file here and changing one line in bootstrap.
package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
)

// RedisCache implements port.Cache on top of go-redis.
type RedisCache struct {
	client *redis.Client
	// prefix namespaces every key. It keeps environments that share a Redis
	// instance from colliding, and makes DeleteByPrefix safe to expose.
	prefix string
}

var _ port.Cache = (*RedisCache)(nil)

// NewRedisCache builds the adapter. prefix is typically the application name.
func NewRedisCache(client *redis.Client, prefix string) *RedisCache {
	return &RedisCache{client: client, prefix: prefix}
}

// key applies the namespace.
func (c *RedisCache) key(k string) string {
	if c.prefix == "" {
		return k
	}
	return c.prefix + ":" + k
}

// Get returns the value for key, or port.ErrCacheMiss.
func (c *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := c.client.Get(ctx, c.key(key)).Bytes()

	// redis.Nil is go-redis' "no such key" sentinel. Translating it here means
	// callers never import go-redis just to recognise a cache miss.
	if errors.Is(err, redis.Nil) {
		return nil, port.ErrCacheMiss
	}
	if err != nil {
		return nil, fmt.Errorf("cache: get %q: %w", key, err)
	}

	return value, nil
}

// Set stores value with an optional ttl.
func (c *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := c.client.Set(ctx, c.key(key), value, ttl).Err(); err != nil {
		return fmt.Errorf("cache: set %q: %w", key, err)
	}
	return nil
}

// Delete removes the given keys.
func (c *RedisCache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	prefixed := make([]string, len(keys))
	for i, k := range keys {
		prefixed[i] = c.key(k)
	}

	if err := c.client.Del(ctx, prefixed...).Err(); err != nil {
		return fmt.Errorf("cache: delete: %w", err)
	}
	return nil
}

// Exists reports whether key is present.
func (c *RedisCache) Exists(ctx context.Context, key string) (bool, error) {
	count, err := c.client.Exists(ctx, c.key(key)).Result()
	if err != nil {
		return false, fmt.Errorf("cache: exists %q: %w", key, err)
	}
	return count > 0, nil
}

// Increment atomically adds delta.
func (c *RedisCache) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	value, err := c.client.IncrBy(ctx, c.key(key), delta).Result()
	if err != nil {
		return 0, fmt.Errorf("cache: increment %q: %w", key, err)
	}
	return value, nil
}

// Expire sets the ttl of an existing key.
func (c *RedisCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if err := c.client.Expire(ctx, c.key(key), ttl).Err(); err != nil {
		return fmt.Errorf("cache: expire %q: %w", key, err)
	}
	return nil
}

// DeleteByPrefix removes every key under a prefix.
//
// It uses SCAN with batched deletes, never KEYS. KEYS is O(n) over the whole
// keyspace and blocks the single-threaded Redis server for the duration — on a
// production dataset that is an outage, not a slow query.
func (c *RedisCache) DeleteByPrefix(ctx context.Context, prefix string) error {
	pattern := c.key(prefix) + "*"

	var (
		cursor uint64
		batch  []string
	)

	for {
		keys, next, err := c.client.Scan(ctx, cursor, pattern, scanBatchSize).Result()
		if err != nil {
			return fmt.Errorf("cache: scan %q: %w", prefix, err)
		}

		batch = append(batch, keys...)

		// Delete in chunks so a large match set does not build one enormous
		// command or hold every key in memory at once.
		if len(batch) >= deleteBatchSize {
			if err := c.client.Del(ctx, batch...).Err(); err != nil {
				return fmt.Errorf("cache: delete by prefix %q: %w", prefix, err)
			}
			batch = batch[:0]
		}

		cursor = next
		if cursor == 0 {
			break
		}
	}

	if len(batch) > 0 {
		if err := c.client.Del(ctx, batch...).Err(); err != nil {
			return fmt.Errorf("cache: delete by prefix %q: %w", prefix, err)
		}
	}

	return nil
}

const (
	scanBatchSize   = 256
	deleteBatchSize = 512
)
