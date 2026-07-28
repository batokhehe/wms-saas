// Command migrate is the schema migration CLI.
//
// It is a separate binary from the API on purpose. Migrations must be runnable
// as a deploy step, a Kubernetes init container or a Docker Compose dependency
// — none of which should require starting an HTTP server. It also means the API
// process holds no credentials capable of altering the schema.
//
// Usage:
//
//	migrate up                    apply all pending migrations
//	migrate down [-steps N]       roll back N migrations (default 1)
//	migrate down -all -confirm    roll back everything
//	migrate version               print the current schema version
//	migrate force -version N      clear a dirty state after manual repair
//	migrate create -name add_foo  scaffold a new migration pair
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/config"
	"github.com/batokhehe/wms-saas/backend/internal/shared/migration"
	"github.com/batokhehe/wms-saas/backend/pkg/logger"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		envFile = flag.String("env", ".env", "path to the environment file (optional)")
		dir     = flag.String("dir", "migrations", "directory holding migration files")
		steps   = flag.Int("steps", 1, "number of migrations to roll back (down)")
		all     = flag.Bool("all", false, "roll back every migration (down)")
		confirm = flag.Bool("confirm", false, "required acknowledgement for destructive commands")
		version = flag.Int("version", -1, "target version (force)")
		name    = flag.String("name", "", "migration name in snake_case (create)")
	)

	flag.Usage = usage
	flag.Parse()

	command := flag.Arg(0)
	if command == "" {
		usage()
		return 2
	}

	// `create` writes files and needs no database, so it runs before any config
	// is loaded. A developer scaffolding a migration should not need a running
	// PostgreSQL instance.
	if command == "create" {
		if err := createMigration(*dir, *name); err != nil {
			fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
			return 1
		}
		return 0
	}

	// WithoutAuth: the migration runner touches only the database. Requiring a
	// JWT signing secret here would mean either a placeholder credential in the
	// deployment manifest or handing the real one to a schema-migration job.
	cfg, err := config.Load(*envFile, config.WithoutAuth())
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		return 1
	}

	log, err := logger.New(logger.Config{
		Level:       cfg.Log.Level,
		Format:      cfg.Log.Format,
		AppName:     cfg.App.Name + "-migrate",
		AppVersion:  cfg.App.Version,
		Environment: cfg.App.Env,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		return 1
	}
	defer func() { _ = log.Sync() }()

	migrator, err := migration.New(cfg.Database, *dir, log)
	if err != nil {
		log.Error("could not open migrator", zap.Error(err))
		return 1
	}
	defer func() { _ = migrator.Close() }()

	switch command {
	case "up":
		if err := migrator.Up(); err != nil {
			log.Error("migration failed", zap.Error(err))
			return 1
		}

	case "down":
		// Rolling back destroys data. In production it additionally requires
		// -confirm, so a mistyped command in the wrong shell cannot drop a
		// tenant's warehouse history.
		target := *steps
		if *all {
			target = 0
		}

		if (*all || cfg.App.IsProduction()) && !*confirm {
			log.Error("refusing to roll back without -confirm",
				zap.Bool("all", *all),
				zap.String("env", cfg.App.Env),
			)
			return 2
		}

		if err := migrator.Down(target); err != nil {
			log.Error("rollback failed", zap.Error(err))
			return 1
		}

	case "version":
		v, dirty, err := migrator.Version()
		if err != nil {
			log.Error("could not read version", zap.Error(err))
			return 1
		}
		fmt.Printf("version=%d dirty=%t\n", v, dirty)
		if dirty {
			// A non-zero exit lets CI fail a deploy on a dirty schema.
			return 1
		}

	case "force":
		if *version < 0 {
			log.Error("force requires -version N")
			return 2
		}
		if !*confirm {
			log.Error("refusing to force a version without -confirm")
			return 2
		}
		if err := migrator.Force(*version); err != nil {
			log.Error("force failed", zap.Error(err))
			return 1
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", command)
		usage()
		return 2
	}

	return 0
}

// migrationNamePattern enforces snake_case so filenames sort and read
// predictably across the whole history.
var migrationNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// createMigration scaffolds an up/down pair.
//
// The version is a UTC timestamp rather than a sequential integer: two
// developers adding a migration on separate branches would otherwise both claim
// the same number and collide at merge time.
func createMigration(dir, name string) error {
	name = strings.TrimSpace(name)

	if name == "" {
		return fmt.Errorf("create requires -name (e.g. -name create_companies_table)")
	}
	if !migrationNamePattern.MatchString(name) {
		return fmt.Errorf("migration name %q must be snake_case: lowercase letters, digits and underscores", name)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	version := time.Now().UTC().Format("20060102150405")

	for _, direction := range []string{"up", "down"} {
		path := filepath.Join(dir, fmt.Sprintf("%s_%s.%s.sql", version, name, direction))

		content := fmt.Sprintf("-- %s: %s (%s)\n\n", version, name, direction)
		if direction == "down" {
			content += "-- Every up migration must be reversible. A migration that\n" +
				"-- cannot be rolled back cannot be deployed safely.\n\n"
		}

		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}

		fmt.Println("created", path)
	}

	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `migrate - WMS SaaS schema migration tool

Commands:
  up                          Apply all pending migrations
  down [-steps N]             Roll back N migrations (default 1)
  down -all -confirm          Roll back every migration
  version                     Print current version; exits 1 if dirty
  force -version N -confirm   Clear a dirty state after manual repair
  create -name <snake_case>   Scaffold a new up/down migration pair

Flags:
`)
	flag.PrintDefaults()
}
