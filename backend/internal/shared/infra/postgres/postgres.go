// Package postgres owns the PostgreSQL connection lifecycle.
//
// It is infrastructure, not an adapter: it produces a *gorm.DB and manages its
// pool. Repositories consume that handle directly, which is the one deliberate
// exception to the ports-and-adapters rule (see internal/shared/module).
package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"github.com/batokhehe/wms-saas/backend/internal/config"
)

// DB wraps the GORM handle together with the means to close it.
type DB struct {
	DB  *gorm.DB
	log *zap.Logger
}

// New opens the connection pool, verifies it with a ping and applies pool
// limits. It fails fast: a service that cannot reach its database should not
// report itself as started.
func New(ctx context.Context, cfg config.DatabaseConfig, log *zap.Logger) (*DB, error) {
	gormCfg := &gorm.Config{
		Logger:         newGormLogger(cfg, log),
		NamingStrategy: schema.NamingStrategy{SingularTable: false},
		// The service assigns its own UUID primary keys, so GORM does not need
		// to wrap every write in a transaction to read generated values back.
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
		// Translate driver errors into gorm.ErrDuplicatedKey and friends, which
		// is what TranslateError below relies on.
		TranslateError: true,
		// GORM stamps CreatedAt/UpdatedAt with time.Now(), which carries the
		// server's local zone. The columns are TIMESTAMPTZ so the instant is
		// stored correctly either way, but the in-memory value would come back
		// in local time — and a test asserting on it would then pass in one
		// timezone and fail in another. Pinning it here keeps every timestamp
		// in the process UTC, matching the port.Clock contract.
		NowFunc: func() time.Time { return time.Now().UTC() },
	}

	db, err := gorm.Open(postgres.Open(cfg.DSN()), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: opening connection: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("postgres: obtaining sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(pingCtx); err != nil {
		// Close the half-open pool so a failed boot does not leak sockets.
		_ = sqlDB.Close()
		return nil, fmt.Errorf("postgres: ping %s:%d: %w", cfg.Host, cfg.Port, err)
	}

	log.Info("postgres connected",
		zap.String("host", cfg.Host),
		zap.Int("port", cfg.Port),
		zap.String("database", cfg.Name),
		zap.Int("max_open_conns", cfg.MaxOpenConns),
	)

	return &DB{DB: db, log: log}, nil
}

// Health verifies the connection is still usable, for the readiness probe.
func (p *DB) Health(ctx context.Context) error {
	sqlDB, err := p.DB.DB()
	if err != nil {
		return fmt.Errorf("postgres: obtaining sql.DB: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("postgres: ping: %w", err)
	}
	return nil
}

// Stats exposes pool counters for diagnostics and metrics.
func (p *DB) Stats() map[string]any {
	sqlDB, err := p.DB.DB()
	if err != nil {
		return map[string]any{}
	}

	s := sqlDB.Stats()
	return map[string]any{
		"open_connections": s.OpenConnections,
		"in_use":           s.InUse,
		"idle":             s.Idle,
		"wait_count":       s.WaitCount,
		"wait_duration":    s.WaitDuration.String(),
	}
}

// Close drains and shuts the pool down.
func (p *DB) Close() error {
	sqlDB, err := p.DB.DB()
	if err != nil {
		return err
	}
	p.log.Info("closing postgres connection pool")
	return sqlDB.Close()
}

// newGormLogger routes GORM's SQL logging through zap so query logs share the
// same format, level and destination as everything else.
func newGormLogger(cfg config.DatabaseConfig, log *zap.Logger) gormlogger.Interface {
	var level gormlogger.LogLevel
	switch strings.ToLower(cfg.LogLevel) {
	case "silent":
		level = gormlogger.Silent
	case "error":
		level = gormlogger.Error
	case "info":
		level = gormlogger.Info
	default:
		level = gormlogger.Warn
	}

	return gormlogger.New(
		&gormZapWriter{log: log.With(zap.String("component", "gorm"))},
		gormlogger.Config{
			SlowThreshold: cfg.SlowThreshold,
			LogLevel:      level,
			// ErrRecordNotFound is normal control flow, not a fault.
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
}

// gormZapWriter adapts GORM's Printf-style writer onto zap.
type gormZapWriter struct{ log *zap.Logger }

func (w *gormZapWriter) Printf(format string, args ...any) {
	w.log.Info(fmt.Sprintf(format, args...))
}
