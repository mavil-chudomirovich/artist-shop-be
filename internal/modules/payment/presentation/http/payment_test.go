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

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file is the transport contract of US1: `POST /orders/{orderId}/payment`
// answers 200 with the link, refuses a request without a session with 401, maps
// every refusal to its documented code and status, and reads the caller's live
// payment back. It mounts the route exactly the way the composition root mounts
// it — a sub-router once at /orders/{orderId}/payment — over a fake of the use
// case, so the answers asserted are the handler's own (contracts/openapi.yaml,
// contracts/error-codes.md).

// testLogger discards log output; the handler only logs on a server error.
var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

var (
	testCustomerID    = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	testCustomerTwoID = uuid.MustParse("33333333-3333-3333-3333-333333333333")
)

// fakeService is a fake of the handler's narrow service. It records the input it
// was handed and the actor the handler placed in the context, and answers a fixed
// view or error.
type fakeService struct {
	view     appdto.PaymentView
	err      error
	gotStart appdto.StartInput
	gotRead  appdto.ReadInput
	gotActor appinterface.Actor
	hasActor bool
}

func (f *fakeService) Start(ctx context.Context, in appdto.StartInput) (appdto.PaymentView, error) {
	f.gotStart = in
	f.gotActor, f.hasActor = appinterface.ActorFromContext(ctx)
	return f.view, f.err
}

func (f *fakeService) Read(ctx context.Context, in appdto.ReadInput) (appdto.PaymentView, error) {
	f.gotRead = in
	f.gotActor, f.hasActor = appinterface.ActorFromContext(ctx)
	return f.view, f.err
}

// paymentHooks supplies the foundation authentication hooks with two fixed
// customer identities, so a test can exercise the session guard.
func paymentHooks() middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			switch r.Header.Get("Authorization") {
			case "Bearer customer-token":
				return &middleware.Identity{Subject: testCustomerID.String(), Role: string(access.RoleCustomer), TokenID: "customer"}, nil
			case "Bearer customer2-token":
				return &middleware.Identity{Subject: testCustomerTwoID.String(), Role: string(access.RoleCustomer), TokenID: "customer2"}, nil
			default:
				return nil, nil
			}
		},
	}
}

// newPaymentRouter mounts the sub-router the way the composition root mounts it.
func newPaymentRouter(svc Service) http.Handler {
	root := chi.NewRouter()
	root.Mount("/api/v1/orders/{orderId}/payment", New(svc, testLogger).Router(paymentHooks()))
	return root
}

// paymentPath is the request path for one order's payment.
func paymentPath(orderID uuid.UUID) string {
	return "/api/v1/orders/" + orderID.String() + "/payment"
}

// perform sends a request with an optional bearer token.
func perform(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// errorBody is the shared error envelope a refusal answers with.
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// paymentBody is the success envelope's payment payload.
type paymentBody struct {
	Data struct {
		ID        uuid.UUID `json:"id"`
		OrderID   uuid.UUID `json:"orderId"`
		Status    string    `json:"status"`
		OrderCode int64     `json:"orderCode"`
		Amount    struct {
			Amount   int64  `json:"amount"`
			Currency string `json:"currency"`
		} `json:"amount"`
		CheckoutURL string `json:"checkoutUrl"`
		QRCode      string `json:"qrCode"`
	} `json:"data"`
}

// livePaymentView is the fixture the fake answers with.
func livePaymentView(orderID uuid.UUID) appdto.PaymentView {
	return appdto.PaymentView{
		ID:          uuid.New(),
		OrderID:     orderID,
		Amount:      appdto.MoneyView{Amount: 120000, Currency: "VND"},
		Status:      constant.StatusPending,
		Provider:    constant.ProviderPayOS,
		OrderCode:   12345,
		CheckoutURL: "https://pay.example/live",
		QRCode:      "qr-live",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
}

// FR-002: a start answers 200 with the payment and the link to pay.
func TestStartPaymentReturnsTheLink(t *testing.T) {
	orderID := uuid.New()
	svc := &fakeService{view: livePaymentView(orderID)}
	rec := perform(newPaymentRouter(svc), http.MethodPost, paymentPath(orderID), "customer-token")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var body paymentBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.Status != "PENDING" || body.Data.CheckoutURL != "https://pay.example/live" {
		t.Fatalf("data = %+v, want PENDING with the checkout URL", body.Data)
	}
	if body.Data.Amount.Amount != 120000 || body.Data.Amount.Currency != "VND" {
		t.Fatalf("amount = %+v, want 120000 VND", body.Data.Amount)
	}
	if svc.gotStart.OrderID != orderID {
		t.Errorf("use case got order %s, want %s", svc.gotStart.OrderID, orderID)
	}
	if !svc.hasActor || svc.gotActor.ID != testCustomerID {
		t.Errorf("the acting account = %+v (present %v), want the session's %s", svc.gotActor, svc.hasActor, testCustomerID)
	}
}

// FR-003: a request without a session answers 401 UNAUTHENTICATED.
func TestStartPaymentRequiresASession(t *testing.T) {
	rec := perform(newPaymentRouter(&fakeService{}), http.MethodPost, paymentPath(uuid.New()), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var body errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error.Code != "UNAUTHENTICATED" {
		t.Fatalf("code = %q, want UNAUTHENTICATED", body.Error.Code)
	}
}

// FR-003: another customer's order answers 404 PAYMENT_ORDER_NOT_FOUND.
func TestStartPaymentAnswersNotFoundForAForeignOrder(t *testing.T) {
	svc := &fakeService{err: domainerr.ErrOrderNotFound}
	rec := perform(newPaymentRouter(svc), http.MethodPost, paymentPath(uuid.New()), "customer-token")
	assertCode(t, rec, http.StatusNotFound, constant.CodeOrderNotFound)
}

// FR-002: an order that is not awaiting payment answers 409
// PAYMENT_ORDER_NOT_PAYABLE.
func TestStartPaymentAnswersNotPayable(t *testing.T) {
	svc := &fakeService{err: domainerr.ErrOrderNotPayable}
	rec := perform(newPaymentRouter(svc), http.MethodPost, paymentPath(uuid.New()), "customer-token")
	assertCode(t, rec, http.StatusConflict, constant.CodeOrderNotPayable)
}

// A live payment whose link can no longer be returned answers 409
// PAYMENT_ALREADY_LIVE.
func TestStartPaymentAnswersAlreadyLive(t *testing.T) {
	svc := &fakeService{err: domainerr.ErrAlreadyLive}
	rec := perform(newPaymentRouter(svc), http.MethodPost, paymentPath(uuid.New()), "customer-token")
	assertCode(t, rec, http.StatusConflict, constant.CodeAlreadyLive)
}

// FR-006: a provider refusal answers 422 PAYMENT_PROVIDER_ERROR with the order
// unchanged.
func TestStartPaymentAnswersProviderError(t *testing.T) {
	svc := &fakeService{err: domainerr.ProviderFailure(context.DeadlineExceeded)}
	rec := perform(newPaymentRouter(svc), http.MethodPost, paymentPath(uuid.New()), "customer-token")
	assertCode(t, rec, http.StatusUnprocessableEntity, constant.CodeProviderError)
}

// FR-017: a read answers 200 with the caller's live payment.
func TestReadPaymentReturnsTheLivePayment(t *testing.T) {
	orderID := uuid.New()
	svc := &fakeService{view: livePaymentView(orderID)}
	rec := perform(newPaymentRouter(svc), http.MethodGet, paymentPath(orderID), "customer-token")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if svc.gotRead.OrderID != orderID {
		t.Errorf("use case got order %s, want %s", svc.gotRead.OrderID, orderID)
	}
}

// FR-017: an order of the caller's with no live payment answers 404
// PAYMENT_NOT_FOUND.
func TestReadPaymentAnswersNotFound(t *testing.T) {
	svc := &fakeService{err: domainerr.ErrNotFound}
	rec := perform(newPaymentRouter(svc), http.MethodGet, paymentPath(uuid.New()), "customer-token")
	assertCode(t, rec, http.StatusNotFound, constant.CodeNotFound)
}

// A malformed order identifier is a request-shape error, not a lookup.
func TestStartPaymentRefusesAMalformedOrderID(t *testing.T) {
	rec := perform(newPaymentRouter(&fakeService{}), http.MethodPost, "/api/v1/orders/not-a-uuid/payment", "customer-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q, want VALIDATION_ERROR", body.Error.Code)
	}
}

// assertCode asserts the status and the machine-readable error code.
func assertCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, status, rec.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != code {
		t.Fatalf("code = %q, want %q", body.Error.Code, code)
	}
}
