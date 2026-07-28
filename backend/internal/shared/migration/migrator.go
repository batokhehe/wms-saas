// Package migration wraps golang-migrate with the project's conventions.
//
// GORM's AutoMigrate is deliberately not used anywhere in this codebase. It
// cannot express a backfill, it will not drop or rename a column safely, it
// produces no reviewable diff, and it makes the schema a side effect of whatever
// struct definitions happen to be compiled into the running binary. Versioned
// SQL is the source of truth; see docs/MigrationGuide.md.
package migration

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file" // registers the file:// source driver
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/config"
)

// Migrator runs schema migrations against PostgreSQL.
type Migrator struct {
	migrate *migrate.Migrate
	log     *zap.Logger
}

// New opens a migrator over the SQL files in dir.
//
// It uses its own database connection rather than borrowing the application
// pool: migrations take locks and can run long, and they must be able to run as
// a separate process (or an init container) with no API server present.
func New(cfg config.DatabaseConfig, dir string, log *zap.Logger) (*Migrator, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("migration: resolving %q: %w", dir, err)
	}

	// file:// URLs require forward slashes even on Windows.
	sourceURL := "file://" + filepath.ToSlash(absDir)

	// golang-migrate requires the URL form; x-migrations-table pins the version
	// table name so it cannot drift if the library's default ever changes.
	databaseURL := cfg.URL() + "&x-migrations-table=schema_migrations"

	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("migration: opening %s: %w", sourceURL, err)
	}

	m.Log = &migrateLogger{log: log.With(zap.String("component", "migrate"))}

	return &Migrator{migrate: m, log: log}, nil
}

// NewWithDriver builds a migrator from an existing database driver. It exists
// for tests, which already hold a connection to a throwaway database.
func NewWithDriver(driver *postgres.Postgres, dir string, log *zap.Logger) (*Migrator, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("migration: resolving %q: %w", dir, err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+filepath.ToSlash(absDir), "postgres", driver,
	)
	if err != nil {
		return nil, fmt.Errorf("migration: opening with driver: %w", err)
	}

	m.Log = &migrateLogger{log: log.With(zap.String("component", "migrate"))}

	return &Migrator{migrate: m, log: log}, nil
}

// Up applies every pending migration.
//
// ErrNoChange means the schema is already current, which is the normal outcome
// of redeploying unchanged code. Treating it as an error would make every
// no-op deploy fail.
func (m *Migrator) Up() error {
	m.log.Info("applying pending migrations")

	if err := m.migrate.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			m.log.Info("schema is already up to date")
			return nil
		}
		return fmt.Errorf("migration: up: %w", err)
	}

	m.logVersion("migrations applied")
	return nil
}

// Down rolls back n migrations. A non-positive n rolls everything back, which
// is why the CLI guards it behind an explicit confirmation flag.
func (m *Migrator) Down(n int) error {
	var err error

	if n <= 0 {
		m.log.Warn("rolling back all migrations")
		err = m.migrate.Down()
	} else {
		m.log.Info("rolling back migrations", zap.Int("steps", n))
		err = m.migrate.Steps(-n)
	}

	if err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			m.log.Info("nothing to roll back")
			return nil
		}
		return fmt.Errorf("migration: down: %w", err)
	}

	m.logVersion("rollback complete")
	return nil
}

// Version reports the current schema version and whether it is dirty.
//
// Dirty means a migration failed part-way and the schema is in an unknown
// state. golang-migrate refuses to proceed until an operator inspects the
// database and calls Force, which is correct: guessing here corrupts data.
func (m *Migrator) Version() (version uint, dirty bool, err error) {
	version, dirty, err = m.migrate.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("migration: version: %w", err)
	}
	return version, dirty, nil
}

// Force sets the version without running any SQL, clearing the dirty flag.
//
// This is a recovery tool for a human who has already inspected the database
// and knows which migrations actually landed. It must never run automatically.
func (m *Migrator) Force(version int) error {
	m.log.Warn("forcing schema version", zap.Int("version", version))

	if err := m.migrate.Force(version); err != nil {
		return fmt.Errorf("migration: force %d: %w", version, err)
	}
	return nil
}

// Close releases the migrator's source and database handles.
func (m *Migrator) Close() error {
	sourceErr, dbErr := m.migrate.Close()
	return errors.Join(sourceErr, dbErr)
}

func (m *Migrator) logVersion(message string) {
	version, dirty, err := m.Version()
	if err != nil {
		m.log.Warn("could not read schema version", zap.Error(err))
		return
	}
	m.log.Info(message, zap.Uint("version", version), zap.Bool("dirty", dirty))
}

// migrateLogger adapts golang-migrate's logger onto zap.
type migrateLogger struct{ log *zap.Logger }

func (l *migrateLogger) Printf(format string, v ...any) {
	l.log.Info(fmt.Sprintf(format, v...))
}

// Verbose tells golang-migrate whether to emit per-statement output. It is
// enabled: during a failed production migration, knowing which statement was
// executing is the difference between a two-minute and a two-hour recovery.
func (l *migrateLogger) Verbose() bool { return true }
