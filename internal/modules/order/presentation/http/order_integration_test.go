//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	authaccount "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/account"
	authpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/postgres"
	cartcheckout "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/infrastructure/implement/checkout"
	cartpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/infrastructure/implement/postgres"
	inventoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/implement"
	inventorymapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
	inventoryavailability "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/availability"
	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
	inventoryreservation "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/reservation"
	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	orderimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	ordermapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	orderauditor "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/auditor"
	orderpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/postgres"
	productcatalog "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/catalog"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file drives US1 end to end against a real PostgreSQL container: the real
// order adapter, the real cart checkout adapter, the real product facts and
// availability over modules 04 and 05, the real reservation use cases, and the
// real HTTP surface. Its centre of gravity is quickstart scenarios 1 and 2 — a
// cart becomes an order awaiting the artist holding nothing, the artist confirms
// so the goods are held (available fell, physical did not move), and paying sells
// the hold once — and the concurrent case: two checkouts of one cart at once
// produce exactly one order (SC-001, FR-001, FR-002, FR-004, FR-005).
//
// Docker is required to run it; the file still compiles without a container so
// `go vet -tags integration ./...` covers it.

// orderIntegrationClock is the production-like clock for the integration run.
type orderIntegrationClock struct{}

func (orderIntegrationClock) Now() time.Time { return time.Now().UTC() }

// variantExists answers the inventory module's ProductLookup over the
// product_variants table, so Reserve can tell an unknown variant from an unstocked
// one. It is a test-local adapter: the boundary the constitution protects is
// between modules, not between a test and a fixture.
type variantExists struct{ pool *pgxpool.Pool }

func (p variantExists) VariantExists(ctx context.Context, variantID uuid.UUID) (bool, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM product_variants WHERE id = $1)`, variantID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// customerAddresses answers the CustomerLookupService contract over the addresses
// table, default-first, so checkout can snapshot the delivery address. It is a
// test-local adapter for the same reason as variantExists. It also carries the
// account's email, read from the users table, so the order module can address a
// status-change notification to the customer (FR-020, research D9).
type customerAddresses struct{ pool *pgxpool.Pool }

func (c customerAddresses) LookupCustomer(ctx context.Context, userID uuid.UUID) (contracts.Customer, error) {
	var email string
	if err := c.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email); err != nil {
		return contracts.Customer{}, err
	}

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
	return contracts.Customer{ID: userID, Email: email, Addresses: addresses}, nil
}

// integrationArtistEmail is the shop operator's address the integration run wires
// into the order service, so a test can read the artist's notifications.
const integrationArtistEmail = "shop@example.com"

// notifyMessage is one email the recording notifier captured.
type notifyMessage struct {
	to      string
	subject string
	body    string
}

// recordingNotifier is the integration run's in-memory Notifier. It records every
// message so a test can prove the artist is told an order needs confirmation and
// the customer is told each status change; fail makes Send return an error so a
// test can prove a delivery failure never fails the order operation (FR-019 to
// FR-021, research D9).
type recordingNotifier struct {
	mu   sync.Mutex
	sent []notifyMessage
	fail bool
}

func (n *recordingNotifier) Send(_ context.Context, to, subject, body string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.fail {
		return errors.New("smtp unavailable")
	}
	n.sent = append(n.sent, notifyMessage{to: to, subject: subject, body: body})
	return nil
}

// messagesTo returns every captured message addressed to one mailbox.
func (n *recordingNotifier) messagesTo(to string) []notifyMessage {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]notifyMessage, 0, len(n.sent))
	for _, message := range n.sent {
		if message.to == to {
			out = append(out, message)
		}
	}
	return out
}

// snapshot copies every captured message, so an assertion can report the whole
// mailbox without holding the lock.
func (n *recordingNotifier) snapshot() []notifyMessage {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]notifyMessage(nil), n.sent...)
}

// setFail makes the notifier refuse every subsequent send, so a test can prove a
// delivery failure never fails the order operation (FR-021).
func (n *recordingNotifier) setFail(fail bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.fail = fail
}

// recordingMedia is the integration run's in-memory MediaStore. It records every
// uploaded payload and every released reference, so a test can prove evidence was
// stored through the port and that a refused request never reached it. It stands
// in for Cloudinary so the dispatch path runs without a real provider (FR-009,
// FR-010).
type recordingMedia struct {
	mu       sync.Mutex
	uploaded [][]byte
	removed  []media.Reference
}

func (m *recordingMedia) Upload(_ context.Context, content []byte, _ int) (media.Reference, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploaded = append(m.uploaded, content)
	n := len(m.uploaded)
	return media.Reference{
		PublicID: fmt.Sprintf("public-%d", n),
		URL:      fmt.Sprintf("https://cdn.example/%d.jpg", n),
		Width:    100,
		Height:   80,
	}, nil
}

func (m *recordingMedia) Remove(_ context.Context, ref media.Reference) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removed = append(m.removed, ref)
	return nil
}

func (m *recordingMedia) uploadCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.uploaded)
}

// orderIntegrationFixture is one migrated PostgreSQL container plus the real HTTP
// surface, so checkout can be driven through the route a customer reaches and the
// operator's desk through the route an administrator reaches.
type orderIntegrationFixture struct {
	pool     *pgxpool.Pool
	root     http.Handler
	svc      *orderimplement.Service
	writer   *audit.Writer
	notifier *recordingNotifier
	media    *recordingMedia
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
		Lookup:    variantExists{pool: pool},
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

	// A recording notifier, wired exactly as the composition root wires the real
	// one, so a test can observe the artist's and the customer's emails (FR-019
	// to FR-021).
	notifier := &recordingNotifier{}

	// A recording media store, wired exactly as the composition root wires the
	// shared Cloudinary adapter, so the dispatch/delivery evidence path runs
	// without a real provider (FR-009, FR-010).
	mediaStore := &recordingMedia{}

	svc := orderimplement.New(orderimplement.Service{
		Orders:       orderpostgres.NewOrderRepository(pool),
		Carts:        cartcheckout.New(cartpostgres.NewCartRepository(pool)),
		Variants:     productcatalog.New(productpostgres.NewVariantRepository(pool)),
		Availability: inventoryavailability.New(inventoryRepo, orderIntegrationClock{}),
		Reservations: inventoryreservation.New(inventoryService),
		Customers:    customerAddresses{pool: pool},
		Accounts:     authaccount.New(authpostgres.NewUserRepository(pool)),
		Tx:           &database.DB{Pool: pool},
		Clock:        orderIntegrationClock{},
		Audit:        orderauditor.New(writer),
		Notifier:     notifier,
		Media:        mediaStore,
		ArtistEmail:  integrationArtistEmail,
		Mapper:       ordermapper.New(),
	})
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	root.Mount(ordersPath, handler.Router(orderHooks()))
	root.Mount(adminOrdersPath, handler.AdminRouter(orderHooks()))
	return &orderIntegrationFixture{pool: pool, root: root, svc: svc, writer: writer, notifier: notifier, media: mediaStore}
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

// seedOrderProduct writes one category, one on-sale product and its on-sale
// default variant straight into the database and puts a physical quantity on the
// variant's shelf. It returns the product identifier; the line-addressed helpers
// below resolve the default variant from it.
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
	code := "product-" + productID.String()[:8]
	if _, err := pool.Exec(ctx, `
		INSERT INTO products
			(id, name, code, description, price_amount, currency,
			 category_id, position)
		VALUES ($1, $2, $3, '', $4, 'VND', $5, 0)`,
		productID, "Product "+code, code, amount, categoryID); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	variantID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO product_variants
			(id, product_id, name, sku, price_amount, position, sell_state)
		VALUES ($1, $2, $3, $4, NULL, 0, 'ACTIVE')`,
		variantID, productID, "Variant "+code, code); err != nil {
		t.Fatalf("insert variant: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO stock_levels (variant_id, quantity) VALUES ($1, $2)`, variantID, quantity); err != nil {
		t.Fatalf("insert stock level: %v", err)
	}
	return productID
}

// defaultVariantID resolves the product's default variant: the first by position,
// created_at and id. A seeded product owns exactly one variant, so this is that
// variant.
func defaultVariantID(t *testing.T, pool *pgxpool.Pool, productID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM product_variants WHERE product_id = $1 ORDER BY position, created_at, id LIMIT 1`,
		productID).Scan(&id); err != nil {
		t.Fatalf("read the default variant: %v", err)
	}
	return id
}

// seedOrderCart writes one cart for the owner with one line for the product's
// default variant, then returns the cart's identifier.
func seedOrderCart(t *testing.T, pool *pgxpool.Pool, ownerID, productID uuid.UUID, quantity, unitPrice int64) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	cartID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO carts (id, user_id) VALUES ($1, $2)`, cartID, ownerID); err != nil {
		t.Fatalf("insert cart: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO cart_items (id, cart_id, variant_id, quantity, unit_price_amount, currency)
		VALUES ($1, $2, $3, $4, $5, 'VND')`,
		uuid.New(), cartID, defaultVariantID(t, pool, productID), quantity, unitPrice); err != nil {
		t.Fatalf("insert cart line: %v", err)
	}
	return cartID
}

// physicalStock reads a product's default variant's physical quantity straight
// from the table.
func physicalStock(t *testing.T, pool *pgxpool.Pool, productID uuid.UUID) int64 {
	t.Helper()
	var quantity int64
	if err := pool.QueryRow(context.Background(),
		`SELECT quantity FROM stock_levels WHERE variant_id = $1`, defaultVariantID(t, pool, productID)).Scan(&quantity); err != nil {
		t.Fatalf("read stock level: %v", err)
	}
	return quantity
}

// productCode reads a product's code straight from the table — the value checkout
// snapshots onto the order line (FR-002). The seeded code differs from the
// product's name, so a dropped code cannot pass the assertion.
func productCode(t *testing.T, pool *pgxpool.Pool, productID uuid.UUID) string {
	t.Helper()
	var code string
	if err := pool.QueryRow(context.Background(),
		`SELECT code FROM products WHERE id = $1`, productID).Scan(&code); err != nil {
		t.Fatalf("read product code: %v", err)
	}
	return code
}

// heldQuantity reads the quantity every active hold has set aside for one
// product's default variant.
func heldQuantity(t *testing.T, pool *pgxpool.Pool, productID uuid.UUID) int64 {
	t.Helper()
	var held int64
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(quantity), 0) FROM stock_holds WHERE variant_id = $1 AND status = 'ACTIVE'`,
		defaultVariantID(t, pool, productID)).Scan(&held); err != nil {
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

// SC-001, quickstart scenarios 1 and 2: a customer checks out through the route,
// the order carries the snapshot and the address, the artist confirms so the goods
// are held (available fell, physical did not move), and paying turns the hold into
// a sale exactly once. The order holds nothing until the artist confirms it.
func TestCheckoutCreatesTheOrderHoldsNothingThenConfirmHoldsTheGoods(t *testing.T) {
	f := newOrderIntegrationFixture(t)
	seedOrderCustomer(t, f.pool, testCustomerID)
	product := seedOrderProduct(t, f.pool, 120000, 10)
	code := productCode(t, f.pool, product)
	cartID := seedOrderCart(t, f.pool, testCustomerID, product, 2, 120000)

	rec := perform(f.root, http.MethodPost, ordersPath, `{}`, "customer-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("checkout: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	var body orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the order body %q: %v", rec.Body.String(), err)
	}
	if body.Data.Status != "PENDING" {
		t.Fatalf("the order must await the artist's confirmation, got %s", body.Data.Status)
	}
	if body.Data.Total.Amount != 240000 || body.Data.Total.Currency != "VND" {
		t.Fatalf("total = %+v, want 240000 VND", body.Data.Total)
	}
	if len(body.Data.Lines) != 1 {
		t.Fatalf("expected one line, got %+v", body.Data.Lines)
	}
	if body.Data.Lines[0].ProductID != product || body.Data.Lines[0].Quantity != 2 ||
		body.Data.Lines[0].UnitPrice.Amount != 120000 ||
		body.Data.Lines[0].ProductName != "Product "+code ||
		body.Data.Lines[0].VariantName != "Variant "+code {
		t.Fatalf("the line must carry the product and variant snapshot, got %+v", body.Data.Lines[0])
	}
	if body.Data.Address.RecipientName != "Nguyễn Văn A" || body.Data.Address.StreetAddress != "1 Đinh Tiên Hoàng" {
		t.Fatalf("the order must carry the delivery address snapshot, got %+v", body.Data.Address)
	}
	if body.Data.Total.Amount != body.Data.Lines[0].Quantity*body.Data.Lines[0].UnitPrice.Amount {
		t.Fatalf("the total must equal the exact sum of the lines, got %d", body.Data.Total.Amount)
	}

	orderID := body.Data.ID

	// FR-001, FR-002: checkout holds nothing — physical stock did not move and no
	// hold was taken; the cart is empty and one order exists.
	if got := physicalStock(t, f.pool, product); got != 10 {
		t.Fatalf("checkout must not move physical stock: got %d, want 10", got)
	}
	if got := heldQuantity(t, f.pool, product); got != 0 {
		t.Fatalf("checkout must hold nothing, got %d held", got)
	}
	if got := cartLineCount(t, f.pool, cartID); got != 0 {
		t.Fatalf("the cart must be empty after checkout, got %d lines", got)
	}
	if got := orderCount(t, f.pool, testCustomerID); got != 1 {
		t.Fatalf("expected exactly one order, got %d", got)
	}

	// FR-004, FR-005, quickstart scenario 2: the artist confirms; the goods are
	// held (available fell, physical unchanged) and the order awaits payment.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/confirm", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var confirmed adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &confirmed); err != nil {
		t.Fatalf("decode the confirmed order %q: %v", rec.Body.String(), err)
	}
	if confirmed.Data.Status != "PAYMENT_PENDING" {
		t.Fatalf("a confirmed order must await payment, got %s", confirmed.Data.Status)
	}
	if got := physicalStock(t, f.pool, product); got != 10 {
		t.Fatalf("confirmation must not move physical stock: got %d, want 10", got)
	}
	if got := heldQuantity(t, f.pool, product); got != 2 {
		t.Fatalf("confirmation must hold the ordered quantity: got %d, want 2", got)
	}

	// FR-014: paying turns the hold into a sale exactly once; replaying the
	// payment sells nothing a second time.
	if err := f.svc.MarkPaid(context.Background(), orderID, "payment-event-1", 2); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if got := physicalStock(t, f.pool, product); got != 8 {
		t.Fatalf("paying must sell the held goods, got %d physical, want 8", got)
	}
	if got := heldQuantity(t, f.pool, product); got != 0 {
		t.Fatalf("paying must clear the hold, got %d held", got)
	}
	if err := f.svc.MarkPaid(context.Background(), orderID, "payment-event-1", 2); err != nil {
		t.Fatalf("a replayed payment must be a no-op success, got %v", err)
	}
	if got := physicalStock(t, f.pool, product); got != 8 {
		t.Fatalf("a replayed payment must not sell again, got %d physical, want 8", got)
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

	// FR-019: once the artist confirms, the order holds its goods; cancelling
	// returns them and answers CANCELLED without moving physical stock.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/confirm", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := heldQuantity(t, f.pool, product); got != 2 {
		t.Fatalf("the confirmed order must hold the goods while unpaid, got %d", got)
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
// illegal move naming the state, ships a paid order — leaving an audit row naming
// the order and the administrator — and a customer's session is refused every
// administrator route (FR-021 to FR-023). Completion is the customer's act and is
// covered by TestCustomerConfirmsReceiptAgainstPostgres.
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

	// FR-005: checkout holds nothing, so the artist confirms first; the order then
	// awaits payment and is ready for the paid transition.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/confirm", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// The paid transition has no HTTP surface — module 08 drives it — so it is
	// driven directly, exactly as the quickstart says (scenario 5a).
	if err := f.svc.MarkPaid(context.Background(), orderID, "payment-event-1", 2); err != nil {
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

	// FR-005, FR-019 (quickstart 5c): dispatching a paid order succeeds and leaves
	// an audit row naming the order and the administrator.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/ship", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("ship: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var shipped adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &shipped); err != nil {
		t.Fatalf("decode the shipped order %q: %v", rec.Body.String(), err)
	}
	if shipped.Data.Status != "SHIPPING" {
		t.Fatalf("the order must answer SHIPPING, got %s", shipped.Data.Status)
	}
	f.requireAudit(t, constant.AuditOrderShipped, orderID, testAdminID)

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

// SC-005, FR-018, FR-026, quickstart scenario 5 against real PostgreSQL: the
// operator's list filters to the confirmation queue oldest-first, the artist
// declines an order awaiting confirmation — it becomes CANCELLED with no stock
// movement and an audit row naming the order and the administrator — and a
// second decline of the now-cancelled order is refused naming its state.
func TestAdminRejectsAnOrderAgainstPostgres(t *testing.T) {
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

	// FR-026 (quickstart 5c): status=PENDING&sort=oldest is the FIFO confirmation
	// queue and carries the unconfirmed order.
	rec = perform(f.root, http.MethodGet, adminOrdersPath+"?status=PENDING&sort=oldest", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("FIFO queue: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var queue adminListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &queue); err != nil {
		t.Fatalf("decode the FIFO queue %q: %v", rec.Body.String(), err)
	}
	if len(queue.Data) != 1 || queue.Data[0].ID != orderID || queue.Data[0].Status != "PENDING" {
		t.Fatalf("the confirmation queue must carry the pending order, got %+v", queue.Data)
	}

	// FR-018 (quickstart 5a): the artist declines it; it becomes CANCELLED with no
	// stock movement, and the act is audited naming the order and the administrator.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/reject", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("reject: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var declined adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &declined); err != nil {
		t.Fatalf("decode the declined order %q: %v", rec.Body.String(), err)
	}
	if declined.Data.ID != orderID || declined.Data.Status != "CANCELLED" {
		t.Fatalf("the order must answer CANCELLED, got %+v", declined.Data)
	}
	if got := heldQuantity(t, f.pool, product); got != 0 {
		t.Fatalf("a declined order must hold no goods, got %d held", got)
	}
	if got := physicalStock(t, f.pool, product); got != 10 {
		t.Fatalf("declining must not move physical stock, got %d", got)
	}
	f.requireAudit(t, constant.AuditOrderRejected, orderID, testAdminID)

	// FR-018 (quickstart 5b): declining an order that is no longer awaiting the
	// artist is refused naming the current state, and writes no second audit row.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/reject", "", "admin-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second reject: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_STATE_TRANSITION_INVALID" {
		t.Fatalf("expected ORDER_STATE_TRANSITION_INVALID, got %s", body.Error.Code)
	}
	if rows := f.auditRows(t, constant.AuditOrderRejected); len(rows) != 1 {
		t.Fatalf("a refused decline must audit nothing further, got %d rows", len(rows))
	}
}

// SC-006, quickstart scenario 6 against real PostgreSQL: an administrator
// transfers a paid order to another existing account through the route — the
// order belongs to the recipient with its lines, state and total unchanged and
// no product's physical or available stock moved — an unpaid order is refused
// 409 ORDER_NOT_TRANSFERABLE, an email no account carries is refused 404
// ORDER_TRANSFER_TARGET_NOT_FOUND, and the act leaves an audit row naming the
// order and the administrator (FR-024).
func TestTransferHandsTheOrderToAnotherAccountAgainstPostgres(t *testing.T) {
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

	recipientEmail := "order-customer-" + testCustomerTwoID.String() + "@example.com"

	// FR-024 (quickstart 6d): an order that is not paid cannot be transferred.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/transfer",
		`{"email":"`+recipientEmail+`"}`, "admin-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("transfer an unpaid order: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_NOT_TRANSFERABLE" {
		t.Fatalf("expected ORDER_NOT_TRANSFERABLE, got %s", body.Error.Code)
	}

	// FR-005: checkout holds nothing, so the artist confirms first; the order then
	// awaits payment and the paid transition turns the hold into a sale.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/confirm", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// The paid transition has no HTTP surface — module 08 drives it — so it is
	// driven directly, exactly as the quickstart says (scenario 5a). Paying turns
	// the hold into a sale: physical stock fell and the hold cleared.
	if err := f.svc.MarkPaid(context.Background(), orderID, "payment-event-transfer", 2); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	physicalAfterPaid := physicalStock(t, f.pool, product)
	heldAfterPaid := heldQuantity(t, f.pool, product)

	// FR-024 (quickstart 6c): an email no account carries is refused.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/transfer",
		`{"email":"nobody@example.com"}`, "admin-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("transfer to an unknown email: expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_TRANSFER_TARGET_NOT_FOUND" {
		t.Fatalf("expected ORDER_TRANSFER_TARGET_NOT_FOUND, got %s", body.Error.Code)
	}

	// FR-024 (quickstart 6a): the transfer hands the order to the recipient.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/transfer",
		`{"email":"`+recipientEmail+`"}`, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("transfer: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var transferred adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &transferred); err != nil {
		t.Fatalf("decode the transferred order %q: %v", rec.Body.String(), err)
	}
	if transferred.Data.ID != orderID || transferred.Data.UserID != testCustomerTwoID {
		t.Fatalf("the order must belong to the recipient, got %+v", transferred.Data)
	}
	// The lines, the state and the total are unchanged.
	if transferred.Data.Status != "PAID" || transferred.Data.Total.Amount != 240000 || len(transferred.Data.Lines) != 1 {
		t.Fatalf("a transfer must not change the state, the total or the lines, got %+v", transferred.Data)
	}
	if transferred.Data.Lines[0].ProductID != product || transferred.Data.Lines[0].Quantity != 2 {
		t.Fatalf("the line must be unchanged, got %+v", transferred.Data.Lines[0])
	}

	// FR-024 (quickstart 6b): no product's physical or available stock moved.
	if got := physicalStock(t, f.pool, product); got != physicalAfterPaid {
		t.Fatalf("a transfer must not move physical stock, got %d want %d", got, physicalAfterPaid)
	}
	if got := heldQuantity(t, f.pool, product); got != heldAfterPaid {
		t.Fatalf("a transfer must not change the hold, got %d want %d", got, heldAfterPaid)
	}

	// FR-023: the transfer leaves an audit row naming the order and the
	// administrator.
	f.requireAudit(t, constant.AuditOrderTransferred, orderID, testAdminID)

	// FR-023 (quickstart 6e): a customer's session is refused the transfer route.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/transfer",
		`{"email":"`+recipientEmail+`"}`, "customer-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("customer on transfer: expected 403, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %s", body.Error.Code)
	}
}

// editHistoryCount reads how many edit-history rows one order has.
func editHistoryCount(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM order_edit_history WHERE order_id = $1`, orderID).Scan(&count); err != nil {
		t.Fatalf("read edit history count: %v", err)
	}
	return count
}

// SC-003, FR-012 to FR-017, quickstart scenario 4 against real PostgreSQL: a
// customer edits an order awaiting confirmation (its lines are replaced, the total
// is recomputed and it stays PENDING, holding nothing), an empty edit is refused
// 409 ORDER_EMPTY, and editing an awaiting-payment order releases its hold exactly
// once, returns it to PENDING and leaves an edit-history row — so an accepted
// order must be re-confirmed. Another customer's edit answers 404 ORDER_NOT_FOUND.
func TestCustomerEditsOrderBeforePayingAgainstPostgres(t *testing.T) {
	f := newOrderIntegrationFixture(t)
	seedOrderCustomer(t, f.pool, testCustomerID)
	firstProduct := seedOrderProduct(t, f.pool, 120000, 10)
	secondProduct := seedOrderProduct(t, f.pool, 50000, 10)
	secondVariant := defaultVariantID(t, f.pool, secondProduct)
	seedOrderCart(t, f.pool, testCustomerID, firstProduct, 2, 120000)

	rec := perform(f.root, http.MethodPost, ordersPath, `{}`, "customer-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("checkout: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode the order body %q: %v", rec.Body.String(), err)
	}
	orderID := created.Data.ID

	// FR-012, FR-013: editing a PENDING order replaces its lines, recomputes the
	// total and leaves it awaiting the artist, holding nothing.
	rec = perform(f.root, http.MethodPut, ordersPath+"/"+orderID.String(),
		`{"lines":[{"variantId":"`+secondVariant.String()+`","quantity":3}]}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("edit: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var edited orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &edited); err != nil {
		t.Fatalf("decode the edited order %q: %v", rec.Body.String(), err)
	}
	if edited.Data.Status != "PENDING" {
		t.Fatalf("editing an order awaiting the artist must leave it PENDING, got %s", edited.Data.Status)
	}
	if len(edited.Data.Lines) != 1 || edited.Data.Lines[0].ProductID != secondProduct || edited.Data.Lines[0].Quantity != 3 {
		t.Fatalf("the line set must be replaced, got %+v", edited.Data.Lines)
	}
	if edited.Data.Total.Amount != 3*50000 {
		t.Fatalf("the total must be recomputed, got %+v", edited.Data.Total)
	}
	if got := heldQuantity(t, f.pool, secondProduct); got != 0 {
		t.Fatalf("editing an order awaiting the artist must hold nothing, got %d held", got)
	}
	if got := editHistoryCount(t, f.pool, orderID); got != 1 {
		t.Fatalf("an accepted edit must leave one history row, got %d", got)
	}

	// FR-015: an edit that would leave the order empty is refused.
	rec = perform(f.root, http.MethodPut, ordersPath+"/"+orderID.String(), `{"lines":[]}`, "customer-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("empty edit: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_EMPTY" {
		t.Fatalf("expected ORDER_EMPTY, got %s", body.Error.Code)
	}

	// FR-005: the artist confirms, holding the edited quantity.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/confirm", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := heldQuantity(t, f.pool, secondProduct); got != 3 {
		t.Fatalf("confirmation must hold the edited quantity, got %d", got)
	}

	// FR-014, SC-003: editing the awaiting-payment order releases its hold exactly
	// once and returns it to awaiting confirmation; nothing physical moves.
	rec = perform(f.root, http.MethodPut, ordersPath+"/"+orderID.String(),
		`{"lines":[{"variantId":"`+secondVariant.String()+`","quantity":1}]}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("edit a confirmed order: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var reEdited orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &reEdited); err != nil {
		t.Fatalf("decode the re-edited order %q: %v", rec.Body.String(), err)
	}
	if reEdited.Data.Status != "PENDING" {
		t.Fatalf("editing an awaiting-payment order must return it to PENDING, got %s", reEdited.Data.Status)
	}
	if got := heldQuantity(t, f.pool, secondProduct); got != 0 {
		t.Fatalf("editing an awaiting-payment order must release the hold exactly once, got %d held", got)
	}
	if got := physicalStock(t, f.pool, secondProduct); got != 10 {
		t.Fatalf("an edit must not move physical stock, got %d", got)
	}
	if got := editHistoryCount(t, f.pool, orderID); got != 2 {
		t.Fatalf("the second accepted edit must leave a second history row, got %d", got)
	}

	// FR-022: another customer's edit answers 404 ORDER_NOT_FOUND.
	rec = perform(f.root, http.MethodPut, ordersPath+"/"+orderID.String(),
		`{"lines":[{"variantId":"`+secondVariant.String()+`","quantity":1}]}`, "customer2-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("another customer's edit: expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_NOT_FOUND" {
		t.Fatalf("expected ORDER_NOT_FOUND, got %s", body.Error.Code)
	}
}

// SC-006, FR-019 to FR-021, quickstart scenarios 1d and 2c against real
// PostgreSQL: checkout emails the artist that the order needs confirmation, the
// artist's confirmation emails the customer the new state, and a notifier that
// fails leaves the order operation correct — cancelling still succeeds and the
// order is CANCELLED.
func TestNotificationsFireAndNeverFailTheOrderAgainstPostgres(t *testing.T) {
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

	// FR-019: the artist is told the new order needs confirmation.
	artist := f.notifier.messagesTo(integrationArtistEmail)
	if len(artist) != 1 {
		t.Fatalf("checkout must email the artist once, got %+v", f.notifier.snapshot())
	}
	if !strings.Contains(artist[0].body, string(constant.StatusPending)) {
		t.Fatalf("the artist's note must name PENDING, got %q", artist[0].body)
	}

	// FR-020: confirming emails the customer the new state.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/confirm", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	customerEmail := "order-customer-" + testCustomerID.String() + "@example.com"
	customer := f.notifier.messagesTo(customerEmail)
	if len(customer) != 1 {
		t.Fatalf("confirming must email the customer once, got %+v", f.notifier.snapshot())
	}
	if !strings.Contains(customer[0].body, string(constant.StatusPaymentPending)) {
		t.Fatalf("the customer's note must name PAYMENT_PENDING, got %q", customer[0].body)
	}

	// FR-021: with the notifier failing, cancelling still succeeds and the order
	// is CANCELLED — a delivery failure never fails the order operation.
	f.notifier.setFail(true)
	rec = perform(f.root, http.MethodPost, ordersPath+"/"+orderID.String()+"/cancel", "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel over a failing notifier: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM orders WHERE id = $1`, orderID).Scan(&status); err != nil {
		t.Fatalf("read the order status: %v", err)
	}
	if status != string(constant.StatusCancelled) {
		t.Fatalf("the order must still be CANCELLED, got %s", status)
	}
}

// assertCustomerNotifiedStatus fails unless one of the customer's captured
// messages names the given status in its body, so a test can prove a transition
// was emailed.
func assertCustomerNotifiedStatus(t *testing.T, messages []notifyMessage, status constant.Status) {
	t.Helper()
	for _, message := range messages {
		if strings.Contains(message.body, string(status)) {
			return
		}
	}
	t.Fatalf("the customer must be emailed %s, got %+v", status, messages)
}

// FR-021, FR-022, FR-023, SC-005 against real PostgreSQL: the customer is emailed
// on each status the delivery lifecycle introduces — SHIPPING, DELIVERED and
// COMPLETED (the last by the sweep's automatic completion) — and the artist is
// emailed when the customer submits feedback; and a notifier that fails leaves the
// order and the feedback correct.
func TestDeliveryAndFeedbackNotificationsFireAgainstPostgres(t *testing.T) {
	f := newOrderIntegrationFixture(t)
	seedOrderCustomer(t, f.pool, testCustomerID)
	seedOrderCustomer(t, f.pool, testCustomerTwoID)
	product := seedOrderProduct(t, f.pool, 120000, 10)
	seedOrderCart(t, f.pool, testCustomerID, product, 2, 120000)
	orderID := checkoutDeliveredOrder(t, f)

	customerEmail := "order-customer-" + testCustomerID.String() + "@example.com"

	// FR-021: the dispatch and the delivery emailed the customer the two new
	// states they introduced.
	assertCustomerNotifiedStatus(t, f.notifier.messagesTo(customerEmail), constant.StatusShipping)
	assertCustomerNotifiedStatus(t, f.notifier.messagesTo(customerEmail), constant.StatusDelivered)

	// FR-012, FR-021: aged past the window, the sweep completes the order and
	// emails the customer COMPLETED, exactly as the customer's own confirmation
	// would.
	backdateDelivery(t, f.pool, orderID, model.DeliveryCompletionWindow+time.Hour)
	if err := f.svc.AutoCompleteDelivered(context.Background()); err != nil {
		t.Fatalf("AutoCompleteDelivered: %v", err)
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusCompleted) {
		t.Fatalf("the aged order must be COMPLETED, got %s", got)
	}
	assertCustomerNotifiedStatus(t, f.notifier.messagesTo(customerEmail), constant.StatusCompleted)

	// FR-022: the customer's feedback on the completed order emails the artist.
	const content = "Dong goi rat can than"
	rec := perform(f.root, http.MethodPost, ordersPath+"/"+orderID.String()+"/feedback",
		`{"content":"`+content+`"}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("feedback: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	artistMessages := f.notifier.messagesTo(integrationArtistEmail)
	feedbackEmailed := false
	for _, message := range artistMessages {
		if strings.Contains(message.body, orderID.String()) && strings.Contains(message.body, content) {
			feedbackEmailed = true
		}
	}
	if !feedbackEmailed {
		t.Fatalf("the artist must be emailed the feedback, got %+v", f.notifier.snapshot())
	}

	// FR-023: with the notifier failing, a delivered order's feedback is still
	// stored and the order is left correct — email never breaks the operation.
	seedOrderCart(t, f.pool, testCustomerTwoID, product, 1, 120000)
	otherOrderID := checkoutDeliveredOrderAs(t, f, "customer2-token")
	f.notifier.setFail(true)
	rec = perform(f.root, http.MethodPost, ordersPath+"/"+otherOrderID.String()+"/feedback",
		`{"content":"van gui duoc"}`, "customer2-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("feedback over a failing notifier: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := feedbackCount(t, f.pool, otherOrderID); got != 1 {
		t.Fatalf("the feedback must still be stored, got %d rows", got)
	}
	if got := orderStatus(t, f.pool, otherOrderID); got != string(constant.StatusDelivered) {
		t.Fatalf("a failing notifier must leave the order DELIVERED, got %s", got)
	}
}

// evidenceJPEG is a minimal payload whose leading bytes are a JPEG signature, so
// the domain sniff accepts it. The trailing bytes stand in for image content.
func evidenceJPEG() []byte { return []byte{0xFF, 0xD8, 0xFF, 0x00, 0x11, 0x22} }

// multipartEvidence builds a multipart/form-data body carrying one "photos" file
// part per payload, and returns the body and its content type, so a test can drive
// the dispatch route's multipart contract.
func multipartEvidence(t *testing.T, payloads [][]byte) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for i, payload := range payloads {
		part, err := writer.CreateFormFile("photos", fmt.Sprintf("photo-%d.jpg", i))
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(payload); err != nil {
			t.Fatalf("write the photo part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the multipart writer: %v", err)
	}
	return buf.String(), writer.FormDataContentType()
}

// performMultipart sends a multipart/form-data request with a bearer token.
func performMultipart(handler http.Handler, path, body, contentType, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// deliveryPhotoCount reads how many evidence photos one order carries at a stage.
func deliveryPhotoCount(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID, stage string) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM order_delivery_photos WHERE order_id = $1 AND stage = $2`,
		orderID, stage).Scan(&count); err != nil {
		t.Fatalf("read delivery photo count: %v", err)
	}
	return count
}

// orderStatus reads one order's stored status.
func orderStatus(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM orders WHERE id = $1`, orderID).Scan(&status); err != nil {
		t.Fatalf("read order status: %v", err)
	}
	return status
}

// orderTotalAmount reads one order's stored committed total, so a test can prove
// a delivery step left the total unchanged (FR-020).
func orderTotalAmount(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID) int64 {
	t.Helper()
	var amount int64
	if err := pool.QueryRow(context.Background(),
		`SELECT total_amount FROM orders WHERE id = $1`, orderID).Scan(&amount); err != nil {
		t.Fatalf("read order total: %v", err)
	}
	return amount
}

// inventoryTransactionCount reads how many inventory-transaction rows the whole
// shop holds, so a test can prove a post-paid move wrote none (FR-020).
func inventoryTransactionCount(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM inventory_transactions`).Scan(&count); err != nil {
		t.Fatalf("read inventory transaction count: %v", err)
	}
	return count
}

// orderDeliveredAt reads one order's delivery instant, or nil when it has none.
func orderDeliveredAt(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID) *time.Time {
	t.Helper()
	var deliveredAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT delivered_at FROM orders WHERE id = $1`, orderID).Scan(&deliveredAt); err != nil {
		t.Fatalf("read order delivery instant: %v", err)
	}
	return deliveredAt
}

// SC-001, SC-004, FR-005, FR-006, FR-009 against real PostgreSQL: dispatching a
// paid order with two photos moves it SHIPPING and persists two SHIPPING-stage
// evidence rows with their provider references; a six-photo request is refused
// ORDER_IMAGE_LIMIT_REACHED before the media store is called (the dispatch edge
// fires once, so the counter is not a concurrency guard); and a second dispatch of
// the same order is refused ORDER_STATE_TRANSITION_INVALID naming SHIPPING.
func TestAdminDispatchesWithPhotosAgainstPostgres(t *testing.T) {
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

	// Confirm then pay, so the order is PAID and ready to dispatch.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/confirm", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if err := f.svc.MarkPaid(context.Background(), orderID, "payment-event-dispatch", 2); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}

	// FR-005, FR-006: dispatch with two photos reaches SHIPPING and persists two
	// SHIPPING-stage evidence rows through the media store.
	body, contentType := multipartEvidence(t, [][]byte{evidenceJPEG(), evidenceJPEG()})
	rec = performMultipart(f.root, adminOrdersPath+"/"+orderID.String()+"/ship", body, contentType, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("ship: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var shipped adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &shipped); err != nil {
		t.Fatalf("decode the shipped order %q: %v", rec.Body.String(), err)
	}
	if shipped.Data.Status != string(constant.StatusShipping) {
		t.Fatalf("the order must answer SHIPPING, got %s", shipped.Data.Status)
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusShipping) {
		t.Fatalf("the stored state must be SHIPPING, got %s", got)
	}
	if got := deliveryPhotoCount(t, f.pool, orderID, string(model.StageShipping)); got != 2 {
		t.Fatalf("expected two SHIPPING-stage photos, got %d", got)
	}
	var publicID, url string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT public_id, url FROM order_delivery_photos WHERE order_id = $1 ORDER BY position LIMIT 1`,
		orderID).Scan(&publicID, &url); err != nil {
		t.Fatalf("read a stored photo: %v", err)
	}
	if publicID == "" || url == "" {
		t.Fatalf("a stored photo must carry the provider reference, got %q/%q", publicID, url)
	}
	if got := f.media.uploadCount(); got != 2 {
		t.Fatalf("two photos must reach the media store, got %d uploads", got)
	}
	f.requireAudit(t, constant.AuditOrderShipped, orderID, testAdminID)

	// FR-006, SC-004: a six-photo request is refused before the media store is
	// called and leaves the order SHIPPING.
	uploadsBefore := f.media.uploadCount()
	six := make([][]byte, 6)
	for i := range six {
		six[i] = evidenceJPEG()
	}
	body, contentType = multipartEvidence(t, six)
	rec = performMultipart(f.root, adminOrdersPath+"/"+orderID.String()+"/ship", body, contentType, "admin-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("six photos: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec).Error.Code; got != "ORDER_IMAGE_LIMIT_REACHED" {
		t.Fatalf("expected ORDER_IMAGE_LIMIT_REACHED, got %s", got)
	}
	if got := f.media.uploadCount(); got != uploadsBefore {
		t.Fatalf("an over-ceiling request must not reach the media store, got %d uploads", got)
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusShipping) {
		t.Fatalf("an over-ceiling request must leave the order SHIPPING, got %s", got)
	}

	// FR-003, SC-001: a second dispatch of the same order is refused naming the
	// current state SHIPPING; the dispatch edge fires once.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/ship", "", "admin-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second dispatch: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_STATE_TRANSITION_INVALID" {
		t.Fatalf("expected ORDER_STATE_TRANSITION_INVALID, got %s", body.Error.Code)
	} else if !strings.Contains(body.Error.Message, string(constant.StatusShipping)) {
		t.Fatalf("the refusal must name SHIPPING, got %q", body.Error.Message)
	}
}

// SC-001, FR-007, FR-008, FR-020 against real PostgreSQL: delivering an
// in-transit order with two photos moves it DELIVERED, stamps delivered_at and
// persists two DELIVERED-stage evidence rows with their provider references; the
// delivery writes no inventory_transactions row and leaves the committed total
// unchanged (the goods were sold at PAID, so a delivery touches neither stock nor
// money).
func TestAdminDeliversWithPhotosAgainstPostgres(t *testing.T) {
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

	// Confirm then pay, so the order is PAID; dispatch it (no photos) so it is
	// SHIPPING and ready to be delivered.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/confirm", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if err := f.svc.MarkPaid(context.Background(), orderID, "payment-event-deliver", 2); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/ship", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("ship: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// The delivery must not touch stock or money: capture both before it runs.
	totalBefore := orderTotalAmount(t, f.pool, orderID)
	transactionsBefore := inventoryTransactionCount(t, f.pool)

	// FR-007, FR-008: deliver with two photos reaches DELIVERED, stamps the
	// delivery instant and persists two DELIVERED-stage evidence rows.
	body, contentType := multipartEvidence(t, [][]byte{evidenceJPEG(), evidenceJPEG()})
	rec = performMultipart(f.root, adminOrdersPath+"/"+orderID.String()+"/deliver", body, contentType, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("deliver: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var delivered adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &delivered); err != nil {
		t.Fatalf("decode the delivered order %q: %v", rec.Body.String(), err)
	}
	if delivered.Data.Status != string(constant.StatusDelivered) {
		t.Fatalf("the order must answer DELIVERED, got %s", delivered.Data.Status)
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusDelivered) {
		t.Fatalf("the stored state must be DELIVERED, got %s", got)
	}
	if orderDeliveredAt(t, f.pool, orderID) == nil {
		t.Fatal("a delivery must stamp delivered_at")
	}
	if got := deliveryPhotoCount(t, f.pool, orderID, string(model.StageDelivered)); got != 2 {
		t.Fatalf("expected two DELIVERED-stage photos, got %d", got)
	}
	// FR-010, FR-017: the delivery answer itself carries the evidence and the
	// delivery instant the application view holds.
	if delivered.Data.DeliveredAt == nil {
		t.Fatal("the delivery answer must carry deliveredAt")
	}
	if len(delivered.Data.DeliveryPhotos) != 2 {
		t.Fatalf("the delivery answer must carry the two photos, got %d", len(delivered.Data.DeliveryPhotos))
	}
	for i, photo := range delivered.Data.DeliveryPhotos {
		if photo.Stage != string(model.StageDelivered) || photo.URL == "" || photo.Width != 100 || photo.Height != 80 {
			t.Fatalf("delivery photo %d is not the stored evidence: %+v", i, photo)
		}
	}
	if delivered.Data.Feedback != nil {
		t.Fatalf("an order with no feedback must answer null, got %+v", delivered.Data.Feedback)
	}
	var publicID, url string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT public_id, url FROM order_delivery_photos WHERE order_id = $1 AND stage = 'DELIVERED' ORDER BY position LIMIT 1`,
		orderID).Scan(&publicID, &url); err != nil {
		t.Fatalf("read a stored photo: %v", err)
	}
	if publicID == "" || url == "" {
		t.Fatalf("a stored photo must carry the provider reference, got %q/%q", publicID, url)
	}

	// FR-020: the delivery wrote no inventory_transactions row and left the total
	// unchanged.
	if got := inventoryTransactionCount(t, f.pool); got != transactionsBefore {
		t.Fatalf("a delivery must write no inventory transaction, got %d want %d", got, transactionsBefore)
	}
	if got := orderTotalAmount(t, f.pool, orderID); got != totalBefore {
		t.Fatalf("a delivery must leave the total unchanged, got %d want %d", got, totalBefore)
	}

	// FR-010, FR-015, FR-017: the customer's own detail projects the same evidence
	// the operator sees, with `feedback` present and null before the customer has
	// written any (the feedback route is exercised by
	// TestCustomerSendsFeedbackAgainstPostgres).
	rec = perform(f.root, http.MethodGet, ordersPath+"/"+orderID.String(), "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("customer detail: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var detail orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode the customer detail %q: %v", rec.Body.String(), err)
	}
	if detail.Data.DeliveredAt == nil {
		t.Fatal("the customer detail must carry deliveredAt after a delivery")
	}
	if len(detail.Data.DeliveryPhotos) != 2 {
		t.Fatalf("the customer detail must carry the two delivered photos, got %d", len(detail.Data.DeliveryPhotos))
	}
	var detailRaw struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detailRaw); err != nil {
		t.Fatalf("decode the raw customer detail %q: %v", rec.Body.String(), err)
	}
	if value, present := detailRaw.Data["feedback"]; !present || string(value) != "null" {
		t.Fatalf("the customer detail must carry feedback: null before feedback is sent, got %s", value)
	}

	f.requireAudit(t, constant.AuditOrderDelivered, orderID, testAdminID)
}

// orderCompletionFacts reads one order's completion instant and the account that
// completed it, so a test can prove the confirmation recorded who and when
// (FR-014).
func orderCompletionFacts(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID) (*time.Time, *uuid.UUID) {
	t.Helper()
	var completedAt *time.Time
	var completedBy *uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT completed_at, completed_by FROM orders WHERE id = $1`, orderID).Scan(&completedAt, &completedBy); err != nil {
		t.Fatalf("read order completion facts: %v", err)
	}
	return completedAt, completedBy
}

// requireCustomerAudit asserts the one row of an action names the acting customer,
// the order and a SUCCESS outcome.
func (f *orderIntegrationFixture) requireCustomerAudit(t *testing.T, action string, orderID, customerID uuid.UUID) {
	t.Helper()
	rows := f.waitForAuditRows(t, action, 1)
	row := rows[0]
	if row.actorID == nil || *row.actorID != customerID {
		t.Fatalf("%s: expected the customer actor %s, got %v", action, customerID, row.actorID)
	}
	if row.actorRole != string(access.RoleCustomer) {
		t.Fatalf("%s: expected the CUSTOMER role, got %q", action, row.actorRole)
	}
	if row.targetType != "order" || row.targetID != orderID.String() {
		t.Fatalf("%s: expected the order as the target, got %q/%q", action, row.targetType, row.targetID)
	}
	if row.outcome != string(audit.OutcomeSuccess) {
		t.Fatalf("%s: expected a SUCCESS outcome, got %q", action, row.outcome)
	}
}

// SC-002, SC-006, FR-011, FR-014, FR-018, FR-020 against real PostgreSQL: after a
// delivery the owner confirms receipt and the order reaches COMPLETED with
// completed_at and completed_by set; another customer is refused the same
// ORDER_NOT_FOUND; a non-delivered order is refused naming its state; the removed
// admin complete route is gone; and the completion writes no inventory_transactions
// row and leaves the committed total unchanged (the goods were sold at PAID, so a
// completion touches neither stock nor money).
func TestCustomerConfirmsReceiptAgainstPostgres(t *testing.T) {
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

	// Confirm, pay and dispatch, so the order is SHIPPING and ready to be
	// delivered.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/confirm", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if err := f.svc.MarkPaid(context.Background(), orderID, "payment-event-complete", 2); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/ship", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("ship: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// FR-003, SC-003: confirming receipt of an order that is not delivered is
	// refused naming the current state SHIPPING, and the order stays as it was.
	rec = perform(f.root, http.MethodPost, ordersPath+"/"+orderID.String()+"/complete", "", "customer-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("complete before delivery: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_STATE_TRANSITION_INVALID" {
		t.Fatalf("expected ORDER_STATE_TRANSITION_INVALID, got %s", body.Error.Code)
	} else if !strings.Contains(body.Error.Message, string(constant.StatusShipping)) {
		t.Fatalf("the refusal must name SHIPPING, got %q", body.Error.Message)
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusShipping) {
		t.Fatalf("a refused confirmation must leave the order SHIPPING, got %s", got)
	}

	// Deliver it, so the order is DELIVERED and awaits the customer's confirmation.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/deliver", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("deliver: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusDelivered) {
		t.Fatalf("the delivered order must be DELIVERED, got %s", got)
	}

	// FR-018, SC-006: another customer's order answers the same ORDER_NOT_FOUND,
	// and the order is left DELIVERED.
	rec = perform(f.root, http.MethodPost, ordersPath+"/"+orderID.String()+"/complete", "", "customer2-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("another customer: expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_NOT_FOUND" {
		t.Fatalf("expected ORDER_NOT_FOUND, got %s", body.Error.Code)
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusDelivered) {
		t.Fatalf("a foreign confirmation must leave the order DELIVERED, got %s", got)
	}

	// FR-011: the old admin complete route is gone — it answers not-found or
	// method-not-allowed, not a completed order.
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/complete", "", "admin-token")
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("the removed admin complete route must answer 404 or 405, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusDelivered) {
		t.Fatalf("the removed admin route must not complete the order, got %s", got)
	}

	// The completion must not touch stock or money: capture both before it runs.
	totalBefore := orderTotalAmount(t, f.pool, orderID)
	transactionsBefore := inventoryTransactionCount(t, f.pool)

	// FR-011, FR-014: the owner confirms receipt and the order reaches COMPLETED
	// with the completion instant and the acting customer recorded.
	rec = perform(f.root, http.MethodPost, ordersPath+"/"+orderID.String()+"/complete", "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var completed orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &completed); err != nil {
		t.Fatalf("decode the completed order %q: %v", rec.Body.String(), err)
	}
	if completed.Data.Status != string(constant.StatusCompleted) {
		t.Fatalf("the order must answer COMPLETED, got %s", completed.Data.Status)
	}
	if completed.Data.CompletedAt == nil {
		t.Fatal("the completed order must answer completedAt")
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusCompleted) {
		t.Fatalf("the stored state must be COMPLETED, got %s", got)
	}
	completedAt, completedBy := orderCompletionFacts(t, f.pool, orderID)
	if completedAt == nil {
		t.Fatal("a confirmation must stamp completed_at")
	}
	if completedBy == nil || *completedBy != testCustomerID {
		t.Fatalf("completion must record the acting customer %s, got %v", testCustomerID, completedBy)
	}
	f.requireCustomerAudit(t, constant.AuditOrderCompleted, orderID, testCustomerID)

	// FR-020: the completion wrote no inventory_transactions row and left the
	// total unchanged.
	if got := inventoryTransactionCount(t, f.pool); got != transactionsBefore {
		t.Fatalf("a completion must write no inventory transaction, got %d want %d", got, transactionsBefore)
	}
	if got := orderTotalAmount(t, f.pool, orderID); got != totalBefore {
		t.Fatalf("a completion must leave the total unchanged, got %d want %d", got, totalBefore)
	}

	// FR-004: COMPLETED is terminal; a second confirmation is refused naming it.
	rec = perform(f.root, http.MethodPost, ordersPath+"/"+orderID.String()+"/complete", "", "customer-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second confirmation: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "ORDER_STATE_TRANSITION_INVALID" {
		t.Fatalf("expected ORDER_STATE_TRANSITION_INVALID, got %s", body.Error.Code)
	} else if !strings.Contains(body.Error.Message, string(constant.StatusCompleted)) {
		t.Fatalf("the refusal must name COMPLETED, got %q", body.Error.Message)
	}
}

// checkoutDeliveredOrder drives the real HTTP surface to a DELIVERED order:
// checkout, the artist's confirmation, the paid transition module 08 drives, the
// dispatch and the delivery. It returns the order identifier, so a test can then
// age the delivery and exercise the five-day sweep against real PostgreSQL.
func checkoutDeliveredOrder(t *testing.T, f *orderIntegrationFixture) uuid.UUID {
	t.Helper()
	return checkoutDeliveredOrderAs(t, f, "customer-token")
}

// checkoutDeliveredOrderAs is checkoutDeliveredOrder for a named session, so a
// test can produce a delivered order for a second customer too.
func checkoutDeliveredOrderAs(t *testing.T, f *orderIntegrationFixture, token string) uuid.UUID {
	t.Helper()
	rec := perform(f.root, http.MethodPost, ordersPath, `{}`, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("checkout: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode the order body %q: %v", rec.Body.String(), err)
	}
	orderID := created.Data.ID

	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/confirm", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if err := f.svc.MarkPaid(context.Background(), orderID, "payment-event-auto-"+orderID.String(), 2); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/ship", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("ship: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = perform(f.root, http.MethodPost, adminOrdersPath+"/"+orderID.String()+"/deliver", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("deliver: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusDelivered) {
		t.Fatalf("the order must be DELIVERED before the sweep, got %s", got)
	}
	return orderID
}

// backdateDelivery moves an order's delivery instant into the past, so a test can
// place the order past the five-day window the sweep reads. The window itself is
// computed by the application from the injected clock, never by the database, so
// aging the stored instant is the only thing a test has to move (research D6).
func backdateDelivery(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID, age time.Duration) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE orders SET delivered_at = $2 WHERE id = $1`,
		orderID, time.Now().UTC().Add(-age)); err != nil {
		t.Fatalf("backdate the delivery: %v", err)
	}
}

// SC-002, FR-012, FR-013, FR-014 against real PostgreSQL: a DELIVERED order past
// delivered_at + 5 days is completed by the sweep with no acting account, exactly
// once across repeats, and the completion is audited naming the order and no
// actor; an order still inside the window is never completed by time, and a
// repeated sweep neither completes nor audits a second time.
func TestAutoCompletesADeliveredOrderPastTheWindowAgainstPostgres(t *testing.T) {
	f := newOrderIntegrationFixture(t)
	seedOrderCustomer(t, f.pool, testCustomerID)
	product := seedOrderProduct(t, f.pool, 120000, 10)
	seedOrderCart(t, f.pool, testCustomerID, product, 2, 120000)
	orderID := checkoutDeliveredOrder(t, f)

	// FR-012, FR-013: delivered inside the window, the sweep leaves the order for
	// the customer to confirm.
	if err := f.svc.AutoCompleteDelivered(context.Background()); err != nil {
		t.Fatalf("AutoCompleteDelivered inside the window: %v", err)
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusDelivered) {
		t.Fatalf("an order inside the window must stay DELIVERED, got %s", got)
	}

	// Age the delivery past the window; the same sweep now completes it.
	backdateDelivery(t, f.pool, orderID, model.DeliveryCompletionWindow+time.Hour)
	if err := f.svc.AutoCompleteDelivered(context.Background()); err != nil {
		t.Fatalf("AutoCompleteDelivered: %v", err)
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusCompleted) {
		t.Fatalf("a delivered order past its window must complete, got %s", got)
	}
	completedAt, completedBy := orderCompletionFacts(t, f.pool, orderID)
	if completedAt == nil {
		t.Fatal("the sweep must stamp completed_at")
	}
	if completedBy != nil {
		t.Fatalf("the sweep completes with no acting customer, got %v", completedBy)
	}
	rows := f.waitForAuditRows(t, constant.AuditOrderCompleted, 1)
	row := rows[0]
	if row.actorID != nil {
		t.Fatalf("the automatic completion must record no actor, got %v", row.actorID)
	}
	if row.actorRole != "" {
		t.Fatalf("the automatic completion must record no role, got %q", row.actorRole)
	}
	if row.targetType != "order" || row.targetID != orderID.String() {
		t.Fatalf("the automatic completion must name the order, got %q/%q", row.targetType, row.targetID)
	}
	if row.outcome != string(audit.OutcomeSuccess) {
		t.Fatalf("expected a SUCCESS outcome, got %q", row.outcome)
	}

	// FR-012: a repeated sweep completes nothing a second time — the order is no
	// longer DELIVERED, so nothing is written and no second audit row appears.
	if err := f.svc.AutoCompleteDelivered(context.Background()); err != nil {
		t.Fatalf("a repeated sweep must not fail: %v", err)
	}
	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusCompleted) {
		t.Fatalf("a repeated sweep must leave the order COMPLETED, got %s", got)
	}
	completedAtAgain, completedByAgain := orderCompletionFacts(t, f.pool, orderID)
	if !completedAtAgain.Equal(*completedAt) || completedByAgain != nil {
		t.Fatalf("a repeated sweep must not rewrite the completion facts, got %v/%v", completedAtAgain, completedByAgain)
	}
	time.Sleep(200 * time.Millisecond)
	if again := f.auditRows(t, constant.AuditOrderCompleted); len(again) != 1 {
		t.Fatalf("a repeated sweep must not audit again, got %d rows", len(again))
	}
}

// SC-002, FR-012, FR-014 against real PostgreSQL: the customer's confirmation and
// the completion sweep race on the same delivered order past its window. Whichever
// commits second finds the order no longer DELIVERED, so the order reaches
// COMPLETED exactly once: one completion, one ORDER_COMPLETED audit row, and the
// completing side recorded (the customer when the confirmation won, nobody when
// the sweep did).
func TestCustomerConfirmationRacesTheCompletionSweepAgainstPostgres(t *testing.T) {
	f := newOrderIntegrationFixture(t)
	seedOrderCustomer(t, f.pool, testCustomerID)
	product := seedOrderProduct(t, f.pool, 120000, 10)
	seedOrderCart(t, f.pool, testCustomerID, product, 2, 120000)
	orderID := checkoutDeliveredOrder(t, f)
	backdateDelivery(t, f.pool, orderID, model.DeliveryCompletionWindow+time.Hour)

	actorCtx := appinterface.WithActor(context.Background(),
		appinterface.Actor{ID: testCustomerID, Role: access.RoleCustomer})

	start := make(chan struct{})
	var wg sync.WaitGroup
	var sweepErr, confirmErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		sweepErr = f.svc.AutoCompleteDelivered(context.Background())
	}()
	go func() {
		defer wg.Done()
		<-start
		_, confirmErr = f.svc.ConfirmReceived(actorCtx, appdto.OrderRefInput{OrderID: orderID})
	}()
	close(start)
	wg.Wait()

	if sweepErr != nil {
		t.Fatalf("the sweep must not fail when it loses the race: %v", sweepErr)
	}
	// The confirmation either wins or is refused naming the completed state;
	// anything else is a real failure.
	if confirmErr != nil && !errors.Is(confirmErr, domainerr.ErrStateTransitionInvalid) {
		t.Fatalf("the confirmation must win or be refused by the state, got %v", confirmErr)
	}

	if got := orderStatus(t, f.pool, orderID); got != string(constant.StatusCompleted) {
		t.Fatalf("the raced order must be COMPLETED exactly once, got %s", got)
	}
	completedAt, completedBy := orderCompletionFacts(t, f.pool, orderID)
	if completedAt == nil {
		t.Fatal("the raced completion must stamp completed_at")
	}
	rows := f.waitForAuditRows(t, constant.AuditOrderCompleted, 1)
	if confirmErr == nil {
		// The customer's confirmation won: it records the customer as the actor.
		if completedBy == nil || *completedBy != testCustomerID {
			t.Fatalf("the customer's completion must record the acting customer, got %v", completedBy)
		}
		if rows[0].actorID == nil || *rows[0].actorID != testCustomerID {
			t.Fatalf("the customer's completion must audit the actor, got %v", rows[0].actorID)
		}
	} else {
		// The sweep won: it records no actor.
		if completedBy != nil {
			t.Fatalf("the sweep's completion records no actor, got %v", completedBy)
		}
		if rows[0].actorID != nil {
			t.Fatalf("the sweep's completion must audit no actor, got %v", rows[0].actorID)
		}
	}

	// Exactly one completion survives the race.
	time.Sleep(200 * time.Millisecond)
	if again := f.auditRows(t, constant.AuditOrderCompleted); len(again) != 1 {
		t.Fatalf("the race must produce a single completion, got %d audit rows", len(again))
	}
}

// SC-005, FR-015, FR-016, FR-018 against real PostgreSQL: the owner sends one
// order-level feedback through the route, it persists and reads back on the order
// detail, a second submission is refused ORDER_FEEDBACK_ALREADY_SENT, and a
// concurrent duplicate insert is refused by the order_feedback_order_key unique
// index - surfaced as the same refusal rather than an internal error. Another
// customer's order answers ORDER_NOT_FOUND and is left without feedback.
func TestCustomerSendsFeedbackAgainstPostgres(t *testing.T) {
	f := newOrderIntegrationFixture(t)
	seedOrderCustomer(t, f.pool, testCustomerID)
	seedOrderCustomer(t, f.pool, testCustomerTwoID)
	product := seedOrderProduct(t, f.pool, 120000, 10)
	seedOrderCart(t, f.pool, testCustomerID, product, 2, 120000)
	orderID := checkoutDeliveredOrder(t, f)

	const content = "Giao hang can than, tranh nguyen ven"

	// FR-015, FR-017: the owner sends feedback for the delivered order and the
	// answer carries it.
	rec := perform(f.root, http.MethodPost, ordersPath+"/"+orderID.String()+"/feedback",
		`{"content":"`+content+`"}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("feedback: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var answered orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("decode the feedback answer %q: %v", rec.Body.String(), err)
	}
	if answered.Data.Feedback == nil || answered.Data.Feedback.Content != content {
		t.Fatalf("the answer must carry the recorded feedback, got %+v", answered.Data.Feedback)
	}

	// FR-022: the artist is emailed that the order carries the feedback, named
	// with the order and its content.
	artistMessages := f.notifier.messagesTo(integrationArtistEmail)
	feedbackEmailed := false
	for _, message := range artistMessages {
		if strings.Contains(message.body, orderID.String()) && strings.Contains(message.body, content) {
			feedbackEmailed = true
		}
	}
	if !feedbackEmailed {
		t.Fatalf("the artist must be emailed the feedback, got %+v", f.notifier.snapshot())
	}

	// It is persisted with the session's author.
	var storedAuthor uuid.UUID
	var storedContent string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT author_id, content FROM order_feedback WHERE order_id = $1`, orderID).
		Scan(&storedAuthor, &storedContent); err != nil {
		t.Fatalf("read the stored feedback: %v", err)
	}
	if storedAuthor != testCustomerID {
		t.Fatalf("the feedback must name the session's account %s, got %s", testCustomerID, storedAuthor)
	}
	if storedContent != content {
		t.Fatalf("the stored feedback must carry the sent content, got %q", storedContent)
	}

	// FR-017: the customer's detail reads the feedback back.
	rec = perform(f.root, http.MethodGet, ordersPath+"/"+orderID.String(), "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("customer detail: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var detail orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode the customer detail %q: %v", rec.Body.String(), err)
	}
	if detail.Data.Feedback == nil || detail.Data.Feedback.Content != content {
		t.Fatalf("the customer detail must carry the feedback, got %+v", detail.Data.Feedback)
	}

	// FR-016: a second submission is refused and does not add a row.
	rec = perform(f.root, http.MethodPost, ordersPath+"/"+orderID.String()+"/feedback",
		`{"content":"second attempt"}`, "customer-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second feedback: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != constant.CodeFeedbackAlreadySent {
		t.Fatalf("expected ORDER_FEEDBACK_ALREADY_SENT, got %s", body.Error.Code)
	}
	if got := feedbackCount(t, f.pool, orderID); got != 1 {
		t.Fatalf("a refused second feedback must not add a row, got %d", got)
	}

	// A fresh delivered order owned by the second customer, so the concurrency
	// check starts from an order with no feedback.
	seedOrderCart(t, f.pool, testCustomerTwoID, product, 1, 120000)
	otherOrderID := checkoutDeliveredOrderAs(t, f, "customer2-token")

	// FR-018: another customer's order answers the same not-found, and nothing is
	// written.
	rec = perform(f.root, http.MethodPost, ordersPath+"/"+otherOrderID.String()+"/feedback",
		`{"content":"not my order"}`, "customer-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("another customer's order: expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != constant.CodeOrderNotFound {
		t.Fatalf("expected ORDER_NOT_FOUND, got %s", body.Error.Code)
	}
	if got := feedbackCount(t, f.pool, otherOrderID); got != 0 {
		t.Fatalf("a foreign feedback must write nothing, got %d rows", got)
	}

	// FR-016, research D8: two concurrent inserts race; the unique index refuses
	// one, and the repository surfaces it as ORDER_FEEDBACK_ALREADY_SENT.
	repo := orderpostgres.NewOrderRepository(f.pool)
	uow := &database.DB{Pool: f.pool}
	contents := []string{"concurrent-a", "concurrent-b"}
	results := make([]error, len(contents))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range contents {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = uow.WithinTx(context.Background(), func(txCtx context.Context) error {
				return repo.InsertFeedback(txCtx, model.Feedback{
					ID:        uuid.New(),
					OrderID:   otherOrderID,
					AuthorID:  testCustomerTwoID,
					Content:   contents[i],
					CreatedAt: time.Now().UTC(),
				})
			})
		}(i)
	}
	close(start)
	wg.Wait()

	inserted, duplicates := 0, 0
	for i, err := range results {
		switch {
		case err == nil:
			inserted++
		case errors.Is(err, domainerr.ErrFeedbackAlreadySent):
			duplicates++
		default:
			t.Fatalf("concurrent insert %d: unexpected error: %v", i, err)
		}
	}
	if inserted != 1 || duplicates != 1 {
		t.Fatalf("the unique index must admit exactly one insert and refuse the other, got %d inserted and %d duplicates",
			inserted, duplicates)
	}
	if got := feedbackCount(t, f.pool, otherOrderID); got != 1 {
		t.Fatalf("the race must leave exactly one feedback row, got %d", got)
	}
}

// feedbackCount reads how many feedback rows one order carries.
func feedbackCount(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM order_feedback WHERE order_id = $1`, orderID).Scan(&count); err != nil {
		t.Fatalf("read the feedback count: %v", err)
	}
	return count
}
