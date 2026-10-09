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

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file is the transport contract of US1: `POST /orders` answers 201 with the
// created order, refuses a request without a session with 401, maps every checkout
// refusal to its documented code and status, and reports a malformed identifier or
// an unknown member as a request-shape error. It mounts the route exactly the way
// the composition root mounts it, over a fake of the use case, so the answers
// asserted are the handler's own (contracts/openapi.yaml, contracts/error-codes.md).

const ordersPath = "/api/v1/orders"

var testLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

var (
	testCustomerID    = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	testCustomerTwoID = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	testAdminID       = uuid.MustParse("44444444-4444-4444-4444-444444444444")
)

// fakeCheckout is a fake of the handler's narrow service. It records the input it
// was handed and the actor the handler placed in the context, and answers a fixed
// view or error.
type fakeCheckout struct {
	view     appdto.OrderView
	err      error
	gotInput appdto.CheckoutInput
	gotActor appinterface.Actor
	calls    int
}

func (f *fakeCheckout) Checkout(ctx context.Context, in appdto.CheckoutInput) (appdto.OrderView, error) {
	f.calls++
	f.gotInput = in
	f.gotActor, _ = appinterface.ActorFromContext(ctx)
	return f.view, f.err
}

// orderHooks supplies the foundation authentication hooks with two fixed customer
// identities and one administrator, so a test can exercise the session and the
// role guards.
func orderHooks() middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			switch r.Header.Get("Authorization") {
			case "Bearer customer-token":
				return &middleware.Identity{Subject: testCustomerID.String(), Role: string(access.RoleCustomer), TokenID: "customer"}, nil
			case "Bearer customer2-token":
				return &middleware.Identity{Subject: testCustomerTwoID.String(), Role: string(access.RoleCustomer), TokenID: "customer2"}, nil
			case "Bearer admin-token":
				return &middleware.Identity{Subject: testAdminID.String(), Role: string(access.RoleAdmin), TokenID: "admin"}, nil
			default:
				return nil, nil
			}
		},
	}
}

// newOrderRouter mounts the group the way the composition root mounts it.
func newOrderRouter(svc Service) http.Handler {
	root := chi.NewRouter()
	root.Mount(ordersPath, New(svc, testLogger).Router(orderHooks()))
	return root
}

// perform sends a request with an optional JSON body and bearer token.
func perform(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type moneyBody struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type orderBody struct {
	Data struct {
		ID        uuid.UUID `json:"id"`
		Status    string    `json:"status"`
		Total     moneyBody `json:"total"`
		ItemCount int64     `json:"itemCount"`
		CreatedAt string    `json:"createdAt"`
		Address   struct {
			RecipientName  string `json:"recipientName"`
			RecipientPhone string `json:"recipientPhone"`
			ProvinceCode   string `json:"provinceCode"`
			ProvinceName   string `json:"provinceName"`
			WardCode       string `json:"wardCode"`
			WardName       string `json:"wardName"`
			StreetAddress  string `json:"streetAddress"`
		} `json:"address"`
		Lines []struct {
			ProductID uuid.UUID `json:"productId"`
			Name      string    `json:"name"`
			Slug      string    `json:"slug"`
			Quantity  int64     `json:"quantity"`
			UnitPrice moneyBody `json:"unitPrice"`
			LineTotal moneyBody `json:"lineTotal"`
		} `json:"lines"`
	} `json:"data"`
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Field string `json:"field"`
			Issue string `json:"issue"`
		} `json:"details"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the error envelope %q: %v", rec.Body.String(), err)
	}
	return body
}

// sampleView is one order view as the use case answers it.
func sampleView() appdto.OrderView {
	product := uuid.New()
	return appdto.OrderView{
		OrderSummaryView: appdto.OrderSummaryView{
			ID:        uuid.New(),
			Status:    constant.StatusPendingPayment,
			Total:     appdto.MoneyView{Amount: 240000, Currency: "VND"},
			ItemCount: 1,
			CreatedAt: time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC),
		},
		Address: appdto.OrderAddressView{
			RecipientName: "Nguyễn Văn A", RecipientPhone: "0912345678",
			ProvinceCode: "01", ProvinceName: "Hà Nội", WardCode: "00001", WardName: "Phúc Xá",
			StreetAddress: "1 Đinh Tiên Hoàng",
		},
		Lines: []appdto.OrderLineView{{
			ProductID: product, Name: "Tranh", Slug: "tranh", Quantity: 2,
			UnitPrice: appdto.MoneyView{Amount: 120000, Currency: "VND"},
			LineTotal: appdto.MoneyView{Amount: 240000, Currency: "VND"},
		}},
	}
}

// contracts/openapi.yaml: a successful checkout answers 201 with the created order
// and the owner is the session's, never the request's (FR-020).
func TestCheckoutReturns201WithTheCreatedOrder(t *testing.T) {
	view := sampleView()
	fake := &fakeCheckout{view: view}
	router := newOrderRouter(fake)

	rec := perform(router, http.MethodPost, ordersPath, `{}`, "customer-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var body orderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the order body %q: %v", rec.Body.String(), err)
	}
	if body.Data.ID != view.ID || body.Data.Status != "PENDING_PAYMENT" {
		t.Fatalf("unexpected order: %+v", body.Data)
	}
	if body.Data.Total.Amount != 240000 || body.Data.Total.Currency != "VND" {
		t.Fatalf("unexpected total: %+v", body.Data.Total)
	}
	if body.Data.ItemCount != 1 || len(body.Data.Lines) != 1 {
		t.Fatalf("unexpected lines: %+v", body.Data.Lines)
	}
	if body.Data.Lines[0].Name != "Tranh" || body.Data.Lines[0].UnitPrice.Amount != 120000 ||
		body.Data.Lines[0].LineTotal.Amount != 240000 || body.Data.Lines[0].Quantity != 2 {
		t.Fatalf("unexpected line: %+v", body.Data.Lines[0])
	}
	if body.Data.Address.RecipientName != "Nguyễn Văn A" || body.Data.Address.StreetAddress != "1 Đinh Tiên Hoàng" {
		t.Fatalf("unexpected address: %+v", body.Data.Address)
	}
	if fake.gotActor.ID != testCustomerID {
		t.Fatalf("the handler must hand the session's account to the use case, got %s", fake.gotActor.ID)
	}
}

// FR-020: an absent or null `addressId` means the default address, and a named one
// is passed through to the use case.
func TestCheckoutPassesTheAddressChoice(t *testing.T) {
	fake := &fakeCheckout{view: sampleView()}
	router := newOrderRouter(fake)

	if rec := perform(router, http.MethodPost, ordersPath, `{}`, "customer-token"); rec.Code != http.StatusCreated {
		t.Fatalf("empty body: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotInput.AddressID != nil {
		t.Fatalf("an omitted addressId must mean the default, got %v", *fake.gotInput.AddressID)
	}

	named := uuid.New()
	rec := perform(router, http.MethodPost, ordersPath, `{"addressId":"`+named.String()+`"}`, "customer-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("named address: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotInput.AddressID == nil || *fake.gotInput.AddressID != named {
		t.Fatalf("the named address must be passed through, got %v", fake.gotInput.AddressID)
	}
}

// contracts/error-codes.md: a request with no session is 401 UNAUTHENTICATED.
func TestCheckoutRefusesWithoutASession(t *testing.T) {
	fake := &fakeCheckout{}
	router := newOrderRouter(fake)

	rec := perform(router, http.MethodPost, ordersPath, `{}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}
	if body := decodeError(t, rec); body.Error.Code != "UNAUTHENTICATED" {
		t.Fatalf("expected UNAUTHENTICATED, got %s", body.Error.Code)
	}
	if fake.calls != 0 {
		t.Fatalf("an unauthenticated request must not reach the use case, got %d calls", fake.calls)
	}
}

// contracts/error-codes.md: every checkout refusal answers its documented code and
// status, and the offending item is named where the contract says.
func TestCheckoutMapsEveryRefusal(t *testing.T) {
	product := uuid.New()
	cases := []struct {
		name   string
		err    error
		code   string
		status int
		field  string
	}{
		{"empty cart", domainerr.ErrCartEmpty, constant.CodeCartEmpty, http.StatusConflict, ""},
		{"off-sale item", domainerr.ItemNotPurchasable(product), constant.CodeItemNotPurchasable, http.StatusConflict, "productId"},
		{"changed price", domainerr.ItemPriceChanged(product), constant.CodeItemPriceChanged, http.StatusConflict, "productId"},
		{"over available", domainerr.QuantityExceedsAvailable(product, 3, 5), constant.CodeQuantityExceedsAvailable, http.StatusConflict, "productId"},
		{"no address", domainerr.ErrNoAddress, constant.CodeNoAddress, http.StatusConflict, ""},
		{"foreign addressId", domainerr.InvalidValue("addressId", "is not one of the customer's addresses"), "VALIDATION_ERROR", http.StatusBadRequest, "addressId"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newOrderRouter(&fakeCheckout{err: tc.err})
			rec := perform(router, http.MethodPost, ordersPath, `{}`, "customer-token")
			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d (%s)", tc.status, rec.Code, rec.Body.String())
			}
			body := decodeError(t, rec)
			if body.Error.Code != tc.code {
				t.Fatalf("expected %s, got %s", tc.code, body.Error.Code)
			}
			if tc.field != "" {
				if len(body.Error.Details) != 1 || body.Error.Details[0].Field != tc.field {
					t.Fatalf("expected a detail naming %s, got %+v", tc.field, body.Error.Details)
				}
			}
		})
	}
}

// contracts/openapi.yaml: a malformed identifier is 400 VALIDATION_ERROR naming the
// field, and an unknown member is 400 MALFORMED_REQUEST (a body cannot name an
// owner).
func TestCheckoutRejectsBadRequestShapes(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		code  string
		field string
	}{
		{
			name:  "addressId is not a UUID",
			body:  `{"addressId":"not-a-uuid"}`,
			code:  "VALIDATION_ERROR",
			field: "addressId",
		},
		{
			name: "unknown member names an owner",
			body: `{"ownerId":"` + uuid.New().String() + `"}`,
			code: "MALFORMED_REQUEST",
		},
		{
			name: "malformed JSON",
			body: `{"addressId":`,
			code: "MALFORMED_REQUEST",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newOrderRouter(&fakeCheckout{view: sampleView()})
			rec := perform(router, http.MethodPost, ordersPath, tc.body, "customer-token")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			body := decodeError(t, rec)
			if body.Error.Code != tc.code {
				t.Fatalf("expected %s, got %s", tc.code, body.Error.Code)
			}
			if tc.field != "" && (len(body.Error.Details) != 1 || body.Error.Details[0].Field != tc.field) {
				t.Fatalf("expected a detail naming %s, got %+v", tc.field, body.Error.Details)
			}
		})
	}
}
