package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/domain/constant"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/httpx"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// Handler adapts the admin dashboard use case to HTTP. It carries no business
// rule: it reads the session, parses the query, calls the use case and maps the
// answer onto the shared envelope (Constitution I).
type Handler struct {
	svc    appinterface.OverviewService
	logger *slog.Logger
}

// New creates the admin HTTP handler.
func New(svc appinterface.OverviewService, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// Overview returns the whole dashboard in one response.
//
// It is reached only behind the administrator role guard, and the acting
// administrator comes from the authenticated session the middleware resolved,
// never from the request (Constitution V). The whole request is read-only (FR-002,
// FR-006).
//
//	@Summary		Read the admin dashboard overview (ADMIN)
//	@Description	Returns every dashboard section in one response: the revenue, order, customer and inventory KPIs, a daily revenue trend, the current-month category breakdown and the five newest orders. Revenue counts PAID, SHIPPED and COMPLETED orders in VND, attributed by the order creation instant; calendar boundaries use the configured shop timezone. The whole request is read-only.
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			days				query	int	false	"Trend length in trailing calendar days (7 or 30; default 7)"	Enums(7,30)
//	@Param			lowStockThreshold	query	int	false	"Inventory warning threshold (0..10000; default 5)"	minimum(0)	maximum(10000)
//	@Success		200					{object}	httpx.SwaggerSuccess{data=httpdto.OverviewResponse}
//	@Failure		400					{object}	httpx.SwaggerError
//	@Failure		401					{object}	httpx.SwaggerError
//	@Failure		403					{object}	httpx.SwaggerError
//	@Failure		429					{object}	httpx.SwaggerError
//	@Failure		500					{object}	httpx.SwaggerError
//	@Router			/admin/dashboard/overview [get]
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.sessionActor(w, r)
	if !ok {
		return
	}
	days, err := daysParam(r)
	if err != nil {
		httpx.WriteError(w, r, err, h.logger)
		return
	}
	threshold, err := lowStockThresholdParam(r)
	if err != nil {
		httpx.WriteError(w, r, err, h.logger)
		return
	}
	ctx := appinterface.WithActor(r.Context(), actor)
	out, err := h.svc.Overview(ctx, appdto.OverviewInput{
		Days:              days,
		LowStockThreshold: threshold,
	})
	if err != nil {
		httpx.WriteError(w, r, err, h.logger)
		return
	}
	httpx.WriteSuccess(w, r, http.StatusOK, toOverviewResponse(out))
}

// sessionActor returns the acting account taken from the authenticated session.
//
// No route accepts an owner identifier: the session is the only source of the
// acting account, which is what makes an actor chosen by a client impossible by
// construction (Constitution V). A missing or unparseable identity answers
// UNAUTHENTICATED.
func (h *Handler) sessionActor(w http.ResponseWriter, r *http.Request) (appinterface.Actor, bool) {
	identity, ok := middleware.IdentityFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, httpx.New(httpx.CodeUnauthenticated), h.logger)
		return appinterface.Actor{}, false
	}
	accountID, err := uuid.Parse(identity.Subject)
	if err != nil {
		httpx.WriteError(w, r, httpx.New(httpx.CodeUnauthenticated), h.logger)
		return appinterface.Actor{}, false
	}
	return appinterface.Actor{ID: accountID, Role: access.Role(identity.Role)}, true
}

// daysParam reads and validates the trend length. An absent value answers the
// default; a present value must be one of the allowed lengths, and anything else
// answers a field-named VALIDATION_ERROR rather than silently falling back
// (FR-003).
func daysParam(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("days")
	if raw == "" {
		return constant.DefaultDays, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, invalidParam("days", "must be 7 or 30")
	}
	for _, allowed := range constant.AllowedDays {
		if parsed == allowed {
			return parsed, nil
		}
	}
	return 0, invalidParam("days", "must be 7 or 30")
}

// lowStockThresholdParam reads and validates the inventory warning threshold. An
// absent value answers the default; a present value must fall inside the accepted
// range, and anything else answers a field-named VALIDATION_ERROR (FR-004).
func lowStockThresholdParam(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("lowStockThreshold")
	if raw == "" {
		return constant.DefaultLowStockThreshold, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, invalidParam("lowStockThreshold", "must be between 0 and 10000")
	}
	if parsed < constant.MinLowStockThreshold || parsed > constant.MaxLowStockThreshold {
		return 0, invalidParam("lowStockThreshold", "must be between 0 and 10000")
	}
	return parsed, nil
}

// invalidParam builds the field-named validation error a rejected query value
// answers with. The detail names the request member and the accepted range only;
// it never carries a storage, table or parse detail (FR-031,
// contracts/error-codes.md).
func invalidParam(field, issue string) *httpx.AppError {
	return httpx.NewWithDetails(httpx.CodeValidation, httpx.Detail{Field: field, Issue: issue})
}

// toOverviewResponse is the single conversion point from the application DTO to
// the HTTP shape. It always allocates on a list, so an empty section serialises
// as an array rather than null (FR-002).
func toOverviewResponse(o appdto.Overview) httpdto.OverviewResponse {
	return httpdto.OverviewResponse{
		KPIs:              toKPIResponse(o.KPIs),
		RevenueTrend:      toRevenuePointResponses(o.RevenueTrend),
		CategoryBreakdown: toCategoryShareResponses(o.CategoryBreakdown),
		RecentOrders:      toRecentOrderResponses(o.RecentOrders),
	}
}

func toKPIResponse(k appdto.KPIs) httpdto.KPIResponse {
	return httpdto.KPIResponse{
		Revenue: httpdto.RevenueKPIResponse{
			CurrentMonthAmount: toMoneyResponse(k.Revenue.Current),
			LastMonthAmount:    toMoneyResponse(k.Revenue.Last),
			GrowthPercent:      k.Revenue.GrowthPercent,
		},
		Orders: httpdto.OrderKPIResponse{
			TotalOrders:              k.Orders.Total,
			ThisMonthOrders:          k.Orders.ThisMonth,
			LastMonthOrders:          k.Orders.LastMonth,
			PendingConfirmationCount: k.Orders.PendingConfirmation,
			GrowthPercent:            k.Orders.GrowthPercent,
		},
		Customers: httpdto.CustomerKPIResponse{
			TotalUsers:       k.Customers.TotalUsers,
			NewUsersThisWeek: k.Customers.NewUsersThisWeek,
		},
		Inventory: httpdto.InventoryKPIResponse{
			LowStockCount: k.Inventory.LowStockCount,
			Threshold:     k.Inventory.Threshold,
		},
	}
}

func toMoneyResponse(m appdto.Money) httpdto.MoneyResponse {
	return httpdto.MoneyResponse{Amount: m.Amount, Currency: m.Currency}
}

func toRevenuePointResponses(points []appdto.RevenuePoint) []httpdto.RevenuePointResponse {
	out := make([]httpdto.RevenuePointResponse, 0, len(points))
	for _, point := range points {
		out = append(out, httpdto.RevenuePointResponse{
			Date:     point.Date,
			DayLabel: point.DayLabel,
			Amount:   toMoneyResponse(point.Amount),
		})
	}
	return out
}

func toCategoryShareResponses(shares []appdto.CategoryShare) []httpdto.CategoryShareResponse {
	out := make([]httpdto.CategoryShareResponse, 0, len(shares))
	for _, share := range shares {
		out = append(out, httpdto.CategoryShareResponse{
			CategoryID:   share.CategoryID,
			CategoryName: share.CategoryName,
			Amount:       toMoneyResponse(share.Amount),
			Percentage:   share.Percentage,
		})
	}
	return out
}

func toRecentOrderResponses(orders []appdto.RecentOrder) []httpdto.RecentOrderResponse {
	out := make([]httpdto.RecentOrderResponse, 0, len(orders))
	for _, order := range orders {
		out = append(out, httpdto.RecentOrderResponse{
			ID:           order.ID,
			Customer:     toCustomerSummaryResponse(order.Customer),
			ItemSummary:  order.ItemSummary,
			ItemCount:    order.ItemCount,
			ItemQuantity: order.ItemQuantity,
			Total:        toMoneyResponse(order.Total),
			Status:       order.Status,
			CreatedAt:    order.CreatedAt,
		})
	}
	return out
}

func toCustomerSummaryResponse(summary *appdto.CustomerSummary) *httpdto.CustomerSummaryResponse {
	if summary == nil {
		return nil
	}
	return &httpdto.CustomerSummaryResponse{
		ID:          summary.ID,
		DisplayName: summary.DisplayName,
		Email:       summary.Email,
	}
}
