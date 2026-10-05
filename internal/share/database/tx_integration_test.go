//go:build integration

package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

var sharedDB *DB

// TestMain boots one PostgreSQL container for the whole package.
func TestMain(m *testing.M) {
	ctx := context.Background()
	pg, err := testsupport.StartPostgres(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot start postgres container: %v\n", err)
		os.Exit(1)
	}
	dsn, err := pg.DSN(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		_ = pg.Stop(ctx)
		os.Exit(1)
	}
	sharedDB, err = New(ctx, config.DatabaseConfig{
		URL: dsn, MaxConns: 4, MinConns: 1, ConnectTimeout: 30 * time.Second,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		_ = pg.Stop(ctx)
		os.Exit(1)
	}

	code := m.Run()

	sharedDB.Close()
	_ = pg.Stop(context.Background())
	os.Exit(code)
}

// testDB resets the working table so each test starts clean.
func testDB(t *testing.T) *DB {
	t.Helper()
	ctx := context.Background()
	if _, err := sharedDB.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS tx_items (id int PRIMARY KEY, name text NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := sharedDB.Pool.Exec(ctx, `TRUNCATE tx_items`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return sharedDB
}

func countItems(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM tx_items`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestWithinTxCommitsAndSharesQuerier(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	err := db.WithinTx(ctx, func(txCtx context.Context) error {
		q := FromContext(txCtx, db.Pool)
		if q == db.Pool {
			t.Fatal("expected the transaction to be stored in the context")
		}
		if _, err := q.Exec(ctx, `INSERT INTO tx_items (id, name) VALUES (1, 'a')`); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTx: %v", err)
	}
	if got := countItems(t, db); got != 1 {
		t.Fatalf("expected committed row, got %d", got)
	}
}

func TestWithinTxRollsBackOnError(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	boom := errors.New("boom")

	err := db.WithinTx(ctx, func(txCtx context.Context) error {
		if _, err := FromContext(txCtx, db.Pool).Exec(ctx, `INSERT INTO tx_items (id, name) VALUES (1, 'a')`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
	if got := countItems(t, db); got != 0 {
		t.Fatalf("expected rollback, got %d rows", got)
	}
}

func TestWithTxRollsBackOnPanic(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected panic to propagate")
			}
		}()
		_ = db.WithTx(ctx, func(txCtx context.Context) error {
			_, _ = FromContext(txCtx, db.Pool).Exec(ctx, `INSERT INTO tx_items (id, name) VALUES (1, 'a')`)
			panic("boom")
		})
	}()

	if got := countItems(t, db); got != 0 {
		t.Fatalf("expected rollback after panic, got %d rows", got)
	}
}

func TestFromContextFallsBackToPool(t *testing.T) {
	db := testDB(t)
	if got := FromContext(context.Background(), db.Pool); got != Querier(db.Pool) {
		t.Fatalf("expected fallback pool, got %T", got)
	}
}

func TestHealthRequiresReady(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if err := db.Health(ctx); err == nil {
		t.Fatal("expected error before SetReady")
	}
	db.SetReady(true)
	if !db.Ready() {
		t.Fatal("expected Ready")
	}
	if err := db.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
	db.SetReady(false)
}

func TestNewRejectsUnreachableDatabase(t *testing.T) {
	_, err := New(context.Background(), config.DatabaseConfig{
		URL: "postgres://app:app@127.0.0.1:1/test?sslmode=disable", ConnectTimeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatal("expected connection error")
	}
}

// TestQuerierImplementations is a compile-time check that the pool and a
// transaction both satisfy Querier, which is why it takes no *testing.T: there is
// nothing to assert at run time and the failure would be a build failure.
func TestQuerierImplementations(_ *testing.T) {
	var _ Querier = (*pgxpool.Pool)(nil)
	var _ Querier = (pgx.Tx)(nil)
}
