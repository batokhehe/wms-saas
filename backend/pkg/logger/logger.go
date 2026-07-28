// Package logger wraps zap with the conventions used across the service:
// JSON in production, human-readable console output in development, and a
// context-aware accessor so request-scoped fields travel with the request.
package logger

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Config describes how the logger should be built. It mirrors config.LogConfig
// but is declared separately so this package stays dependency-free and reusable.
type Config struct {
	Level       string
	Format      string
	AppName     string
	AppVersion  string
	Environment string
}

// contextKey is unexported so no other package can collide with our context key.
type contextKey struct{}

// New builds the root logger. Every log line carries the service name, version
// and environment, which is what makes logs from multiple tenants and replicas
// separable once they land in a central log store.
func New(cfg Config) (*zap.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.EncodeDuration = zapcore.StringDurationEncoder
	encoderCfg.StacktraceKey = "stacktrace"

	var encoder zapcore.Encoder
	switch strings.ToLower(cfg.Format) {
	case "console", "text":
		encoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	case "json", "":
		encoder = zapcore.NewJSONEncoder(encoderCfg)
	default:
		return nil, fmt.Errorf("logger: unknown format %q (want json or console)", cfg.Format)
	}

	core := zapcore.NewCore(encoder, zapcore.Lock(zapcore.AddSync(stdout())), level)

	log := zap.New(core,
		// Report the real call site rather than this wrapper.
		zap.AddCaller(),
		zap.AddCallerSkip(0),
		// Attach stack traces from error level up; below that they are noise.
		zap.AddStacktrace(zapcore.ErrorLevel),
		zap.Fields(
			zap.String("service", cfg.AppName),
			zap.String("version", cfg.AppVersion),
			zap.String("env", cfg.Environment),
		),
	)

	return log, nil
}

// WithContext stores a logger in ctx so downstream layers can log with the
// request's fields (request id, tenant, user) already attached.
func WithContext(ctx context.Context, log *zap.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, log)
}

// FromContext returns the logger stored by WithContext, or a no-op logger when
// none is present. Returning a usable logger instead of nil means callers never
// need a nil check.
func FromContext(ctx context.Context) *zap.Logger {
	if ctx == nil {
		return zap.NewNop()
	}
	if log, ok := ctx.Value(contextKey{}).(*zap.Logger); ok && log != nil {
		return log
	}
	return zap.NewNop()
}

func parseLevel(level string) (zapcore.Level, error) {
	if level == "" {
		return zapcore.InfoLevel, nil
	}
	parsed, err := zapcore.ParseLevel(strings.ToLower(level))
	if err != nil {
		return 0, fmt.Errorf("logger: unknown level %q: %w", level, err)
	}
	return parsed, nil
}
