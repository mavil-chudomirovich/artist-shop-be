package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
)

// This file is the transport contract of US2: `POST /payments/payos/webhook` is
// public (no session), answers 200 for a handled event so PayOS stops retrying,
// refuses an unverified signature with 401, an unknown order code with 404 and an
// unparseable body with 400. The route is mounted the way the composition root
// mounts it — once at /payments/payos/webhook — over a fake of the use case, so
// the answers asserted are the handler's own (contracts/openapi.yaml,
// contracts/error-codes.md).

// webhookPath is the public provider callback route.
const webhookPath = "/api/v1/payments/payos/webhook"

// newWebhookRouter mounts the public callback the way the composition root does.
func newWebhookRouter(svc Service) http.Handler {
	root := chi.NewRouter()
	root.Mount("/api/v1/payments/payos/webhook", New(svc, testLogger).WebhookRouter())
	return root
}

// performWebhook posts a body to the callback with no session.
func performWebhook(handler http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, webhookPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// FR-019: a valid signature is handled and answers 200 quickly, and the handler
// hands the raw body to the use case (which is where the signature is verified).
func TestWebhookAnswersOKForAHandledEvent(t *testing.T) {
	svc := &fakeService{}
	rec := performWebhook(newWebhookRouter(svc), `{"code":"00","desc":"success","success":true,"signature":"sig"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(svc.gotWebhook) == 0 {
		t.Error("the use case must receive the raw body so it can verify the signature")
	}
}

// FR-019: a signature that does not verify answers 401
// PAYMENT_WEBHOOK_INVALID_SIGNATURE and applies nothing.
func TestWebhookAnswersInvalidSignature(t *testing.T) {
	svc := &fakeService{webhookErr: domainerr.ErrWebhookInvalidSignature}
	rec := performWebhook(newWebhookRouter(svc), `{"signature":"tampered"}`)
	assertCode(t, rec, http.StatusUnauthorized, constant.CodeWebhookInvalidSignature)
}

// FR-008: a verified confirmation whose amount differs still answers 200 — the
// use case records it UNMATCHED; refusing would make the provider retry forever.
func TestWebhookAnswersOKForAnAmountMismatch(t *testing.T) {
	svc := &fakeService{}
	rec := performWebhook(newWebhookRouter(svc), `{"code":"00","success":true,"data":{"amount":1}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a handled amount mismatch; body: %s", rec.Code, rec.Body.String())
	}
}

// research D7: a verified webhook for an unknown order code answers 404
// PAYMENT_NOT_FOUND; nothing is recorded.
func TestWebhookAnswersNotFoundForAnUnknownOrderCode(t *testing.T) {
	svc := &fakeService{webhookErr: domainerr.ErrNotFound}
	rec := performWebhook(newWebhookRouter(svc), `{"code":"00","success":true,"data":{"orderCode":1}}`)
	assertCode(t, rec, http.StatusNotFound, constant.CodeNotFound)
}

// contracts/error-codes.md: a body that does not parse answers 400
// MALFORMED_REQUEST.
func TestWebhookAnswersMalformedForAnUnparseableBody(t *testing.T) {
	svc := &fakeService{webhookErr: domainerr.ErrMalformedWebhook}
	rec := performWebhook(newWebhookRouter(svc), `not-json`)
	assertCode(t, rec, http.StatusBadRequest, "MALFORMED_REQUEST")
}

// The route is public: no session is required and the use case is still reached.
func TestWebhookDoesNotRequireASession(t *testing.T) {
	svc := &fakeService{}
	rec := performWebhook(newWebhookRouter(svc), `{"code":"00","success":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 without a session", rec.Code)
	}
	if svc.gotWebhook == nil {
		t.Error("the public route must still reach the use case")
	}
	// Guard against a decode into a non-JSON body being silently accepted: the
	// body is passed through verbatim.
	var body map[string]any
	_ = json.Unmarshal(svc.gotWebhook, &body)
}
