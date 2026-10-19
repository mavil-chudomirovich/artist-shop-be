//go:build integration

package implement

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	inventoryimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/implement"
	inventorymapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
	inventoryavailability "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/availability"
	inventorypostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/postgres"
	inventoryreservation "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/infrastructure/implement/reservation"
	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	ordermapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
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

// lifecycleVariantExists answers the inventory module's ProductLookup over the
// product_variants table, so Reserve can tell an unknown variant from an unstocked
// one. It is a test-local adapter: the boundary the constitution protects is
// between modules, not between a test and a fixture.
type lifecycleVariantExists struct{ pool *pgxpool.Pool }

func (p lifecycleVariantExists) VariantExists(ctx context.Context, variantID uuid.UUID) (bool, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM product_variants WHERE id = $1)`, variantID).Scan(&exists); err != nil {
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
		Lookup:    lifecycleVariantExists{pool: pool},
		Tx:        &database.DB{Pool: pool},
		Clock:     lifecycleClock{},
		Mapper:    inventorymapper.New(),
	})
	svc := New(Service{
		Orders:       orderpostgres.NewOrderRepository(pool),
		Availability: inventoryavailability.New(inventoryRepo, lifecycleClock{}),
		Reservations: inventoryreservation.New(inventoryService),
		Tx:           &database.DB{Pool: pool},
		Clock:        lifecycleClock{},
		Mapper:       ordermapper.New(),
	})
	return &lifecycleDBFixture{pool: pool, svc: svc, inventory: inventoryService}
}

// insertProduct writes one category, one on-sale product and its on-sale default
// variant straight into the database and puts a physical quantity on the variant's
// shelf. It returns the variant identifier, which is the unit reserve, sale and
// release act on. The test may do this: the boundary the constitution protects is
// between modules, not between a test and a fixture.
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
	code := "product-" + productID.String()[:8]
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO products
			(id, name, code, description, price_amount, currency,
			 category_id, position)
		VALUES ($1, $2, $3, '', $4, 'VND', $5, 0)`,
		productID, "Product "+code, code, amount, categoryID); err != nil {
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
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO stock_levels (variant_id, quantity) VALUES ($1, $2)`, variantID, quantity); err != nil {
		t.Fatalf("insert stock level: %v", err)
	}
	return variantID
}

// createOrder inserts one order over the given lines and returns it. The order is
// placed in PAYMENT_PENDING, the state the paid and expiry paths act on.
func (f *lifecycleDBFixture) createOrder(t *testing.T, owner uuid.UUID, _ time.Time, lines ...model.OrderLine) *model.Order {
	t.Helper()
	order := model.NewOrder(owner, model.Address{RecipientName: "Nguyễn Văn A", FacebookURL: "https://facebook.com/test.customer"}, lines, time.Now().UTC())
	order.Status = constant.StatusPaymentPending
	if err := f.svc.Orders.Create(context.Background(), order); err != nil {
		t.Fatalf("create order: %v", err)
	}
	return order
}

// reserve holds the ordered quantity through the real reservation use cases.
func (f *lifecycleDBFixture) reserve(t *testing.T, order *model.Order, variantID uuid.UUID, quantity int64) {
	t.Helper()
	if err := f.svc.Reservations.Reserve(context.Background(), order.ID, variantID, quantity); err != nil {
		t.Fatalf("reserve: %v", err)
	}
}

func (f *lifecycleDBFixture) physicalStock(t *testing.T, variantID uuid.UUID) int64 {
	t.Helper()
	var quantity int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT quantity FROM stock_levels WHERE variant_id = $1`, variantID).Scan(&quantity); err != nil {
		t.Fatalf("read stock level: %v", err)
	}
	return quantity
}

func (f *lifecycleDBFixture) heldQuantity(t *testing.T, variantID uuid.UUID) int64 {
	t.Helper()
	var held int64
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(quantity), 0) FROM stock_holds WHERE variant_id = $1 AND status = 'ACTIVE'`,
		variantID).Scan(&held); err != nil {
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

// FR-009, research D9: the storage state check refuses a value outside the six,
// so the domain's transition table cannot be bypassed by writing one.
func TestStorageRefusesAnUnlistedOrderState(t *testing.T) {
	f := newLifecycleDBFixture(t)
	product := f.insertProduct(t, 1000, 5)
	order := f.createOrder(t, uuid.New(), time.Now().Add(time.Hour), model.OrderLine{
		ProductID: product, VariantID: product, ProductName: "Tranh", ProductCode: product.String(),
		VariantName: "Tranh", SKU: product.String(),
		UnitPrice: model.Price{Amount: 1000, Currency: "VND"}, Quantity: 1,
	})

	if _, err := f.pool.Exec(context.Background(),
		`UPDATE orders SET status = 'BOGUS' WHERE id = $1`, order.ID); err == nil {
		t.Fatal("the storage must refuse a status outside the six")
	}
}

// FR-014, SC-004, quickstart scenario 5a: confirming payment turns the hold into a
// sale, and confirming it again sells the held quantity exactly once.
func TestConfirmingPaymentSellsTheHeldQuantityOnce(t *testing.T) {
	f := newLifecycleDBFixture(t)
	product := f.insertProduct(t, 120000, 10)
	order := f.createOrder(t, uuid.New(), time.Now().Add(time.Hour), model.OrderLine{
		ProductID: product, VariantID: product, ProductName: "Tranh sơn dầu", ProductCode: product.String(),
		VariantName: "Tranh sơn dầu", SKU: product.String(),
		UnitPrice: model.Price{Amount: 120000, Currency: "VND"}, Quantity: 2,
	})
	f.reserve(t, order, product, 2)

	if got := f.heldQuantity(t, product); got != 2 {
		t.Fatalf("held = %d, want 2 before payment", got)
	}

	if err := f.svc.MarkPaid(context.Background(), order.ID, "payment-event-1", order.Version); err != nil {
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
	if err := f.svc.MarkPaid(context.Background(), order.ID, "payment-event-1", order.Version); err != nil {
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
		ProductID: product, VariantID: product, ProductName: "Tranh sơn dầu", ProductCode: product.String(),
		VariantName: "Tranh sơn dầu", SKU: product.String(),
		UnitPrice: model.Price{Amount: 120000, Currency: "VND"}, Quantity: 2,
	})
	f.reserve(t, order, product, 2)

	// Drive the order and its hold past the window, so the order's sweep and
	// module 05's both see the same expired deadline.
	past := time.Now().Add(-time.Minute)
	if _, err := f.pool.Exec(context.Background(), `UPDATE orders SET payment_expires_at = $2 WHERE id = $1`, order.ID, past); err != nil {
		t.Fatalf("backdate the order's payment deadline: %v", err)
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

// FR-008, FR-009, SC-004, quickstart scenario 7b: an order still awaiting the
// artist has no payment deadline, so the order's expiry sweep never selects it —
// however long it waits — and module 05's hold sweep has nothing of its to free;
// the order stays PENDING and holds nothing.
func TestAnOrderAwaitingTheArtistNeverExpires(t *testing.T) {
	f := newLifecycleDBFixture(t)
	product := f.insertProduct(t, 120000, 5)
	order := model.NewOrder(uuid.New(), model.Address{RecipientName: "Nguyễn Văn A", FacebookURL: "https://facebook.com/test.customer"}, []model.OrderLine{{
		ProductID: product, VariantID: product, ProductName: "Tranh sơn dầu", ProductCode: product.String(),
		VariantName: "Tranh sơn dầu", SKU: product.String(),
		UnitPrice: model.Price{Amount: 120000, Currency: "VND"}, Quantity: 2,
	}}, time.Now().UTC())
	if order.Status != constant.StatusPending || order.PaymentExpiresAt != nil {
		t.Fatalf("a fresh order must await the artist with no deadline, got %s / %v", order.Status, order.PaymentExpiresAt)
	}
	if err := f.svc.Orders.Create(context.Background(), order); err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Both sweeps run; neither has anything of this order to act on.
	if err := f.svc.ExpireOrders(context.Background()); err != nil {
		t.Fatalf("order sweep: %v", err)
	}
	if err := f.inventory.ExpireHolds(context.Background()); err != nil {
		t.Fatalf("inventory sweep: %v", err)
	}

	if got := f.statusOf(t, order.ID); got != constant.StatusPending {
		t.Fatalf("an order awaiting the artist must stay PENDING, got %s", got)
	}
	if got := f.heldQuantity(t, product); got != 0 {
		t.Fatalf("an order awaiting the artist holds nothing, got %d held", got)
	}
	if got := f.releasedHolds(t, order.ID); got != 0 {
		t.Fatalf("an order awaiting the artist has no holds to release, got %d", got)
	}
	if got := f.physicalStock(t, product); got != 5 {
		t.Fatalf("physical = %d, want 5 unchanged", got)
	}
}

// FR-005, FR-024, SC-002, SC-005, quickstart scenario 7c: two PENDING orders
// competing for the last unit. Confirmation holds every line in the order's own
// transaction and module 05's Reserve serialises the two on the level row lock, so
// exactly one commits and the other is refused naming the shortage; the available
// quantity stays at zero and never goes negative.
func TestTwoConfirmationsForTheLastUnitConfirmAtMostOne(t *testing.T) {
	f := newLifecycleDBFixture(t)
	product := f.insertProduct(t, 120000, 1)
	orderLine := model.OrderLine{
		ProductID: product, VariantID: product, ProductName: "Tranh sơn dầu", ProductCode: product.String(),
		VariantName: "Tranh sơn dầu", SKU: product.String(),
		UnitPrice: model.Price{Amount: 120000, Currency: "VND"}, Quantity: 1,
	}
	first := model.NewOrder(uuid.New(), model.Address{RecipientName: "Nguyễn Văn A", FacebookURL: "https://facebook.com/test.customer"},
		[]model.OrderLine{orderLine}, time.Now().UTC())
	second := model.NewOrder(uuid.New(), model.Address{RecipientName: "Trần Thị B", FacebookURL: "https://facebook.com/test.customer"},
		[]model.OrderLine{orderLine}, time.Now().UTC())
	orders := []*model.Order{first, second}
	for _, order := range orders {
		if err := f.svc.Orders.Create(context.Background(), order); err != nil {
			t.Fatalf("create order: %v", err)
		}
	}

	// Confirm both at once, so they genuinely compete for the last unit.
	results := make([]error, len(orders))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, order := range orders {
		wg.Add(1)
		go func(i int, orderID uuid.UUID) {
			defer wg.Done()
			<-start
			_, results[i] = f.svc.ConfirmByAdmin(actorContext(uuid.New()), appdto.OrderRefInput{OrderID: orderID})
		}(i, order.ID)
	}
	close(start)
	wg.Wait()

	confirmed, short := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			confirmed++
		case errors.Is(err, domainerr.ErrQuantityExceedsAvailable):
			short++
		default:
			t.Fatalf("unexpected confirmation outcome: %v", err)
		}
	}
	if confirmed != 1 || short != 1 {
		t.Fatalf("exactly one confirmation must commit, got %d confirmed and %d refused", confirmed, short)
	}

	// The winner is PAYMENT_PENDING; the loser stayed PENDING, holding nothing.
	paymentPending, pending := 0, 0
	for _, order := range orders {
		switch f.statusOf(t, order.ID) {
		case constant.StatusPaymentPending:
			paymentPending++
		case constant.StatusPending:
			pending++
		default:
			t.Fatalf("unexpected order status %s", f.statusOf(t, order.ID))
		}
	}
	if paymentPending != 1 || pending != 1 {
		t.Fatalf("expected one PAYMENT_PENDING and one PENDING, got %d and %d", paymentPending, pending)
	}

	// Availability never went negative: one unit is physical, exactly one is held,
	// so zero remains available (SC-002, SC-005).
	if got := f.physicalStock(t, product); got != 1 {
		t.Fatalf("physical stock = %d, want 1 — confirmation never moves the shelf", got)
	}
	if got := f.heldQuantity(t, product); got != 1 {
		t.Fatalf("held = %d, want exactly 1 — the losing confirmation held nothing", got)
	}
	available, err := f.svc.Availability.AvailableQuantity(context.Background(), []uuid.UUID{product})
	if err != nil {
		t.Fatalf("read availability: %v", err)
	}
	if len(available) != 1 || available[0].Available != 0 {
		t.Fatalf("available = %+v, want one entry at 0 — never negative", available)
	}
}

// SC-004, FR-015, FR-016, research D9: cancelling an awaiting-payment order
// releases its held goods and tells the payment module through the port, against
// real PostgreSQL. The payment side is best-effort and reports nothing, so a
// payment module that does nothing never blocks the order's cancellation.
func TestCancelReleasesGoodsAndNotifiesThePaymentModule(t *testing.T) {
	f := newLifecycleDBFixture(t)
	product := f.insertProduct(t, 120000, 10)
	order := f.createOrder(t, uuid.New(), time.Now().Add(time.Hour), model.OrderLine{
		ProductID: product, VariantID: product, ProductName: "Tranh sơn dầu", ProductCode: product.String(),
		VariantName: "Tranh sơn dầu", SKU: product.String(),
		UnitPrice: model.Price{Amount: 120000, Currency: "VND"}, Quantity: 2,
	})
	f.reserve(t, order, product, 2)

	port := &fakeCancellation{}
	f.svc.Cancellations = port

	if err := f.svc.Cancel(context.Background(), order.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got := f.statusOf(t, order.ID); got != constant.StatusCancelled {
		t.Fatalf("status = %s, want CANCELLED", got)
	}
	if got := f.heldQuantity(t, product); got != 0 {
		t.Fatalf("held = %d, want 0 — cancelling releases the goods", got)
	}
	if len(port.ids) != 1 || port.ids[0] != order.ID {
		t.Fatalf("the payment module was notified with %v, want exactly [%s]", port.ids, order.ID)
	}
}
