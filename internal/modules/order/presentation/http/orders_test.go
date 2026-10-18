package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

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

	completeView appdto.OrderView
	completeErr  error

	gotListInput     appdto.ListInput
	gotListActor     appinterface.Actor
	gotRefInput      appdto.OrderRefInput
	gotRefActor      appinterface.Actor
	gotCancelInput   appdto.OrderRefInput
	gotCancelActor   appinterface.Actor
	gotCompleteInput appdto.OrderRefInput
	gotCompleteActor appinterface.Actor
	cancelCalls      int
	completeCalls    int
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

func (f *fakeOrders) ConfirmReceived(ctx context.Context, in appdto.OrderRefInput) (appdto.OrderView, error) {
	f.gotCompleteInput = in
	f.gotCompleteActor, _ = appinterface.ActorFromContext(ctx)
	f.completeCalls++
	return f.completeView, f.completeErr
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

	// The delivery facts are detail-only: a list row must not carry them
	// (research D11).
	var raw struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode the raw list body %q: %v", rec.Body.String(), err)
	}
	if len(raw.Data) != 1 {
		t.Fatalf("expected one list row, got %d", len(raw.Data))
	}
	for _, member := range []string{"deliveredAt", "completedAt", "deliveryPhotos", "feedback"} {
		if _, present := raw.Data[0][member]; present {
			t.Fatalf("a list row must not carry %q", member)
		}
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

// FR-010, FR-015, FR-017: the customer detail carries the delivery facts the
// application view holds — both instants, the dispatch and delivery evidence, and
// the feedback (contracts/openapi.yaml, OrderDetail).
func TestGetMineReturnsTheDeliveryFacts(t *testing.T) {
	view := withDeliveryFacts(sampleView())
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
	if body.Data.DeliveredAt == nil || body.Data.CompletedAt == nil {
		t.Fatalf("the fulfilled order must answer both instants, got %+v", body.Data)
	}
	assertInstant(t, *body.Data.DeliveredAt, *view.DeliveredAt)
	assertInstant(t, *body.Data.CompletedAt, *view.CompletedAt)

	if len(body.Data.DeliveryPhotos) != len(view.DeliveryPhotos) {
		t.Fatalf("expected %d delivery photos, got %d", len(view.DeliveryPhotos), len(body.Data.DeliveryPhotos))
	}
	for i, want := range view.DeliveryPhotos {
		got := body.Data.DeliveryPhotos[i]
		if got.ID != want.ID || got.Stage != want.Stage || got.URL != want.URL ||
			got.Width != want.Width || got.Height != want.Height || got.Position != want.Position {
			t.Fatalf("delivery photo %d: got %+v, want %+v", i, got, want)
		}
	}

	if body.Data.Feedback == nil {
		t.Fatal("the fulfilled order must answer its feedback")
	}
	if body.Data.Feedback.Content != view.Feedback.Content {
		t.Fatalf("expected feedback %q, got %q", view.Feedback.Content, body.Data.Feedback.Content)
	}
	assertInstant(t, body.Data.Feedback.CreatedAt, view.Feedback.CreatedAt)
}

// FR-017: an order with no delivery facts yet still serialises the members — the
// instants and the feedback as null and the evidence as an empty array — so a
// client can rely on the contract's shapes rather than on omitted members.
func TestGetMineSerialisesAnUnfulfilledOrdersDeliveryFacts(t *testing.T) {
	view := sampleView()
	fake := &fakeOrders{detail: view}
	router := newOrderRouter(fake)

	rec := perform(router, http.MethodGet, ordersPath+"/"+view.ID.String(), "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var raw struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode the raw order body %q: %v", rec.Body.String(), err)
	}
	for _, member := range []string{"deliveredAt", "completedAt", "feedback"} {
		value, present := raw.Data[member]
		if !present {
			t.Fatalf("the detail must carry %q", member)
		}
		if string(value) != "null" {
			t.Fatalf("%q must be null before the order is fulfilled, got %s", member, value)
		}
	}
	photos, present := raw.Data["deliveryPhotos"]
	if !present || string(photos) != "[]" {
		t.Fatalf("deliveryPhotos must be an empty array, got %s", photos)
	}

	var body orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the order body %q: %v", rec.Body.String(), err)
	}
	if body.Data.DeliveryPhotos == nil || len(body.Data.DeliveryPhotos) != 0 {
		t.Fatalf("expected an empty, non-null photo list, got %+v", body.Data.DeliveryPhotos)
	}
}

// assertInstant proves an RFC3339 member names the same instant as the view's.
func assertInstant(t *testing.T, got string, want time.Time) {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("parse the RFC3339 instant %q: %v", got, err)
	}
	if !parsed.Equal(want) {
		t.Fatalf("expected instant %s, got %s", want.Format(time.RFC3339Nano), got)
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

// FR-011, FR-014: confirming receipt of a delivered order answers 200 with the
// completed order, and the session's actor and the addressed identifier are
// passed through.
func TestConfirmReceivedReturnsTheCompletedOrder(t *testing.T) {
	completed := sampleView()
	completed.Status = constant.StatusCompleted
	completedAt := completed.CreatedAt.Add(72 * time.Hour)
	completed.CompletedAt = &completedAt
	fake := &fakeOrders{completeView: completed}
	router := newOrderRouter(fake)

	rec := perform(router, http.MethodPost, ordersPath+"/"+completed.ID.String()+"/complete", "", "customer-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the order body %q: %v", rec.Body.String(), err)
	}
	if body.Data.Status != "COMPLETED" {
		t.Fatalf("the completed order must answer COMPLETED, got %s", body.Data.Status)
	}
	if body.Data.CompletedAt == nil {
		t.Fatal("the completed order must answer completedAt")
	}
	assertInstant(t, *body.Data.CompletedAt, completedAt)
	if fake.gotCompleteActor.ID != testCustomerID || fake.gotCompleteInput.OrderID != completed.ID {
		t.Fatalf("the session's actor and the addressed id must be passed through, got %+v / %+v",
			fake.gotCompleteActor, fake.gotCompleteInput)
	}
}

// contracts/error-codes.md: another customer's (or an unknown) order answers the
// same 404 ORDER_NOT_FOUND, so the route never confirms the order exists.
func TestConfirmReceivedAnswersNotFoundForAnotherCustomersOrder(t *testing.T) {
	fake := &fakeOrders{completeErr: domainerr.ErrNotFound}
	router := newOrderRouter(fake)

	rec := perform(router, http.MethodPost, ordersPath+"/"+uuid.New().String()+"/complete", "", "customer2-token")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != constant.CodeOrderNotFound {
		t.Fatalf("expected ORDER_NOT_FOUND, got %s", body.Error.Code)
	}
}

// contracts/error-codes.md: confirming an order that is not delivered answers 409
// ORDER_STATE_TRANSITION_INVALID naming the current state.
func TestConfirmReceivedRefusesANonDeliveredOrder(t *testing.T) {
	fake := &fakeOrders{completeErr: domainerr.StateTransitionInvalid(constant.StatusShipping, constant.StatusCompleted)}
	router := newOrderRouter(fake)

	rec := perform(router, http.MethodPost, ordersPath+"/"+uuid.New().String()+"/complete", "", "customer-token")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeError(t, rec)
	if body.Error.Code != constant.CodeStateTransitionInvalid {
		t.Fatalf("expected ORDER_STATE_TRANSITION_INVALID, got %s", body.Error.Code)
	}
	if !strings.Contains(body.Error.Message, string(constant.StatusShipping)) {
		t.Fatalf("the refusal must name the current state SHIPPING, got %q", body.Error.Message)
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
		{http.MethodPost, ordersPath + "/" + uuid.New().String() + "/complete"},
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
			if fake.calls != 0 || fake.cancelCalls != 0 || fake.completeCalls != 0 || fake.gotRefInput.OrderID != uuid.Nil {
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

	for _, path := range []string{ordersPath + "/not-a-uuid", ordersPath + "/not-a-uuid/cancel", ordersPath + "/not-a-uuid/complete"} {
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

func (f *fakeCheckout) ConfirmReceived(context.Context, appdto.OrderRefInput) (appdto.OrderView, error) {
	return appdto.OrderView{}, nil
}
