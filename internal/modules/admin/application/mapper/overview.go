// Package mapper converts the admin module's domain read models into the
// application DTOs. It is the single place where those two shapes are
// translated, and it owns the small wording derivations the response needs: the
// trend's day label and the recent order's item summary.
package mapper

import (
	"fmt"
	"strings"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/domain/model"
)

// Mapper converts admin read models into application DTOs. It carries no
// dependency: everything it needs is already in the read model.
type Mapper struct{}

// New creates a Mapper.
func New() *Mapper { return &Mapper{} }

// Overview maps the whole snapshot. An empty section maps to an empty slice,
// never to nil, so the response serialises arrays rather than null.
func (m *Mapper) Overview(o model.Overview) dto.Overview {
	return dto.Overview{
		KPIs:              m.kpis(o.KPIs),
		RevenueTrend:      m.revenueTrend(o.RevenueTrend),
		CategoryBreakdown: m.categoryBreakdown(o.CategoryBreakdown),
		RecentOrders:      m.recentOrders(o.RecentOrders),
	}
}

// kpis maps the four KPI groups.
func (m *Mapper) kpis(k model.KPIs) dto.KPIs {
	return dto.KPIs{
		Revenue: dto.RevenueKPI{
			Current:       m.money(k.Revenue.Current),
			Last:          m.money(k.Revenue.Last),
			GrowthPercent: k.Revenue.GrowthPercent,
		},
		Orders: dto.OrderKPI{
			Total:               k.Orders.Total,
			ThisMonth:           k.Orders.ThisMonth,
			LastMonth:           k.Orders.LastMonth,
			PendingConfirmation: k.Orders.PendingConfirmation,
			GrowthPercent:       k.Orders.GrowthPercent,
		},
		Customers: dto.CustomerKPI{
			TotalUsers:       k.Customers.TotalUsers,
			NewUsersThisWeek: k.Customers.NewUsersThisWeek,
		},
		Inventory: dto.InventoryKPI{
			LowStockCount: k.Inventory.LowStockCount,
			Threshold:     k.Inventory.Threshold,
		},
	}
}

// revenueTrend maps the daily series, deriving each point's day label.
func (m *Mapper) revenueTrend(points []model.RevenuePoint) []dto.RevenuePoint {
	out := make([]dto.RevenuePoint, 0, len(points))
	for _, point := range points {
		out = append(out, dto.RevenuePoint{
			Date:     point.Date,
			DayLabel: m.dayLabel(point.Date),
			Amount:   m.money(point.Amount),
		})
	}
	return out
}

// categoryBreakdown maps the category shares, preserving the caller's order.
func (m *Mapper) categoryBreakdown(shares []model.CategoryShare) []dto.CategoryShare {
	out := make([]dto.CategoryShare, 0, len(shares))
	for _, share := range shares {
		out = append(out, dto.CategoryShare{
			CategoryID:   share.CategoryID,
			CategoryName: share.CategoryName,
			Amount:       m.money(share.Amount),
			Percentage:   share.Percentage,
		})
	}
	return out
}

// recentOrders maps the newest orders. The customer summary is passed through as
// a nullable label; the line facts become the summary and the counts.
func (m *Mapper) recentOrders(orders []model.RecentOrder) []dto.RecentOrder {
	out := make([]dto.RecentOrder, 0, len(orders))
	for _, order := range orders {
		out = append(out, dto.RecentOrder{
			ID:           order.ID,
			Customer:     m.customer(order.Customer),
			ItemSummary:  itemSummary(order),
			ItemCount:    order.LineCount,
			ItemQuantity: order.TotalQuantity,
			Total:        m.money(order.Total),
			Status:       order.Status,
			CreatedAt:    order.CreatedAt,
		})
	}
	return out
}

// customer maps the nullable account label.
func (m *Mapper) customer(summary *model.CustomerSummary) *dto.CustomerSummary {
	if summary == nil {
		return nil
	}
	return &dto.CustomerSummary{
		ID:          summary.ID,
		DisplayName: summary.DisplayName,
		Email:       summary.Email,
	}
}

// money maps a domain money value; the two carry the same integer amount and
// currency.
func (m *Mapper) money(value model.Money) dto.Money {
	return dto.Money{Amount: value.Amount, Currency: value.Currency}
}

// dayLabel derives the short label for a YYYY-MM-DD day. US1 uses the DD/MM
// shape as a working placeholder; the day-of-week shape for a 7-day trend is
// owned by US2 (T038).
func (m *Mapper) dayLabel(date string) string {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return parsed.Format("02/01")
}

// itemSummary describes the newest order's first line. US1 uses the product name
// and quantity as a working placeholder; the variant part and the " +N" remainder
// are owned by US5 (T057).
func itemSummary(order model.RecentOrder) string {
	name := strings.TrimSpace(order.FirstLineName)
	if name == "" {
		return ""
	}
	if order.FirstLineQuantity > 0 {
		return fmt.Sprintf("%s (x%d)", name, order.FirstLineQuantity)
	}
	return name
}
