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

// insertOrderWithLines writes one order with several lines through the real
// repository, so the recent-order read can be exercised against a real database
// (T053). Lines must carry their own Position and quantity; the helper totals
// them and stamps the order's total.
func (f *orderStatsFixture) insertOrderWithLines(t *testing.T, userID uuid.UUID, createdAt time.Time, lines []model.OrderLine) uuid.UUID {
	t.Helper()
	id := uuid.New()
	total := int64(0)
	currency := "VND"
	for i := range lines {
		lines[i].ID = uuid.New()
		lines[i].OrderID = id
		if lines[i].CreatedAt.IsZero() {
			lines[i].CreatedAt = createdAt
		}
		if lines[i].UnitPrice.Currency != "" {
			currency = lines[i].UnitPrice.Currency
		}
		total += lines[i].UnitPrice.Amount * lines[i].Quantity
	}
	order := &model.Order{
		ID:        id,
		UserID:    userID,
		Status:    constant.StatusPaid,
		Total:     model.Price{Amount: total, Currency: currency},
		Address:   statsAddress(),
		Version:   1,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
		Lines:     lines,
	}
	if err := f.repo.Create(context.Background(), order); err != nil {
		t.Fatalf("create order: %v", err)
	}
	return id
}

// T053: the recent-order read returns at most five orders, newest first by
// created_at then id, and carries the first line by position plus the line count
// and total quantity. A customer id whose account no longer exists is retained
// as a loose reference (FR-025, FR-026, FR-027, research D10).
func TestRecentOrdersReturnsFiveNewestWithLineFacts(t *testing.T) {
	f := newOrderStatsFixture(t)
	ctx := context.Background()

	base := time.Date(2026, time.February, 10, 12, 0, 0, 0, f.loc)
	// Seven orders with strictly increasing creation times.
	ids := make([]uuid.UUID, 7)
	for i := 0; i < 7; i++ {
		ids[i] = f.insertOrder(t, constant.StatusPaid, "VND", base.Add(time.Duration(i)*time.Hour), 1000, 1)
	}

	got, err := New(f.repo).RecentOrders(ctx, 5)
	if err != nil {
		t.Fatalf("RecentOrders: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 recent orders, got %d", len(got))
	}
	want := map[uuid.UUID]bool{}
	for i := 2; i < 7; i++ {
		want[ids[i]] = true
	}
	for _, r := range got {
		if !want[r.ID] {
			t.Fatalf("unexpected order %s in the recent set", r.ID)
		}
	}
	// Newest first.
	for i := 1; i < len(got); i++ {
		if got[i-1].CreatedAt.Before(got[i].CreatedAt) {
			t.Fatalf("recent orders must be newest first: %+v", got)
		}
	}

	// Multi-line order: first line by position, line count and total quantity.
	deletedCustomer := uuid.New()
	multiCreated := base.Add(10 * time.Hour)
	lines := []model.OrderLine{
		{ProductName: "Second", ProductCode: "c2", VariantName: "V2", SKU: "s2", UnitPrice: model.Price{Amount: 200, Currency: "VND"}, Quantity: 2, Position: 1},
		{ProductName: "First", ProductCode: "c1", VariantName: "V1", SKU: "s1", UnitPrice: model.Price{Amount: 100, Currency: "VND"}, Quantity: 3, Position: 0},
		{ProductName: "Third", ProductCode: "c3", VariantName: "", SKU: "s3", UnitPrice: model.Price{Amount: 50, Currency: "VND"}, Quantity: 1, Position: 2},
	}
	multiID := f.insertOrderWithLines(t, deletedCustomer, multiCreated, lines)

	got2, err := New(f.repo).RecentOrders(ctx, 5)
	if err != nil {
		t.Fatalf("RecentOrders: %v", err)
	}
	var found *contracts.RecentOrder
	for i := range got2 {
		if got2[i].ID == multiID {
			found = &got2[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("the multi-line order must be among the recent five")
	}
	if found.FirstLineName != "First" {
		t.Fatalf("first line by position: got %q, want First", found.FirstLineName)
	}
	if found.FirstLineVariantName != "V1" {
		t.Fatalf("first line variant: got %q, want V1", found.FirstLineVariantName)
	}
	if found.FirstLineQuantity != 3 {
		t.Fatalf("first line quantity: got %d, want 3", found.FirstLineQuantity)
	}
	if found.LineCount != 3 {
		t.Fatalf("line count: got %d, want 3", found.LineCount)
	}
	if found.TotalQuantity != 6 {
		t.Fatalf("total quantity: got %d, want 6", found.TotalQuantity)
	}
	// Deleted-account customer id is retained as a loose reference.
	if found.CustomerID != deletedCustomer {
		t.Fatalf("customer id must be retained, got %s want %s", found.CustomerID, deletedCustomer)
	}
}

// snapshot on the single line, so the category read can be exercised against a
// real database (T047).
func (f *orderStatsFixture) insertOrderWithCategory(t *testing.T, status constant.Status, currency string, createdAt time.Time, unitPrice, quantity int64, category *model.CategorySnapshot) uuid.UUID {
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
		Category:    category,
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

// T047: the category snapshot persists on the line and is returned by the read,
// so a later change to the product's category cannot move historical revenue.
func TestCategoryRevenueKeepsTheSnapshottedCategory(t *testing.T) {
	f := newOrderStatsFixture(t)
	ctx := context.Background()

	catA := uuid.New()
	catB := uuid.New()
	catNameA := "Category A"
	catNameB := "Category B"

	// Seed the categories so the backfill-style scenario is realistic; the
	// snapshot itself is what the read uses, not this table.
	if _, err := f.pool.Exec(ctx, `INSERT INTO categories (id, name, normalized_name, slug, normalized_slug, description, position, is_visible) VALUES ($1, $2, $3, $4, $5, '', 0, true)`, catA, catNameA, "category-a", "category-a", "category-a"); err != nil {
		t.Fatalf("seed category A: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO categories (id, name, normalized_name, slug, normalized_slug, description, position, is_visible) VALUES ($1, $2, $3, $4, $5, '', 0, true)`, catB, catNameB, "category-b", "category-b", "category-b"); err != nil {
		t.Fatalf("seed category B: %v", err)
	}

	window := contracts.TimeWindow{
		From: time.Date(2026, time.February, 1, 0, 0, 0, 0, f.loc),
		To:   time.Date(2026, time.March, 1, 0, 0, 0, 0, f.loc),
	}
	at := time.Date(2026, time.February, 10, 12, 0, 0, 0, f.loc)

	// Line sold under category A.
	f.insertOrderWithCategory(t, constant.StatusPaid, "VND", at, 1000, 1, &model.CategorySnapshot{ID: catA, Name: catNameA})
	// Line sold under category B.
	f.insertOrderWithCategory(t, constant.StatusPaid, "VND", at, 2000, 1, &model.CategorySnapshot{ID: catB, Name: catNameB})
	// Legacy line with no snapshot.
	f.insertOrderWithCategory(t, constant.StatusPaid, "VND", at, 500, 1, nil)

	got, err := New(f.repo).CategoryRevenue(ctx, window)
	if err != nil {
		t.Fatalf("CategoryRevenue: %v", err)
	}

	byID := make(map[string]int64)
	var nullAmount int64
	var nullFound bool
	for _, c := range got {
		if c.CategoryID == nil {
			nullAmount = c.Amount.Amount
			nullFound = true
			if c.CategoryName != "" {
				t.Fatalf("null snapshot must carry an empty name from the read, got %q", c.CategoryName)
			}
		} else {
			byID[c.CategoryID.String()] = c.Amount.Amount
		}
	}
	if byID[catA.String()] != 1000 {
		t.Fatalf("category A revenue = %d, want 1000", byID[catA.String()])
	}
	if byID[catB.String()] != 2000 {
		t.Fatalf("category B revenue = %d, want 2000", byID[catB.String()])
	}
	if !nullFound || nullAmount != 500 {
		t.Fatalf("null snapshot must be returned as a null id with its revenue, got found=%v amount=%d", nullFound, nullAmount)
	}

	// A later category change (rename) must not move the snapshotted revenue:
	// rename category A in the categories table and re-read.
	if _, err := f.pool.Exec(ctx, `UPDATE categories SET name = 'Renamed A' WHERE id = $1`, catA); err != nil {
		t.Fatalf("rename category: %v", err)
	}
	got2, err := New(f.repo).CategoryRevenue(ctx, window)
	if err != nil {
		t.Fatalf("CategoryRevenue after rename: %v", err)
	}
	for _, c := range got2 {
		if c.CategoryID != nil && c.CategoryID.String() == catA.String() {
			if c.CategoryName != catNameA {
				t.Fatalf("snapshotted name must not follow the rename, got %q want %q", c.CategoryName, catNameA)
			}
		}
	}
}
