// Package statistics adapts the order module's own repository reads to the
// contracts.OrderStatistics port. The admin module depends on that port; this
// adapter is supplied at the composition root, so the dashboard reads order-owned
// data through module 07's own read (Constitution I, research D7).
package statistics

import (
	"context"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// Repository is the slice of the order module's repository this adapter needs.
// Declaring it here keeps the adapter honest about its dependency and lets a test
// supply a fake (research D7).
type Repository interface {
	// KPIs answers every order and revenue KPI in one read.
	KPIs(ctx context.Context, current, last model.TimeWindow) (model.OrderKPIs, error)
	// DailyRevenue answers the window's revenue per shop-timezone day.
	DailyRevenue(ctx context.Context, window model.TimeWindow, timeZone string) ([]model.DailyRevenue, error)
	// CategoryRevenue answers the window's revenue grouped by line category.
	CategoryRevenue(ctx context.Context, window model.TimeWindow) ([]model.CategoryRevenue, error)
	// RecentOrders answers at most limit newest orders, newest first.
	RecentOrders(ctx context.Context, limit int) ([]model.RecentOrder, error)
}

// Adapter answers the dashboard's order reads over the order module's own
// repository. It never opens a transaction: each read joins the transaction the
// context already carries (Constitution I).
type Adapter struct {
	// Orders is the order module's repository port.
	Orders Repository
}

// New creates the order statistics adapter.
func New(orders Repository) *Adapter {
	return &Adapter{Orders: orders}
}

// currencyVND is the only currency the dashboard sums: the MVP shop is VND-only,
// so a future order in another currency must not silently join a VND total
// (research D5).
const currencyVND = "VND"

// KPIs answers every order and revenue KPI in one read, setting the currency the
// SQL filtered for (FR-010, FR-012 to FR-014).
func (a *Adapter) KPIs(ctx context.Context, current, last contracts.TimeWindow) (contracts.OrderKPIs, error) {
	kpis, err := a.Orders.KPIs(ctx, window(current), window(last))
	if err != nil {
		return contracts.OrderKPIs{}, err
	}
	return contracts.OrderKPIs{
		CurrentRevenue:      money(kpis.CurrentRevenue.Amount),
		LastRevenue:         money(kpis.LastRevenue.Amount),
		CurrentOrders:       kpis.CurrentOrders,
		LastOrders:          kpis.LastOrders,
		TotalOrders:         kpis.TotalOrders,
		PendingConfirmation: kpis.PendingConfirmation,
	}, nil
}

// DailyRevenue answers the window's revenue per shop-timezone day (FR-019).
func (a *Adapter) DailyRevenue(ctx context.Context, window contracts.TimeWindow, timeZone string) ([]contracts.DailyRevenue, error) {
	days, err := a.Orders.DailyRevenue(ctx, modelWindow(window), timeZone)
	if err != nil {
		return nil, err
	}
	out := make([]contracts.DailyRevenue, 0, len(days))
	for _, day := range days {
		out = append(out, contracts.DailyRevenue{
			Date:   day.Date,
			Amount: money(day.Amount.Amount),
		})
	}
	return out, nil
}

// CategoryRevenue answers the window's revenue grouped by the snapshotted
// category; a nil id stays nil, which the admin mapper names "Khác" (FR-021,
// FR-023).
func (a *Adapter) CategoryRevenue(ctx context.Context, window contracts.TimeWindow) ([]contracts.CategoryRevenue, error) {
	categories, err := a.Orders.CategoryRevenue(ctx, modelWindow(window))
	if err != nil {
		return nil, err
	}
	out := make([]contracts.CategoryRevenue, 0, len(categories))
	for _, category := range categories {
		out = append(out, contracts.CategoryRevenue{
			CategoryID:   category.CategoryID,
			CategoryName: category.CategoryName,
			Amount:       money(category.Amount.Amount),
		})
	}
	return out, nil
}

// RecentOrders answers at most limit newest orders (FR-025, FR-026).
func (a *Adapter) RecentOrders(ctx context.Context, limit int) ([]contracts.RecentOrder, error) {
	orders, err := a.Orders.RecentOrders(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]contracts.RecentOrder, 0, len(orders))
	for _, order := range orders {
		currency := order.Total.Currency
		if currency == "" {
			currency = currencyVND
		}
		out = append(out, contracts.RecentOrder{
			ID:                   order.ID,
			CustomerID:           order.CustomerID,
			Total:                contracts.Money{Amount: order.Total.Amount, Currency: currency},
			Status:               string(order.Status),
			CreatedAt:            order.CreatedAt,
			FirstLineName:        order.FirstLineName,
			FirstLineVariantName: order.FirstLineVariantName,
			FirstLineQuantity:    order.FirstLineQuantity,
			LineCount:            order.LineCount,
			TotalQuantity:        order.TotalQuantity,
		})
	}
	return out, nil
}

// window converts the cross-module window into the order module's read model. The
// two carry the same half-open instants; the conversion keeps the module's
// domain free of the contract package (Constitution I).
func window(w contracts.TimeWindow) model.TimeWindow {
	return model.TimeWindow{From: w.From, To: w.To}
}

// modelWindow is window under the name the DailyRevenue and CategoryRevenue
// signatures read more naturally.
func modelWindow(w contracts.TimeWindow) model.TimeWindow { return window(w) }

// money wraps an amount in the VND currency the revenue reads filter for.
func money(amount int64) contracts.Money {
	return contracts.Money{Amount: amount, Currency: currencyVND}
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would make
// that dependency-free package import a module (Constitution I).
var _ contracts.OrderStatistics = (*Adapter)(nil)
