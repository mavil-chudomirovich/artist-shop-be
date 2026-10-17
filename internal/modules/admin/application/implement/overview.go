package implement

import (
	"context"
	"sort"
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
// and the newest orders. The windows are the fair shop-timezone ones from the
// domain: month-to-date against the same previous-month period, a Monday week and
// the trailing trend days. US2 owns the windows, growth and the trend zero-fill;
// US3 owns the low-stock filter and the customer KPIs. The category and
// recent-order composition rules remain owned by US4/US5 (T050, T056).
func (s *Service) Overview(ctx context.Context, in dto.OverviewInput) (dto.Overview, error) {
	days := in.Days
	if days <= 0 {
		days = constant.DefaultDays
	}

	now := s.Clock.Now()
	location := s.location()

	currentWindow := model.CurrentMonth(now, location)
	previousWindow := model.PreviousMonthSamePeriod(now, location)
	weekWindow := model.CurrentWeek(now, location)
	trendWindow := model.TrailingDays(now, location, days)

	orderKPIs, err := s.Orders.KPIs(ctx, contractWindow(currentWindow), contractWindow(previousWindow))
	if err != nil {
		return dto.Overview{}, err
	}
	trend, err := s.Orders.DailyRevenue(ctx, contractWindow(trendWindow), location.String())
	if err != nil {
		return dto.Overview{}, err
	}
	categories, err := s.Orders.CategoryRevenue(ctx, contractWindow(currentWindow))
	if err != nil {
		return dto.Overview{}, err
	}
	recentOrders, err := s.Orders.RecentOrders(ctx, constant.RecentOrderLimit)
	if err != nil {
		return dto.Overview{}, err
	}
	// Resolve the recent orders' customers in one bulk read, so a deleted
	// account becomes a null customer instead of breaking the response
	// (FR-026, research D8). Distinct ids are asked for once.
	customerIDs := make([]uuid.UUID, 0, len(recentOrders))
	seenCustomers := make(map[uuid.UUID]struct{}, len(recentOrders))
	for _, order := range recentOrders {
		if _, ok := seenCustomers[order.CustomerID]; ok {
			continue
		}
		seenCustomers[order.CustomerID] = struct{}{}
		customerIDs = append(customerIDs, order.CustomerID)
	}
	customerSummaries, err := s.Customers.CustomerSummaries(ctx, customerIDs)
	if err != nil {
		return dto.Overview{}, err
	}
	customersByID := make(map[uuid.UUID]contracts.CustomerSummary, len(customerSummaries))
	for _, summary := range customerSummaries {
		customersByID[summary.ID] = summary
	}
	customerKPIs, err := s.Customers.KPIs(ctx, contractWindow(weekWindow))
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

	overview := s.assemble(trendWindow.Dates(), orderKPIs, trend, categories, recentOrders, customersByID, customerKPIs, lowStockCount, in.LowStockThreshold)

	s.recordView(ctx, days, in.LowStockThreshold, len(recentOrders))

	return s.Mapper.Overview(overview, days), nil
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

// assemble builds the admin read model from the contract reads. Growth is the
// nullable domain arithmetic; the trend is zero-filled over `dates`, the exact
// days the trend window names (FR-011, FR-013, FR-019). The category percentage
// and the resolved customer label remain US4/US5 placeholders.
func (s *Service) assemble(
	dates []string,
	kpis contracts.OrderKPIs,
	trend []contracts.DailyRevenue,
	categories []contracts.CategoryRevenue,
	recentOrders []contracts.RecentOrder,
	customersByID map[uuid.UUID]contracts.CustomerSummary,
	customers contracts.CustomerKPIs,
	lowStockCount int64,
	threshold int64,
) model.Overview {
	return model.Overview{
		KPIs: model.KPIs{
			Revenue: model.RevenueKPI{
				Current:       money(kpis.CurrentRevenue),
				Last:          money(kpis.LastRevenue),
				GrowthPercent: model.GrowthPercent(kpis.CurrentRevenue.Amount, kpis.LastRevenue.Amount),
			},
			Orders: model.OrderKPI{
				Total:               kpis.TotalOrders,
				ThisMonth:           kpis.CurrentOrders,
				LastMonth:           kpis.LastOrders,
				PendingConfirmation: kpis.PendingConfirmation,
				GrowthPercent:       model.GrowthPercent(kpis.CurrentOrders, kpis.LastOrders),
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
		RevenueTrend:      s.trend(dates, trend),
		CategoryBreakdown: s.categoryBreakdown(categories, kpis.CurrentRevenue.Amount),
		RecentOrders:      s.recentOrders(recentOrders, customersByID),
	}
}

// trend maps the daily read onto the exact calendar days the trend window names.
// A day the shop earned nothing on is absent from the read, so it is zero-filled
// with the shop currency: the series always has one point per day, oldest first,
// and every point is a money object (FR-019, FR-005).
func (s *Service) trend(dates []string, days []contracts.DailyRevenue) []model.RevenuePoint {
	byDate := make(map[string]contracts.Money, len(days))
	for _, day := range days {
		byDate[day.Date] = day.Amount
	}
	out := make([]model.RevenuePoint, 0, len(dates))
	for _, date := range dates {
		amount, ok := byDate[date]
		if !ok {
			amount = contracts.Money{Currency: currencyVND}
		}
		out = append(out, model.RevenuePoint{
			Date:   date,
			Amount: money(amount),
		})
	}
	return out
}

// categoryBreakdown maps the category read into the admin read model: it names
// the unresolved bucket "Khác", computes each share's percentage against the
// KPI current-month revenue, drops categories with no revenue, and sorts by
// amount descending, then name ascending, then id (FR-021, FR-023, FR-024).
func (s *Service) categoryBreakdown(categories []contracts.CategoryRevenue, currentRevenue int64) []model.CategoryShare {
	out := make([]model.CategoryShare, 0, len(categories))
	for _, category := range categories {
		name := category.CategoryName
		if category.CategoryID == nil {
			name = constant.UnknownCategoryName
		}
		out = append(out, model.CategoryShare{
			CategoryID:   category.CategoryID,
			CategoryName: name,
			Amount:       money(category.Amount),
			Percentage:   model.Percentage(category.Amount.Amount, currentRevenue),
		})
	}

	// Keep only categories that actually earned revenue (FR-024).
	filtered := out[:0]
	for _, share := range out {
		if share.Amount.Amount > 0 {
			filtered = append(filtered, share)
		}
	}
	out = filtered

	// Sort by amount desc, then name asc, then id (nil last) (FR-024).
	// Use a stable sort so equal keys keep their input order.
	// (import "sort")
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Amount.Amount != out[j].Amount.Amount {
			return out[i].Amount.Amount > out[j].Amount.Amount
		}
		if out[i].CategoryName != out[j].CategoryName {
			return out[i].CategoryName < out[j].CategoryName
		}
		// then id: nil sorts last
		idI, idJ := out[i].CategoryID, out[j].CategoryID
		if idI == nil && idJ == nil {
			return false
		}
		if idI == nil {
			return false
		}
		if idJ == nil {
			return true
		}
		return idI.String() < idJ.String()
	})

	return out
}

// recentOrders maps the newest orders, resolving each customer from the bulk
// read (nil when the account no longer exists) (FR-026, US5).
func (s *Service) recentOrders(orders []contracts.RecentOrder, customersByID map[uuid.UUID]contracts.CustomerSummary) []model.RecentOrder {
	out := make([]model.RecentOrder, 0, len(orders))
	for _, order := range orders {
		var customer *model.CustomerSummary
		if summary, ok := customersByID[order.CustomerID]; ok {
			c := model.CustomerSummary{ID: summary.ID, DisplayName: summary.DisplayName, Email: summary.Email}
			customer = &c
		}
		out = append(out, model.RecentOrder{
			ID:                   order.ID,
			Customer:             customer,
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

// currencyVND is the only currency the MVP shop trades in; a day the read does
// not carry is zero-filled in it, so every trend point is a VND money object
// (spec Assumptions "MVP chỉ VND").
const currencyVND = "VND"

// contractWindow converts the domain window into the cross-module read window.
// The two carry the same half-open instants; the conversion keeps the domain free
// of the contract package (Constitution I).
func contractWindow(w model.Window) contracts.TimeWindow {
	return contracts.TimeWindow{From: w.From, To: w.To}
}

// The service must satisfy the module's own use-case interface.
var _ appinterface.OverviewService = (*Service)(nil)
