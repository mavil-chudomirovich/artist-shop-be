// Package migrate applies versioned schema migrations with a PostgreSQL
// advisory lock so concurrent starts cannot apply them twice (FR-004).
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	// Registers the "pgx" driver with database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/mavil-chudomirovich/artist-shop-be/migrations"
)

// advisoryLockKey is an arbitrary constant used for the migration lock.
const advisoryLockKey int64 = 8589934592

var (
	setupOnce sync.Once
	setupErr  error
)

// Runner applies and inspects migrations against a database.
type Runner struct {
	db          *sql.DB
	lockTimeout time.Duration
}

// New opens a dedicated connection for migration work. The caller must Close it.
func New(dsn string, lockTimeout time.Duration) (*Runner, error) {
	setupOnce.Do(func() {
		goose.SetBaseFS(migrations.FS)
		setupErr = goose.SetDialect("postgres")
	})
	if setupErr != nil {
		return nil, fmt.Errorf("configure migration tool: %w", setupErr)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open migration database: %w", err)
	}
	return &Runner{db: db, lockTimeout: lockTimeout}, nil
}

// Close releases the migration connection.
func (r *Runner) Close() error { return r.db.Close() }

// Up applies all pending migrations under an advisory lock.
func (r *Runner) Up(ctx context.Context) error {
	return r.withLock(ctx, func() error {
		if err := goose.UpContext(ctx, r.db, "."); err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		return nil
	})
}

// Down rolls back the most recent migration under an advisory lock.
func (r *Runner) Down(ctx context.Context) error {
	return r.withLock(ctx, func() error {
		if err := goose.DownContext(ctx, r.db, "."); err != nil {
			return fmt.Errorf("roll back migration: %w", err)
		}
		return nil
	})
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

// withLock holds a PostgreSQL advisory lock for the duration of fn.
func (r *Runner) withLock(ctx context.Context, fn func() error) error {
	lockCtx := ctx
	if r.lockTimeout > 0 {
		var cancel context.CancelFunc
		lockCtx, cancel = context.WithTimeout(ctx, r.lockTimeout)
		defer cancel()
	}

	conn, err := r.db.Conn(lockCtx)
	if err != nil {
		return fmt.Errorf("reserve migration connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(lockCtx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockKey)
	}()

	return fn()
}
