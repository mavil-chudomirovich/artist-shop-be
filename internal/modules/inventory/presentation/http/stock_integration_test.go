//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	inventoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/implement"
	inventorymapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
	productavailability "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/availability"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file drives US1 end to end against a real PostgreSQL container: the real
// adapter, the real existence lookup over module 04's repository, and the real
// HTTP surface. Its centre of gravity is quickstart.md scenario 2c — SC-001:
// after each manual change the sum of the ledger's deltas equals the stored
// level, which is the reconciliation that makes the ledger trustworthy.
//
// Docker is required to run it; the file still compiles without a container so
// `go vet -tags integration ./...` covers it.

// integrationClock is the production-like clock for the integration run.
type integrationClock struct{}

func (integrationClock) Now() time.Time { return time.Now().UTC() }

// insertInventoryProduct writes one category and one on-sale product straight
// into the database, so the inventory tables have a real product to reference.
func insertInventoryProduct(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	categoryID := uuid.New()
	suffix := categoryID.String()[:8]
	if _, err := pool.Exec(ctx, `
		INSERT INTO categories
			(id, name, normalized_name, slug, normalized_slug, description, position, is_visible)
		VALUES ($1, $2, $3, $4, $5, '', 0, true)`,
		categoryID, "Category "+suffix, "category-"+suffix,
		"category-"+suffix, "category-"+suffix); err != nil {
		t.Fatalf("insert category: %v", err)
	}

	productID := uuid.New()
	slug := "product-" + productID.String()[:8]
	if _, err := pool.Exec(ctx, `
		INSERT INTO products
			(id, name, slug, normalized_slug, description, price_amount, currency,
			 category_id, position, sell_state, is_set, is_preorder)
		VALUES ($1, $2, $3, $4, '', 1000, 'VND', $5, 0, 'ACTIVE', false, false)`,
		productID, "Product "+slug, slug, slug, categoryID); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	return productID
}

// assertLedgerReconciles asserts SC-001: the sum of a product's ledger deltas
// equals its stored level, and both equal want.
func assertLedgerReconciles(t *testing.T, pool *pgxpool.Pool, productID uuid.UUID, want int64) {
	t.Helper()
	ctx := context.Background()

	var sum int64
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0) FROM inventory_transactions WHERE product_id = $1`,
		productID).Scan(&sum); err != nil {
		t.Fatalf("sum the ledger: %v", err)
	}
	var level int64
	if err := pool.QueryRow(ctx,
		`SELECT quantity FROM stock_levels WHERE product_id = $1`, productID).Scan(&level); err != nil {
		t.Fatalf("read the level: %v", err)
	}
	if sum != level {
		t.Fatalf("SC-001 broken: SUM(delta)=%d but stock_levels.quantity=%d", sum, level)
	}
	if level != want {
		t.Fatalf("expected the level %d, got %d", want, level)
	}
}

func TestManualStockOperationsAgainstPostgres(t *testing.T) {
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

	productID := insertInventoryProduct(t, pool)
	neverStockedID := insertInventoryProduct(t, pool)

	// The real service over the real adapter; the existence question is answered
	// by module 04's adapter over its own repository. Only the lookup half is
	// reached here, so the product service it also carries is nil.
	lookup := productavailability.New(nil, productpostgres.NewProductRepository(pool))
	svc := inventoryimplement.New(inventoryimplement.Service{
		Inventory: inventorypostgres.NewInventoryRepository(pool),
		Lookup:    lookup,
		Tx:        &database.DB{Pool: pool},
		Clock:     integrationClock{},
		Audit:     nil,
		Mapper:    inventorymapper.New(),
	})
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	root.Mount(inventoryAdminPath, handler.AdminRouter(inventoryHooks(nil)))

	// quickstart 1a: a product that exists but has never been stocked answers zero.
	rec := performJSON(root, http.MethodGet, inventoryAdminPath+"/"+neverStockedID.String(), "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("read a never-stocked product: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stock := decodeStock(t, rec); stock.Data.PhysicalQuantity != 0 || stock.Data.AvailableQuantity != 0 {
		t.Fatalf("a never-stocked product answers zero, got %+v", stock.Data)
	}

	// quickstart 1b: an unknown product is a real not-found.
	rec = performJSON(root, http.MethodGet, inventoryAdminPath+"/"+uuid.New().String(), "", "admin-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("read an unknown product: expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "PRODUCT_NOT_FOUND" {
		t.Fatalf("expected PRODUCT_NOT_FOUND, got %s", body.Error.Code)
	}

	// quickstart 2a-2c: restock to 10 and reconcile.
	rec = performJSON(root, http.MethodPost, inventoryAdminPath+"/"+productID.String()+"/restock", `{"quantity":10}`, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("restock: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stock := decodeStock(t, rec); stock.Data.PhysicalQuantity != 10 || stock.Data.AvailableQuantity != 10 {
		t.Fatalf("restock: unexpected stock answer: %+v", stock.Data)
	}
	assertLedgerReconciles(t, pool, productID, 10)

	// quickstart 4a: damage part of it and reconcile.
	rec = performJSON(root, http.MethodPost, inventoryAdminPath+"/"+productID.String()+"/damage", `{"quantity":4}`, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("damage: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stock := decodeStock(t, rec); stock.Data.PhysicalQuantity != 6 {
		t.Fatalf("damage: unexpected stock answer: %+v", stock.Data)
	}
	assertLedgerReconciles(t, pool, productID, 6)

	// quickstart 4a: correct the count to 5 and reconcile.
	rec = performJSON(root, http.MethodPost, inventoryAdminPath+"/"+productID.String()+"/adjustment", `{"quantity":5}`, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("adjustment: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if stock := decodeStock(t, rec); stock.Data.PhysicalQuantity != 5 {
		t.Fatalf("adjustment: unexpected stock answer: %+v", stock.Data)
	}
	assertLedgerReconciles(t, pool, productID, 5)

	// The ledger carries one row per change with the kind each operation implies.
	var kinds []string
	rows, err := pool.Query(ctx,
		`SELECT kind FROM inventory_transactions WHERE product_id = $1 ORDER BY created_at, id`, productID)
	if err != nil {
		t.Fatalf("read the movement kinds: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatalf("scan the movement kind: %v", err)
		}
		kinds = append(kinds, kind)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the movement kinds: %v", err)
	}
	want := []string{"RESTOCK", "DAMAGE", "ADJUSTMENT"}
	if len(kinds) != len(want) {
		t.Fatalf("expected %d movements, got %v", len(want), kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("movement %d: expected %s, got %s", i, want[i], kinds[i])
		}
	}
}
