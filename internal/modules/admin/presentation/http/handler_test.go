package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/interface"
	httpdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/presentation/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file exercises the US1 transport: an administrator reaches the overview
// and the answer travels in the shared {data, meta} envelope with the documented
// JSON member names. The guest/customer boundaries and the invalid-parameter
// cases are owned by US6.

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

var testAdminID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

// stubOverviewService answers the one use case with controlled data and records
// the actor the handler placed in the context.
type stubOverviewService struct {
	out       appdto.Overview
	err       error
	called    bool
	lastInput appdto.OverviewInput
	seenActor appinterface.Actor
}

func (s *stubOverviewService) Overview(ctx context.Context, in appdto.OverviewInput) (appdto.Overview, error) {
	s.called = true
	s.lastInput = in
	s.seenActor, _ = appinterface.ActorFromContext(ctx)
	return s.out, s.err
}

var _ appinterface.OverviewService = (*stubOverviewService)(nil)

func sampleOverview() appdto.Overview {
	categoryID := uuid.New()
	orderID := uuid.New()
	customerID := uuid.New()
	return appdto.Overview{
		KPIs: appdto.KPIs{
			Revenue: appdto.RevenueKPI{
				Current: appdto.Money{Amount: 48250000, Currency: "VND"},
				Last:    appdto.Money{Amount: 42000000, Currency: "VND"},
			},
			Orders: appdto.OrderKPI{
				Total:               1482,
				ThisMonth:           186,
				LastMonth:           172,
				PendingConfirmation: 9,
			},
			Customers: appdto.CustomerKPI{TotalUsers: 412, NewUsersThisWeek: 28},
			Inventory: appdto.InventoryKPI{LowStockCount: 3, Threshold: 5},
		},
		RevenueTrend: []appdto.RevenuePoint{
			{Date: "2026-10-16", DayLabel: "16/10", Amount: appdto.Money{Amount: 4200000, Currency: "VND"}},
		},
		CategoryBreakdown: []appdto.CategoryShare{
			{CategoryID: &categoryID, CategoryName: "Acrylic Stand", Amount: appdto.Money{Amount: 22195000, Currency: "VND"}, Percentage: 46.0},
		},
		RecentOrders: []appdto.RecentOrder{
			{
				ID:           orderID,
				Customer:     &appdto.CustomerSummary{ID: customerID, DisplayName: "Nguyễn Minh Anh", Email: "minhanh.ng@gmail.com"},
				ItemSummary:  "Acrylic Stand Elf Băng Giá (x2) +1",
				ItemCount:    2,
				ItemQuantity: 3,
				Total:        appdto.Money{Amount: 360000, Currency: "VND"},
				Status:       "COMPLETED",
				CreatedAt:    time.Date(2026, time.October, 17, 4, 2, 0, 0, time.UTC),
			},
		},
	}
}

func adminOverviewHooks() middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			if r.Header.Get("Authorization") == "Bearer admin-token" {
				return &middleware.Identity{Subject: testAdminID.String(), Role: string(access.RoleAdmin), TokenID: "admin"}, nil
			}
			return nil, nil
		},
	}
}

func newOverviewRouter(svc appinterface.OverviewService, hooks middleware.AuthHooks) http.Handler {
	handler := New(svc, testLogger)
	root := chi.NewRouter()
	// The shared pipeline supplies the request correlation the envelope echoes.
	root.Use(middleware.Correlation)
	root.Mount("/api/v1/admin/dashboard", handler.AdminRouter(hooks))
	return root
}

func performOverview(handler http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard/overview", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// FR-002: an administrator's request answers 200 with the data section under the
// shared envelope.
func TestOverviewReturnsTheEnvelopeForAnAdministrator(t *testing.T) {
	svc := &stubOverviewService{out: sampleOverview()}

	rec := performOverview(newOverviewRouter(svc, adminOverviewHooks()), "admin-token")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !svc.called {
		t.Fatal("the administrator route must reach the overview use case")
	}
	if svc.seenActor.ID != testAdminID || svc.seenActor.Role != access.RoleAdmin {
		t.Fatalf("the handler must hand the session actor to the use case, got %+v", svc.seenActor)
	}

	var body struct {
		Data httpdto.OverviewResponse `json:"data"`
		Meta struct {
			RequestID string `json:"requestId"`
			Timestamp string `json:"timestamp"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the overview body %q: %v", rec.Body.String(), err)
	}
	if body.Meta.RequestID == "" || body.Meta.Timestamp == "" {
		t.Fatalf("the envelope meta is incomplete: %+v", body.Meta)
	}
	if body.Data.KPIs.Revenue.CurrentMonthAmount.Amount != 48250000 {
		t.Fatalf("the data section did not travel: %+v", body.Data)
	}
}

// The documented JSON member names are the wire contract: money objects,
// growthPercent/customer/categoryId nullability, and the count members.
func TestOverviewSerialisesTheDocumentedMemberNames(t *testing.T) {
	svc := &stubOverviewService{out: sampleOverview()}

	rec := performOverview(newOverviewRouter(svc, adminOverviewHooks()), "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var raw struct {
		Data map[string]json.RawMessage `json:"data"`
		Meta map[string]json.RawMessage `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode the raw body %q: %v", rec.Body.String(), err)
	}
	for _, member := range []string{"kpis", "revenueTrend", "categoryBreakdown", "recentOrders"} {
		if _, ok := raw.Data[member]; !ok {
			t.Fatalf("data is missing %q: %v", member, rec.Body.String())
		}
	}
	if _, ok := raw.Meta["requestId"]; !ok {
		t.Fatalf("meta is missing requestId: %s", rec.Body.String())
	}

	// The trend point carries date, dayLabel and a money object.
	var trend []map[string]json.RawMessage
	if err := json.Unmarshal(raw.Data["revenueTrend"], &trend); err != nil {
		t.Fatalf("decode revenueTrend: %v", err)
	}
	if len(trend) != 1 {
		t.Fatalf("expected one trend point, got %d", len(trend))
	}
	for _, member := range []string{"date", "dayLabel", "amount"} {
		if _, ok := trend[0][member]; !ok {
			t.Fatalf("a trend point is missing %q: %v", member, trend[0])
		}
	}

	// The recent order carries the count members and a nullable customer.
	var recents []map[string]json.RawMessage
	if err := json.Unmarshal(raw.Data["recentOrders"], &recents); err != nil {
		t.Fatalf("decode recentOrders: %v", err)
	}
	for _, member := range []string{"id", "customer", "itemSummary", "itemCount", "itemQuantity", "total", "status", "createdAt"} {
		if _, ok := recents[0][member]; !ok {
			t.Fatalf("a recent order is missing %q: %v", member, recents[0])
		}
	}
}

// An empty dashboard is still a valid envelope: the sections are arrays, never
// null, and the money objects stay present.
func TestOverviewSerialisesEmptySectionsAsArrays(t *testing.T) {
	svc := &stubOverviewService{out: appdto.Overview{
		KPIs: appdto.KPIs{
			Revenue:   appdto.RevenueKPI{Current: appdto.Money{Currency: "VND"}, Last: appdto.Money{Currency: "VND"}},
			Inventory: appdto.InventoryKPI{Threshold: 5},
		},
	}}

	rec := performOverview(newOverviewRouter(svc, adminOverviewHooks()), "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, member := range []string{"\"revenueTrend\":[]", "\"categoryBreakdown\":[]", "\"recentOrders\":[]"} {
		if !strings.Contains(body, member) {
			t.Fatalf("an empty section must serialise as an array: %s", body)
		}
	}
}
