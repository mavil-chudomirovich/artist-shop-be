//go:build integration

package statistics

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// lowStockFixture is one migrated PostgreSQL container plus the adapter under
// test over the real inventory repository.
type lowStockFixture struct {
	pool *pgxpool.Pool
	repo *inventorypostgres.InventoryRepository
}

func newLowStockFixture(t *testing.T) *lowStockFixture {
	t.Helper()
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
	t.Cleanup(pool.Close)
	return &lowStockFixture{pool: pool, repo: inventorypostgres.NewInventoryRepository(pool)}
}

// insertVariant writes one category, product and ACTIVE variant directly, so a
// stock row has a real variant to reference.
func (f *lowStockFixture) insertVariant(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	categoryID := uuid.New()
	suffix := categoryID.String()[:8]
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO categories
			(id, name, normalized_name, slug, normalized_slug, description, position, is_visible)
		VALUES ($1, $2, $3, $4, $5, '', 0, true)`,
		categoryID, "Category "+suffix, "category-"+suffix,
		"category-"+suffix, "category-"+suffix); err != nil {
		t.Fatalf("insert category: %v", err)
	}

	productID := uuid.New()
	code := "code-" + productID.String()[:8]
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO products
			(id, name, code, description, price_amount, currency, category_id, position)
		VALUES ($1, $2, $3, '', 1000, 'VND', $4, 0)`,
		productID, "Product "+code, code, categoryID); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	variantID := uuid.New()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO product_variants
			(id, product_id, name, sku, price_amount, position, sell_state)
		VALUES ($1, $2, $3, $4, NULL, 0, 'ACTIVE')`,
		variantID, productID, "Variant "+code, code); err != nil {
		t.Fatalf("insert variant: %v", err)
	}
	return variantID
}

func (f *lowStockFixture) setLevel(t *testing.T, variantID uuid.UUID, quantity int64) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO stock_levels (variant_id, quantity) VALUES ($1, $2)`, variantID, quantity); err != nil {
		t.Fatalf("insert stock level: %v", err)
	}
}

func (f *lowStockFixture) insertHold(t *testing.T, variantID uuid.UUID, quantity int64, status string, expiresAt time.Time) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO stock_holds
			(id, variant_id, order_id, quantity, status, expires_at, created_at, resolved_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.New(), variantID, uuid.New(), quantity, status, expiresAt, time.Now().UTC(), nil); err != nil {
		t.Fatalf("insert stock hold: %v", err)
	}
}

// FR-017, FR-018, SC-005, research D9: available is physical minus active,
// unexpired holds floored at zero; an expired hold is already excluded; a variant
// with no stock row is absent; and the threshold comparison is exclusive.
func TestLowStockQueryAgainstPostgres(t *testing.T) {
	f := newLowStockFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	below := f.insertVariant(t)
	f.setLevel(t, below, 5)
	f.insertHold(t, below, 2, "ACTIVE", now.Add(time.Hour))

	exact := f.insertVariant(t)
	f.setLevel(t, exact, 5)

	expiredHold := f.insertVariant(t)
	f.setLevel(t, expiredHold, 5)
	f.insertHold(t, expiredHold, 2, "ACTIVE", now.Add(-time.Minute))

	neverStocked := f.insertVariant(t)

	floored := f.insertVariant(t)
	f.setLevel(t, floored, 2)
	f.insertHold(t, floored, 3, "ACTIVE", now.Add(time.Hour))

	got, err := New(f.repo, fakeClock{at: now}).LowStockVariants(ctx, 5)
	if err != nil {
		t.Fatalf("LowStockVariants: %v", err)
	}

	byID := make(map[uuid.UUID]int64, len(got))
	for _, variant := range got {
		byID[variant.VariantID] = variant.Available
	}

	if available, ok := byID[below]; !ok || available != 3 {
		t.Fatalf("a held variant must report available 3, got %d (present=%v)", available, ok)
	}
	if _, ok := byID[exact]; ok {
		t.Fatal("available equal to the threshold is not below it")
	}
	if _, ok := byID[expiredHold]; ok {
		t.Fatal("an expired hold must not lower availability")
	}
	if _, ok := byID[neverStocked]; ok {
		t.Fatal("a variant with no stock row must not be a candidate")
	}
	if available, ok := byID[floored]; !ok || available != 0 {
		t.Fatalf("availability must floor at zero, got %d (present=%v)", available, ok)
	}
	if len(byID) != 2 {
		t.Fatalf("expected exactly the below and floored variants, got %+v", byID)
	}
}
