// Command api is the HTTP entrypoint of the WMS SaaS backend.
//
// It stays deliberately thin: parse flags, load configuration, hand off to
// bootstrap. All wiring lives in internal/bootstrap so that a second binary
// (the Asynq worker) can reuse the same object graph.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/bootstrap"
	"github.com/batokhehe/wms-saas/backend/internal/config"
)

func main() {
	// run returns the exit code rather than calling os.Exit directly, because
	// os.Exit skips deferred functions and would defeat graceful shutdown.
	os.Exit(run())
}

func run() int {
	envFile := flag.String("env", ".env", "path to the environment file (optional)")
	flag.Parse()

	cfg, err := config.Load(*envFile)
	if err != nil {
		// The logger is configured from cfg, so a config failure has to be
		// reported on bare stderr.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		return 1
	}

	ctx := context.Background()

	app, err := bootstrap.New(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: startup: %v\n", err)
		return 1
	}

	log := app.Logger()
	// Flushing buffered log entries is best-effort: on some platforms syncing
	// stdout returns an error even though the write succeeded.
	defer func() { _ = log.Sync() }()

	if err := app.Run(ctx); err != nil {
		log.Error("application terminated with error", zap.Error(err))
		return 1
	}

	return 0
}
