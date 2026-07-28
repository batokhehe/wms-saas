// Package asynq owns the Asynq client lifecycle.
//
// It produces and manages the producer client. Turning that client into
// something business code may use is the job of internal/shared/adapter/queue.
package asynq

import (
	"fmt"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/config"
)

// Client wraps the Asynq producer.
type Client struct {
	Client *asynq.Client
	log    *zap.Logger
}

// RedisOptions translates the Redis config into the connection options Asynq
// expects. Exported so the worker binary can reuse it verbatim.
func RedisOptions(cfg config.RedisConfig) asynq.RedisClientOpt {
	return asynq.RedisClientOpt{
		Addr:         cfg.Address(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		PoolSize:     cfg.PoolSize,
	}
}

// New creates the task producer and verifies broker connectivity.
func New(cfg config.RedisConfig, log *zap.Logger) (*Client, error) {
	client := asynq.NewClient(RedisOptions(cfg))

	if err := client.Ping(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("asynq: ping %s: %w", cfg.Address(), err)
	}

	log.Info("asynq client ready",
		zap.String("address", cfg.Address()),
		zap.Int("db", cfg.DB),
	)

	return &Client{Client: client, log: log}, nil
}

// Close flushes and releases the producer's connections.
func (c *Client) Close() error {
	c.log.Info("closing asynq client")
	return c.Client.Close()
}
