package bootstrap

import (
	"context"
	"errors"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/config"
)

// App ties the container and the HTTP server into a single runnable unit.
type App struct {
	container *Container
	server    *Server
}

// New builds the application from configuration.
func New(ctx context.Context, cfg *config.Config) (*App, error) {
	container, err := NewContainer(ctx, cfg)
	if err != nil {
		return nil, err
	}

	router := NewRouter(container)
	server := NewServer(router, cfg.HTTP, container.Logger)

	return &App{container: container, server: server}, nil
}

// Logger exposes the root logger so main can report failures through it.
func (a *App) Logger() *zap.Logger { return a.container.Logger }

// Run starts the server and blocks until the process is asked to stop or the
// server fails.
//
// Shutdown is strictly ordered, and the order is the whole point of doing this
// by hand:
//
//  1. Stop accepting new connections and drain in-flight requests.
//  2. Only then close Postgres, Redis and Asynq.
//
// Closing the database first would cause every request still being served to
// fail, which is exactly what graceful shutdown is meant to prevent.
func (a *App) Run(ctx context.Context) error {
	log := a.container.Logger

	// SIGINT covers Ctrl+C locally; SIGTERM is what Docker and Kubernetes send
	// before the kill timer starts.
	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- a.server.Start()
	}()

	select {
	case err := <-serverErr:
		// The listener died on its own, typically because the port is taken.
		// Release resources before reporting, then return the real cause.
		if shutdownErr := a.shutdownResources(log); shutdownErr != nil {
			log.Error("resource shutdown failed after server error", zap.Error(shutdownErr))
		}
		return err

	case <-signalCtx.Done():
		log.Info("shutdown signal received, draining")
	}

	// Detach from signalCtx: it is already cancelled, so shutdown must run on a
	// fresh context or every step would abort immediately. A second signal is
	// still honoured by the orchestrator's own kill timer.
	stop()

	var errs []error

	if err := a.server.Shutdown(context.Background()); err != nil {
		log.Error("http server shutdown failed", zap.Error(err))
		errs = append(errs, err)
	}

	// Drain whatever Start returned so the goroutine cannot leak.
	select {
	case err := <-serverErr:
		if err != nil {
			errs = append(errs, err)
		}
	case <-time.After(time.Second):
	}

	if err := a.shutdownResources(log); err != nil {
		errs = append(errs, err)
	}

	log.Info("shutdown complete")

	return errors.Join(errs...)
}

// shutdownResources closes infrastructure under its own time budget.
func (a *App) shutdownResources(log *zap.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := a.container.Close(ctx); err != nil {
		log.Error("resource cleanup failed", zap.Error(err))
		return err
	}

	return nil
}
