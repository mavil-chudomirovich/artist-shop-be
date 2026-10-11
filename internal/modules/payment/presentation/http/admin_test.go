package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/middleware"
)

// This file is the transport contract of US5: `GET /admin/payments` lists every
// payment, newest first, optionally narrowed to one state — status=UNMATCHED is
// the operator's reconciliation queue — behind the administrator role guard, so a
// customer's session is refused 403 FORBIDDEN. A status outside the four payment
// states is 400 VALIDATION_ERROR naming the member (contracts/openapi.yaml,
// contracts/error-codes.md). It mounts the route the way the composition root
// mounts it — once at /admin/payments — over a fake of the use case, so the
// answers asserted are the handler's own.

// adminPaymentsPath is the operator's payment desk route.
const adminPaymentsPath = "/api/v1/admin/payments"

// testPaymentAdminID is the administrator identity the admin hooks answer, so the
// handler test can prove the session's account is what reaches the use case.
var testPaymentAdminID = uuid.MustParse("44444444-4444-4444-4444-444444444444")

// ListAll satisfies the handler's Service interface for the customer and webhook
// tests, whose routes never reach the operator list. The administrator tests use
// fakePaymentAdmin so they can assert the input and the actor.
func (f *fakeService) ListAll(context.Context, appdto.ListInput) (appdto.PaymentPage, error) {
	return appdto.PaymentPage{}, nil
}

// fakePaymentAdmin is a fake of the handler's service for the administrator
// routes. It answers a configurable page and records the input and the actor the
// handler placed in the context. The customer methods are never reached by the
// administrator routes, so they panic if a route drifts into one.
type fakePaymentAdmin struct {
	page     appdto.PaymentPage
	listErr  error
	calls    int
	gotList  appdto.ListInput
	gotActor appinterface.Actor
	hasActor bool
}

func (f *fakePaymentAdmin) Start(context.Context, appdto.StartInput) (appdto.PaymentView, error) {
	panic("the administrator routes never start a payment")
}

func (f *fakePaymentAdmin) Read(context.Context, appdto.ReadInput) (appdto.PaymentView, error) {
	panic("the administrator routes never read a customer payment")
}

func (f *fakePaymentAdmin) HandleWebhook(context.Context, []byte) error {
	panic("the administrator routes never handle a webhook")
}

func (f *fakePaymentAdmin) ListAll(ctx context.Context, in appdto.ListInput) (appdto.PaymentPage, error) {
	f.calls++
	f.gotList = in
	f.gotActor, f.hasActor = appinterface.ActorFromContext(ctx)
	return f.page, f.listErr
}

// paymentAdminHooks supplies the foundation authentication hooks with a customer
// and an administrator, so a test can exercise the role guard.
func paymentAdminHooks() middleware.AuthHooks {
	return middleware.AuthHooks{
		Authenticate: func(_ context.Context, r *http.Request) (*middleware.Identity, error) {
			switch r.Header.Get("Authorization") {
			case "Bearer customer-token":
				return &middleware.Identity{Subject: testCustomerID.String(), Role: string(access.RoleCustomer), TokenID: "customer"}, nil
			case "Bearer admin-token":
				return &middleware.Identity{Subject: testPaymentAdminID.String(), Role: string(access.RoleAdmin), TokenID: "admin"}, nil
			default:
				return nil, nil
			}
		},
	}
}

// newPaymentAdminRouter mounts the administrator group the way the composition
// root mounts it.
func newPaymentAdminRouter(svc Service) http.Handler {
	root := chi.NewRouter()
	root.Mount(adminPaymentsPath, New(svc, testLogger).AdminRouter(paymentAdminHooks()))
	return root
}

// adminPaymentListBody decodes the operator's paginated list answer.
type adminPaymentListBody struct {
	Data []struct {
		ID                uuid.UUID `json:"id"`
		OrderID           uuid.UUID `json:"orderId"`
		UserID            uuid.UUID `json:"userId"`
		OrderCode         int64     `json:"orderCode"`
		Status            string    `json:"status"`
		ProviderReference string    `json:"providerReference"`
		UnmatchedReason   string    `json:"unmatchedReason"`
		Amount            struct {
			Amount   int64  `json:"amount"`
			Currency string `json:"currency"`
		} `json:"amount"`
	} `json:"data"`
	Meta struct {
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
		Total    int64 `json:"total"`
	} `json:"meta"`
}

// adminErrorBody decodes the shared error envelope including the details, so the
// VALIDATION_ERROR can be asserted to name the offending member.
type adminErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Details []struct {
			Field string `json:"field"`
			Issue string `json:"issue"`
		} `json:"details"`
	} `json:"error"`
}

// sampleAdminPaymentView is one payment as the operator reads it.
func sampleAdminPaymentView() appdto.AdminPaymentView {
	return appdto.AdminPaymentView{
		PaymentView: appdto.PaymentView{
			ID:              uuid.New(),
			OrderID:         uuid.New(),
			Amount:          appdto.MoneyView{Amount: 120000, Currency: "VND"},
			Status:          constant.StatusUnmatched,
			Provider:        constant.ProviderPayOS,
			OrderCode:       12345,
			UnmatchedReason: "amount mismatch",
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		},
		UserID: testCustomerID,
	}
}

// FR-018: the operator's list answers 200 with one page of every payment, newest
// first, each row carrying its owner, state, reason and provider reference, and
// the actor the handler passes is the session's.
func TestAdminListReturnsEveryPaymentWithItsOwner(t *testing.T) {
	row := sampleAdminPaymentView()
	fake := &fakePaymentAdmin{page: appdto.PaymentPage{
		Payments: []appdto.AdminPaymentView{row},
		Page:     1,
		PageSize: 20,
		Total:    1,
	}}
	rec := perform(newPaymentAdminRouter(fake), http.MethodGet, adminPaymentsPath+"?page=1&pageSize=20", "admin-token")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var body adminPaymentListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the admin list body %q: %v", rec.Body.String(), err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("data = %+v, want one payment", body.Data)
	}
	if body.Data[0].ID != row.ID || body.Data[0].UserID != testCustomerID || body.Data[0].Status != "UNMATCHED" {
		t.Fatalf("unexpected admin row: %+v", body.Data[0])
	}
	if body.Data[0].UnmatchedReason != "amount mismatch" || body.Data[0].Amount.Amount != 120000 {
		t.Fatalf("the row must carry its reason and amount: %+v", body.Data[0])
	}
	if body.Meta.Page != 1 || body.Meta.PageSize != 20 || body.Meta.Total != 1 {
		t.Fatalf("unexpected meta: %+v", body.Meta)
	}
	if fake.gotList.Page != 1 || fake.gotList.PageSize != 20 {
		t.Fatalf("the window must be passed through, got %+v", fake.gotList)
	}
	if !fake.hasActor || fake.gotActor.ID != testPaymentAdminID {
		t.Fatalf("the handler must hand the session's account to the use case, got %+v (present %v)", fake.gotActor, fake.hasActor)
	}
}

// FR-018, research D15: the operator's list accepts an optional state filter and
// passes it through; with none supplied it is unfiltered.
func TestAdminListPassesTheStatusFilter(t *testing.T) {
	fake := &fakePaymentAdmin{}
	router := newPaymentAdminRouter(fake)

	rec := perform(router, http.MethodGet, adminPaymentsPath+"?status=UNMATCHED", "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if fake.gotList.Status == nil || *fake.gotList.Status != constant.StatusUnmatched {
		t.Fatalf("the status filter must be passed through, got %v", fake.gotList.Status)
	}

	rec = perform(router, http.MethodGet, adminPaymentsPath, "admin-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if fake.gotList.Status != nil {
		t.Fatalf("the default list must be unfiltered, got %v", *fake.gotList.Status)
	}
}

// contracts/error-codes.md: a status outside the four payment states is 400
// VALIDATION_ERROR naming the member, before it reaches the use case.
func TestAdminListRejectsAnInvalidStatus(t *testing.T) {
	fake := &fakePaymentAdmin{}
	rec := perform(newPaymentAdminRouter(fake), http.MethodGet, adminPaymentsPath+"?status=NOT_A_STATE", "admin-token")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	var body adminErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q, want VALIDATION_ERROR", body.Error.Code)
	}
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "status" {
		t.Fatalf("the detail must name status, got %+v", body.Error.Details)
	}
	if fake.calls != 0 {
		t.Fatal("an invalid filter must not reach the use case")
	}
}

// FR-018, Constitution V: a customer's session is refused 403 FORBIDDEN on the
// operator list, before reaching the use case.
func TestAdminListRefusesACustomerSession(t *testing.T) {
	fake := &fakePaymentAdmin{}
	rec := perform(newPaymentAdminRouter(fake), http.MethodGet, adminPaymentsPath, "customer-token")

	assertCode(t, rec, http.StatusForbidden, "FORBIDDEN")
	if fake.calls != 0 {
		t.Fatal("a forbidden request must not reach the use case")
	}
}

// The operator list requires a session: no token answers 401 UNAUTHENTICATED.
func TestAdminListRequiresASession(t *testing.T) {
	rec := perform(newPaymentAdminRouter(&fakePaymentAdmin{}), http.MethodGet, adminPaymentsPath, "")
	assertCode(t, rec, http.StatusUnauthorized, "UNAUTHENTICATED")
}
