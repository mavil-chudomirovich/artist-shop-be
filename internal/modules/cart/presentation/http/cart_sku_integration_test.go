//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	cartimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/implement"
	cartmapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/mapper"
	cartcheckout "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/infrastructure/implement/checkout"
	cartpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/infrastructure/implement/postgres"
	inventoryavailability "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/availability"
	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
	orderdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	orderimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/implement"
	orderappinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	ordermapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	orderpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/postgres"
	productcatalog "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/catalog"
	productpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file drives US1's cart-by-variant and the checkout against a real
// PostgreSQL container: the real cart adapter, the real product facts over module
// 04's repository, module 05's real availability, the real order adapter and the
// real HTTP surface. Its centre of gravity is what a fake cannot prove — the line's
// identity is the variant (FR-018), the three refusals hold against real data, and
// a checkout stores one order line per (product code, SKU) carrying the snapshot
// (FR-020, SC-003). Docker is required to run it; the file still compiles without a
// container so `go vet -tags integration` covers it.

// skuCartClock is the production-like clock the availability read and the order
// service need.
type skuCartClock struct{}

func (skuCartClock) Now() time.Time { return time.Now().UTC() }

// fixedCustomerLookup answers the order's CustomerLookupService with one default
// address, so a checkout has somewhere to go without wiring module 02. It is a
// test-local adapter: the boundary the constitution protects is between modules,
// not between a test and a fixture.
type fixedCustomerLookup struct{}

func (fixedCustomerLookup) LookupCustomer(_ context.Context, userID uuid.UUID) (contracts.Customer, error) {
	return contracts.Customer{
		ID: userID,
		Addresses: []contracts.CustomerAddress{{
			ID: uuid.New(), RecipientName: "Nguyễn Văn A", RecipientPhone: "0912345678",
			ProvinceCode: "01", ProvinceName: "Hà Nội",
			WardCode: "00001", WardName: "Phúc Xá",
			StreetAddress: "1 Đinh Tiên Hoàng", IsDefault: true,
			FacebookURL: "https://facebook.com/test.customer",
		}},
	}, nil
}

// skuCartFixture is one migrated PostgreSQL container plus the real cart HTTP
// surface and the real order use case, so a checkout can be driven directly.
type skuCartFixture struct {
	pool   *pgxpool.Pool
	root   http.Handler
	orders *orderimplement.Service
}

func newSKUCartFixture(t *testing.T) *skuCartFixture {
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

	cartRepo := cartpostgres.NewCartRepository(pool)
	inventoryRepo := inventorypostgres.NewInventoryRepository(pool)
	variantFacts := productcatalog.New(productpostgres.NewVariantRepository(pool))

	cartService := cartimplement.New(cartimplement.Service{
		Carts:        cartRepo,
		Variants:     variantFacts,
		Availability: inventoryavailability.New(inventoryRepo, skuCartClock{}),
		Tx:           &database.DB{Pool: pool},
		Clock:        skuCartClock{},
		Mapper:       cartmapper.New(),
	})
	handler := New(cartService, testLogger)
	root := chi.NewRouter()
	root.Mount(cartPath, handler.Router(cartHooks(nil)))

	orderService := orderimplement.New(orderimplement.Service{
		Orders:       orderpostgres.NewOrderRepository(pool),
		Carts:        cartcheckout.New(cartRepo),
		Variants:     variantFacts,
		Availability: inventoryavailability.New(inventoryRepo, skuCartClock{}),
		Customers:    fixedCustomerLookup{},
		Tx:           &database.DB{Pool: pool},
		Clock:        skuCartClock{},
		Mapper:       ordermapper.New(),
	})
	return &skuCartFixture{pool: pool, root: root, orders: orderService}
}

// seedSKUCustomer writes the signed-in account the test hooks resolve, so the carts
// table has a real owner to reference.
func (f *skuCartFixture) seedSKUCustomer(t *testing.T) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'hash')`,
		testCustomerID, "cart-sku@example.com"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

// skuCartProduct is one seeded product.
type skuCartProduct struct {
	id uuid.UUID
}

// seedSKUProduct writes one category and one product straight into the database,
// so a variant can be attached and a checkout can snapshot its code and name.
func (f *skuCartFixture) seedSKUProduct(t *testing.T, code string, defaultAmount int64) skuCartProduct {
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
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO products
			(id, name, code, description, price_amount, currency,
			 category_id, position)
		VALUES ($1, $2, $3, '', $4, 'VND', $5, 0)`,
		productID, "Product "+code, code, defaultAmount, categoryID); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	return skuCartProduct{id: productID}
}

// seedSKUVariant writes one variant of a product in the given sell state with an
// optional own price, and puts stock on its shelf.
func (f *skuCartFixture) seedSKUVariant(t *testing.T, productID uuid.UUID, name, sku, sellState string, amount *int64, stock int64) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	variantID := uuid.New()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO product_variants
			(id, product_id, name, sku, price_amount, position, sell_state)
		VALUES ($1, $2, $3, $4, $5, 0, $6)`,
		variantID, productID, name, sku, amount, sellState); err != nil {
		t.Fatalf("insert variant: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO stock_levels (variant_id, quantity) VALUES ($1, $2)`, variantID, stock); err != nil {
		t.Fatalf("insert stock level: %v", err)
	}
	return variantID
}

func skuCartAmount(value int64) *int64 { return &value }

// FR-018, FR-002 against real PostgreSQL: adding a variant creates exactly one
// line carrying the product name, the variant name and the variant's effective
// price captured at add time; adding to a cart holds no stock.
func TestCartAddByVariantAgainstPostgres(t *testing.T) {
	f := newSKUCartFixture(t)
	f.seedSKUCustomer(t)
	product := f.seedSKUProduct(t, "card-1", 150000)
	variant := f.seedSKUVariant(t, product.id, "Khổ lớn", "CARD-1-L", "ACTIVE", skuCartAmount(120000), 10)

	rec := performJSON(f.root, http.MethodPost, cartPath+"/items",
		`{"variantId":"`+variant.String()+`","quantity":2}`, "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("add: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeCart(t, rec)
	if len(body.Data.Lines) != 1 {
		t.Fatalf("adding a variant must create exactly one line, got %+v", body.Data.Lines)
	}
	line := body.Data.Lines[0]
	if line.VariantID != variant || line.Quantity != 2 {
		t.Fatalf("unexpected line identity: %+v", line)
	}
	if line.ProductName == nil || *line.ProductName != "Product card-1" ||
		line.VariantName == nil || *line.VariantName != "Khổ lớn" {
		t.Fatalf("the line must carry the product and variant names, got %+v", line)
	}
	if line.UnitPrice.Amount != 120000 || line.LineTotal.Amount != 240000 {
		t.Fatalf("unexpected money: unit=%+v total=%+v", line.UnitPrice, line.LineTotal)
	}
	if body.Data.Subtotal == nil || body.Data.Subtotal.Amount != 240000 {
		t.Fatalf("unexpected subtotal: %+v", body.Data.Subtotal)
	}

	// FR-011: adding to a cart holds no stock.
	var stock int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT quantity FROM stock_levels WHERE variant_id = $1`, variant).Scan(&stock); err != nil {
		t.Fatalf("read stock: %v", err)
	}
	if stock != 10 {
		t.Fatalf("adding to a cart must not change stock, got %d", stock)
	}
}

// FR-006, FR-007 against real PostgreSQL: the three refusals — adding without a
// variant, adding a variant that is not on sale, and adding more than is available
// — and the not-found an unknown variant answers.
func TestCartAddRefusalsAgainstPostgres(t *testing.T) {
	f := newSKUCartFixture(t)
	f.seedSKUCustomer(t)
	product := f.seedSKUProduct(t, "card-1", 150000)
	announced := f.seedSKUVariant(t, product.id, "Sắp ra mắt", "CARD-1-N", "COMING_SOON", nil, 10)
	scarce := f.seedSKUVariant(t, product.id, "Hiếm", "CARD-1-S", "ACTIVE", nil, 1)

	t.Run("without a variant", func(t *testing.T) {
		rec := performJSON(f.root, http.MethodPost, cartPath+"/items", `{"quantity":1}`, "customer-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
		}
		body := decodeError(t, rec)
		if body.Error.Code != "VALIDATION_ERROR" || len(body.Error.Details) != 1 || body.Error.Details[0].Field != "variantId" {
			t.Fatalf("expected VALIDATION_ERROR naming variantId, got %+v", body.Error)
		}
	})

	t.Run("not on sale", func(t *testing.T) {
		rec := performJSON(f.root, http.MethodPost, cartPath+"/items",
			`{"variantId":"`+announced.String()+`","quantity":1}`, "customer-token")
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
		}
		if body := decodeError(t, rec); body.Error.Code != "CART_PRODUCT_NOT_PURCHASABLE" {
			t.Fatalf("expected CART_PRODUCT_NOT_PURCHASABLE, got %s", body.Error.Code)
		}
	})

	t.Run("more than available", func(t *testing.T) {
		rec := performJSON(f.root, http.MethodPost, cartPath+"/items",
			`{"variantId":"`+scarce.String()+`","quantity":5}`, "customer-token")
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
		}
		body := decodeError(t, rec)
		if body.Error.Code != "CART_QUANTITY_EXCEEDS_AVAILABLE" || len(body.Error.Details) != 1 || body.Error.Details[0].Field != "quantity" {
			t.Fatalf("expected CART_QUANTITY_EXCEEDS_AVAILABLE naming quantity, got %+v", body.Error)
		}
	})

	// FR-008: a variant no product carries is the shared not-found. A variant that
	// exists but is not buyable is the distinct not-purchasable refusal above, so
	// the two are never confused.
	t.Run("unknown variant", func(t *testing.T) {
		rec := performJSON(f.root, http.MethodPost, cartPath+"/items",
			`{"variantId":"`+uuid.New().String()+`","quantity":1}`, "customer-token")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
		}
		if body := decodeError(t, rec); body.Error.Code != "PRODUCT_NOT_FOUND" {
			t.Fatalf("expected PRODUCT_NOT_FOUND, got %s", body.Error.Code)
		}
	})

	// A variant whose product was removed is gone too: adding it answers the same
	// not-found.
	t.Run("removed product's variant", func(t *testing.T) {
		goneProduct := f.seedSKUProduct(t, "gone-1", 100000)
		gone := f.seedSKUVariant(t, goneProduct.id, "Gone", "GONE-1-D", "ACTIVE", nil, 5)
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, goneProduct.id); err != nil {
			t.Fatalf("delete product: %v", err)
		}
		rec := performJSON(f.root, http.MethodPost, cartPath+"/items",
			`{"variantId":"`+gone.String()+`","quantity":1}`, "customer-token")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
		}
		if body := decodeError(t, rec); body.Error.Code != "PRODUCT_NOT_FOUND" {
			t.Fatalf("expected PRODUCT_NOT_FOUND, got %s", body.Error.Code)
		}
	})
}

// SC-003, FR-019, FR-020 against real PostgreSQL: a checkout of a cart holding two
// variants of one product stores exactly one order line per (product code, SKU),
// each carrying the snapshot — product code, SKU, variant name, unit price and
// quantity — and empties the cart.
func TestCheckoutStoresOneLinePerProductCodeAndSKUAgainstPostgres(t *testing.T) {
	f := newSKUCartFixture(t)
	f.seedSKUCustomer(t)
	product := f.seedSKUProduct(t, "card-1", 150000)
	small := f.seedSKUVariant(t, product.id, "Khổ nhỏ", "CARD-1-S", "ACTIVE", skuCartAmount(120000), 5)
	large := f.seedSKUVariant(t, product.id, "Khổ lớn", "CARD-1-L", "ACTIVE", skuCartAmount(180000), 5)

	// Two variants of one product become two cart lines.
	for _, add := range []struct {
		variant  uuid.UUID
		quantity int
	}{{small, 1}, {large, 2}} {
		rec := performJSON(f.root, http.MethodPost, cartPath+"/items",
			`{"variantId":"`+add.variant.String()+`","quantity":`+strconv.Itoa(add.quantity)+`}`, "customer-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("add %s: expected 200, got %d (%s)", add.variant, rec.Code, rec.Body.String())
		}
	}
	if body := decodeCart(t, performJSON(f.root, http.MethodGet, cartPath, "", "customer-token")); len(body.Data.Lines) != 2 {
		t.Fatalf("two variants must be two lines, got %+v", body.Data.Lines)
	}

	ctx := orderappinterface.WithActor(context.Background(),
		orderappinterface.Actor{ID: testCustomerID, Role: access.RoleCustomer})
	view, err := f.orders.Checkout(ctx, orderdto.CheckoutInput{})
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if len(view.Lines) != 2 {
		t.Fatalf("the order must carry one line per variant, got %+v", view.Lines)
	}

	// One order line per (product code, SKU), each carrying the snapshot.
	rows, err := f.pool.Query(context.Background(),
		`SELECT product_code, sku, variant_name, unit_price_amount, quantity
		   FROM order_items WHERE order_id = $1 ORDER BY sku`, view.ID)
	if err != nil {
		t.Fatalf("read order lines: %v", err)
	}
	defer rows.Close()
	type storedLine struct {
		productCode string
		sku         string
		variantName string
		unitPrice   int64
		quantity    int64
	}
	var stored []storedLine
	for rows.Next() {
		var line storedLine
		if err := rows.Scan(&line.productCode, &line.sku, &line.variantName, &line.unitPrice, &line.quantity); err != nil {
			t.Fatalf("scan order line: %v", err)
		}
		stored = append(stored, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate order lines: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("expected exactly one line per (product code, SKU), got %+v", stored)
	}
	if stored[0].productCode != "card-1" || stored[0].sku != "CARD-1-L" || stored[0].variantName != "Khổ lớn" ||
		stored[0].unitPrice != 180000 || stored[0].quantity != 2 {
		t.Fatalf("the large line's snapshot is wrong: %+v", stored[0])
	}
	if stored[1].productCode != "card-1" || stored[1].sku != "CARD-1-S" || stored[1].variantName != "Khổ nhỏ" ||
		stored[1].unitPrice != 120000 || stored[1].quantity != 1 {
		t.Fatalf("the small line's snapshot is wrong: %+v", stored[1])
	}

	// The cart is emptied by the checkout.
	if body := decodeCart(t, performJSON(f.root, http.MethodGet, cartPath, "", "customer-token")); len(body.Data.Lines) != 0 {
		t.Fatalf("the cart must be empty after checkout, got %+v", body.Data.Lines)
	}
}
