//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	// Registers the "pgx" driver with database/sql for goose.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// TestMigration00018BackfillsShippedAndRefusesUnlistedStates proves against real
// PostgreSQL what the string guard cannot: that an order already stored as SHIPPED
// is backfilled to SHIPPING by 00018 with no data loss, that the widened
// orders_status_ck refuses a value outside the seven states, and that the
// migration's Down/Up is reversible (SC-007, FR-001, FR-002, research D2).
func TestMigration00018BackfillsShippedAndRefusesUnlistedStates(t *testing.T) {
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Apply every migration up to and including 00017, before the rename.
	applyUpToAdmin(t, ctx, db, 17)

	// Seed an order whose status is the old SHIPPED value the six-state check of
	// 00011 still allows at this version.
	orderID := uuid.New()
	createdAt := time.Date(2026, time.February, 10, 12, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orders (id, user_id, status, total_amount, currency,
			recipient_name, recipient_phone, province_code, province_name, ward_code, ward_name, street_address,
			created_at, updated_at)
		VALUES ($1, $2, 'SHIPPED', 2000, 'VND',
			'R', '0912345678', '01', 'P', '00001', 'W', 'S',
			$3, $3)`, orderID, uuid.New(), createdAt); err != nil {
		t.Fatalf("seed SHIPPED order: %v", err)
	}

	// Apply 00018.
	if err := goose.UpContext(ctx, db, "."); err != nil {
		t.Fatalf("apply migration 00018: %v", err)
	}

	// The row now reads SHIPPING with no data loss.
	var (
		status    string
		total     int64
		currency  string
		storedAt  time.Time
		delivered sql.NullTime
		completed sql.NullTime
		by        sql.NullString
	)
	if err := db.QueryRowContext(ctx,
		`SELECT status, total_amount, currency, created_at, delivered_at, completed_at, completed_by
		 FROM orders WHERE id = $1`, orderID).
		Scan(&status, &total, &currency, &storedAt, &delivered, &completed, &by); err != nil {
		t.Fatalf("read the migrated order: %v", err)
	}
	if status != "SHIPPING" {
		t.Fatalf("the SHIPPED order must be backfilled to SHIPPING, got %s", status)
	}
	if total != 2000 || currency != "VND" || !storedAt.Equal(createdAt) {
		t.Fatalf("the backfill lost data: total %d currency %s created_at %v", total, currency, storedAt)
	}
	if delivered.Valid || completed.Valid || by.Valid {
		t.Fatalf("the three new columns must stay NULL on a migrated row, got %v/%v/%v", delivered, completed, by)
	}

	// The widened check refuses a value outside the seven states, including the
	// renamed SHIPPED value.
	for _, bad := range []string{"SHIPPED", "BOGUS"} {
		if _, err := db.ExecContext(ctx, `UPDATE orders SET status = $1 WHERE id = $2`, bad, orderID); err == nil {
			t.Fatalf("orders_status_ck must refuse the unlisted status %s", bad)
		}
	}

	// Down then Up is reversible: the value returns to SHIPPED and then SHIPPING.
	if err := goose.DownToContext(ctx, db, ".", 17); err != nil {
		t.Fatalf("down to 17: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = $1`, orderID).Scan(&status); err != nil {
		t.Fatalf("read the rolled-back order: %v", err)
	}
	if status != "SHIPPED" {
		t.Fatalf("the Down must restore SHIPPED, got %s", status)
	}
	if _, err := db.ExecContext(ctx, `SELECT delivered_at FROM orders LIMIT 1`); err == nil {
		t.Fatalf("delivered_at column should be gone after the Down")
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = $1`, orderID).Scan(&status); err != nil {
		t.Fatalf("read the re-migrated order: %v", err)
	}
	if status != "SHIPPING" {
		t.Fatalf("the second Up must backfill SHIPPING again, got %s", status)
	}
}
