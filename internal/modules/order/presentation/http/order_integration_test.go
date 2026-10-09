//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	cartcheckout "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/infrastructure/implement/checkout"
	cartpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/infrastructure/implement/postgres"
	inventoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/implement"
	inventorymapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
	inventoryavailability "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/availability"
	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
	inventoryreservation "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/reservation"
	orderimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/implement"
	ordermapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	orderpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/postgres"
	productcatalog "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/catalog"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file drives US1 end to end against a real PostgreSQL container: the real
// order adapter, the real cart checkout adapter, the real product facts and
// availability over modules 04 and 05, the real reservation use cases, and the
// real HTTP surface. Its centre of gravity is quickstart scenarios 2 and 4 — a
// cart becomes an order carrying a snapshot of both lines and the address, the
// goods are held (available fell, physical did not move) and the cart is empty —
// and the concurrent case: two checkouts of one cart at once produce exactly one
// order (SC-001, SC-002, FR-013, FR-017).
//
// Docker is required to run it; the file still compiles without a container so
// `go vet -tags integration ./...` covers it.

// orderIntegrationClock is the production-like clock for the integration run.
type orderIntegrationClock struct{}

func (orderIntegrationClock) Now() time.Time { return time.Now().UTC() }

// productExists answers the inventory module's ProductLookup over the products
// table, so Reserve can tell an unknown product from an unstocked one. It is a
// test-local adapter: the boundary the constitution protects is between modules,
// not between a test and a fixture.
type productExists struct{ pool *pgxpool.Pool }

func (p productExists) ProductExists(ctx context.Context, productID uuid.UUID) (bool, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM products WHERE id = $1)`, productID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// customerAddresses answers the CustomerLookupService contract over the addresses
// table, default-first, so checkout can snapshot the delivery address. It is a
// test-local adapter for the same reason as productExists.
type customerAddresses struct{ pool *pgxpool.Pool }

func (c customerAddresses) LookupCustomer(ctx context.Context, userID uuid.UUID) (contracts.Customer, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT id, recipient_name, recipient_phone, province_code, province_name,
		       ward_code, ward_name, street_address, is_default
		FROM addresses
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY is_default DESC, updated_at DESC`, userID)
	if err != nil {
		return contracts.Customer{}, err
	}
	defer rows.Close()

	addresses := make([]contracts.CustomerAddress, 0)
	for rows.Next() {
		var address contracts.CustomerAddress
		if err := rows.Scan(&address.ID, &address.RecipientName, &address.RecipientPhone,
			&address.ProvinceCode, &address.ProvinceName, &address.WardCode, &address.WardName,
			&address.StreetAddress, &address.IsDefault); err != nil {
			return contracts.Customer{}, err
		}
		addresses = append(addresses, address)
	}
	if err := rows.Err(); err != nil {
		return contracts.Customer{}, err
	}
	return contracts.Customer{ID: userID, Addresses: addresses}, nil
}

// orderIntegrationFixture is one migrated PostgreSQL container plus the real HTTP
// surface, so checkout can be driven through the route a customer reaches.
type orderIntegrationFixture struct {
	pool *pgxpool.Pool
	root http.Handler
}

func newOrderIntegrationFixture(t *testing.T) *orderIntegrationFixture {
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

	inventoryRepo := inventorypostgres.NewInventoryRepository(pool)
	inventoryService := inventoryimplement.New(inventoryimplement.Service{
		Inventory: inventoryRepo,
		Lookup:    productExists{pool: pool},
		Tx:        &database.DB{Pool: pool},
		Clock:     orderIntegrationClock{},
		Mapper:    inventorymapper.New(),
	})

	svc := orderimplement.New(orderimplement.Service{
		Orders:       orderpostgres.NewOrderRepository(pool),
		Carts:        cartcheckout.New(cartpostgres.NewCartRepository(pool)),
		Products:     productcatalog.New(productpostgres.NewProductRepository(pool)),
		Availability: inventoryavailability.New(inventoryRepo, orderIntegrationClock{}),
		Reservations: inventoryreservation.New(inventoryService),
		Customers:    customerAddresses{pool: pool},
		Tx:           &database.DB{Pool: pool},
		Clock:        orderIntegrationClock{},
		Mapper:       ordermapper.New(),
	})
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	root.Mount(ordersPath, handler.Router(orderHooks()))
	return &orderIntegrationFixture{pool: pool, root: root}
}

// seedOrderCustomer writes the signed-in account the test hooks resolve, plus one
// delivery address, so a checkout has an owner and somewhere to go. The email is
// derived from the identifier so two customers can be seeded in one database.
func seedOrderCustomer(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'hash')`,
		userID, "order-customer-"+userID.String()+"@example.com"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO addresses
			(id, user_id, recipient_name, recipient_phone, province_code, province_name,
			 ward_code, ward_name, street_address, is_default)
		VALUES ($1, $2, 'Nguyễn Văn A', '0912345678', '01', 'Hà Nội',
			'00001', 'Phúc Xá', '1 Đinh Tiên Hoàng', true)`,
		uuid.New(), userID); err != nil {
		t.Fatalf("insert address: %v", err)
	}
}

// seedOrderProduct writes one category and one on-sale product straight into the
// database and puts a physical quantity on its shelf.
func seedOrderProduct(t *testing.T, pool *pgxpool.Pool, amount, quantity int64) uuid.UUID {
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
	if _, err := pool.Exec(ctx,
		`INSERT INTO stock_levels (product_id, quantity) VALUES ($1, $2)`, productID, quantity); err != nil {
		t.Fatalf("insert stock level: %v", err)
	}
	return productID
}

// seedOrderCart writes one cart for the owner with one line for the product, then
// returns the cart's identifier.
func seedOrderCart(t *testing.T, pool *pgxpool.Pool, ownerID, productID uuid.UUID, quantity, unitPrice int64) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	cartID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO carts (id, user_id) VALUES ($1, $2)`, cartID, ownerID); err != nil {
		t.Fatalf("insert cart: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO cart_items (id, cart_id, product_id, quantity, unit_price_amount, currency)
		VALUES ($1, $2, $3, $4, $5, 'VND')`,
		uuid.New(), cartID, productID, quantity, unitPrice); err != nil {
		t.Fatalf("insert cart line: %v", err)
	}
	return cartID
}

// physicalStock reads a product's physical quantity straight from the table.
func physicalStock(t *testing.T, pool *pgxpool.Pool, productID uuid.UUID) int64 {
	t.Helper()
	var quantity int64
	if err := pool.QueryRow(context.Background(),
		`SELECT quantity FROM stock_levels WHERE product_id = $1`, productID).Scan(&quantity); err != nil {
		t.Fatalf("read stock level: %v", err)
	}
	return quantity
}

// heldQuantity reads the quantity every active hold has set aside for one product.
func heldQuantity(t *testing.T, pool *pgxpool.Pool, productID uuid.UUID) int64 {
	t.Helper()
	var held int64
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(quantity), 0) FROM stock_holds WHERE product_id = $1 AND status = 'ACTIVE'`,
		productID).Scan(&held); err != nil {
		t.Fatalf("read held quantity: %v", err)
	}
	return held
}

// cartLineCount reads how many lines a cart still holds.
func cartLineCount(t *testing.T, pool *pgxpool.Pool, cartID uuid.UUID) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM cart_items WHERE cart_id = $1`, cartID).Scan(&count); err != nil {
		t.Fatalf("read cart line count: %v", err)
	}
	return count
}

// orderCount reads how many orders one owner has.
func orderCount(t *testing.T, pool *pgxpool.Pool, ownerID uuid.UUID) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM orders WHERE user_id = $1`, ownerID).Scan(&count); err != nil {
		t.Fatalf("read order count: %v", err)
	}
	return count
}

// SC-001, quickstart scenario 2: a customer checks out through the route, the
// order carries the snapshot and the address, the goods are held (available fell,
// physical did not move) and the cart is empty.
func TestCheckoutCreatesTheOrderHoldsTheGoodsAndEmptiesTheCart(t *testing.T) {
	f := newOrderIntegrationFixture(t)
	seedOrderCustomer(t, f.pool, testCustomerID)
	product := seedOrderProduct(t, f.pool, 120000, 10)
	cartID := seedOrderCart(t, f.pool, testCustomerID, product, 2, 120000)

	rec := perform(f.root, http.MethodPost, ordersPath, `{}`, "customer-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("checkout: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	var body orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the order body %q: %v", rec.Body.String(), err)
	}
	if body.Data.Status != "PENDING_PAYMENT" {
		t.Fatalf("the order must be awaiting payment, got %s", body.Data.Status)
	}
	if body.Data.Total.Amount != 240000 || body.Data.Total.Currency != "VND" {
		t.Fatalf("total = %+v, want 240000 VND", body.Data.Total)
	}
	if len(body.Data.Lines) != 1 {
		t.Fatalf("expected one line, got %+v", body.Data.Lines)
	}
	if body.Data.Lines[0].ProductID != product || body.Data.Lines[0].Quantity != 2 ||
		body.Data.Lines[0].UnitPrice.Amount != 120000 || body.Data.Lines[0].Name == "" {
		t.Fatalf("the line must carry the product's snapshot, got %+v", body.Data.Lines[0])
	}
	if body.Data.Address.RecipientName != "Nguyễn Văn A" || body.Data.Address.StreetAddress != "1 Đinh Tiên Hoàng" {
		t.Fatalf("the order must carry the delivery address snapshot, got %+v", body.Data.Address)
	}
	if body.Data.Total.Amount != body.Data.Lines[0].Quantity*body.Data.Lines[0].UnitPrice.Amount {
		t.Fatalf("the total must equal the exact sum of the lines, got %d", body.Data.Total.Amount)
	}

	// FR-013: the goods are held, not sold — physical stock did not move, the
	// held quantity rose.
	if got := physicalStock(t, f.pool, product); got != 10 {
		t.Fatalf("checkout must not move physical stock: got %d, want 10", got)
	}
	if got := heldQuantity(t, f.pool, product); got != 2 {
		t.Fatalf("checkout must hold the ordered quantity: got %d, want 2", got)
	}

	// FR-007: the cart is emptied.
	if got := cartLineCount(t, f.pool, cartID); got != 0 {
		t.Fatalf("the cart must be empty after checkout, got %d lines", got)
	}
	if got := orderCount(t, f.pool, testCustomerID); got != 1 {
		t.Fatalf("expected exactly one order, got %d", got)
	}
}

// SC-002, quickstart scenario 4: two checkouts of one cart at once produce exactly
// one order; the other is refused because the cart is now empty.
func TestConcurrentCheckoutsOfOneCartCreateOneOrder(t *testing.T) {
	f := newOrderIntegrationFixture(t)
	seedOrderCustomer(t, f.pool, testCustomerID)
	product := seedOrderProduct(t, f.pool, 120000, 10)
	seedOrderCart(t, f.pool, testCustomerID, product, 1, 120000)

	const racers = 2
	recs := make([]int, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			recs[i] = perform(f.root, http.MethodPost, ordersPath, `{}`, "customer-token").Code
		}(i)
	}
	close(start)
	wg.Wait()

	var created, refused int
	for i, code := range recs {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			refused++
		default:
			t.Fatalf("checkout %d: expected 201 or 409, got %d", i, code)
		}
	}
	if created != 1 || refused != 1 {
		t.Fatalf("expected exactly one checkout to create and one to be refused, got %d created and %d refused", created, refused)
	}
	if got := orderCount(t, f.pool, testCustomerID); got != 1 {
		t.Fatalf("one cart must never make two orders, got %d", got)
	}
}

// listOrders drives the customer list and decodes it, asserting the status.
func listOrders(t *testing.T, root http.Handler, token string, want int) orderListBody {
	t.Helper()
	rec := perform(root, http.MethodGet, ordersPath, "", token)
	if rec.Code != want {
		t.Fatalf("list orders: expected %d, got %d (%s)", want, rec.Code, rec.Body.String())
	}
	var body orderListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the order list %q: %v", rec.Body.String(), err)
	}
	return body
}

// SC-005, quickstart scenario 3: one owner's orders are not another's, reading
// another's answers 404 ORDER_NOT_FOUND, and cancelling an unpaid order makes its
// goods available again (FR-018 to FR-020).
func TestCustomerSeesAndManagesOnlyTheirOwnOrders(t *testing.T) {
	f := newOrderIntegrationFixture(t)
	seedOrderCustomer(t, f.pool, testCustomerID)
	seedOrderCustomer(t, f.pool, testCustomerTwoID)
	product := seedOrderProduct(t, f.pool, 120000, 10)
	seedOrderCart(t, f.pool, testCustomerID, product, 2, 120000)

	rec := perform(f.root, http.MethodPost, ordersPath, `{}`, "customer-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("checkout: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode the order body %q: %v", rec.Body.String(), err)
	}
	orderID := created.Data.ID

	// FR-018: the caller's list carries their order; another customer's does not.
	mine := listOrders(t, f.root, "customer-token", http.StatusOK)
	if len(mine.Data) != 1 || mine.Data[0].ID != orderID {
		t.Fatalf("the caller's list must carry their order, got %+v", mine.Data)
	}
	theirs := listOrders(t, f.root, "customer2-token", http.StatusOK)
	if len(theirs.Data) != 0 {
		t.Fatalf("another customer's list must not carry the order, got %+v", theirs.Data)
	}

	// FR-020: reading another customer's order answers 404 ORDER_NOT_FOUND, and
	// the caller's own read answers 200.
	rec = perform(f.root, http.MethodGet, ordersPath+"/"+orderID.String(), "", "customer2-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("another customer's read: expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_NOT_FOUND" {
		t.Fatalf("expected ORDER_NOT_FOUND, got %s", body.Error.Code)
	}
	rec = perform(f.root, http.MethodGet, ordersPath+"/"+orderID.String(), "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("the caller's read: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// FR-019: the unpaid order holds its goods; cancelling returns them and
	// answers CANCELLED without moving physical stock.
	if got := heldQuantity(t, f.pool, product); got != 2 {
		t.Fatalf("the order must hold the goods while unpaid, got %d", got)
	}
	rec = perform(f.root, http.MethodPost, ordersPath+"/"+orderID.String()+"/cancel", "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var cancelled orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &cancelled); err != nil {
		t.Fatalf("decode the cancelled order %q: %v", rec.Body.String(), err)
	}
	if cancelled.Data.Status != "CANCELLED" {
		t.Fatalf("the order must answer CANCELLED, got %s", cancelled.Data.Status)
	}
	if got := heldQuantity(t, f.pool, product); got != 0 {
		t.Fatalf("cancelling must make the goods available again, got %d held", got)
	}
	if got := physicalStock(t, f.pool, product); got != 10 {
		t.Fatalf("cancelling must not move physical stock, got %d", got)
	}

	// A second cancel is refused, naming the current state.
	rec = perform(f.root, http.MethodPost, ordersPath+"/"+orderID.String()+"/cancel", "", "customer-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second cancel: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_STATE_TRANSITION_INVALID" {
		t.Fatalf("expected ORDER_STATE_TRANSITION_INVALID, got %s", body.Error.Code)
	}
}
