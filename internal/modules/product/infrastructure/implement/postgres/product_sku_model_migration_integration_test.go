//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	// Registers the "pgx" driver with database/sql for goose.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
	"github.com/mavil-chudomirovich/artist-shop-be/migrations"
)

// This file proves the 00014 backfill on a database that ran the pre-00014
// schema: it applies every migration up to 00013, seeds legacy products (an
// on-sale one with a primary picture and a stock row, and an announced one with
// none), then applies 00014 and asserts every product ends with a code equal to
// its slug and exactly one default variant, and that the inventory repoint maps
// each stock row to that product's default variant (research D11, FR-026).

// applyUpTo applies every migration up to and including version.
func applyUpTo(t *testing.T, ctx context.Context, db *sql.DB, version int64) {
	t.Helper()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("configure migration dialect: %v", err)
	}
	if err := goose.UpToContext(ctx, db, ".", version); err != nil {
		t.Fatalf("apply migrations up to %d: %v", version, err)
	}
}

// TestMigration00014BackfillsCodesAndDefaultVariants drives the migration across
// a legacy database and checks the one-to-one repoint.
func TestMigration00014BackfillsCodesAndDefaultVariants(t *testing.T) {
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	applyUpTo(t, ctx, db, 13)

	// A legacy database: a category, an on-sale product with a primary picture and
	// a stock row, and an announced product with neither.
	categoryID := "11111111-1111-1111-1111-111111111111"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO categories (id, name, normalized_name, slug, normalized_slug, description, position, is_visible)
		VALUES ($1, 'Legacy', 'legacy', 'legacy', 'legacy', '', 0, true)`, categoryID); err != nil {
		t.Fatalf("seed category: %v", err)
	}

	onSaleID := "22222222-2222-2222-2222-222222222222"
	announcedID := "33333333-3333-3333-3333-333333333333"
	for _, seed := range []struct {
		id        string
		name      string
		slug      string
		state     string
		price     int64
		createdAt time.Time
	}{
		{onSaleID, "Legacy On Sale", "legacy-on-sale", "ACTIVE", 1000, time.Now().UTC()},
		{announcedID, "Legacy Announced", "legacy-announced", "COMING_SOON", 2500, time.Now().UTC()},
	} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO products
				(id, name, slug, normalized_slug, description, price_amount, currency,
				 category_id, position, sell_state, is_set, is_preorder,
				 preorder_expected_at, created_at, updated_at)
			VALUES ($1, $2, $3, $3, 'legacy description', $4, 'VND',
			 $5, 0, $6, false, false, NULL, $7, $7)`,
			seed.id, seed.name, seed.slug, seed.price, categoryID, seed.state, seed.createdAt); err != nil {
			t.Fatalf("seed product %s: %v", seed.slug, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO product_images
			(id, product_id, public_id, secure_url, width, height, position, is_primary)
		VALUES ('44444444-4444-4444-4444-444444444444', $1, 'demo/legacy', 'https://example.test/legacy.jpg', 10, 10, 0, true)`,
		onSaleID); err != nil {
		t.Fatalf("seed the primary picture: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO stock_levels (product_id, quantity, updated_at) VALUES ($1, 7, now())`, onSaleID); err != nil {
		t.Fatalf("seed the stock level: %v", err)
	}

	// Apply the rest, including 00014.
	if err := goose.UpContext(ctx, db, "."); err != nil {
		t.Fatalf("apply migration 00014: %v", err)
	}

	// Every product carries a code equal to its slug and exactly one variant.
	rows, err := db.QueryContext(ctx, `
		SELECT p.id, p.slug, p.code, p.name, p.price_amount,
		       (SELECT count(*) FROM product_variants v WHERE v.product_id = p.id)
		FROM products p
		ORDER BY p.slug`)
	if err != nil {
		t.Fatalf("read the migrated products: %v", err)
	}
	defer rows.Close()

	seen := 0
	for rows.Next() {
		seen++
		var id, slug, code, name string
		var price int64
		var variants int
		if err := rows.Scan(&id, &slug, &code, &name, &price, &variants); err != nil {
			t.Fatalf("scan the migrated product: %v", err)
		}
		if code != slug {
			t.Errorf("product %s kept code %q, want its slug %q", id, code, slug)
		}
		if variants != 1 {
			t.Errorf("product %s ended with %d variants, want exactly one", id, variants)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migrated products: %v", err)
	}
	if seen != 2 {
		t.Fatalf("expected the two seeded products after the migration, got %d", seen)
	}

	// The default variant mirrors the product's old fields, and its image is the
	// product's primary picture.
	var variantName, variantSKU, variantState, variantImage string
	var variantPrice int64
	var variantPosition int
	if err := db.QueryRowContext(ctx, `
		SELECT name, sku, price_amount, sell_state, position, COALESCE(image_secure_url, '')
		FROM product_variants WHERE product_id = $1`, onSaleID).Scan(
		&variantName, &variantSKU, &variantPrice, &variantState, &variantPosition, &variantImage); err != nil {
		t.Fatalf("read the on-sale product's default variant: %v", err)
	}
	if variantName != "Legacy On Sale" || variantSKU != "legacy-on-sale" || variantPrice != 1000 ||
		variantState != "ACTIVE" || variantPosition != 0 {
		t.Fatalf("the default variant did not mirror the product: %q %q %d %q %d",
			variantName, variantSKU, variantPrice, variantState, variantPosition)
	}
	if variantImage != "https://example.test/legacy.jpg" {
		t.Fatalf("the default variant image did not come from the primary picture: %q", variantImage)
	}

	// The announced product's default variant carries its old state and no image.
	var announcedState string
	var announcedImage *string
	if err := db.QueryRowContext(ctx, `
		SELECT sell_state, image_secure_url FROM product_variants WHERE product_id = $1`,
		announcedID).Scan(&announcedState, &announcedImage); err != nil {
		t.Fatalf("read the announced product's default variant: %v", err)
	}
	if announcedState != "COMING_SOON" {
		t.Fatalf("the announced variant lost its state: %q", announcedState)
	}
	if announcedImage != nil {
		t.Fatalf("a product with no picture must backfill no variant image, got %q", *announcedImage)
	}

	// The inventory repoint maps the stock row to the product's default variant
	// and preserves the quantity.
	var stockVariantID string
	var stockQuantity int64
	if err := db.QueryRowContext(ctx,
		`SELECT variant_id, quantity FROM stock_levels`).Scan(&stockVariantID, &stockQuantity); err != nil {
		t.Fatalf("read the repointed stock level: %v", err)
	}
	if stockQuantity != 7 {
		t.Fatalf("the stock quantity was not preserved, got %d", stockQuantity)
	}
	var defaultVariantID string
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM product_variants WHERE product_id = $1`, onSaleID).Scan(&defaultVariantID); err != nil {
		t.Fatalf("read the default variant id: %v", err)
	}
	if stockVariantID != defaultVariantID {
		t.Fatalf("the stock row points at variant %s, want the default variant %s", stockVariantID, defaultVariantID)
	}

	// The product-level flags and 014's grouping tables are gone.
	for _, column := range []string{"sell_state", "is_preorder", "preorder_expected_at", "is_set", "variant_group_id", "variant_label"} {
		var exists bool
		if err := db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_name = 'products' AND column_name = $1)`, column).Scan(&exists); err != nil {
			t.Fatalf("look up column %s: %v", column, err)
		}
		if exists {
			t.Errorf("the migration left the products.%s column in place", column)
		}
	}
	for _, tbl := range []string{"product_variant_groups", "product_variant_group_images", "product_set_items"} {
		var exists bool
		if err := db.QueryRowContext(ctx,
			`SELECT to_regclass($1) IS NOT NULL`, "public."+tbl).Scan(&exists); err != nil {
			t.Fatalf("look up table %s: %v", tbl, err)
		}
		if exists {
			t.Errorf("the migration left feature 014's %s table in place", tbl)
		}
	}
}
