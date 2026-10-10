package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
)

// This file is the transport contract of US3: `GET /orders`, `GET /orders/{id}`
// and `POST /orders/{id}/cancel` are all session-guarded, the acting customer is
// the session's and never the request's, another customer's identifier answers
// `404 ORDER_NOT_FOUND`, and a second cancel answers
// `409 ORDER_STATE_TRANSITION_INVALID`. It mounts the routes the way the
// composition root mounts them, over a fake of the use case, so the answers
// asserted are the handler's own (contracts/openapi.yaml, contracts/error-codes.md).

// fakeOrders is a fake of the handler's service covering the customer-order
// routes. It records the actor the handler put in the context and the input it
// was handed, and answers the fixed results each test configures. It embeds the
// checkout fake so a single value satisfies the whole service surface.
type fakeOrders struct {
	fakeCheckout

	list    appdto.OrderPage
	listErr error

	detail    appdto.OrderView
	detailErr error

	cancelResponses []cancelResponse

	gotListInput   appdto.ListInput
	gotListActor   appinterface.Actor
	gotRefInput    appdto.OrderRefInput
	gotRefActor    appinterface.Actor
	gotCancelInput appdto.OrderRefInput
	gotCancelActor appinterface.Actor
	cancelCalls    int
}

type cancelResponse struct {
	view appdto.OrderView
	err  error
}

func (f *fakeOrders) ListMine(ctx context.Context, in appdto.ListInput) (appdto.OrderPage, error) {
	f.gotListInput = in
	f.gotListActor, _ = appinterface.ActorFromContext(ctx)
	return f.list, f.listErr
}

func (f *fakeOrders) GetMine(ctx context.Context, in appdto.OrderRefInput) (appdto.OrderView, error) {
	f.gotRefInput = in
	f.gotRefActor, _ = appinterface.ActorFromContext(ctx)
	return f.detail, f.detailErr
}

func (f *fakeOrders) CancelMine(ctx context.Context, in appdto.OrderRefInput) (appdto.OrderView, error) {
	f.gotCancelInput = in
	f.gotCancelActor, _ = appinterface.ActorFromContext(ctx)
	call := f.cancelCalls
	f.cancelCalls++
	if call < len(f.cancelResponses) {
		return f.cancelResponses[call].view, f.cancelResponses[call].err
	}
	return appdto.OrderView{}, nil
}

// orderListBody decodes the paginated list answer.
type orderListBody struct {
	Data []struct {
		ID        uuid.UUID `json:"id"`
		Status    string    `json:"status"`
		Total     moneyBody `json:"total"`
		ItemCount int64     `json:"itemCount"`
		CreatedAt string    `json:"createdAt"`
	} `json:"data"`
	Meta struct {
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
		Total    int64 `json:"total"`
	} `json:"meta"`
}

// FR-018, FR-020: the list returns only the caller's orders, paginated, and the
// owner the handler passes is the session's — never the request's.
func TestListMineReturnsTheCallersOrders(t *testing.T) {
	fake := &fakeOrders{
		list: appdto.OrderPage{
			Orders: []appdto.OrderSummaryView{
				{
					ID:        uuid.New(),
					Status:    constant.StatusPaymentPending,
					Total:     appdto.MoneyView{Amount: 240000, Currency: "VND"},
					ItemCount: 1,
					CreatedAt: sampleView().CreatedAt,
				},
			},
			Page:     1,
			PageSize: 2,
			Total:    1,
		},
	}
	router := newOrderRouter(fake)

	rec := perform(router, http.MethodGet, ordersPath+"?page=1&pageSize=2", "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body orderListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the list body %q: %v", rec.Body.String(), err)
	}
	if len(body.Data) != 1 || body.Data[0].Status != "PAYMENT_PENDING" || body.Data[0].Total.Amount != 240000 {
		t.Fatalf("unexpected list: %+v", body.Data)
	}
	if body.Meta.Page != 1 || body.Meta.PageSize != 2 || body.Meta.Total != 1 {
		t.Fatalf("unexpected meta: %+v", body.Meta)
	}
	if fake.gotListInput.Page != 1 || fake.gotListInput.PageSize != 2 {
		t.Fatalf("the window must be passed through, got %+v", fake.gotListInput)
	}
	if fake.gotListActor.ID != testCustomerID {
		t.Fatalf("the handler must hand the session's account to the use case, got %s", fake.gotListActor.ID)
	}
}

// FR-018, FR-020: reading one's own order returns it in full, with the session's
// actor and the addressed identifier passed through.
func TestGetMineReturnsTheCallersOrder(t *testing.T) {
	view := sampleView()
	fake := &fakeOrders{detail: view}
	router := newOrderRouter(fake)

	rec := perform(router, http.MethodGet, ordersPath+"/"+view.ID.String(), "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the order body %q: %v", rec.Body.String(), err)
	}
	if body.Data.ID != view.ID || len(body.Data.Lines) != 1 {
		t.Fatalf("unexpected order: %+v", body.Data)
	}
	if fake.gotRefInput.OrderID != view.ID {
		t.Fatalf("the addressed identifier must be passed through, got %s", fake.gotRefInput.OrderID)
	}
	if fake.gotRefActor.ID != testCustomerID {
		t.Fatalf("the handler must hand the session's account to the use case, got %s", fake.gotRefActor.ID)
	}
}

// contracts/error-codes.md: another customer's (or an unknown) order answers the
// same 404 ORDER_NOT_FOUND, so the route never confirms the order exists.
func TestGetMineAnswersNotFoundForAnotherCustomersOrder(t *testing.T) {
	fake := &fakeOrders{detailErr: domainerr.ErrNotFound}
	router := newOrderRouter(fake)

	rec := perform(router, http.MethodGet, ordersPath+"/"+uuid.New().String(), "", "customer2-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != constant.CodeOrderNotFound {
		t.Fatalf("expected ORDER_NOT_FOUND, got %s", body.Error.Code)
	}
}

// contracts/error-codes.md: cancelling an unpaid order answers 200 with the
// cancelled order; a second attempt answers 409 ORDER_STATE_TRANSITION_INVALID.
func TestCancelMineReturnsTheCancelledOrderThenRefusesTheSecondAttempt(t *testing.T) {
	cancelled := sampleView()
	cancelled.Status = constant.StatusCancelled
	fake := &fakeOrders{
		cancelResponses: []cancelResponse{
			{view: cancelled},
			{err: domainerr.StateTransitionInvalid(constant.StatusCancelled, constant.StatusCancelled)},
		},
	}
	router := newOrderRouter(fake)
	path := ordersPath + "/" + cancelled.ID.String() + "/cancel"

	rec := perform(router, http.MethodPost, path, "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("first cancel: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the order body %q: %v", rec.Body.String(), err)
	}
	if body.Data.Status != "CANCELLED" {
		t.Fatalf("the cancelled order must answer CANCELLED, got %s", body.Data.Status)
	}
	if fake.gotCancelActor.ID != testCustomerID || fake.gotCancelInput.OrderID != cancelled.ID {
		t.Fatalf("the session's actor and the addressed id must be passed through, got %+v / %+v", fake.gotCancelActor, fake.gotCancelInput)
	}

	rec = perform(router, http.MethodPost, path, "", "customer-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second cancel: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != constant.CodeStateTransitionInvalid {
		t.Fatalf("expected ORDER_STATE_TRANSITION_INVALID, got %s", body.Error.Code)
	}
}

// contracts/error-codes.md: every customer-order route refuses a request with no
// session with 401 UNAUTHENTICATED, before reaching the use case.
func TestOrderRoutesRefuseWithoutASession(t *testing.T) {
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, ordersPath},
		{http.MethodGet, ordersPath + "/" + uuid.New().String()},
		{http.MethodPost, ordersPath + "/" + uuid.New().String() + "/cancel"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			fake := &fakeOrders{}
			router := newOrderRouter(fake)
			rec := perform(router, tc.method, tc.path, "", "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
			}
			if body := decodeError(t, rec); body.Error.Code != "UNAUTHENTICATED" {
				t.Fatalf("expected UNAUTHENTICATED, got %s", body.Error.Code)
			}
			if fake.calls != 0 || fake.cancelCalls != 0 || fake.gotRefInput.OrderID != uuid.Nil {
				t.Fatal("an unauthenticated request must not reach the use case")
			}
		})
	}
}

// contracts/openapi.yaml: a malformed order identifier is 400 VALIDATION_ERROR
// naming `orderId`, not a lookup that returns nothing.
func TestOrderRoutesRejectAMalformedOrderIdentifier(t *testing.T) {
	fake := &fakeOrders{}
	router := newOrderRouter(fake)

	for _, path := range []string{ordersPath + "/not-a-uuid", ordersPath + "/not-a-uuid/cancel"} {
		method := http.MethodGet
		if path != ordersPath+"/not-a-uuid" {
			method = http.MethodPost
		}
		rec := perform(router, method, path, "", "customer-token")
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

// A pagination window outside the permitted range is a 400 VALIDATION_ERROR, a
// foundation concern, before the use case is reached.
func TestListMineRejectsAnOutOfRangeWindow(t *testing.T) {
	for _, query := range []string{"?page=0", "?pageSize=0", "?pageSize=101", "?pageSize=abc"} {
		t.Run(query, func(t *testing.T) {
			fake := &fakeOrders{}
			router := newOrderRouter(fake)
			rec := perform(router, http.MethodGet, ordersPath+query, "", "customer-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			if body := decodeError(t, rec); body.Error.Code != "VALIDATION_ERROR" {
				t.Fatalf("expected VALIDATION_ERROR, got %s", body.Error.Code)
			}
		})
	}
}

// The US1 fake predates the customer-order routes; these stubs keep it
// satisfying the handler's service interface so its checkout tests are
// unaffected. The customer-order tests use fakeOrders.
func (f *fakeCheckout) ListMine(context.Context, appdto.ListInput) (appdto.OrderPage, error) {
	return appdto.OrderPage{}, nil
}

func (f *fakeCheckout) GetMine(context.Context, appdto.OrderRefInput) (appdto.OrderView, error) {
	return appdto.OrderView{}, nil
}

func (f *fakeCheckout) CancelMine(context.Context, appdto.OrderRefInput) (appdto.OrderView, error) {
	return appdto.OrderView{}, nil
}
