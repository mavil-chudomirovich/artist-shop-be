package database

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

// DB wraps the PostgreSQL connection pool and tracks readiness state.
type DB struct {
	Pool  *pgxpool.Pool
	ready atomic.Bool
}

// New creates and verifies a pooled connection to PostgreSQL (FR-003).
func New(ctx context.Context, cfg config.DatabaseConfig) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	ctx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Ping checks database connectivity.
func (d *DB) Ping(ctx context.Context) error {
	if d == nil || d.Pool == nil {
		return fmt.Errorf("database is not initialized")
	}
	return d.Pool.Ping(ctx)
}

// SetReady records whether the database and schema are usable.
func (d *DB) SetReady(ready bool) { d.ready.Store(ready) }

// Ready reports the current readiness state.
func (d *DB) Ready() bool { return d != nil && d.ready.Load() }

// Health returns an error when the database is not ready or not reachable.
func (d *DB) Health(ctx context.Context) error {
	if !d.Ready() {
		return fmt.Errorf("database not ready")
	}
	return d.Ping(ctx)
}

// Close releases the connection pool.
func (d *DB) Close() {
	if d != nil && d.Pool != nil {
		d.Pool.Close()
	}
}
