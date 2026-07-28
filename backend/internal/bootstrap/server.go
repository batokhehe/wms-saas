package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/config"
)

// Server wraps net/http with the timeouts and shutdown semantics the service
// needs.
type Server struct {
	http *http.Server
	cfg  config.HTTPConfig
	log  *zap.Logger
}

// NewServer builds the HTTP server.
//
// Every timeout is set explicitly. Go's zero-value http.Server has no timeouts
// at all, which lets a slow or malicious client hold a connection (and its
// goroutine) open indefinitely.
func NewServer(handler *gin.Engine, cfg config.HTTPConfig, log *zap.Logger) *Server {
	return &Server{
		http: &http.Server{
			Addr:         cfg.Address(),
			Handler:      handler,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
			IdleTimeout:  cfg.IdleTimeout,
			// Bound the header read separately so a client that sends headers
			// one byte at a time cannot occupy a connection.
			ReadHeaderTimeout: 10 * time.Second,
			MaxHeaderBytes:    1 << 20, // 1 MiB
			ErrorLog:          zap.NewStdLog(log),
		},
		cfg: cfg,
		log: log,
	}
}

// Start begins serving and blocks until the server stops.
//
// http.ErrServerClosed is the normal outcome of a graceful shutdown and is
// translated to nil, so callers only see genuine failures.
func (s *Server) Start() error {
	s.log.Info("http server listening", zap.String("address", s.cfg.Address()))

	if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server: listen on %s: %w", s.cfg.Address(), err)
	}

	return nil
}

// Shutdown stops accepting new connections and waits for in-flight requests to
// finish, up to the configured shutdown timeout.
//
// If the budget expires, the server is force-closed: dropping a few long
// requests is preferable to an orchestrator SIGKILLing the process mid-write.
func (s *Server) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, s.cfg.ShutdownTimeout)
	defer cancel()

	s.log.Info("http server shutting down",
		zap.Duration("grace_period", s.cfg.ShutdownTimeout),
	)

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		s.log.Warn("graceful shutdown exceeded grace period, forcing close", zap.Error(err))

		if closeErr := s.http.Close(); closeErr != nil {
			return fmt.Errorf("server: force close: %w", closeErr)
		}
		return fmt.Errorf("server: graceful shutdown: %w", err)
	}

	s.log.Info("http server stopped accepting connections")
	return nil
}
