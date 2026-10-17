// Package migrate applies versioned schema migrations when the service starts,
// so a fresh deploy converges without a manual step (FR-004). It mirrors the
// smart-wardrobe pattern: embedded SQL via go:embed, a goose provider guarded
// by a Postgres session-level advisory lock, and a structured log line per
// applied migration.
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	// Registers the "pgx" driver with database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/mavil-chudomirovich/artist-shop-be/migrations"
)

var (
	setupOnce sync.Once
	setupErr  error
)

// Runner applies and inspects migrations against a database.
type Runner struct {
	db          *sql.DB
	lockTimeout time.Duration
	logger      *slog.Logger
}

// New opens a dedicated connection for migration work. The caller must Close it.
// An optional logger enables per-migration progress lines; when omitted or nil
// a discard logger keeps test output quiet.
func New(dsn string, lockTimeout time.Duration, loggers ...*slog.Logger) (*Runner, error) {
	setupOnce.Do(func() {
		goose.SetBaseFS(migrations.FS)
		setupErr = goose.SetDialect("postgres")
	})
	if setupErr != nil {
		return nil, fmt.Errorf("configure migration tool: %w", setupErr)
	}

	logger := firstLogger(loggers)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open migration database: %w", err)
	}
	return &Runner{db: db, lockTimeout: lockTimeout, logger: logger}, nil
}

// Close releases the migration connection.
func (r *Runner) Close() error { return r.db.Close() }

// Up applies all pending migrations under a session-level advisory lock, so
// concurrent starts serialize instead of applying a version twice.
func (r *Runner) Up(ctx context.Context) error {
	ctx, cancel := r.boundContext(ctx)
	defer cancel()

	provider, err := r.newProvider()
	if err != nil {
		return err
	}
	r.logger.Info("running database migrations")
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, res := range results {
		r.logger.Info("migration applied",
			slog.String("migration", res.Source.Path),
			slog.Duration("duration", res.Duration),
		)
	}
	r.logger.Info("database migrations applied successfully", slog.Int("count", len(results)))
	return nil
}

// Down rolls back the most recent migration under a session-level advisory lock.
func (r *Runner) Down(ctx context.Context) error {
	ctx, cancel := r.boundContext(ctx)
	defer cancel()

	provider, err := r.newProvider()
	if err != nil {
		return err
	}
	res, err := provider.Down(ctx)
	if err != nil {
		return fmt.Errorf("roll back migration: %w", err)
	}
	r.logger.Info("migration rolled back", slog.String("migration", res.Source.Path))
	return nil
}

// Status prints the current migration status.
func (r *Runner) Status(ctx context.Context) error {
	if err := goose.StatusContext(ctx, r.db, "."); err != nil {
		return fmt.Errorf("migration status: %w", err)
	}
	return nil
}

// Version returns the current schema version.
func (r *Runner) Version(ctx context.Context) (int64, error) {
	v, err := goose.GetDBVersionContext(ctx, r.db)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return v, nil
}

// newProvider builds a goose provider over the embedded SQL tree, guarded by
// a Postgres session-level advisory lock. The lock lives on the migration
// session itself, so it holds for the whole run — unlike a manual
// pg_advisory_lock taken on one pooled connection while goose migrates on
// another.
func (r *Runner) newProvider() (*goose.Provider, error) {
	sessionLocker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("create migration session locker: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		r.db,
		migrations.FS,
		goose.WithSessionLocker(sessionLocker),
		goose.WithTableName("goose_db_version"),
	)
	if err != nil {
		return nil, fmt.Errorf("create migration provider: %w", err)
	}
	return provider, nil
}

// boundContext caps the whole migration run (lock wait + apply) at the
// configured lock timeout, when one is set.
func (r *Runner) boundContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.lockTimeout > 0 {
		return context.WithTimeout(ctx, r.lockTimeout)
	}
	return ctx, func() {}
}

// firstLogger returns the first usable logger, or a discard logger when none
// is supplied (integration tests omit it to stay quiet).
func firstLogger(loggers []*slog.Logger) *slog.Logger {
	for _, l := range loggers {
		if l != nil {
			return l
		}
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
