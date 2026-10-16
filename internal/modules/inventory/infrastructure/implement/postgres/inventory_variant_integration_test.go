//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	inventorydto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	inventoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/implement"
	inventorymapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
	productimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/implement"
	productmapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/mapper"
	productavailability "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/availability"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
)

// This file proves US3 at the storage layer: each variant is its own stock and
// selling unit (FR-014, FR-015, FR-022, SC-004). It reuses the fixture and the raw
// helpers of inventory_integration_test.go, because a second migration runner and
// a second category insert would be a second answer to the same question.
//
// Its centre of gravity is what a fake cannot prove: two variants' level rows are
// independent, a hold/sale/release on one names only that variant's ledger rows,
// and the automatic ACTIVE <-> OUT_OF_STOCK edge runs through the product module's
// real adapter in the same transaction as the stock change, while a signal aimed at
// an announced or retired variant is a tolerant no-op.

// variantTestClock is the production-like injected clock these use cases read.
type variantTestClock struct{}

func (variantTestClock) Now() time.Time { return time.Now().UTC() }

// seedVariantProduct writes one category, one product and one variant per requested
// sell state straight into the database, returning the product and one variant
// identifier per state, in order. The default price (1000 VND) is the fallback a
// variant with no own price takes. The test may do this because it is not
// production code: the boundary the constitution protects is between modules, not
// between a test and a fixture.
func seedVariantProduct(t *testing.T, f *inventoryFixture, states ...string) (uuid.UUID, []uuid.UUID) {
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
	slug := "product-" + productID.String()[:8]
	code := "CODE-" + productID.String()[:8]
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO products
			(id, name, code, slug, normalized_slug, description, price_amount, currency,
			 category_id, position)
		VALUES ($1, $2, $3, $4, $4, '', 1000, 'VND', $5, 0)`,
		productID, "Product "+slug, code, slug, categoryID); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	out := make([]uuid.UUID, 0, len(states))
	for i, state := range states {
		variantID := uuid.New()
		if _, err := f.pool.Exec(ctx, `
			INSERT INTO product_variants
				(id, product_id, name, sku, price_amount, position, sell_state)
			VALUES ($1, $2, $3, $4, NULL, $5, $6)`,
			variantID, productID, "Variant "+variantID.String()[:8],
			"SKU-"+variantID.String()[:8], i, state); err != nil {
			t.Fatalf("insert variant: %v", err)
		}
		out = append(out, variantID)
	}
	return productID, out
}

// levelOf reads a variant's stored physical quantity, treating a variant with no
// level row as zero the way the adapter does.
func levelOf(t *testing.T, f *inventoryFixture, variantID uuid.UUID) int64 {
	t.Helper()
	var quantity int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COALESCE((SELECT quantity FROM stock_levels WHERE variant_id = $1), 0)`,
		variantID).Scan(&quantity); err != nil {
		t.Fatalf("read level: %v", err)
	}
	return quantity
}

// activeHeldOf reads the quantity a variant's active holds currently set aside.
func activeHeldOf(t *testing.T, f *inventoryFixture, variantID uuid.UUID) int64 {
	t.Helper()
	held, err := f.repo.ActiveHeld(context.Background(), variantID, time.Now().UTC())
	if err != nil {
		t.Fatalf("active held: %v", err)
	}
	return held
}

// variantSellState reads a variant's stored sell state straight from the table, so
// the assertion is on what was committed rather than on a returned DTO.
func variantSellState(t *testing.T, f *inventoryFixture, variantID uuid.UUID) string {
	t.Helper()
	var state string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT sell_state FROM product_variants WHERE id = $1`, variantID).Scan(&state); err != nil {
		t.Fatalf("read sell state: %v", err)
	}
	return state
}

// assertMovementKinds asserts a variant's ledger carries exactly the expected
// kinds, in the order they were written, so a change recorded against a sibling
// shows up as an unexpected row rather than passing silently.
func assertMovementKinds(t *testing.T, f *inventoryFixture, variantID uuid.UUID, want ...string) {
	t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT kind FROM inventory_transactions WHERE variant_id = $1 ORDER BY created_at, id`, variantID)
	if err != nil {
		t.Fatalf("read movements: %v", err)
	}
	defer rows.Close()
	got := make([]string, 0, len(want))
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatalf("scan movement: %v", err)
		}
		got = append(got, kind)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate movements: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("variant %s ledger = %v, want %v", variantID, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("variant %s movement %d = %s, want %s", variantID, i, got[i], want[i])
		}
	}
}

// newVariantInventoryService builds the real inventory use cases over the real
// adapters: the variant-keyed product availability signal and existence lookup run
// through module 04's own variant repository, so a crossing walks the product's own
// transitions inside the inventory transaction (Constitution III, FR-015).
func newVariantInventoryService(t *testing.T, pool *pgxpool.Pool) *inventoryimplement.Service {
	t.Helper()
	variantRepo := productpostgres.NewVariantRepository(pool)
	productService := productimplement.New(productimplement.Service{
		Products: productpostgres.NewProductRepository(pool),
		Variants: variantRepo,
		Mapper:   productmapper.New(),
	})
	adapter := productavailability.New(productService, variantRepo)
	return inventoryimplement.New(inventoryimplement.Service{
		Inventory:    NewInventoryRepository(pool),
		Lookup:       adapter,
		Availability: adapter,
		Tx:           &database.DB{Pool: pool},
		Clock:        variantTestClock{},
		Mapper:       inventorymapper.New(),
	})
}

// FR-014, SC-004: each variant carries its own level row; increasing one leaves the
// sibling untouched and decreasing one changes no sibling, in either direction.
func TestPerVariantStockIsIndependent(t *testing.T) {
	f := newInventoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_, variants := seedVariantProduct(t, f, "ACTIVE", "ACTIVE")
	first, second := variants[0], variants[1]

	if _, err := f.repo.Increase(ctx, first, 5, now); err != nil {
		t.Fatalf("Increase(first): %v", err)
	}
	if got := levelOf(t, f, first); got != 5 {
		t.Fatalf("first variant level = %d, want 5", got)
	}
	if got := levelOf(t, f, second); got != 0 {
		t.Fatalf("stocking one variant must not move its sibling: got %d", got)
	}

	if _, err := f.repo.Increase(ctx, second, 3, now); err != nil {
		t.Fatalf("Increase(second): %v", err)
	}
	if got := levelOf(t, f, first); got != 5 {
		t.Fatalf("stocking the sibling must not move the first variant: got %d", got)
	}
	if got := levelOf(t, f, second); got != 3 {
		t.Fatalf("second variant level = %d, want 3", got)
	}

	if _, err := f.repo.Decrease(ctx, first, 2, now); err != nil {
		t.Fatalf("Decrease(first): %v", err)
	}
	if got := levelOf(t, f, first); got != 3 {
		t.Fatalf("first variant level after decrease = %d, want 3", got)
	}
	if got := levelOf(t, f, second); got != 3 {
		t.Fatalf("decreasing one variant must not move its sibling: got %d", got)
	}
}

// FR-014, FR-022, SC-004, quickstart scenario C: reserve, sale and release operate
// per variant and record their changes on that variant's own ledger. A sale on one
// variant leaves the sibling's shelf and ledger alone, and releasing the sibling's
// hold returns it without a physical move.
func TestReserveSaleReleaseArePerVariantAndWriteTheLedger(t *testing.T) {
	f := newInventoryFixture(t)
	ctx := context.Background()
	svc := newVariantInventoryService(t, f.pool)
	_, variants := seedVariantProduct(t, f, "ACTIVE", "ACTIVE")
	first, second := variants[0], variants[1]

	if _, err := svc.Restock(ctx, inventorydto.RestockInput{VariantID: first, Quantity: 10}); err != nil {
		t.Fatalf("Restock(first): %v", err)
	}
	if _, err := svc.Restock(ctx, inventorydto.RestockInput{VariantID: second, Quantity: 4}); err != nil {
		t.Fatalf("Restock(second): %v", err)
	}
	orderID := uuid.New()
	if err := svc.Reserve(ctx, inventorydto.ReserveInput{OrderID: orderID, VariantID: first, Quantity: 2}); err != nil {
		t.Fatalf("Reserve(first): %v", err)
	}
	if err := svc.Reserve(ctx, inventorydto.ReserveInput{OrderID: orderID, VariantID: second, Quantity: 1}); err != nil {
		t.Fatalf("Reserve(second): %v", err)
	}

	// Holds are per variant and the shelf did not move.
	if got := levelOf(t, f, first); got != 10 {
		t.Fatalf("holding a variant must not move its shelf: got %d", got)
	}
	if got := levelOf(t, f, second); got != 4 {
		t.Fatalf("holding a variant must not move its shelf: got %d", got)
	}
	if got := activeHeldOf(t, f, first); got != 2 {
		t.Fatalf("first variant held = %d, want 2", got)
	}
	if got := activeHeldOf(t, f, second); got != 1 {
		t.Fatalf("second variant held = %d, want 1", got)
	}

	// A sale on the first variant moves only it and appends only its SALE movement.
	if err := svc.ApplySale(ctx, inventorydto.SaleInput{
		OrderID: orderID, VariantID: first, SourceReference: "evt-variant-first",
	}); err != nil {
		t.Fatalf("ApplySale(first): %v", err)
	}
	if got := levelOf(t, f, first); got != 8 {
		t.Fatalf("first variant level after sale = %d, want 8", got)
	}
	if got := levelOf(t, f, second); got != 4 {
		t.Fatalf("selling one variant must not move its sibling: got %d", got)
	}
	assertMovementKinds(t, f, first, "RESTOCK", "SALE")
	assertMovementKinds(t, f, second, "RESTOCK")

	// Releasing the sibling's hold returns it: no physical move, no ledger row.
	if err := svc.Release(ctx, inventorydto.ReleaseInput{OrderID: orderID, VariantID: second}); err != nil {
		t.Fatalf("Release(second): %v", err)
	}
	if got := activeHeldOf(t, f, second); got != 0 {
		t.Fatalf("second variant held after release = %d, want 0", got)
	}
	if got := levelOf(t, f, second); got != 4 {
		t.Fatalf("releasing a hold must not move the shelf: got %d", got)
	}
	assertMovementKinds(t, f, second, "RESTOCK")
}

// FR-015, SC-004, quickstart scenarios C and D: the automatic ACTIVE <->
// OUT_OF_STOCK edge moves a variant as its availability crosses zero and returns,
// in the same transaction as the stock change, and touches no sibling. A signal
// aimed at an announced or retired variant is a tolerant no-op, and a hold that
// takes the last available unit moves the variant out of stock while the physical
// count is untouched.
func TestAvailabilityCrossingMovesTheVariantSellState(t *testing.T) {
	f := newInventoryFixture(t)
	ctx := context.Background()
	svc := newVariantInventoryService(t, f.pool)
	_, variants := seedVariantProduct(t, f, "ACTIVE", "ACTIVE", "COMING_SOON", "DISCONTINUED")
	active, sibling, announced, retired := variants[0], variants[1], variants[2], variants[3]

	// A restock from zero on an already on-sale variant is a no-op signal: it stays
	// on sale, and the sibling is never mentioned.
	if _, err := svc.Restock(ctx, inventorydto.RestockInput{VariantID: active, Quantity: 1}); err != nil {
		t.Fatalf("Restock(active): %v", err)
	}
	if got := variantSellState(t, f, active); got != "ACTIVE" {
		t.Fatalf("a restock from zero must leave an on-sale variant on sale, got %s", got)
	}

	// Damaging the last unit crosses zero: only this variant moves out of stock.
	if _, err := svc.Damage(ctx, inventorydto.DamageInput{VariantID: active, Quantity: 1}); err != nil {
		t.Fatalf("Damage(active): %v", err)
	}
	if got := variantSellState(t, f, active); got != "OUT_OF_STOCK" {
		t.Fatalf("damaging the last unit must move the variant out of stock, got %s", got)
	}
	if got := variantSellState(t, f, sibling); got != "ACTIVE" {
		t.Fatalf("one variant running out must not move its sibling, got %s", got)
	}

	// A restock that lifts availability off zero returns it to sale.
	if _, err := svc.Restock(ctx, inventorydto.RestockInput{VariantID: active, Quantity: 2}); err != nil {
		t.Fatalf("Restock(active) back: %v", err)
	}
	if got := variantSellState(t, f, active); got != "ACTIVE" {
		t.Fatalf("a restock that lifts availability off zero must move the variant back on sale, got %s", got)
	}

	// A signal that does not apply is a no-op: an announced variant is never put
	// on sale by stock, and a retired one never returns to sale.
	if _, err := svc.Restock(ctx, inventorydto.RestockInput{VariantID: announced, Quantity: 2}); err != nil {
		t.Fatalf("Restock(announced): %v", err)
	}
	if got := variantSellState(t, f, announced); got != "COMING_SOON" {
		t.Fatalf("an announced variant must not be put on sale by stock, got %s", got)
	}
	if _, err := svc.Restock(ctx, inventorydto.RestockInput{VariantID: retired, Quantity: 2}); err != nil {
		t.Fatalf("Restock(retired): %v", err)
	}
	if got := variantSellState(t, f, retired); got != "DISCONTINUED" {
		t.Fatalf("a retired variant must never return to sale, got %s", got)
	}

	// Holding the last available unit crosses availability to zero while the
	// physical count is unchanged.
	_, held := seedVariantProduct(t, f, "ACTIVE")
	last := held[0]
	if _, err := svc.Restock(ctx, inventorydto.RestockInput{VariantID: last, Quantity: 1}); err != nil {
		t.Fatalf("Restock(last): %v", err)
	}
	if err := svc.Reserve(ctx, inventorydto.ReserveInput{OrderID: uuid.New(), VariantID: last, Quantity: 1}); err != nil {
		t.Fatalf("Reserve(last): %v", err)
	}
	if got := variantSellState(t, f, last); got != "OUT_OF_STOCK" {
		t.Fatalf("holding the last unit must move the variant out of stock, got %s", got)
	}
	if got := levelOf(t, f, last); got != 1 {
		t.Fatalf("a hold must not move the physical count, got %d", got)
	}
}
