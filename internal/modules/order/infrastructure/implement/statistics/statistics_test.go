package statistics

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// fakeRepository implements the four reads the adapter uses and records the
// arguments it was called with, so a test can prove the window conversion and the
// fixed limit.
type fakeRepository struct {
	kpis       model.OrderKPIs
	days       []model.DailyRevenue
	categories []model.CategoryRevenue
	orders     []model.RecentOrder
	err        error

	current, last model.TimeWindow
	timeZone      string
	limit         int
}

func (f *fakeRepository) KPIs(_ context.Context, current, last model.TimeWindow) (model.OrderKPIs, error) {
	f.current, f.last = current, last
	return f.kpis, f.err
}

func (f *fakeRepository) DailyRevenue(_ context.Context, window model.TimeWindow, timeZone string) ([]model.DailyRevenue, error) {
	f.current, f.timeZone = window, timeZone
	return f.days, f.err
}

func (f *fakeRepository) CategoryRevenue(_ context.Context, window model.TimeWindow) ([]model.CategoryRevenue, error) {
	f.current = window
	return f.categories, f.err
}

func (f *fakeRepository) RecentOrders(_ context.Context, limit int) ([]model.RecentOrder, error) {
	f.limit = limit
	return f.orders, f.err
}

func testWindow(from time.Time) contracts.TimeWindow {
	return contracts.TimeWindow{From: from, To: from.Add(time.Hour)}
}

// research D5, D7: the adapter maps the domain reads to the contract DTOs, sets
// VND on every revenue amount and converts the window unchanged.
func TestKPIsMapsTheDomainReadAndSetsVND(t *testing.T) {
	from := time.Date(2026, time.October, 17, 0, 0, 0, 0, time.UTC)
	repo := &fakeRepository{kpis: model.OrderKPIs{
		CurrentRevenue:      model.Price{Amount: 1000},
		LastRevenue:         model.Price{Amount: 800},
		CurrentOrders:       5,
		LastOrders:          4,
		TotalOrders:         40,
		PendingConfirmation: 2,
	}}

	got, err := New(repo).KPIs(context.Background(), testWindow(from), testWindow(from.Add(-time.Hour)))
	if err != nil {
		t.Fatalf("KPIs: %v", err)
	}
	if got.CurrentRevenue.Amount != 1000 || got.CurrentRevenue.Currency != "VND" ||
		got.LastRevenue.Amount != 800 || got.LastRevenue.Currency != "VND" {
		t.Fatalf("revenue must be a VND money object, got %+v/%+v", got.CurrentRevenue, got.LastRevenue)
	}
	if got.CurrentOrders != 5 || got.LastOrders != 4 || got.TotalOrders != 40 || got.PendingConfirmation != 2 {
		t.Fatalf("counts did not map: %+v", got)
	}
	if repo.current.To.Sub(repo.current.From) != time.Hour {
		t.Fatalf("the current window must keep its length, got %+v", repo.current)
	}
}

// FR-019: daily revenue maps the date and wraps the amount in VND, passing the
// IANA zone through.
func TestDailyRevenueMapsDateAndZone(t *testing.T) {
	repo := &fakeRepository{days: []model.DailyRevenue{{Date: "2026-10-17", Amount: model.Price{Amount: 4500}}}}

	got, err := New(repo).DailyRevenue(context.Background(), testWindow(time.Now()), "Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatalf("DailyRevenue: %v", err)
	}
	if len(got) != 1 || got[0].Date != "2026-10-17" || got[0].Amount.Amount != 4500 || got[0].Amount.Currency != "VND" {
		t.Fatalf("daily revenue did not map: %+v", got)
	}
	if repo.timeZone != "Asia/Ho_Chi_Minh" {
		t.Fatalf("the timezone must be passed through, got %q", repo.timeZone)
	}
}

// FR-021, FR-023: a nil category id stays nil and the amount is VND.
func TestCategoryRevenuePreservesTheNilBucket(t *testing.T) {
	categoryID := uuid.New()
	repo := &fakeRepository{categories: []model.CategoryRevenue{
		{CategoryID: &categoryID, CategoryName: "Tranh", Amount: model.Price{Amount: 700}},
		{CategoryID: nil, CategoryName: "", Amount: model.Price{Amount: 300}},
	}}

	got, err := New(repo).CategoryRevenue(context.Background(), testWindow(time.Now()))
	if err != nil {
		t.Fatalf("CategoryRevenue: %v", err)
	}
	if len(got) != 2 || got[0].CategoryID == nil || *got[0].CategoryID != categoryID {
		t.Fatalf("the named category did not map: %+v", got)
	}
	if got[1].CategoryID != nil || got[1].Amount.Amount != 300 || got[1].Amount.Currency != "VND" {
		t.Fatalf("the unresolved bucket must keep a nil id and VND amount: %+v", got[1])
	}
}

// FR-025, FR-026: recent orders map every field, the limit is passed through and
// an empty stored currency falls back to VND.
func TestRecentOrdersMapsEveryFieldAndTheLimit(t *testing.T) {
	order := model.RecentOrder{
		ID:                   uuid.New(),
		CustomerID:           uuid.New(),
		Total:                model.Price{Amount: 9900},
		Status:               constant.StatusPaid,
		CreatedAt:            time.Date(2026, time.October, 17, 4, 2, 0, 0, time.UTC),
		FirstLineName:        "Acrylic Stand",
		FirstLineVariantName: "Elf Băng Giá",
		FirstLineQuantity:    2,
		LineCount:            2,
		TotalQuantity:        3,
	}
	repo := &fakeRepository{orders: []model.RecentOrder{order}}

	got, err := New(repo).RecentOrders(context.Background(), 5)
	if err != nil {
		t.Fatalf("RecentOrders: %v", err)
	}
	if repo.limit != 5 {
		t.Fatalf("the limit must be passed through, got %d", repo.limit)
	}
	if len(got) != 1 {
		t.Fatalf("expected one order, got %d", len(got))
	}
	recent := got[0]
	if recent.ID != order.ID || recent.CustomerID != order.CustomerID ||
		recent.Total.Amount != 9900 || recent.Total.Currency != "VND" ||
		recent.Status != string(constant.StatusPaid) ||
		recent.FirstLineName != order.FirstLineName ||
		recent.FirstLineVariantName != order.FirstLineVariantName ||
		recent.FirstLineQuantity != 2 || recent.LineCount != 2 || recent.TotalQuantity != 3 {
		t.Fatalf("recent order did not map: %+v", recent)
	}
}
