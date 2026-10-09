//go:build integration

package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	cartimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/implement"
	cartmapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/mapper"
	cartpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/infrastructure/implement/postgres"
	inventoryavailability "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/availability"
	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
	productcatalog "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/catalog"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file drives US1 end to end against a real PostgreSQL container: the real
// cart adapter, the real product facts over module 04's repository, and the real
// HTTP surface. Its centre of gravity is quickstart scenarios 1 to 4 — the cart is
// built and read with the subtotal equal to the exact sum of the lines (SC-001) —
// plus FR-011: adding to a cart changes no stock. It also proves end to end that
// two adds sent at once converge on one line whose quantity is the sum, never two
// lines.
//
// Docker is required to run it; the file still compiles without a container so
// `go vet -tags integration ./...` covers it.
//
// The "two adds that together exceed availability leave the line no higher than
// what is available" case named by tasks.md T026 rests on the availability refusal
// and the buyable projection that are US2 (T029/T030). It is deliberately not
// asserted here: US1 adds a product, captures its price and raises its line, and
// never reads availability.

// integrationClock is the production-like clock for the integration run.
type integrationClock struct{}

func (integrationClock) Now() time.Time { return time.Now().UTC() }

// cartIntegrationFixture is one migrated PostgreSQL container plus the real HTTP
// surface, so the cart can be driven through the routes a customer reaches.
type cartIntegrationFixture struct {
	pool *pgxpool.Pool
	root http.Handler
}

func newCartIntegrationFixture(t *testing.T) *cartIntegrationFixture {
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

	// The real service over the real adapters: the product facts come from module
	// 04's own read and availability from module 05's, both through the contracts.
	svc := cartimplement.New(cartimplement.Service{
		Carts:        cartpostgres.NewCartRepository(pool),
		Products:     productcatalog.New(productpostgres.NewProductRepository(pool)),
		Availability: inventoryavailability.New(inventorypostgres.NewInventoryRepository(pool), integrationClock{}),
		Tx:           &database.DB{Pool: pool},
		Clock:        integrationClock{},
		Mapper:       cartmapper.New(),
	})
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	root.Mount(cartPath, handler.Router(cartHooks(nil)))
	return &cartIntegrationFixture{pool: pool, root: root}
}

// seedCartCustomer writes the signed-in account the test hooks resolve, so the
// carts table has a real owner to reference.
func seedCartCustomer(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'hash')`,
		testCustomerID, "cart-customer@example.com"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

// seedCartProduct writes one category and one on-sale product straight into the
// database, so the cart can capture a real product's price and name.
func seedCartProduct(t *testing.T, pool *pgxpool.Pool, amount int64) uuid.UUID {
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
		VALUES ($1, $2, $3, $4, '', $5, 'VND', $6, 0, 'ACTIVE', false, false)`,
		productID, "Product "+slug, slug, slug, amount, categoryID); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	return productID
}

// seedStock puts a physical quantity on a product's shelf, so an "adding holds no
// stock" check has a number to compare against (FR-011).
func seedStock(t *testing.T, pool *pgxpool.Pool, productID uuid.UUID, quantity int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO stock_levels (product_id, quantity) VALUES ($1, $2)`, productID, quantity); err != nil {
		t.Fatalf("insert stock level: %v", err)
	}
}

// storedStock reads a product's physical quantity straight from the table.
func storedStock(t *testing.T, pool *pgxpool.Pool, productID uuid.UUID) int64 {
	t.Helper()
	var quantity int64
	if err := pool.QueryRow(context.Background(),
		`SELECT quantity FROM stock_levels WHERE product_id = $1`, productID).Scan(&quantity); err != nil {
		t.Fatalf("read stock level: %v", err)
	}
	return quantity
}

// addBody builds the add request body for one product.
func addBody(productID uuid.UUID, quantity int64) string {
	return fmt.Sprintf(`{"productId":%q,"quantity":%d}`, productID.String(), quantity)
}

// SC-001, quickstart scenarios 1-4: a customer builds the cart through the routes,
// every step answers what the action implies, the subtotal equals the exact sum of
// the lines, and adding changes no stock (FR-011).
func TestCartBuildsAgainstPostgres(t *testing.T) {
	f := newCartIntegrationFixture(t)
	seedCartCustomer(t, f.pool)
	product := seedCartProduct(t, f.pool, 120000)
	seedStock(t, f.pool, product, 10)

	// Scenario 1: a customer with no cart reads an empty cart, no row created.
	rec := performJSON(f.root, http.MethodGet, cartPath, "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty read: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeCart(t, rec); len(body.Data.Lines) != 0 || body.Data.Subtotal != nil {
		t.Fatalf("empty read: expected no lines and a null subtotal, got %+v", body.Data)
	}

	// Scenario 2: add two units; the line carries the captured price and total.
	rec = performJSON(f.root, http.MethodPost, cartPath+"/items", addBody(product, 2), "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("add: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeCart(t, rec)
	if len(body.Data.Lines) != 1 {
		t.Fatalf("add: expected one line, got %+v", body.Data.Lines)
	}
	if body.Data.Lines[0].Quantity != 2 || body.Data.Lines[0].UnitPrice.Amount != 120000 ||
		body.Data.Lines[0].LineTotal.Amount != 240000 {
		t.Fatalf("add: unexpected line %+v", body.Data.Lines[0])
	}
	if body.Data.Subtotal == nil || body.Data.Subtotal.Amount != 240000 {
		t.Fatalf("add: unexpected subtotal %+v", body.Data.Subtotal)
	}

	// FR-011: adding to a cart holds no stock.
	if got := storedStock(t, f.pool, product); got != 10 {
		t.Fatalf("adding to a cart must not change stock: got %d, want 10", got)
	}

	// Scenario 3: adding the same product again raises the one line.
	rec = performJSON(f.root, http.MethodPost, cartPath+"/items", addBody(product, 1), "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("raise: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body = decodeCart(t, rec)
	if len(body.Data.Lines) != 1 || body.Data.Lines[0].Quantity != 3 {
		t.Fatalf("raise: expected one line at quantity 3, got %+v", body.Data.Lines)
	}
	if body.Data.Subtotal == nil || body.Data.Subtotal.Amount != 360000 {
		t.Fatalf("raise: unexpected subtotal %+v", body.Data.Subtotal)
	}

	// Scenario 4a: change the quantity; the subtotal follows.
	rec = performJSON(f.root, http.MethodPatch, cartPath+"/items/"+product.String(), `{"quantity":5}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("change: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body = decodeCart(t, rec)
	if len(body.Data.Lines) != 1 || body.Data.Lines[0].Quantity != 5 {
		t.Fatalf("change: expected quantity 5, got %+v", body.Data.Lines)
	}
	if body.Data.Subtotal == nil || body.Data.Subtotal.Amount != 5*120000 {
		t.Fatalf("change: subtotal = %+v, want %d", body.Data.Subtotal, 5*120000)
	}

	// SC-001: the subtotal equals the exact sum of the lines.
	var sum int64
	for _, line := range body.Data.Lines {
		sum += line.Quantity * line.UnitPrice.Amount
	}
	if body.Data.Subtotal.Amount != sum {
		t.Fatalf("SC-001: subtotal %d != the exact sum %d", body.Data.Subtotal.Amount, sum)
	}

	// FR-011 again after a change.
	if got := storedStock(t, f.pool, product); got != 10 {
		t.Fatalf("changing a cart line must not change stock: got %d, want 10", got)
	}

	// Scenario 4b: remove the line; the cart is empty again.
	rec = performJSON(f.root, http.MethodDelete, cartPath+"/items/"+product.String(), "", "customer-token")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove: expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = performJSON(f.root, http.MethodGet, cartPath, "", "customer-token")
	if body := decodeCart(t, rec); len(body.Data.Lines) != 0 {
		t.Fatalf("after a removal the cart must be empty, got %+v", body.Data.Lines)
	}
}

// The same product added on two devices at once converges to one line whose
// quantity is the sum, never two lines. The row lock and the (cart_id, product_id)
// upsert are what make it so; a fake cannot prove it.
func TestConcurrentAddsConvergeToOneLineAgainstPostgres(t *testing.T) {
	f := newCartIntegrationFixture(t)
	seedCartCustomer(t, f.pool)
	product := seedCartProduct(t, f.pool, 100000)
	seedStock(t, f.pool, product, 10)

	const racers = 2
	codes := make([]int, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i] = performJSON(f.root, http.MethodPost, cartPath+"/items", addBody(product, 1), "customer-token").Code
		}(i)
	}
	close(start)
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("concurrent add %d: expected 200, got %d", i, code)
		}
	}

	var lines, quantity int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*), COALESCE(SUM(quantity), 0) FROM cart_items WHERE product_id = $1`, product).
		Scan(&lines, &quantity); err != nil {
		t.Fatalf("read the cart lines: %v", err)
	}
	if lines != 1 || quantity != 2 {
		t.Fatalf("concurrent adds must converge to one line at quantity 2, got %d lines at quantity %d", lines, quantity)
	}
}
