package implement

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// Service is the admin dashboard use case. It is a read-only orchestrator: it
// owns no data, opens no transaction and never mutates any capability. Every
// figure comes from one of the four cross-module read contracts, supplied by the
// owning modules at the composition root (Constitution I, FR-006, FR-028).
type Service struct {
	// Orders answers the order and revenue reads.
	Orders appinterface.OrderStatistics
	// Customers answers the customer reads.
	Customers appinterface.CustomerStatistics
	// Inventory answers the low-stock candidates.
	Inventory appinterface.InventoryStatistics
	// Variants answers the DISCONTINUED subset.
	Variants appinterface.ProductVariantStates
	// Clock reads the current instant; the dashboard reads it exactly once.
	Clock appinterface.Clock
	// Auditor records the single dashboard-view event.
	Auditor appinterface.Auditor
	// Config carries the shop timezone every calendar boundary uses.
	Config appinterface.Config
	// Mapper converts the assembled read model into application DTOs.
	Mapper *mapper.Mapper
}

// New creates the admin dashboard service.
func New(deps Service) *Service { return &deps }

// Overview assembles the whole dashboard in one read-only request.
//
// It reads the clock once, so every window, the trend's "today" and the audit
// agree on "now"; it then performs a fixed, bounded set of reads — one per
// section — and assembles the four KPI groups, the trend, the category breakdown
// and the newest orders. The exact window, growth, zero-fill, category, customer
// and recent-order rules are owned by US2–US5 (T037, T043, T050, T056); US1
// assembles a working shape around the basic current/previous calendar-month
// window.
func (s *Service) Overview(ctx context.Context, in dto.OverviewInput) (dto.Overview, error) {
	days := in.Days
	if days <= 0 {
		days = constant.DefaultDays
	}

	now := s.Clock.Now()
	location := s.location()

	// The US1 window is a deliberate placeholder: the current calendar month
	// against the full previous calendar month. US2 replaces it with the fair
	// month-to-date comparison and the clamped previous window (T037).
	currentWindow, previousWindow := monthWindows(now, location)
	weekWindow := weekWindow(now, location)
	trendWindow := trailingWindow(now, location, days)

	orderKPIs, err := s.Orders.KPIs(ctx, currentWindow, previousWindow)
	if err != nil {
		return dto.Overview{}, err
	}
	trend, err := s.Orders.DailyRevenue(ctx, trendWindow, location.String())
	if err != nil {
		return dto.Overview{}, err
	}
	categories, err := s.Orders.CategoryRevenue(ctx, currentWindow)
	if err != nil {
		return dto.Overview{}, err
	}
	recentOrders, err := s.Orders.RecentOrders(ctx, constant.RecentOrderLimit)
	if err != nil {
		return dto.Overview{}, err
	}
	customerKPIs, err := s.Customers.KPIs(ctx, weekWindow)
	if err != nil {
		return dto.Overview{}, err
	}
	candidates, err := s.Inventory.LowStockVariants(ctx, in.LowStockThreshold)
	if err != nil {
		return dto.Overview{}, err
	}
	lowStockCount, err := s.lowStockCount(ctx, candidates)
	if err != nil {
		return dto.Overview{}, err
	}

	overview := s.assemble(orderKPIs, trend, categories, recentOrders, customerKPIs, lowStockCount, in.LowStockThreshold)

	s.recordView(ctx, days, in.LowStockThreshold, len(recentOrders))

	return s.Mapper.Overview(overview), nil
}

// location is the shop timezone the calendar boundaries use. The composition
// always sets it; a nil value falls back to UTC rather than panicking.
func (s *Service) location() *time.Location {
	if s.Config.Location != nil {
		return s.Config.Location
	}
	return time.UTC
}

// recordView writes the single ADMIN_DASHBOARD_VIEWED event. The actor comes from
// the session; the metadata is only the request shape and the returned order
// count, so no PII ever reaches the audit trail (FR-030, Constitution VI).
func (s *Service) recordView(ctx context.Context, days int, threshold int64, recentOrderCount int) {
	var actorID *uuid.UUID
	var actorRole string
	if actor, ok := appinterface.ActorFromContext(ctx); ok {
		actorID = &actor.ID
		actorRole = string(actor.Role)
	}
	s.Auditor.Record(ctx, constant.AuditDashboardViewed, string(audit.OutcomeSuccess),
		actorID, actorRole,
		constant.TargetTypeDashboard, constant.TargetIDOverview,
		map[string]any{
			"days":              days,
			"lowStockThreshold": threshold,
			"recentOrderCount":  recentOrderCount,
		},
	)
}

// lowStockCount removes the DISCONTINUED variants from the candidates. The
// inventory contract already floored availability and dropped never-stocked
// variants; the product contract answers which ids are discontinued, so the
// dashboard counts without reading either module's table (FR-018, Constitution I).
func (s *Service) lowStockCount(ctx context.Context, candidates []contracts.LowStockVariant) (int64, error) {
	ids := make([]uuid.UUID, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.VariantID)
	}
	discontinued, err := s.Variants.DiscontinuedVariantIDs(ctx, ids)
	if err != nil {
		return 0, err
	}
	drop := make(map[uuid.UUID]struct{}, len(discontinued))
	for _, id := range discontinued {
		drop[id] = struct{}{}
	}
	var count int64
	for _, candidate := range candidates {
		if _, gone := drop[candidate.VariantID]; !gone {
			count++
		}
	}
	return count, nil
}

// assemble builds the admin read model from the contract reads. Growth, the
// trend zero-fill, the category percentage and the resolved customer label are
// left at their US1 placeholder values; US2–US5 fill them.
func (s *Service) assemble(
	kpis contracts.OrderKPIs,
	trend []contracts.DailyRevenue,
	categories []contracts.CategoryRevenue,
	recentOrders []contracts.RecentOrder,
	customers contracts.CustomerKPIs,
	lowStockCount int64,
	threshold int64,
) model.Overview {
	return model.Overview{
		KPIs: model.KPIs{
			Revenue: model.RevenueKPI{
				Current: money(kpis.CurrentRevenue),
				Last:    money(kpis.LastRevenue),
				// GrowthPercent is owned by US2 (T036/T037).
			},
			Orders: model.OrderKPI{
				Total:               kpis.TotalOrders,
				ThisMonth:           kpis.CurrentOrders,
				LastMonth:           kpis.LastOrders,
				PendingConfirmation: kpis.PendingConfirmation,
			},
			Customers: model.CustomerKPI{
				TotalUsers:       customers.TotalUsers,
				NewUsersThisWeek: customers.NewUsersThisWeek,
			},
			Inventory: model.InventoryKPI{
				LowStockCount: lowStockCount,
				Threshold:     threshold,
			},
		},
		RevenueTrend:      s.trend(trend),
		CategoryBreakdown: s.categoryBreakdown(categories),
		RecentOrders:      s.recentOrders(recentOrders),
	}
}

// trend maps the daily read. The trend has only the days the shop earned on; the
// zero-fill to exactly `days` points is owned by US2 (T037).
func (s *Service) trend(days []contracts.DailyRevenue) []model.RevenuePoint {
	out := make([]model.RevenuePoint, 0, len(days))
	for _, day := range days {
		out = append(out, model.RevenuePoint{
			Date:   day.Date,
			Amount: money(day.Amount),
		})
	}
	return out
}

// categoryBreakdown maps the category read. The unresolved bucket's label
// ("Khác"), the percentage and the ordering/filter are owned by US4 (T050).
func (s *Service) categoryBreakdown(categories []contracts.CategoryRevenue) []model.CategoryShare {
	out := make([]model.CategoryShare, 0, len(categories))
	for _, category := range categories {
		out = append(out, model.CategoryShare{
			CategoryID:   category.CategoryID,
			CategoryName: category.CategoryName,
			Amount:       money(category.Amount),
		})
	}
	return out
}

// recentOrders maps the newest orders. The resolved customer label and the full
// item summary are owned by US5 (T056/T057); US1 carries the raw line facts and
// lets the mapper format a basic summary.
func (s *Service) recentOrders(orders []contracts.RecentOrder) []model.RecentOrder {
	out := make([]model.RecentOrder, 0, len(orders))
	for _, order := range orders {
		out = append(out, model.RecentOrder{
			ID:                   order.ID,
			Total:                money(order.Total),
			Status:               order.Status,
			CreatedAt:            order.CreatedAt,
			FirstLineName:        order.FirstLineName,
			FirstLineVariantName: order.FirstLineVariantName,
			FirstLineQuantity:    order.FirstLineQuantity,
			LineCount:            order.LineCount,
			TotalQuantity:        order.TotalQuantity,
		})
	}
	return out
}

// money maps a contract money value into the domain money value.
func money(value contracts.Money) model.Money {
	return model.Money{Amount: value.Amount, Currency: value.Currency}
}

// monthWindows returns the US1 placeholder windows: the current calendar month up
// to the request instant, and the full previous calendar month. US2 replaces both
// with the month-to-date comparison and its clamp (T035/T037).
func monthWindows(now time.Time, location *time.Location) (contracts.TimeWindow, contracts.TimeWindow) {
	local := now.In(location)
	monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	previousStart := monthStart.AddDate(0, -1, 0)
	return contracts.TimeWindow{From: monthStart, To: now},
		contracts.TimeWindow{From: previousStart, To: monthStart}
}

// weekWindow returns the current week from Monday 00:00 in the shop timezone to
// the request instant (FR-009).
func weekWindow(now time.Time, location *time.Location) contracts.TimeWindow {
	local := now.In(location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	// Go's Weekday puts Sunday first; shift so Monday is 0.
	offset := (int(local.Weekday()) + 6) % 7
	return contracts.TimeWindow{From: dayStart.AddDate(0, 0, -offset), To: now}
}

// trailingWindow returns the `days` calendar days ending today, in the shop
// timezone, up to the request instant.
func trailingWindow(now time.Time, location *time.Location, days int) contracts.TimeWindow {
	if days < 1 {
		days = 1
	}
	local := now.In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return contracts.TimeWindow{From: today.AddDate(0, 0, -(days - 1)), To: now}
}

// The service must satisfy the module's own use-case interface.
var _ appinterface.OverviewService = (*Service)(nil)
