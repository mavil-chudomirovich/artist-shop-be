//go:build integration

package audit

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

func TestInsertIsIdempotentAndAppendOnly(t *testing.T) {
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	runner, err := migrate.New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer runner.Close()
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	repo := NewRepository(pool)
	event := Event{
		EventID:    uuid.New(),
		Action:     "PAYMENT_CAPTURED",
		Outcome:    OutcomeSuccess,
		OccurredAt: time.Now().UTC(),
	}

	if err := repo.Insert(ctx, event); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := repo.Insert(ctx, event); err != nil {
		t.Fatalf("duplicate insert should be ignored: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM audit_logs WHERE event_id = $1", event.EventID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one row, got %d", count)
	}

	if _, err := pool.Exec(ctx, "UPDATE audit_logs SET action = 'TAMPERED' WHERE event_id = $1", event.EventID); err == nil {
		t.Fatal("expected update to be rejected on append-only table")
	}
}
