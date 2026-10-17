package implement

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// This file exercises the US1 contract of the admin orchestration: it assembles
// the whole response from a fixed set of read ports, reads the clock exactly
// once, and records exactly one audit event that carries no PII. The exact
// window/growth/zero-fill/category/customer/recent rules are owned by US2–US5 and
// are asserted in their own tests.

// --- read ports ---------------------------------------------------------

type fakeOrderStatistics struct {
	calls      []string
	kpis       contracts.OrderKPIs
	trend      []contracts.DailyRevenue
	categories []contracts.CategoryRevenue
	recents    []contracts.RecentOrder

	// The windows and zone the adapter was called with, so a test can prove the
	// application built the fair shop-timezone windows (US2).
	kpiCurrent     contracts.TimeWindow
	kpiLast        contracts.TimeWindow
	trendWindow    contracts.TimeWindow
	trendTimeZone  string
	categoryWindow contracts.TimeWindow
	recentLimit    int
}

func (f *fakeOrderStatistics) KPIs(_ context.Context, current, last contracts.TimeWindow) (contracts.OrderKPIs, error) {
	f.calls = append(f.calls, "KPIs")
	f.kpiCurrent, f.kpiLast = current, last
	return f.kpis, nil
}

func (f *fakeOrderStatistics) DailyRevenue(_ context.Context, window contracts.TimeWindow, timeZone string) ([]contracts.DailyRevenue, error) {
	f.calls = append(f.calls, "DailyRevenue")
	f.trendWindow, f.trendTimeZone = window, timeZone
	return f.trend, nil
}

func (f *fakeOrderStatistics) CategoryRevenue(_ context.Context, window contracts.TimeWindow) ([]contracts.CategoryRevenue, error) {
	f.calls = append(f.calls, "CategoryRevenue")
	f.categoryWindow = window
	return f.categories, nil
}

func (f *fakeOrderStatistics) RecentOrders(_ context.Context, limit int) ([]contracts.RecentOrder, error) {
	f.calls = append(f.calls, "RecentOrders")
	f.recentLimit = limit
	return f.recents, nil
}

type fakeCustomerStatistics struct {
	calls     []string
	kpis      contracts.CustomerKPIs
	summaries []contracts.CustomerSummary

	// week is the window the application asked for (US2).
	week contracts.TimeWindow

	// summaryIDs is the ids the application asked about in the bulk read (US5).
	summaryIDs []uuid.UUID
}

func (f *fakeCustomerStatistics) KPIs(_ context.Context, week contracts.TimeWindow) (contracts.CustomerKPIs, error) {
	f.calls = append(f.calls, "KPIs")
	f.week = week
	return f.kpis, nil
}

func (f *fakeCustomerStatistics) CustomerSummaries(_ context.Context, ids []uuid.UUID) ([]contracts.CustomerSummary, error) {
	f.calls = append(f.calls, "CustomerSummaries")
	f.summaryIDs = ids
	return f.summaries, nil
}

type fakeInventoryStatistics struct {
	calls     []string
	variants  []contracts.LowStockVariant
	threshold int64
}

func (f *fakeInventoryStatistics) LowStockVariants(_ context.Context, threshold int64) ([]contracts.LowStockVariant, error) {
	f.calls = append(f.calls, "LowStockVariants")
	f.threshold = threshold
	return f.variants, nil
}

type fakeVariantStates struct {
	calls        []string
	discontinued []uuid.UUID
	asked        []uuid.UUID
}

func (f *fakeVariantStates) DiscontinuedVariantIDs(_ context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	f.calls = append(f.calls, "DiscontinuedVariantIDs")
	f.asked = ids
	return f.discontinued, nil
}

// --- clock and auditor --------------------------------------------------

type fakeClock struct {
	now   time.Time
	calls int
}

func (c *fakeClock) Now() time.Time {
	c.calls++
	return c.now
}

type recordedEvent struct {
	action     string
	outcome    string
	actorID    *uuid.UUID
	actorRole  string
	targetType string
	targetID   string
	metadata   map[string]any
}

type fakeAuditor struct {
	events []recordedEvent
}

func (a *fakeAuditor) Record(_ context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any) {
	a.events = append(a.events, recordedEvent{
		action:     action,
		outcome:    outcome,
		actorID:    actorID,
		actorRole:  actorRole,
		targetType: targetType,
		targetID:   targetID,
		metadata:   metadata,
	})
}

// --- harness ------------------------------------------------------------

type overviewHarness struct {
	orders    *fakeOrderStatistics
	customers *fakeCustomerStatistics
	inventory *fakeInventoryStatistics
	variants  *fakeVariantStates
	clock     *fakeClock
	auditor   *fakeAuditor
	service   *Service
}

func fixedLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatalf("load the shop timezone: %v", err)
	}
	return loc
}

func newOverviewHarness(t *testing.T) *overviewHarness {
	t.Helper()
	h := &overviewHarness{
		orders:    &fakeOrderStatistics{},
		customers: &fakeCustomerStatistics{},
		inventory: &fakeInventoryStatistics{},
		variants:  &fakeVariantStates{},
		clock:     &fakeClock{now: time.Date(2026, time.October, 17, 4, 0, 0, 0, time.UTC)},
		auditor:   &fakeAuditor{},
	}
	h.service = New(Service{
		Orders:    h.orders,
		Customers: h.customers,
		Inventory: h.inventory,
		Variants:  h.variants,
		Clock:     h.clock,
		Auditor:   h.auditor,
		Config:    appinterface.Config{Location: fixedLocation(t)},
		Mapper:    mapper.New(),
	})
	return h
}

func (h *overviewHarness) run(t *testing.T) dto.Overview {
	t.Helper()
	return h.runWith(t, constant.DefaultDays)
}

// runWith runs one request with an explicit trend length, so a test can exercise
// both the 7-day and 30-day shapes.
func (h *overviewHarness) runWith(t *testing.T, days int) dto.Overview {
	t.Helper()
	actorID := uuid.New()
	ctx := appinterface.WithActor(context.Background(), appinterface.Actor{ID: actorID, Role: access.RoleAdmin})
	out, err := h.service.Overview(ctx, dto.OverviewInput{
		Days:              days,
		LowStockThreshold: constant.DefaultLowStockThreshold,
	})
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	return out
}

// --- tests --------------------------------------------------------------

// FR-002: one request returns every section — all four KPI groups, the trend,
// the category breakdown and the recent orders.
func TestOverviewAssemblesEverySection(t *testing.T) {
	h := newOverviewHarness(t)
	h.orders.kpis = contracts.OrderKPIs{
		CurrentRevenue:      contracts.Money{Amount: 500, Currency: "VND"},
		LastRevenue:         contracts.Money{Amount: 400, Currency: "VND"},
		CurrentOrders:       10,
		LastOrders:          8,
		TotalOrders:         100,
		PendingConfirmation: 3,
	}
	h.orders.trend = []contracts.DailyRevenue{{Date: "2026-10-17", Amount: contracts.Money{Amount: 100, Currency: "VND"}}}
	categoryID := uuid.New()
	h.orders.categories = []contracts.CategoryRevenue{{
		CategoryID:   &categoryID,
		CategoryName: "Acrylic Stand",
		Amount:       contracts.Money{Amount: 300, Currency: "VND"},
	}}
	orderID := uuid.New()
	h.orders.recents = []contracts.RecentOrder{{
		ID:                orderID,
		CustomerID:        uuid.New(),
		Total:             contracts.Money{Amount: 200, Currency: "VND"},
		Status:            "PAID",
		CreatedAt:         h.clock.now,
		FirstLineName:     "Stand",
		FirstLineQuantity: 2,
		LineCount:         2,
		TotalQuantity:     3,
	}}
	h.customers.kpis = contracts.CustomerKPIs{TotalUsers: 50, NewUsersThisWeek: 5}
	lowID := uuid.New()
	discontinuedID := uuid.New()
	h.inventory.variants = []contracts.LowStockVariant{
		{VariantID: lowID, Available: 2},
		{VariantID: discontinuedID, Available: 1},
	}
	h.variants.discontinued = []uuid.UUID{discontinuedID}

	out := h.run(t)

	if out.KPIs.Revenue.Current.Amount != 500 || out.KPIs.Revenue.Current.Currency != "VND" {
		t.Fatalf("current revenue did not map: %+v", out.KPIs.Revenue.Current)
	}
	if out.KPIs.Revenue.Last.Amount != 400 {
		t.Fatalf("last revenue did not map: %+v", out.KPIs.Revenue.Last)
	}
	if out.KPIs.Orders.Total != 100 || out.KPIs.Orders.ThisMonth != 10 || out.KPIs.Orders.LastMonth != 8 || out.KPIs.Orders.PendingConfirmation != 3 {
		t.Fatalf("order KPI did not map: %+v", out.KPIs.Orders)
	}
	if out.KPIs.Customers.TotalUsers != 50 || out.KPIs.Customers.NewUsersThisWeek != 5 {
		t.Fatalf("customer KPI did not map: %+v", out.KPIs.Customers)
	}
	// T043 owns the accurate rule; US1 still drops the discontinued candidate it
	// was told about, so the count is one of the two candidates.
	if out.KPIs.Inventory.LowStockCount != 1 || out.KPIs.Inventory.Threshold != 5 {
		t.Fatalf("inventory KPI did not map: %+v", out.KPIs.Inventory)
	}
	if len(out.RevenueTrend) != constant.DefaultDays {
		t.Fatalf("the trend must be zero-filled to the requested days, got %d", len(out.RevenueTrend))
	}
	if out.RevenueTrend[len(out.RevenueTrend)-1].Amount.Amount != 100 {
		t.Fatalf("the seeded day's revenue did not map: %+v", out.RevenueTrend)
	}
	if out.RevenueTrend[0].Amount.Amount != 0 || out.RevenueTrend[0].Amount.Currency != "VND" {
		t.Fatalf("a day the read does not carry must be a zero VND point: %+v", out.RevenueTrend[0])
	}
	if len(out.CategoryBreakdown) != 1 || out.CategoryBreakdown[0].Amount.Amount != 300 {
		t.Fatalf("category breakdown did not map: %+v", out.CategoryBreakdown)
	}
	if len(out.RecentOrders) != 1 || out.RecentOrders[0].ID != orderID || out.RecentOrders[0].Total.Amount != 200 {
		t.Fatalf("recent orders did not map: %+v", out.RecentOrders)
	}
	if out.RecentOrders[0].Status != "PAID" || out.RecentOrders[0].ItemCount != 2 || out.RecentOrders[0].ItemQuantity != 3 {
		t.Fatalf("recent order context did not map: %+v", out.RecentOrders[0])
	}
}

// FR-005, SC-007: every money value is an integer amount plus a currency; the
// trend and category sections use the money object too.
func TestOverviewCarriesMoneyObjectsEverywhere(t *testing.T) {
	h := newOverviewHarness(t)
	h.orders.kpis = contracts.OrderKPIs{
		CurrentRevenue: contracts.Money{Amount: 1_000_000, Currency: "VND"},
		LastRevenue:    contracts.Money{Amount: 0, Currency: "VND"},
	}
	h.orders.trend = []contracts.DailyRevenue{{Date: "2026-10-17", Amount: contracts.Money{Amount: 5000, Currency: "VND"}}}
	h.orders.categories = []contracts.CategoryRevenue{{Amount: contracts.Money{Amount: 7000, Currency: "VND"}}}
	h.orders.recents = []contracts.RecentOrder{{Total: contracts.Money{Amount: 9000, Currency: "VND"}}}

	out := h.run(t)

	if out.RevenueTrend[0].Amount.Currency != "VND" || out.CategoryBreakdown[0].Amount.Currency != "VND" || out.RecentOrders[0].Total.Currency != "VND" {
		t.Fatalf("money objects must carry a currency everywhere: trend=%+v category=%+v recent=%+v",
			out.RevenueTrend[0].Amount, out.CategoryBreakdown[0].Amount, out.RecentOrders[0].Total)
	}
}

// US1 acceptance 3: empty data still returns a valid response: zero money and
// empty (never null) sections.
func TestOverviewHandlesEmptyData(t *testing.T) {
	h := newOverviewHarness(t)
	h.orders.kpis = contracts.OrderKPIs{
		CurrentRevenue: contracts.Money{Currency: "VND"},
		LastRevenue:    contracts.Money{Currency: "VND"},
	}

	out := h.run(t)

	if out.KPIs.Revenue.Current.Amount != 0 {
		t.Fatalf("empty revenue must be zero: %+v", out.KPIs.Revenue.Current)
	}
	// An empty read still yields one zero-filled point per requested day
	// (FR-019); the earlier US1 "empty trend" placeholder is gone.
	if len(out.RevenueTrend) != constant.DefaultDays {
		t.Fatalf("an empty trend must still be zero-filled to %d days, got %d", constant.DefaultDays, len(out.RevenueTrend))
	}
	for _, point := range out.RevenueTrend {
		if point.Amount.Amount != 0 || point.Amount.Currency != "VND" {
			t.Fatalf("every zero-filled point must be a zero VND money object, got %+v", point)
		}
	}
	if out.CategoryBreakdown == nil || len(out.CategoryBreakdown) != 0 {
		t.Fatalf("an empty category breakdown must be an empty slice, got %#v", out.CategoryBreakdown)
	}
	if out.RecentOrders == nil || len(out.RecentOrders) != 0 {
		t.Fatalf("empty recent orders must be an empty slice, got %#v", out.RecentOrders)
	}
}

// FR-030, SC-009: exactly one ADMIN_DASHBOARD_VIEWED event per successful view,
// naming the administrator from the session and carrying no PII.
func TestOverviewRecordsExactlyOneAuditEventWithoutPII(t *testing.T) {
	h := newOverviewHarness(t)
	actorID := uuid.New()
	ctx := appinterface.WithActor(context.Background(), appinterface.Actor{ID: actorID, Role: access.RoleAdmin})

	if _, err := h.service.Overview(ctx, dto.OverviewInput{Days: 30, LowStockThreshold: 9}); err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if len(h.auditor.events) != 1 {
		t.Fatalf("expected exactly one audit event, got %d", len(h.auditor.events))
	}
	event := h.auditor.events[0]
	if event.action != constant.AuditDashboardViewed || event.outcome != "SUCCESS" {
		t.Fatalf("unexpected audit action/outcome: %+v", event)
	}
	if event.targetType != constant.TargetTypeDashboard || event.targetID != constant.TargetIDOverview {
		t.Fatalf("unexpected audit target: %+v", event)
	}
	if event.actorID == nil || *event.actorID != actorID || event.actorRole != string(access.RoleAdmin) {
		t.Fatalf("the audit actor must come from the session: %+v", event)
	}
	for _, key := range []string{"days", "lowStockThreshold", "recentOrderCount"} {
		if _, ok := event.metadata[key]; !ok {
			t.Fatalf("audit metadata is missing %q: %+v", key, event.metadata)
		}
	}
	if len(event.metadata) != 3 {
		t.Fatalf("audit metadata must carry exactly three members, got %+v", event.metadata)
	}
	for key, value := range event.metadata {
		kind := reflect.ValueOf(value).Kind()
		if kind != reflect.Int && kind != reflect.Int64 {
			t.Fatalf("audit metadata %q must be an integer, never PII, got %T", key, value)
		}
	}
}

// FR-006, FR-028: the application is read-only. Structurally it carries no write
// or UnitOfWork port, and every port it does carry is called through read
// methods only.
func TestTheApplicationDependsOnlyOnReadPorts(t *testing.T) {
	allowed := map[string]bool{
		"OrderStatistics":      true,
		"CustomerStatistics":   true,
		"InventoryStatistics":  true,
		"ProductVariantStates": true,
		"Clock":                true,
		"Auditor":              true,
		"Config":               true,
		"Mapper":               true,
	}
	typ := reflect.TypeOf(Service{})
	if typ.NumField() == 0 {
		t.Fatal("the service carries no ports at all")
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := field.Type.Name()
		if field.Type.Kind() == reflect.Pointer {
			name = field.Type.Elem().Name()
		}
		if !allowed[name] {
			t.Fatalf("the application carries a non-read dependency %q of type %s", field.Name, field.Type)
		}
	}

	h := newOverviewHarness(t)
	h.run(t)

	// Every port method reached is a read: the fakes expose no other method.
	for _, call := range h.orders.calls {
		if call != "KPIs" && call != "DailyRevenue" && call != "CategoryRevenue" && call != "RecentOrders" {
			t.Fatalf("unexpected order read %q", call)
		}
	}
	if len(h.orders.calls) != 4 {
		t.Fatalf("the order port must be read exactly four times, got %v", h.orders.calls)
	}
	if len(h.customers.calls) == 0 {
		t.Fatal("the customer port must be read")
	}
	if len(h.inventory.calls) == 0 {
		t.Fatal("the inventory port must be read")
	}
	if len(h.variants.calls) == 0 {
		t.Fatal("the variant-state port must be read")
	}
}

// FR-032, SC-006, T066(a): the dashboard issues a fixed number of read calls —
// exactly one per contract method across the four read ports (4 order + 2
// customer + 1 inventory + 1 product = 8) — and that count does not grow with
// the size of the fixture. This is the bounded-read half of SC-006, which a unit
// test can prove without a database; the 2-second target is measured by the
// DB-backed integration test in presentation/http.
func TestOverviewMakesAFixedNumberOfReadCallsRegardlessOfFixtureSize(t *testing.T) {
	small := newOverviewHarness(t)
	small.run(t)
	if got := countReadCalls(t, small); got != 8 {
		t.Fatalf("small fixture: expected exactly 8 read calls, got %d", got)
	}

	// A much larger fixture must not add calls: a thousand low-stock candidates
	// are still resolved in one inventory read plus one discontinued-id read, and
	// the recent orders' customers in one bulk read.
	large := newOverviewHarness(t)
	large.inventory.variants = make([]contracts.LowStockVariant, 0, 1000)
	for i := 0; i < 1000; i++ {
		large.inventory.variants = append(large.inventory.variants, contracts.LowStockVariant{VariantID: uuid.New(), Available: int64(i % 5)})
	}
	large.orders.categories = []contracts.CategoryRevenue{
		{Amount: contracts.Money{Amount: 100, Currency: "VND"}},
		{Amount: contracts.Money{Amount: 200, Currency: "VND"}},
	}
	large.orders.recents = make([]contracts.RecentOrder, 0, 5)
	for i := 0; i < 5; i++ {
		large.orders.recents = append(large.orders.recents, contracts.RecentOrder{
			ID:                uuid.New(),
			CustomerID:        uuid.New(),
			Total:             contracts.Money{Amount: 100, Currency: "VND"},
			Status:            "PAID",
			CreatedAt:         large.clock.now,
			FirstLineName:     "Stand",
			FirstLineQuantity: 1,
			LineCount:         1,
			TotalQuantity:     1,
		})
	}
	large.customers.summaries = []contracts.CustomerSummary{}
	large.run(t)
	if got := countReadCalls(t, large); got != 8 {
		t.Fatalf("large fixture: expected exactly 8 read calls, got %d", got)
	}

	// The large fixture genuinely carried more data; only the call count is fixed.
	if len(large.inventory.variants) != 1000 {
		t.Fatalf("test setup: the large fixture must carry 1000 candidates, got %d", len(large.inventory.variants))
	}
}

// countReadCalls asserts each contract method of the four read ports was called
// exactly once and returns the total. Any extra call — or a call that scales with
// the fixture — fails the test.
func countReadCalls(t *testing.T, h *overviewHarness) int {
	t.Helper()
	orderCalls := map[string]int{}
	for _, call := range h.orders.calls {
		orderCalls[call]++
	}
	for _, method := range []string{"KPIs", "DailyRevenue", "CategoryRevenue", "RecentOrders"} {
		if orderCalls[method] != 1 {
			t.Fatalf("order port method %s must be called exactly once, got %d (%v)", method, orderCalls[method], h.orders.calls)
		}
	}
	customerCalls := map[string]int{}
	for _, call := range h.customers.calls {
		customerCalls[call]++
	}
	for _, method := range []string{"KPIs", "CustomerSummaries"} {
		if customerCalls[method] != 1 {
			t.Fatalf("customer port method %s must be called exactly once, got %d (%v)", method, customerCalls[method], h.customers.calls)
		}
	}
	if len(h.inventory.calls) != 1 || h.inventory.calls[0] != "LowStockVariants" {
		t.Fatalf("inventory port must be read exactly once, got %v", h.inventory.calls)
	}
	if len(h.variants.calls) != 1 || h.variants.calls[0] != "DiscontinuedVariantIDs" {
		t.Fatalf("product port must be read exactly once, got %v", h.variants.calls)
	}
	return len(h.orders.calls) + len(h.customers.calls) + len(h.inventory.calls) + len(h.variants.calls)
}

// The dashboard reads the clock once per request, so every section agrees on
// "now".
func TestOverviewReadsTheClockOnce(t *testing.T) {
	h := newOverviewHarness(t)
	h.run(t)

	if h.clock.calls != 1 {
		t.Fatalf("expected the clock to be read once, got %d", h.clock.calls)
	}
}

// --- US2: fair windows, growth and the zero-filled trend -----------------

// FR-008, US2 acceptance 2, T032/T035: the application hands the order port the
// month-to-date window and the previous-month same-period window, clamped to the
// current month's first day when the elapsed duration overruns the previous one.
func TestOverviewUsesTheMonthToDateAndClampedPreviousWindows(t *testing.T) {
	loc := fixedLocation(t)
	h := newOverviewHarness(t)
	// 31-day March against 28-day February forces the clamp.
	h.clock.now = time.Date(2026, time.March, 31, 12, 0, 0, 0, loc)

	h.run(t)

	marchFirst := time.Date(2026, time.March, 1, 0, 0, 0, 0, loc)
	if !h.orders.kpiCurrent.From.Equal(marchFirst) || !h.orders.kpiCurrent.To.Equal(h.clock.now) {
		t.Fatalf("current window = %+v, want the March month-to-date [%s, now)", h.orders.kpiCurrent, marchFirst)
	}
	februaryFirst := time.Date(2026, time.February, 1, 0, 0, 0, 0, loc)
	if !h.orders.kpiLast.From.Equal(februaryFirst) || !h.orders.kpiLast.To.Equal(marchFirst) {
		t.Fatalf("last window = %+v, want February clamped to the current month's first day", h.orders.kpiLast)
	}
	// The category breakdown uses the same current-month window as the revenue KPI.
	if !h.orders.categoryWindow.From.Equal(marchFirst) || !h.orders.categoryWindow.To.Equal(h.clock.now) {
		t.Fatalf("category window = %+v, want the current month window", h.orders.categoryWindow)
	}
}

// FR-009: the customer week is Monday 00:00 in the shop timezone to now.
func TestOverviewUsesTheMondayWeekWindow(t *testing.T) {
	loc := fixedLocation(t)
	h := newOverviewHarness(t)
	// 2026-10-17 is a Saturday; the week began Monday 2026-10-12.
	h.clock.now = time.Date(2026, time.October, 17, 4, 0, 0, 0, time.UTC)

	h.run(t)

	monday := time.Date(2026, time.October, 12, 0, 0, 0, 0, loc)
	if !h.customers.week.From.Equal(monday) || !h.customers.week.To.Equal(h.clock.now) {
		t.Fatalf("week window = %+v, want Monday %s to now", h.customers.week, monday)
	}
	if h.customers.week.From.Weekday() != time.Monday {
		t.Fatalf("the week must start on Monday, got %s", h.customers.week.From.Weekday())
	}
}

// FR-011, FR-013: growth is the nullable, two-decimal ratio of the two windows;
// a zero base is nil, never 0 or 100.
func TestOverviewGrowthMatchesTheNullableRule(t *testing.T) {
	t.Run("non-zero base computes and rounds", func(t *testing.T) {
		h := newOverviewHarness(t)
		h.orders.kpis = contracts.OrderKPIs{
			CurrentRevenue: contracts.Money{Amount: 1000, Currency: "VND"},
			LastRevenue:    contracts.Money{Amount: 800, Currency: "VND"},
			CurrentOrders:  10,
			LastOrders:     8,
		}

		out := h.run(t)

		if out.KPIs.Revenue.GrowthPercent == nil || *out.KPIs.Revenue.GrowthPercent != 25.0 {
			t.Fatalf("revenue growth = %v, want 25.0", out.KPIs.Revenue.GrowthPercent)
		}
		if out.KPIs.Orders.GrowthPercent == nil || *out.KPIs.Orders.GrowthPercent != 25.0 {
			t.Fatalf("order growth = %v, want 25.0", out.KPIs.Orders.GrowthPercent)
		}
	})

	t.Run("zero base is nil", func(t *testing.T) {
		h := newOverviewHarness(t)
		h.orders.kpis = contracts.OrderKPIs{
			CurrentRevenue: contracts.Money{Amount: 5000, Currency: "VND"},
			LastRevenue:    contracts.Money{Currency: "VND"},
			CurrentOrders:  10,
			LastOrders:     0,
		}

		out := h.run(t)

		if out.KPIs.Revenue.GrowthPercent != nil {
			t.Fatalf("revenue growth on a zero base must be nil, got %v", *out.KPIs.Revenue.GrowthPercent)
		}
		if out.KPIs.Orders.GrowthPercent != nil {
			t.Fatalf("order growth on a zero base must be nil, got %v", *out.KPIs.Orders.GrowthPercent)
		}
	})
}

// FR-019, FR-020, T037/T038: the trend always has exactly `days` points, oldest
// first, from the domain date list; a day without revenue is zero-filled, and the
// labels are T2..CN for 7 days and DD/MM for 30.
func TestOverviewZeroFillsTheTrendFromTheDomainDates(t *testing.T) {
	h := newOverviewHarness(t)
	h.orders.trend = []contracts.DailyRevenue{
		{Date: "2026-10-15", Amount: contracts.Money{Amount: 1000, Currency: "VND"}},
		{Date: "2026-10-17", Amount: contracts.Money{Amount: 3000, Currency: "VND"}},
	}

	out := h.run(t)

	wantDates := []string{
		"2026-10-11", "2026-10-12", "2026-10-13", "2026-10-14",
		"2026-10-15", "2026-10-16", "2026-10-17",
	}
	if len(out.RevenueTrend) != len(wantDates) {
		t.Fatalf("expected %d trend points, got %d", len(wantDates), len(out.RevenueTrend))
	}
	amounts := map[string]int64{"2026-10-15": 1000, "2026-10-17": 3000}
	for i, point := range out.RevenueTrend {
		if point.Date != wantDates[i] {
			t.Fatalf("trend[%d].date = %q, want %q", i, point.Date, wantDates[i])
		}
		if point.Amount.Amount != amounts[point.Date] || point.Amount.Currency != "VND" {
			t.Fatalf("trend[%d] amount = %+v, want %d VND", i, point.Amount, amounts[point.Date])
		}
	}
	// 2026-10-11 is a Sunday and 2026-10-17 a Saturday: the 7-day labels are the
	// weekday forms.
	if out.RevenueTrend[0].DayLabel != "CN" || out.RevenueTrend[1].DayLabel != "T2" ||
		out.RevenueTrend[len(out.RevenueTrend)-1].DayLabel != "T7" {
		t.Fatalf("7-day day labels must be T2..CN, got %q and %q", out.RevenueTrend[0].DayLabel, out.RevenueTrend[1].DayLabel)
	}
	if !h.orders.trendWindow.From.Equal(time.Date(2026, time.October, 11, 0, 0, 0, 0, fixedLocation(t))) {
		t.Fatalf("trend window from = %s, want the oldest local day", h.orders.trendWindow.From)
	}
	if h.orders.trendTimeZone != fixedLocation(t).String() {
		t.Fatalf("trend timezone = %q, want the shop timezone", h.orders.trendTimeZone)
	}

	thirty := h.runWith(t, 30)
	if len(thirty.RevenueTrend) != 30 {
		t.Fatalf("a 30-day trend must have 30 points, got %d", len(thirty.RevenueTrend))
	}
	if thirty.RevenueTrend[0].DayLabel != "18/09" || thirty.RevenueTrend[len(thirty.RevenueTrend)-1].DayLabel != "17/10" {
		t.Fatalf("30-day day labels must be DD/MM, got %q and %q",
			thirty.RevenueTrend[0].DayLabel, thirty.RevenueTrend[len(thirty.RevenueTrend)-1].DayLabel)
	}
}

// FR-007, research D4, US2 acceptance 4: the calendar boundaries follow the shop
// timezone, not UTC. A request at 23:30 UTC on 31 October is 06:30 on 1 November
// in Asia/Ho_Chi_Minh, so it belongs to November.
func TestOverviewUsesTheShopTimezoneAtTheMonthBoundary(t *testing.T) {
	loc := fixedLocation(t)
	h := newOverviewHarness(t)
	h.clock.now = time.Date(2026, time.October, 31, 23, 30, 0, 0, time.UTC)

	out := h.run(t)

	novemberFirst := time.Date(2026, time.November, 1, 0, 0, 0, 0, loc)
	if !h.orders.kpiCurrent.From.Equal(novemberFirst) {
		t.Fatalf("the current month must start at November 1 local, got %s", h.orders.kpiCurrent.From)
	}
	if out.RevenueTrend[len(out.RevenueTrend)-1].Date != "2026-11-01" {
		t.Fatalf("the trend must end on the local day 2026-11-01, got %q", out.RevenueTrend[len(out.RevenueTrend)-1].Date)
	}
}

// --- US3: accurate low-stock and customer KPIs ---------------------------

// FR-015, FR-016, FR-018, research D9, T042/T043: the low-stock count removes the
// DISCONTINUED variants from the inventory candidates before counting (inventory
// already floored availability and dropped never-stocked variants), the requested
// threshold reaches the inventory read unchanged and is echoed, both customer KPIs
// come straight from the customer read, and the variant-state port is asked about
// exactly the candidate ids — so the admin never reads the product table itself.
func TestOverviewFiltersDiscontinuedCandidatesAndMapsCustomerKPIs(t *testing.T) {
	h := newOverviewHarness(t)
	low, discontinued, alsoLow := uuid.New(), uuid.New(), uuid.New()
	h.inventory.variants = []contracts.LowStockVariant{
		{VariantID: low, Available: 1},
		{VariantID: discontinued, Available: 0},
		{VariantID: alsoLow, Available: 4},
	}
	h.variants.discontinued = []uuid.UUID{discontinued}
	h.customers.kpis = contracts.CustomerKPIs{TotalUsers: 128, NewUsersThisWeek: 7}

	out := h.run(t)

	if out.KPIs.Inventory.LowStockCount != 2 {
		t.Fatalf("lowStockCount = %d, want the two non-discontinued candidates", out.KPIs.Inventory.LowStockCount)
	}
	if out.KPIs.Inventory.Threshold != constant.DefaultLowStockThreshold {
		t.Fatalf("threshold = %d, want it echoed from the request", out.KPIs.Inventory.Threshold)
	}
	if h.inventory.threshold != constant.DefaultLowStockThreshold {
		t.Fatalf("the inventory read was asked for threshold %d, want %d",
			h.inventory.threshold, constant.DefaultLowStockThreshold)
	}
	if out.KPIs.Customers.TotalUsers != 128 || out.KPIs.Customers.NewUsersThisWeek != 7 {
		t.Fatalf("customer KPIs did not map: %+v", out.KPIs.Customers)
	}
	wantAsked := []uuid.UUID{low, discontinued, alsoLow}
	if !reflect.DeepEqual(h.variants.asked, wantAsked) {
		t.Fatalf("the variant-state read must be asked about exactly the candidates: got %v want %v", h.variants.asked, wantAsked)
	}
}

// FR-024, T049: the category section uses the KPI current-month revenue as the
// denominator, and the unresolved ("Khác") bucket is counted in the breakdown.
func TestOverviewCategoryUsesCurrentRevenueAsDenominatorAndCountsUnknownBucket(t *testing.T) {
	h := newOverviewHarness(t)
	h.orders.kpis = contracts.OrderKPIs{
		CurrentRevenue: contracts.Money{Amount: 1000, Currency: "VND"},
		LastRevenue:    contracts.Money{Amount: 0, Currency: "VND"},
	}
	knownID := uuid.New()
	unknownID := (*uuid.UUID)(nil)
	h.orders.categories = []contracts.CategoryRevenue{
		{CategoryID: &knownID, CategoryName: "Acrylic", Amount: contracts.Money{Amount: 250, Currency: "VND"}},
		{CategoryID: unknownID, CategoryName: "", Amount: contracts.Money{Amount: 750, Currency: "VND"}},
	}

	out := h.run(t)

	if len(out.CategoryBreakdown) != 2 {
		t.Fatalf("expected 2 categories including the unknown bucket, got %d", len(out.CategoryBreakdown))
	}
	// percentages: 250/1000*100 = 25.0, 750/1000*100 = 75.0
	found := map[string]float64{}
	for _, c := range out.CategoryBreakdown {
		found[c.CategoryName] = c.Percentage
	}
	if found["Acrylic"] != 25.0 {
		t.Fatalf("Acrylic percentage = %v, want 25.0", found["Acrylic"])
	}
	if found[constant.UnknownCategoryName] != 75.0 {
		t.Fatalf("unknown bucket percentage = %v, want 75.0", found[constant.UnknownCategoryName])
	}
	// unknown bucket must carry a nil id
	for _, c := range out.CategoryBreakdown {
		if c.CategoryName == constant.UnknownCategoryName && c.CategoryID != nil {
			t.Fatalf("unknown bucket must have a nil id, got %v", c.CategoryID)
		}
	}
}

// FR-026, T055: recent orders resolve their customers in a single bulk read,
// a missing account becomes a null customer, and at most five orders are mapped.
func TestOverviewResolvesRecentCustomersInOneBulkCall(t *testing.T) {
	h := newOverviewHarness(t)
	existingID := uuid.New()
	missingID := uuid.New()
	orderExisting := uuid.New()
	orderMissing := uuid.New()
	h.orders.recents = []contracts.RecentOrder{
		{ID: orderExisting, CustomerID: existingID, Total: contracts.Money{Amount: 100, Currency: "VND"}, Status: "PAID", CreatedAt: h.clock.now, FirstLineName: "Stand", FirstLineQuantity: 1, LineCount: 1, TotalQuantity: 1},
		{ID: orderMissing, CustomerID: missingID, Total: contracts.Money{Amount: 200, Currency: "VND"}, Status: "SHIPPED", CreatedAt: h.clock.now, FirstLineName: "Print", FirstLineQuantity: 2, LineCount: 2, TotalQuantity: 2},
	}
	h.customers.summaries = []contracts.CustomerSummary{
		{ID: existingID, DisplayName: "Alice", Email: "alice@example.com"},
	}

	out := h.run(t)

	// One bulk CustomerSummaries call.
	customerCalls := 0
	for _, c := range h.customers.calls {
		if c == "CustomerSummaries" {
			customerCalls++
		}
	}
	if customerCalls != 1 {
		t.Fatalf("expected exactly one CustomerSummaries call, got %d", customerCalls)
	}
	// Deduplicated ids: both distinct customer ids asked once.
	if len(h.customers.summaryIDs) != 2 {
		t.Fatalf("expected 2 distinct customer ids, got %d: %v", len(h.customers.summaryIDs), h.customers.summaryIDs)
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range h.customers.summaryIDs {
		seen[id] = true
	}
	if !seen[existingID] || !seen[missingID] {
		t.Fatalf("the bulk read must ask about every distinct customer id, got %v", h.customers.summaryIDs)
	}
	// Missing account -> nil customer.
	byID := map[uuid.UUID]dto.RecentOrder{}
	for _, r := range out.RecentOrders {
		byID[r.ID] = r
	}
	if byID[orderExisting].Customer == nil || byID[orderExisting].Customer.DisplayName != "Alice" {
		t.Fatalf("existing customer did not resolve: %+v", byID[orderExisting].Customer)
	}
	if byID[orderMissing].Customer != nil {
		t.Fatalf("missing customer must be nil, got %+v", byID[orderMissing].Customer)
	}
	// At most five orders mapped (the contract limit is five).
	if len(out.RecentOrders) > 5 {
		t.Fatalf("expected at most 5 recent orders, got %d", len(out.RecentOrders))
	}
	if h.orders.recentLimit != constant.RecentOrderLimit {
		t.Fatalf("recent orders must be requested with limit %d, got %d", constant.RecentOrderLimit, h.orders.recentLimit)
	}
}

// state about any id, so the read stays bounded by the low-stock set rather than
// by the catalogue.
func TestOverviewCountsZeroWhenNoLowStockCandidates(t *testing.T) {
	h := newOverviewHarness(t)

	out := h.run(t)

	if out.KPIs.Inventory.LowStockCount != 0 {
		t.Fatalf("lowStockCount = %d, want 0 for an empty candidate set", out.KPIs.Inventory.LowStockCount)
	}
	if len(h.variants.asked) != 0 {
		t.Fatalf("an empty candidate set must ask about no ids, got %v", h.variants.asked)
	}
}
