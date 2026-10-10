package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// This file is the transport contract of US4: `GET /admin/orders`,
// `GET /admin/orders/{id}`, `POST /admin/orders/{id}/ship` and
// `POST /admin/orders/{id}/complete` are all guarded by the administrator role,
// a customer's session is refused `403 FORBIDDEN`, an unknown identifier answers
// `404 ORDER_NOT_FOUND`, and an illegal move answers
// `409 ORDER_STATE_TRANSITION_INVALID`. It mounts the routes the way the
// composition root mounts them, over a fake of the use case, so the answers
// asserted are the handler's own (contracts/openapi.yaml, contracts/error-codes.md).

const adminOrdersPath = "/api/v1/admin/orders"

// fakeAdmin is a fake of the handler's service covering the administrator-order
// routes. It records the actor the handler put in the context and the input it
// was handed, and answers the fixed results each test configures. It embeds the
// customer-order fake so a single value satisfies the whole service surface.
type fakeAdmin struct {
	fakeOrders

	list    appdto.AdminOrderPage
	listErr error

	detail    appdto.AdminOrderView
	detailErr error

	shipView     appdto.AdminOrderView
	shipErr      error
	completeView appdto.AdminOrderView
	completeErr  error

	gotListInput  appdto.ListInput
	gotListActor  appinterface.Actor
	gotRefInput   appdto.OrderRefInput
	gotRefActor   appinterface.Actor
	shipCalls     int
	completeCalls int
}

func (f *fakeAdmin) ListAll(ctx context.Context, in appdto.ListInput) (appdto.AdminOrderPage, error) {
	f.gotListInput = in
	f.gotListActor, _ = appinterface.ActorFromContext(ctx)
	return f.list, f.listErr
}

func (f *fakeAdmin) GetByIDAdmin(ctx context.Context, in appdto.OrderRefInput) (appdto.AdminOrderView, error) {
	f.gotRefInput = in
	f.gotRefActor, _ = appinterface.ActorFromContext(ctx)
	return f.detail, f.detailErr
}

func (f *fakeAdmin) ShipByAdmin(ctx context.Context, in appdto.OrderRefInput) (appdto.AdminOrderView, error) {
	f.gotRefInput = in
	f.gotRefActor, _ = appinterface.ActorFromContext(ctx)
	f.shipCalls++
	return f.shipView, f.shipErr
}

func (f *fakeAdmin) CompleteByAdmin(ctx context.Context, in appdto.OrderRefInput) (appdto.AdminOrderView, error) {
	f.gotRefInput = in
	f.gotRefActor, _ = appinterface.ActorFromContext(ctx)
	f.completeCalls++
	return f.completeView, f.completeErr
}

// adminSummaryData is one row of the administrator's order list as the contract
// shows it: the customer summary plus the owner.
type adminSummaryData struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"userId"`
	Status    string    `json:"status"`
	Total     moneyBody `json:"total"`
	ItemCount int64     `json:"itemCount"`
	CreatedAt string    `json:"createdAt"`
}

// adminListBody decodes the paginated administrator list answer.
type adminListBody struct {
	Data []adminSummaryData `json:"data"`
	Meta struct {
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
		Total    int64 `json:"total"`
	} `json:"meta"`
}

// adminDetailBody decodes the administrator detail answer.
type adminDetailBody struct {
	Data struct {
		adminSummaryData
		Address struct {
			RecipientName string `json:"recipientName"`
			StreetAddress string `json:"streetAddress"`
		} `json:"address"`
		Lines []struct {
			ProductID uuid.UUID `json:"productId"`
			Quantity  int64     `json:"quantity"`
		} `json:"lines"`
	} `json:"data"`
}

// newOrderAdminRouter mounts the administrator group the way the composition root
// mounts it.
func newOrderAdminRouter(svc Service) http.Handler {
	root := chi.NewRouter()
	root.Mount(adminOrdersPath, New(svc, testLogger).AdminRouter(orderHooks()))
	return root
}

// sampleAdminView is one order view as the administrator use case answers it.
func sampleAdminView() appdto.AdminOrderView {
	base := sampleView()
	return appdto.AdminOrderView{
		AdminOrderSummaryView: appdto.AdminOrderSummaryView{
			OrderSummaryView: base.OrderSummaryView,
			UserID:           testCustomerID,
		},
		Address: base.Address,
		Lines:   base.Lines,
	}
}

// FR-021: the administrator list returns every order with its owner, state and
// total, and the actor the handler passes is the session's.
func TestAdminListReturnsEveryOrderWithItsOwner(t *testing.T) {
	row := sampleAdminView()
	fake := &fakeAdmin{
		list: appdto.AdminOrderPage{
			Orders:   []appdto.AdminOrderSummaryView{row.AdminOrderSummaryView},
			Page:     1,
			PageSize: 20,
			Total:    1,
		},
	}
	router := newOrderAdminRouter(fake)

	rec := perform(router, http.MethodGet, adminOrdersPath+"?page=1&pageSize=20", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body adminListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the admin list body %q: %v", rec.Body.String(), err)
	}
	if len(body.Data) != 1 || body.Data[0].UserID != testCustomerID || body.Data[0].Status != "PAYMENT_PENDING" {
		t.Fatalf("unexpected admin list: %+v", body.Data)
	}
	if body.Meta.Page != 1 || body.Meta.PageSize != 20 || body.Meta.Total != 1 {
		t.Fatalf("unexpected meta: %+v", body.Meta)
	}
	if fake.gotListInput.Page != 1 || fake.gotListInput.PageSize != 20 {
		t.Fatalf("the window must be passed through, got %+v", fake.gotListInput)
	}
	if fake.gotListActor.ID != testAdminID {
		t.Fatalf("the handler must hand the session's account to the use case, got %s", fake.gotListActor.ID)
	}
}

// FR-021: the administrator reads any order in full, with the session's actor and
// the addressed identifier passed through.
func TestAdminDetailReturnsAnyOrderInFull(t *testing.T) {
	view := sampleAdminView()
	fake := &fakeAdmin{detail: view}
	router := newOrderAdminRouter(fake)

	rec := perform(router, http.MethodGet, adminOrdersPath+"/"+view.ID.String(), "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the admin order body %q: %v", rec.Body.String(), err)
	}
	if body.Data.ID != view.ID || body.Data.UserID != testCustomerID || len(body.Data.Lines) != 1 {
		t.Fatalf("unexpected order: %+v", body.Data)
	}
	if fake.gotRefInput.OrderID != view.ID {
		t.Fatalf("the addressed identifier must be passed through, got %s", fake.gotRefInput.OrderID)
	}
	if fake.gotRefActor.ID != testAdminID {
		t.Fatalf("the handler must hand the session's account to the use case, got %s", fake.gotRefActor.ID)
	}
}

// FR-022: ship then complete answer 200 with the advanced order.
func TestAdminShipAndCompleteReturnTheAdvancedOrder(t *testing.T) {
	shipped := sampleAdminView()
	shipped.Status = constant.StatusShipped
	completed := sampleAdminView()
	completed.Status = constant.StatusCompleted
	fake := &fakeAdmin{shipView: shipped, completeView: completed}
	router := newOrderAdminRouter(fake)

	rec := perform(router, http.MethodPost, adminOrdersPath+"/"+shipped.ID.String()+"/ship", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("ship: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var shipBody adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &shipBody); err != nil {
		t.Fatalf("decode the shipped order %q: %v", rec.Body.String(), err)
	}
	if shipBody.Data.Status != "SHIPPED" {
		t.Fatalf("the shipped order must answer SHIPPED, got %s", shipBody.Data.Status)
	}

	rec = perform(router, http.MethodPost, adminOrdersPath+"/"+completed.ID.String()+"/complete", "", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var completeBody adminDetailBody
	if err := json.Unmarshal(rec.Body.Bytes(), &completeBody); err != nil {
		t.Fatalf("decode the completed order %q: %v", rec.Body.String(), err)
	}
	if completeBody.Data.Status != "COMPLETED" {
		t.Fatalf("the completed order must answer COMPLETED, got %s", completeBody.Data.Status)
	}
	if fake.gotRefActor.ID != testAdminID || fake.gotRefInput.OrderID != completed.ID {
		t.Fatalf("the session's actor and the addressed id must be passed through, got %+v / %+v", fake.gotRefActor, fake.gotRefInput)
	}
}

// contracts/error-codes.md: an illegal move answers 409
// ORDER_STATE_TRANSITION_INVALID.
func TestAdminMoveRefusesAnIllegalMove(t *testing.T) {
	fake := &fakeAdmin{shipErr: domainerr.StateTransitionInvalid(constant.StatusPaymentPending, constant.StatusShipped)}
	router := newOrderAdminRouter(fake)

	rec := perform(router, http.MethodPost, adminOrdersPath+"/"+uuid.New().String()+"/ship", "", "admin-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != constant.CodeStateTransitionInvalid {
		t.Fatalf("expected ORDER_STATE_TRANSITION_INVALID, got %s", body.Error.Code)
	}
}

// contracts/error-codes.md: an unknown order answers 404 ORDER_NOT_FOUND.
func TestAdminDetailAnswersNotFoundForAnUnknownOrder(t *testing.T) {
	fake := &fakeAdmin{detailErr: domainerr.ErrNotFound}
	router := newOrderAdminRouter(fake)

	rec := perform(router, http.MethodGet, adminOrdersPath+"/"+uuid.New().String(), "", "admin-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != constant.CodeOrderNotFound {
		t.Fatalf("expected ORDER_NOT_FOUND, got %s", body.Error.Code)
	}
}

// FR-023, contracts/error-codes.md: a customer's session is refused 403 FORBIDDEN
// on every administrator route, before reaching the use case.
func TestAdminRoutesRefuseACustomerSession(t *testing.T) {
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, adminOrdersPath},
		{http.MethodGet, adminOrdersPath + "/" + uuid.New().String()},
		{http.MethodPost, adminOrdersPath + "/" + uuid.New().String() + "/ship"},
		{http.MethodPost, adminOrdersPath + "/" + uuid.New().String() + "/complete"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			fake := &fakeAdmin{}
			router := newOrderAdminRouter(fake)
			rec := perform(router, tc.method, tc.path, "", "customer-token")
			if rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d (%s)", rec.Code, rec.Body.String())
			}
			if body := decodeError(t, rec); body.Error.Code != "FORBIDDEN" {
				t.Fatalf("expected FORBIDDEN, got %s", body.Error.Code)
			}
			if fake.shipCalls != 0 || fake.completeCalls != 0 || fake.gotRefInput.OrderID != uuid.Nil {
				t.Fatal("a forbidden request must not reach the use case")
			}
		})
	}
}

// contracts/error-codes.md: an administrator route with no session answers 401
// UNAUTHENTICATED, before reaching the use case.
func TestAdminRoutesRefuseWithoutASession(t *testing.T) {
	fake := &fakeAdmin{}
	router := newOrderAdminRouter(fake)

	rec := perform(router, http.MethodGet, adminOrdersPath, "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "UNAUTHENTICATED" {
		t.Fatalf("expected UNAUTHENTICATED, got %s", body.Error.Code)
	}
	if fake.gotListInput.Page != 0 {
		t.Fatal("an unauthenticated request must not reach the use case")
	}
}

// contracts/openapi.yaml: a malformed order identifier is 400 VALIDATION_ERROR
// naming `orderId`, not a lookup that returns nothing.
func TestAdminRoutesRejectAMalformedOrderIdentifier(t *testing.T) {
	fake := &fakeAdmin{}
	router := newOrderAdminRouter(fake)

	for _, path := range []string{adminOrdersPath + "/not-a-uuid", adminOrdersPath + "/not-a-uuid/ship"} {
		method := http.MethodGet
		if path != adminOrdersPath+"/not-a-uuid" {
			method = http.MethodPost
		}
		rec := perform(router, method, path, "", "admin-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s %s: expected 400, got %d (%s)", method, path, rec.Code, rec.Body.String())
		}
		body := decodeError(t, rec)
		if body.Error.Code != "VALIDATION_ERROR" {
			t.Fatalf("expected VALIDATION_ERROR, got %s", body.Error.Code)
		}
		if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "orderId" {
			t.Fatalf("expected a detail naming orderId, got %+v", body.Error.Details)
		}
	}
}

// The US1 fake predates the administrator-order routes; these stubs keep it
// satisfying the handler's service interface so its checkout tests are
// unaffected. The administrator tests use fakeAdmin.
func (f *fakeCheckout) ListAll(context.Context, appdto.ListInput) (appdto.AdminOrderPage, error) {
	return appdto.AdminOrderPage{}, nil
}

func (f *fakeCheckout) GetByIDAdmin(context.Context, appdto.OrderRefInput) (appdto.AdminOrderView, error) {
	return appdto.AdminOrderView{}, nil
}

func (f *fakeCheckout) ShipByAdmin(context.Context, appdto.OrderRefInput) (appdto.AdminOrderView, error) {
	return appdto.AdminOrderView{}, nil
}

func (f *fakeCheckout) CompleteByAdmin(context.Context, appdto.OrderRefInput) (appdto.AdminOrderView, error) {
	return appdto.AdminOrderView{}, nil
}
