// Package redis owns the Redis connection lifecycle.
//
// It produces and manages the client. Turning that client into something
// business code may use is the job of internal/shared/adapter/cache.
package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/config"
)

// Client wraps the go-redis client with startup verification and lifecycle
// management.
type Client struct {
	Client *goredis.Client
	log    *zap.Logger
}

// New dials Redis and verifies the connection with a PING.
func New(ctx context.Context, cfg config.RedisConfig, log *zap.Logger) (*Client, error) {
	client := goredis.NewClient(&goredis.Options{
		Addr:         cfg.Address(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis: ping %s: %w", cfg.Address(), err)
	}

	log.Info("redis connected",
		zap.String("address", cfg.Address()),
		zap.Int("db", cfg.DB),
		zap.Int("pool_size", cfg.PoolSize),
	)

	return &Client{Client: client, log: log}, nil
}

// Health verifies the connection is still usable, for the readiness probe.
func (c *Client) Health(ctx context.Context) error {
	if err := c.Client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: ping: %w", err)
	}
	return nil
}

// Stats exposes pool counters for diagnostics and metrics.
func (c *Client) Stats() map[string]any {
	s := c.Client.PoolStats()
	return map[string]any{
		"total_conns": s.TotalConns,
		"idle_conns":  s.IdleConns,
		"stale_conns": s.StaleConns,
		"hits":        s.Hits,
		"misses":      s.Misses,
		"timeouts":    s.Timeouts,
	}
}

// Close releases the connection pool.
func (c *Client) Close() error {
	c.log.Info("closing redis connection pool")
	return c.Client.Close()
}
