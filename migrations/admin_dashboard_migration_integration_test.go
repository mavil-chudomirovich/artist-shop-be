//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	// Registers the "pgx" driver with database/sql for goose.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// applyUpTo applies every migration up to and including version.
func applyUpToAdmin(t *testing.T, ctx context.Context, db *sql.DB, version int64) {
	t.Helper()
	goose.SetBaseFS(FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("configure migration dialect: %v", err)
	}
	if err := goose.UpToContext(ctx, db, ".", version); err != nil {
		t.Fatalf("apply migrations up to %d: %v", version, err)
	}
}

// TestMigration00017BackfillsResolvableLines proves the 00017 backfill fills the
// snapshot for lines whose variant/product/category still resolve and leaves
// unresolvable lines null (T052).
func TestMigration00017BackfillsResolvableLines(t *testing.T) {
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	applyUpToAdmin(t, ctx, db, 16)

	// Seed a category, a product in it, and a variant.
	categoryID := uuid.New()
	productID := uuid.New()
	variantID := uuid.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO categories (id, name, normalized_name, slug, normalized_slug, description, position, is_visible)
		VALUES ($1, 'Cat A', 'cat-a', 'cat-a', 'cat-a', '', 0, true)`, categoryID); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO products (id, name, slug, normalized_slug, description, price_amount, currency,
			category_id, position, sell_state, is_set, is_preorder,
			preorder_expected_at, created_at, updated_at, code)
		VALUES ($1, 'Prod', 'prod', 'prod', 'desc', 1000, 'VND',
		 $2, 0, 'ACTIVE', false, false, NULL, now(), now(), 'prod')`, productID, categoryID); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO product_variants (id, product_id, name, sku, price_amount, currency, position, sell_state, created_at, updated_at)
		VALUES ($1, $2, 'Var', 'sku-1', 1000, 'VND', 0, 'ACTIVE', now(), now())`, variantID, productID); err != nil {
		t.Fatalf("seed variant: %v", err)
	}

	// Seed an order with two lines: one resolvable, one whose variant is gone.
	orderID := uuid.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO orders (id, user_id, status, total_amount, currency,
			recipient_name, recipient_phone, province_code, province_name, ward_code, ward_name, street_address,
			expires_at, created_at, updated_at)
		VALUES ($1, $2, 'PAID', 2000, 'VND',
			'R', '0912345678', '01', 'P', '00001', 'W', 'S',
			now() + interval '1 hour', now(), now())`, orderID, uuid.New()); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	// Resolvable line: variant_id points at the seeded variant.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO order_items (id, order_id, product_id, name, slug, unit_price_amount, currency, quantity, position, created_at, variant_id, product_code, variant_name, sku)
		VALUES ($1, $2, $3, 'Prod', 'prod', 1000, 'VND', 1, 0, now(), $4, 'prod', 'Var', 'sku-1')`,
		uuid.New(), orderID, productID, variantID); err != nil {
		t.Fatalf("seed resolvable line: %v", err)
	}
	// Unresolvable line: variant_id null (product gone).
	if _, err := db.ExecContext(ctx, `
		INSERT INTO order_items (id, order_id, product_id, name, slug, unit_price_amount, currency, quantity, position, created_at, variant_id, product_code, variant_name, sku)
		VALUES ($1, $2, $3, 'Gone', 'gone', 1000, 'VND', 1, 1, now(), NULL, 'removed', '', 'removed-1')`,
		uuid.New(), orderID, uuid.New()); err != nil {
		t.Fatalf("seed unresolvable line: %v", err)
	}

	// Apply 00017.
	if err := goose.UpContext(ctx, db, "."); err != nil {
		t.Fatalf("apply migration 00017: %v", err)
	}

	// Resolvable line must be backfilled.
	var catID *uuid.UUID
	row := db.QueryRowContext(ctx, `SELECT category_id, category_name FROM order_items WHERE variant_id = $1`, variantID)
	var rawID sql.NullString
	var rawName sql.NullString
	if err := row.Scan(&rawID, &rawName); err != nil {
		t.Fatalf("scan backfilled line: %v", err)
	}
	if !rawID.Valid {
		t.Fatalf("resolvable line was not backfilled (category_id null)")
	}
	parsed, err := uuid.Parse(rawID.String)
	if err != nil {
		t.Fatalf("parse category id: %v", err)
	}
	catID = &parsed
	if catID.String() != categoryID.String() {
		t.Fatalf("backfilled category_id = %s, want %s", catID, categoryID)
	}
	if !rawName.Valid || rawName.String != "Cat A" {
		t.Fatalf("backfilled category_name = %v, want Cat A", rawName)
	}

	// Unresolvable line must stay null.
	var nullCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM order_items WHERE variant_id IS NULL AND category_id IS NULL AND category_name IS NULL`).Scan(&nullCount); err != nil {
		t.Fatalf("count null lines: %v", err)
	}
	if nullCount < 1 {
		t.Fatalf("expected at least one unresolvable line to stay null, got %d", nullCount)
	}

	// both-or-none check refuses a half snapshot.
	if _, err := db.ExecContext(ctx, `UPDATE order_items SET category_id = $1 WHERE variant_id IS NULL`, uuid.New()); err == nil {
		t.Fatalf("half snapshot (id without name) must be refused by the check constraint")
	}

	// Down then Up is reversible.
	if err := goose.DownToContext(ctx, db, ".", 16); err != nil {
		t.Fatalf("down to 16: %v", err)
	}
	// columns should be gone
	if _, err := db.ExecContext(ctx, `SELECT category_id FROM order_items LIMIT 1`); err == nil {
		t.Fatalf("category_id column should be gone after down")
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT category_id, category_name FROM order_items LIMIT 1`); err != nil {
		t.Fatalf("columns should be back after up: %v", err)
	}
}
