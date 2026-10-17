//go:build integration

package statistics

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	orderpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file exercises the order statistics adapter against a real PostgreSQL
// container. It proves what the fake-repository unit test cannot: that the
// revenue/order KPI query counts exactly the earned states and VND, that the two
// windows split orders by their created instant, and that daily revenue buckets
// by the shop timezone rather than UTC — so an order placed at local midnight
// lands on the operator's calendar day (FR-010, FR-012 to FR-014, FR-019,
// research D3, D4, D7). Docker is required; the file still compiles without a
// container so `go vet -tags integration ./...` covers it.

// orderStatsFixture is one migrated PostgreSQL container plus the adapter under
// test over the real order repository.
type orderStatsFixture struct {
	pool *pgxpool.Pool
	repo *orderpostgres.OrderRepository
	loc  *time.Location
}

func newOrderStatsFixture(t *testing.T) *orderStatsFixture {
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

	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatalf("load the shop timezone: %v", err)
	}
	return &orderStatsFixture{pool: pool, repo: orderpostgres.NewOrderRepository(pool), loc: loc}
}

// statsAddress is the delivery address the fixture's orders carry. The insert
// path requires every captured address column, so the fixture supplies one.
func statsAddress() model.Address {
	return model.Address{
		RecipientName:  "Nguyễn Văn A",
		RecipientPhone: "0912345678",
		ProvinceCode:   "01",
		ProvinceName:   "Hà Nội",
		WardCode:       "00001",
		WardName:       "Phúc Xá",
		StreetAddress:  "1 Đinh Tiên Hoàng",
	}
}

// insertOrder writes one order with a single line through the real repository, so
// the KPI query sees the same rows a checkout would persist. unitPrice and
// quantity are positive, as orders_total_ck and order_items_unit_price_ck demand.
func (f *orderStatsFixture) insertOrder(t *testing.T, status constant.Status, currency string, createdAt time.Time, unitPrice, quantity int64) uuid.UUID {
	t.Helper()
	id := uuid.New()
	line := model.OrderLine{
		ID:          uuid.New(),
		OrderID:     id,
		ProductID:   uuid.New(),
		VariantID:   uuid.New(),
		ProductName: "Product " + id.String()[:8],
		ProductCode: "code-" + id.String()[:8],
		VariantName: "Variant",
		SKU:         "sku-" + id.String()[:8],
		UnitPrice:   model.Price{Amount: unitPrice, Currency: currency},
		Quantity:    quantity,
		Position:    0,
		CreatedAt:   createdAt,
	}
	order := &model.Order{
		ID:        id,
		UserID:    uuid.New(),
		Status:    status,
		Total:     model.Price{Amount: unitPrice * quantity, Currency: currency},
		Address:   statsAddress(),
		Version:   1,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
		Lines:     []model.OrderLine{line},
	}
	if err := f.repo.Create(context.Background(), order); err != nil {
		t.Fatalf("create order: %v", err)
	}
	return id
}

// FR-010, FR-012 to FR-014: current/last revenue count only PAID, SHIPPED and
// COMPLETED in VND within their window; the order counts include every state and
// the all-time counts ignore the windows.
func TestKPIsCountOnlyEarnedRevenueInVNDAcrossTheWindows(t *testing.T) {
	f := newOrderStatsFixture(t)
	ctx := context.Background()

	current := contracts.TimeWindow{
		From: time.Date(2026, time.February, 1, 0, 0, 0, 0, f.loc),
		To:   time.Date(2026, time.March, 1, 0, 0, 0, 0, f.loc),
	}
	last := contracts.TimeWindow{
		From: time.Date(2026, time.January, 1, 0, 0, 0, 0, f.loc),
		To:   time.Date(2026, time.February, 1, 0, 0, 0, 0, f.loc),
	}
	inCurrent := time.Date(2026, time.February, 10, 12, 0, 0, 0, f.loc)
	inLast := time.Date(2026, time.January, 15, 12, 0, 0, 0, f.loc)
	older := time.Date(2025, time.December, 20, 12, 0, 0, 0, f.loc)

	f.insertOrder(t, constant.StatusPaid, "VND", inCurrent, 1000, 1)
	f.insertOrder(t, constant.StatusShipped, "VND", inCurrent, 2000, 1)
	f.insertOrder(t, constant.StatusCompleted, "VND", inCurrent, 3000, 1)
	f.insertOrder(t, constant.StatusPending, "VND", inCurrent, 4000, 1)
	f.insertOrder(t, constant.StatusPaymentPending, "VND", inCurrent, 5000, 1)
	f.insertOrder(t, constant.StatusCancelled, "VND", inCurrent, 6000, 1)
	// A PAID order in another currency must not join the VND revenue, but it is
	// still an order.
	f.insertOrder(t, constant.StatusPaid, "USD", inCurrent, 7000, 1)
	f.insertOrder(t, constant.StatusPaid, "VND", inLast, 800, 1)
	f.insertOrder(t, constant.StatusPending, "VND", inLast, 900, 1)
	f.insertOrder(t, constant.StatusPaid, "VND", older, 100, 1)

	got, err := New(f.repo).KPIs(ctx, current, last)
	if err != nil {
		t.Fatalf("KPIs: %v", err)
	}

	if got.CurrentRevenue.Amount != 6000 || got.CurrentRevenue.Currency != "VND" {
		t.Fatalf("current revenue = %+v, want 6000 VND (PAID+SHIPPED+COMPLETED only)", got.CurrentRevenue)
	}
	if got.LastRevenue.Amount != 800 || got.LastRevenue.Currency != "VND" {
		t.Fatalf("last revenue = %+v, want 800 VND", got.LastRevenue)
	}
	if got.CurrentOrders != 7 {
		t.Fatalf("current orders = %d, want every state in the window (7)", got.CurrentOrders)
	}
	if got.LastOrders != 2 {
		t.Fatalf("last orders = %d, want every state in the window (2)", got.LastOrders)
	}
	if got.TotalOrders != 10 {
		t.Fatalf("total orders = %d, want the all-time count (10)", got.TotalOrders)
	}
	if got.PendingConfirmation != 2 {
		t.Fatalf("pending confirmation = %d, want the all-time PENDING count (2)", got.PendingConfirmation)
	}
}

// FR-019, research D4, US2 acceptance 4: daily revenue buckets by the shop
// timezone, so an instant just after local midnight belongs to the next local day
// even though its UTC date is still the previous one.
func TestDailyRevenueBucketsByTheShopTimezone(t *testing.T) {
	f := newOrderStatsFixture(t)
	ctx := context.Background()

	window := contracts.TimeWindow{
		From: time.Date(2026, time.January, 30, 0, 0, 0, 0, f.loc),
		To:   time.Date(2026, time.February, 3, 0, 0, 0, 0, f.loc),
	}
	// 2026-01-31 17:00 UTC is exactly 2026-02-01 00:00 in Asia/Ho_Chi_Minh.
	atLocalMidnight := time.Date(2026, time.January, 31, 17, 0, 0, 0, time.UTC)
	// One second earlier is still local 2026-01-31 23:59:59.
	beforeMidnight := atLocalMidnight.Add(-time.Second)

	f.insertOrder(t, constant.StatusPaid, "VND", atLocalMidnight, 100, 1)
	f.insertOrder(t, constant.StatusPaid, "VND", beforeMidnight, 200, 1)

	days, err := New(f.repo).DailyRevenue(ctx, window, f.loc.String())
	if err != nil {
		t.Fatalf("DailyRevenue: %v", err)
	}

	byDay := make(map[string]int64, len(days))
	for _, day := range days {
		if day.Amount.Currency != "VND" {
			t.Fatalf("daily revenue must be VND, got %+v", day.Amount)
		}
		byDay[day.Date] += day.Amount.Amount
	}
	if byDay["2026-02-01"] != 100 {
		t.Fatalf("an order at local midnight must bucket to 2026-02-01, got %v", byDay)
	}
	if byDay["2026-01-31"] != 200 {
		t.Fatalf("an order one second before local midnight stays on 2026-01-31, got %v", byDay)
	}
	if len(byDay) != 2 {
		t.Fatalf("expected exactly two local days, got %v", byDay)
	}
}
