//go:build integration

package implement

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	inventoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/implement"
	inventorymapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
	inventoryreservation "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/reservation"
	ordermapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	orderpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file drives US2 against a real PostgreSQL container: the real order
// adapter and the real inventory reservation use cases, so the guarantees a fake
// cannot prove are proven where they live. Its centre of gravity is quickstart
// scenarios 5 and 7: the storage state check refuses an unlisted state, confirming
// payment twice sells a held quantity once, and an order left unpaid past its
// window frees its goods exactly once across the order's sweep and module 05's
// (SC-003, SC-004, FR-009, FR-012, FR-014).
//
// Docker is required to run it; the file still compiles without a container so
// `go vet -tags integration ./...` covers it.

// lifecycleDBFixture is one migrated PostgreSQL container plus the real order
// service and the inventory service whose sweeps share the same holds.
type lifecycleDBFixture struct {
	pool      *pgxpool.Pool
	svc       *Service
	inventory *inventoryimplement.Service
}

// lifecycleClock is the production-like clock the fixture's use cases read.
type lifecycleClock struct{}

func (lifecycleClock) Now() time.Time { return time.Now().UTC() }

// lifecycleProductExists answers the inventory module's ProductLookup over the
// products table, so Reserve can tell an unknown product from an unstocked one.
// It is a test-local adapter: the boundary the constitution protects is between
// modules, not between a test and a fixture.
type lifecycleProductExists struct{ pool *pgxpool.Pool }

func (p lifecycleProductExists) ProductExists(ctx context.Context, productID uuid.UUID) (bool, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM products WHERE id = $1)`, productID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func newLifecycleDBFixture(t *testing.T) *lifecycleDBFixture {
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
		Lookup:    lifecycleProductExists{pool: pool},
		Tx:        &database.DB{Pool: pool},
		Clock:     lifecycleClock{},
		Mapper:    inventorymapper.New(),
	})
	svc := New(Service{
		Orders:       orderpostgres.NewOrderRepository(pool),
		Reservations: inventoryreservation.New(inventoryService),
		Tx:           &database.DB{Pool: pool},
		Clock:        lifecycleClock{},
		Mapper:       ordermapper.New(),
	})
	return &lifecycleDBFixture{pool: pool, svc: svc, inventory: inventoryService}
}

// insertProduct writes one category and one on-sale product straight into the
// database and puts a physical quantity on its shelf. The test may do this: the
// boundary the constitution protects is between modules, not between a test and a
// fixture.
func (f *lifecycleDBFixture) insertProduct(t *testing.T, amount, quantity int64) uuid.UUID {
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
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO products
			(id, name, slug, normalized_slug, description, price_amount, currency,
			 category_id, position, sell_state, is_set, is_preorder)
		VALUES ($1, $2, $3, $4, '', $5, 'VND', $6, 0, 'ACTIVE', false, false)`,
		productID, "Product "+slug, slug, slug, amount, categoryID); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO stock_levels (product_id, quantity) VALUES ($1, $2)`, productID, quantity); err != nil {
		t.Fatalf("insert stock level: %v", err)
	}
	return productID
}

// createOrder inserts one order over the given lines and returns it.
func (f *lifecycleDBFixture) createOrder(t *testing.T, owner uuid.UUID, expiresAt time.Time, lines ...model.OrderLine) *model.Order {
	t.Helper()
	order := model.NewOrder(owner, model.Address{RecipientName: "Nguyễn Văn A"}, lines, expiresAt, time.Now().UTC())
	if err := f.svc.Orders.Create(context.Background(), order); err != nil {
		t.Fatalf("create order: %v", err)
	}
	return order
}

// reserve holds the ordered quantity through the real reservation use cases.
func (f *lifecycleDBFixture) reserve(t *testing.T, order *model.Order, productID uuid.UUID, quantity int64) {
	t.Helper()
	if err := f.svc.Reservations.Reserve(context.Background(), order.ID, productID, quantity); err != nil {
		t.Fatalf("reserve: %v", err)
	}
}

func (f *lifecycleDBFixture) physicalStock(t *testing.T, productID uuid.UUID) int64 {
	t.Helper()
	var quantity int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT quantity FROM stock_levels WHERE product_id = $1`, productID).Scan(&quantity); err != nil {
		t.Fatalf("read stock level: %v", err)
	}
	return quantity
}

func (f *lifecycleDBFixture) heldQuantity(t *testing.T, productID uuid.UUID) int64 {
	t.Helper()
	var held int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(quantity), 0) FROM stock_holds WHERE product_id = $1 AND status = 'ACTIVE'`,
		productID).Scan(&held); err != nil {
		t.Fatalf("read held quantity: %v", err)
	}
	return held
}

// releasedHolds counts how many of an order's holds were released, so a
// double-free is visible as a number greater than one.
func (f *lifecycleDBFixture) releasedHolds(t *testing.T, orderID uuid.UUID) int64 {
	t.Helper()
	var count int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM stock_holds WHERE order_id = $1 AND status = 'RELEASED'`,
		orderID).Scan(&count); err != nil {
		t.Fatalf("count released holds: %v", err)
	}
	return count
}

func (f *lifecycleDBFixture) statusOf(t *testing.T, orderID uuid.UUID) constant.Status {
	t.Helper()
	order, err := f.svc.Orders.FindByID(context.Background(), orderID)
	if err != nil {
		t.Fatalf("find order: %v", err)
	}
	return order.Status
}

// FR-009, research D9: the storage state check refuses a value outside the five,
// so the domain's transition table cannot be bypassed by writing one.
func TestStorageRefusesAnUnlistedOrderState(t *testing.T) {
	f := newLifecycleDBFixture(t)
	product := f.insertProduct(t, 1000, 5)
	order := f.createOrder(t, uuid.New(), time.Now().Add(time.Hour), model.OrderLine{
		ProductID: product, Name: "Tranh", Slug: "tranh",
		UnitPrice: model.Price{Amount: 1000, Currency: "VND"}, Quantity: 1,
	})

	if _, err := f.pool.Exec(context.Background(),
		`UPDATE orders SET status = 'BOGUS' WHERE id = $1`, order.ID); err == nil {
		t.Fatal("the storage must refuse a status outside the five")
	}
}

// FR-014, SC-004, quickstart scenario 5a: confirming payment turns the hold into a
// sale, and confirming it again sells the held quantity exactly once.
func TestConfirmingPaymentSellsTheHeldQuantityOnce(t *testing.T) {
	f := newLifecycleDBFixture(t)
	product := f.insertProduct(t, 120000, 10)
	order := f.createOrder(t, uuid.New(), time.Now().Add(time.Hour), model.OrderLine{
		ProductID: product, Name: "Tranh sơn dầu", Slug: "tranh-son-dau",
		UnitPrice: model.Price{Amount: 120000, Currency: "VND"}, Quantity: 2,
	})
	f.reserve(t, order, product, 2)

	if got := f.heldQuantity(t, product); got != 2 {
		t.Fatalf("held = %d, want 2 before payment", got)
	}

	if err := f.svc.MarkPaid(context.Background(), order.ID, "payment-event-1"); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if got := f.statusOf(t, order.ID); got != constant.StatusPaid {
		t.Fatalf("status = %s, want PAID", got)
	}
	if got := f.physicalStock(t, product); got != 8 {
		t.Fatalf("physical = %d, want 8 after selling 2 of 10", got)
	}
	if got := f.heldQuantity(t, product); got != 0 {
		t.Fatalf("held = %d, want 0 after the sale", got)
	}

	// Replaying the same payment event is a no-op success: the held quantity is
	// sold once, not twice (FR-014, idempotent-success).
	if err := f.svc.MarkPaid(context.Background(), order.ID, "payment-event-1"); err != nil {
		t.Fatalf("replaying the payment must be a no-op success, got %v", err)
	}
	if got := f.physicalStock(t, product); got != 8 {
		t.Fatalf("physical = %d after the replay, want 8 — the sale applies once", got)
	}
	if got := f.heldQuantity(t, product); got != 0 {
		t.Fatalf("held = %d after the replay, want 0", got)
	}
}

// FR-012, SC-004, quickstart scenario 7f: an order left unpaid past its window
// cancels itself and its goods free exactly once across the order's sweep and
// module 05's; running both sweeps again frees nothing more.
func TestExpiredOrderFreesItsGoodsExactlyOnceAcrossBothSweeps(t *testing.T) {
	f := newLifecycleDBFixture(t)
	product := f.insertProduct(t, 120000, 10)
	order := f.createOrder(t, uuid.New(), time.Now().Add(time.Hour), model.OrderLine{
		ProductID: product, Name: "Tranh sơn dầu", Slug: "tranh-son-dau",
		UnitPrice: model.Price{Amount: 120000, Currency: "VND"}, Quantity: 2,
	})
	f.reserve(t, order, product, 2)

	// Drive the order and its hold past the window, so the order's sweep and
	// module 05's both see the same expired deadline.
	past := time.Now().Add(-time.Minute)
	if _, err := f.pool.Exec(context.Background(), `UPDATE orders SET expires_at = $2 WHERE id = $1`, order.ID, past); err != nil {
		t.Fatalf("backdate the order's expiry: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE stock_holds SET expires_at = $2 WHERE order_id = $1`, order.ID, past); err != nil {
		t.Fatalf("backdate the hold's expiry: %v", err)
	}

	if err := f.svc.ExpireOrders(context.Background()); err != nil {
		t.Fatalf("order sweep: %v", err)
	}
	if got := f.statusOf(t, order.ID); got != constant.StatusCancelled {
		t.Fatalf("status = %s, want CANCELLED after expiry", got)
	}
	if err := f.inventory.ExpireHolds(context.Background()); err != nil {
		t.Fatalf("inventory sweep: %v", err)
	}

	if got := f.releasedHolds(t, order.ID); got != 1 {
		t.Fatalf("released holds = %d, want exactly 1 across both sweeps", got)
	}
	if got := f.heldQuantity(t, product); got != 0 {
		t.Fatalf("held = %d, want 0 after expiry", got)
	}
	if got := f.physicalStock(t, product); got != 10 {
		t.Fatalf("physical = %d, want 10 — expiry never moves the shelf", got)
	}

	// Both sweeps again: nothing is freed a second time.
	if err := f.svc.ExpireOrders(context.Background()); err != nil {
		t.Fatalf("second order sweep: %v", err)
	}
	if err := f.inventory.ExpireHolds(context.Background()); err != nil {
		t.Fatalf("second inventory sweep: %v", err)
	}
	if got := f.releasedHolds(t, order.ID); got != 1 {
		t.Fatalf("released holds = %d after a replay, want exactly 1", got)
	}
	if got := f.physicalStock(t, product); got != 10 {
		t.Fatalf("physical = %d after a replay, want 10", got)
	}
}
