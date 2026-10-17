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
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/domain/model"
)

// Mapper converts admin read models into application DTOs. It carries no
// dependency: everything it needs is already in the read model.
type Mapper struct{}

// New creates a Mapper.
func New() *Mapper { return &Mapper{} }

// Overview maps the whole snapshot. An empty section maps to an empty slice,
// never to nil, so the response serialises arrays rather than null. days is the
// requested trend length, which chooses the day-label shape (FR-020).
func (m *Mapper) Overview(o model.Overview, days int) dto.Overview {
	return dto.Overview{
		KPIs:              m.kpis(o.KPIs),
		RevenueTrend:      m.revenueTrend(o.RevenueTrend, days),
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
func (m *Mapper) revenueTrend(points []model.RevenuePoint, days int) []dto.RevenuePoint {
	out := make([]dto.RevenuePoint, 0, len(points))
	for _, point := range points {
		out = append(out, dto.RevenuePoint{
			Date:     point.Date,
			DayLabel: m.dayLabel(point.Date, days),
			Amount:   m.money(point.Amount),
		})
	}
	return out
}

// categoryBreakdown maps the category shares, preserving the caller's order. A
// share whose category could not be resolved (nil id) is labelled "Khác"
// (FR-023); the percentage is carried through unchanged (FR-024).
func (m *Mapper) categoryBreakdown(shares []model.CategoryShare) []dto.CategoryShare {
	out := make([]dto.CategoryShare, 0, len(shares))
	for _, share := range shares {
		name := share.CategoryName
		if share.CategoryID == nil {
			name = constant.UnknownCategoryName
		}
		out = append(out, dto.CategoryShare{
			CategoryID:   share.CategoryID,
			CategoryName: name,
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

// dayLabel derives the short label for a YYYY-MM-DD day in the shop timezone. A
// 7-day trend shows the weekday as T2..T7/CN; a 30-day trend shows DD/MM
// (FR-020). A date that cannot be parsed is passed through unchanged. The weekday
// of a date is timezone-independent, so parsing as UTC yields the operator's day
// of week.
func (m *Mapper) dayLabel(date string, days int) string {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	if days == 7 {
		return weekdayLabel(parsed.Weekday())
	}
	return parsed.Format("02/01")
}

// weekdayLabel is the trend label of a weekday: Vietnamese T2..T7 for Monday to
// Saturday and CN for Sunday (FR-020).
func weekdayLabel(day time.Weekday) string {
	switch day {
	case time.Monday:
		return "T2"
	case time.Tuesday:
		return "T3"
	case time.Wednesday:
		return "T4"
	case time.Thursday:
		return "T5"
	case time.Friday:
		return "T6"
	case time.Saturday:
		return "T7"
	default:
		return "CN"
	}
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
