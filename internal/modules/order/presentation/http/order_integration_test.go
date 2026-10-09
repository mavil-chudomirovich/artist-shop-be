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
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	orderauditor "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/auditor"
	orderpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/postgres"
	productcatalog "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/catalog"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
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
// surface, so checkout can be driven through the route a customer reaches and the
// operator's desk through the route an administrator reaches.
type orderIntegrationFixture struct {
	pool   *pgxpool.Pool
	root   http.Handler
	svc    *orderimplement.Service
	writer *audit.Writer
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

	// The real audit writer, so a successful administrator move leaves a row in
	// audit_logs exactly as the composition root wires it (FR-023).
	writer := audit.NewWriter(audit.NewRepository(pool), testLogger, 64, 1, 1)
	writer.Start(ctx)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		writer.Stop(stopCtx)
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
		Audit:        orderauditor.New(writer),
		Mapper:       ordermapper.New(),
	})
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	root.Mount(ordersPath, handler.Router(orderHooks()))
	root.Mount(adminOrdersPath, handler.AdminRouter(orderHooks()))
	return &orderIntegrationFixture{pool: pool, root: root, svc: svc, writer: writer}
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

// adminAuditRow is one audit_logs row the order module wrote.
type adminAuditRow struct {
	actorID    *uuid.UUID
	actorRole  string
	targetType string
	targetID   string
	outcome    string
}

// waitForAuditRows waits until the given action has the expected number of
// persisted rows, so the assertion cannot race the asynchronous writer.
func (f *orderIntegrationFixture) waitForAuditRows(t *testing.T, action string, want int) []adminAuditRow {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows := f.auditRows(t, action)
		if len(rows) == want {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d %s rows, got %d", want, action, len(rows))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *orderIntegrationFixture) auditRows(t *testing.T, action string) []adminAuditRow {
	t.Helper()
	const query = `
		SELECT actor_id, actor_role, target_type, target_id, outcome
		FROM audit_logs WHERE action = $1 ORDER BY occurred_at`
	rows, err := f.pool.Query(context.Background(), query, action)
	if err != nil {
		t.Fatalf("read the audit trail: %v", err)
	}
	defer rows.Close()
	var events []adminAuditRow
	for rows.Next() {
		var event adminAuditRow
		if err := rows.Scan(&event.actorID, &event.actorRole, &event.targetType, &event.targetID, &event.outcome); err != nil {
			t.Fatalf("scan an audit row: %v", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate the audit trail: %v", err)
	}
	return events
}

// requireAudit asserts the one row of an action names the administrator, the
// order and a SUCCESS outcome.
func (f *orderIntegrationFixture) requireAudit(t *testing.T, action string, orderID, adminID uuid.UUID) {
	t.Helper()
	rows := f.waitForAuditRows(t, action, 1)
	row := rows[0]
	if row.actorID == nil || *row.actorID != adminID {
		t.Fatalf("%s: expected the administrator actor %s, got %v", action, adminID, row.actorID)
	}
	if row.actorRole != string(access.RoleAdmin) {
		t.Fatalf("%s: expected the ADMIN role, got %q", action, row.actorRole)
	}
	if row.targetType != "order" || row.targetID != orderID.String() {
		t.Fatalf("%s: expected the order as the target, got %q/%q", action, row.targetType, row.targetID)
	}
	if row.outcome != string(audit.OutcomeSuccess) {
		t.Fatalf("%s: expected a SUCCESS outcome, got %q", action, row.outcome)
	}
}

// SC-005, quickstart scenarios 5b/5c/5d and 6e against real PostgreSQL: the
// operator lists every order with its owner, reads one in full, is refused an
// illegal move naming the state, ships then completes a paid order — each act
// leaving an audit row naming the order and the administrator — and a customer's
// session is refused every administrator route (FR-021 to FR-023).
func TestAdminRunsTheOrderDeskAgainstPostgres(t *testing.T) {
	f := newOrderIntegrationFixture(t)
	seedOrderCustomer(t, f.pool, testCustomerID)
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

	// The paid transition has no HTTP surface — module 08 drives it — so it is
	// driven directly, exactly as the quickstart says (scenario 5a).
	if err := f.svc.MarkPaid(context.Background(), orderID, "payment-event-1"); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}

	// FR-021: the administrator list carries every order with its owner, state and
	// total.
	rec = perform(f.root, http.MethodGet, adminOrdersPath, "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var list adminListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode the admin list %q: %v", rec.Body.String(), err)
	}
	if len(list.Data) != 1 || list.Data[0].ID != orderID || list.Data[0].UserID != testCustomerID ||
		list.Data[0].Status != "PAID" || list.Data[0].Total.Amount != 240000 {
		t.Fatalf("the admin list must carry the order with its owner, got %+v", list.Data)
	}

	// FR-021: the administrator reads any order in full.
	rec = perform(f.root, http.MethodGet, adminOrdersPath+"/"+orderID.String(), "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin read: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var detail adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode the admin order %q: %v", rec.Body.String(), err)
	}
	if detail.Data.ID != orderID || detail.Data.UserID != testCustomerID || len(detail.Data.Lines) != 1 {
		t.Fatalf("the admin read must return the order in full, got %+v", detail.Data)
	}

	// FR-022 (quickstart 5b/5d): completing a paid order is an illegal move; it is
	// refused naming the current state and writes nothing.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/complete", "", "admin-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("complete before ship: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_STATE_TRANSITION_INVALID" {
		t.Fatalf("expected ORDER_STATE_TRANSITION_INVALID, got %s", body.Error.Code)
	}
	if rows := f.auditRows(t, constant.AuditOrderCompleted); len(rows) != 0 {
		t.Fatalf("a refused move must audit nothing, got %+v", rows)
	}

	// FR-022, FR-023 (quickstart 5c): ship then complete succeed, and each leaves
	// an audit row naming the order and the administrator.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/ship", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("ship: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var shipped adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &shipped); err != nil {
		t.Fatalf("decode the shipped order %q: %v", rec.Body.String(), err)
	}
	if shipped.Data.Status != "SHIPPED" {
		t.Fatalf("the order must answer SHIPPED, got %s", shipped.Data.Status)
	}
	f.requireAudit(t, constant.AuditOrderShipped, orderID, testAdminID)

	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/complete", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var completed adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &completed); err != nil {
		t.Fatalf("decode the completed order %q: %v", rec.Body.String(), err)
	}
	if completed.Data.Status != "COMPLETED" {
		t.Fatalf("the order must answer COMPLETED, got %s", completed.Data.Status)
	}
	f.requireAudit(t, constant.AuditOrderCompleted, orderID, testAdminID)

	// FR-023 (quickstart 6e): a customer's session is refused every administrator
	// route.
	for _, path := range []string{adminOrdersPath, adminOrdersPath + "/" + orderID.String()} {
		rec = perform(f.root, http.MethodGet, path, "", "customer-token")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("customer on %s: expected 403, got %d (%s)", path, rec.Code, rec.Body.String())
		}
		if body := decodeError(t, rec); body.Error.Code != "FORBIDDEN" {
			t.Fatalf("customer on %s: expected FORBIDDEN, got %s", path, body.Error.Code)
		}
	}
}
