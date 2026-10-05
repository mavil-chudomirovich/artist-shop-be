package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is implemented by both *pgxpool.Pool and pgx.Tx, allowing
// repositories to run inside or outside a transaction transparently.
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type querierKey struct{}

// WithQuerier stores a querier (typically a transaction) in the context.
func WithQuerier(ctx context.Context, q Querier) context.Context {
	return context.WithValue(ctx, querierKey{}, q)
}

// FromContext returns the querier stored in the context, or the provided
// fallback (usually the pool) when none is present.
func FromContext(ctx context.Context, fallback Querier) Querier {
	if q, ok := ctx.Value(querierKey{}).(Querier); ok {
		return q
	}
	return fallback
}

// WithinTx is the UnitOfWork adapter: it runs fn inside a transaction.
func (d *DB) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return d.WithTx(ctx, fn)
}

// WithTx runs fn inside a database transaction, committing on success and
// rolling back on error or panic. The transaction is also placed in the context
// so nested repository calls participate in the same transaction.
func (d *DB) WithTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	txCtx := WithQuerier(ctx, tx)
	if err = fn(txCtx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rbErr)
		}
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
