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
}

func (f *fakeOrderStatistics) KPIs(context.Context, contracts.TimeWindow, contracts.TimeWindow) (contracts.OrderKPIs, error) {
	f.calls = append(f.calls, "KPIs")
	return f.kpis, nil
}

func (f *fakeOrderStatistics) DailyRevenue(context.Context, contracts.TimeWindow, string) ([]contracts.DailyRevenue, error) {
	f.calls = append(f.calls, "DailyRevenue")
	return f.trend, nil
}

func (f *fakeOrderStatistics) CategoryRevenue(context.Context, contracts.TimeWindow) ([]contracts.CategoryRevenue, error) {
	f.calls = append(f.calls, "CategoryRevenue")
	return f.categories, nil
}

func (f *fakeOrderStatistics) RecentOrders(context.Context, int) ([]contracts.RecentOrder, error) {
	f.calls = append(f.calls, "RecentOrders")
	return f.recents, nil
}

type fakeCustomerStatistics struct {
	calls     []string
	kpis      contracts.CustomerKPIs
	summaries []contracts.CustomerSummary
}

func (f *fakeCustomerStatistics) KPIs(context.Context, contracts.TimeWindow) (contracts.CustomerKPIs, error) {
	f.calls = append(f.calls, "KPIs")
	return f.kpis, nil
}

func (f *fakeCustomerStatistics) CustomerSummaries(context.Context, []uuid.UUID) ([]contracts.CustomerSummary, error) {
	f.calls = append(f.calls, "CustomerSummaries")
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
	actorID := uuid.New()
	ctx := appinterface.WithActor(context.Background(), appinterface.Actor{ID: actorID, Role: access.RoleAdmin})
	out, err := h.service.Overview(ctx, dto.OverviewInput{
		Days:              constant.DefaultDays,
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
	if len(out.RevenueTrend) != 1 || out.RevenueTrend[0].Amount.Amount != 100 {
		t.Fatalf("revenue trend did not map: %+v", out.RevenueTrend)
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
	if out.RevenueTrend == nil || len(out.RevenueTrend) != 0 {
		t.Fatalf("an empty trend must be an empty slice, got %#v", out.RevenueTrend)
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

// The dashboard reads the clock once per request, so every section agrees on
// "now".
func TestOverviewReadsTheClockOnce(t *testing.T) {
	h := newOverviewHarness(t)
	h.run(t)

	if h.clock.calls != 1 {
		t.Fatalf("expected the clock to be read once, got %d", h.clock.calls)
	}
}
