package payos

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

// This file proves the PayOS signature algorithm against fixed vectors derived
// from the PayOS documentation (research D2, D3): the create-link signature over
// the fields sorted alphabetically
// (amount&cancelUrl&description&orderCode&returnUrl), and the webhook signature
// over the data object's fields sorted alphabetically with nulls rendered as empty
// strings. It also exercises the HTTP calls against an httptest server, so the
// request path, headers and refusals are proven without a live provider.

// checksumKey is the checksum key from the PayOS documentation's own sample; the
// fixed vectors below were computed with it.
const checksumKey = "1a54716c8f0efb2744fb28b6e38b25da7f67a925d98bc1c18bd8faaecadd7675"

// documentedData is the PayOS documentation's webhook sample data object.
var documentedData = map[string]any{
	"orderCode":              123,
	"amount":                 3000,
	"description":            "VQRIO123",
	"accountNumber":          "12345678",
	"reference":              "TF230204212323",
	"transactionDateTime":    "2023-02-04 18:25:00",
	"currency":               "VND",
	"paymentLinkId":          "124c33293c43417ab7879e14c8d9eb18",
	"code":                   "00",
	"desc":                   "Thành công",
	"counterAccountBankId":   "",
	"counterAccountBankName": "",
	"counterAccountName":     "",
	"counterAccountNumber":   "",
	"virtualAccountName":     "",
	"virtualAccountNumber":   "",
}

// documentedWebhookSignature is the PayOS documentation's own sample webhook
// signature: HMAC_SHA256(checksumKey, <documentedData sorted alphabetically,
// key=value&...>) — the fixed vector this test pins. The sorted string is
// accountNumber, amount, code, counterAccountBankId, ... (alphabetical).
const documentedWebhookSignature = "412e915d2871504ed31be63c8f62a149a4410d34c4c42affc9006ef9917eaa03"

// testHMAC is an independent HMAC-SHA256, so the tests below prove the
// implementation's string construction rather than validating it against itself.
func testHMAC(key, data string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// research D2: the create-link signature is HMAC_SHA256(checksumKey, fields sorted
// alphabetically as key=value&...) over exactly amount, cancelUrl, description,
// orderCode and returnUrl.
func TestCreateLinkSignatureMatchesTheDocumentedFields(t *testing.T) {
	in := dto.CreateLinkInput{
		OrderCode:   123,
		Amount:      3000,
		Description: "VQRIO123",
		CancelURL:   "https://shop.example/cancel",
		ReturnURL:   "https://shop.example/return",
	}
	const want = "60224b08adffbddddfc1bf4ab0f94111aa450d00b860c5acd27aa9db002592c3"

	if got := signCreateLink(checksumKey, in); got != want {
		t.Fatalf("create-link signature = %s, want %s", got, want)
	}

	// The literal documented string the vector is computed over, so the field set
	// and its alphabetical ordering are pinned, not just the hex.
	const documented = "amount=3000&cancelUrl=https://shop.example/cancel&description=VQRIO123&orderCode=123&returnUrl=https://shop.example/return"
	if got := testHMAC(checksumKey, documented); got != want {
		t.Fatalf("the fixed vector does not match the documented string: %s", got)
	}
}

// research D3: the webhook signature is HMAC_SHA256(checksumKey, data fields sorted
// alphabetically). The documented sample vector must verify.
func TestVerifyWebhookAcceptsTheDocumentedVector(t *testing.T) {
	body := webhookBody(t, documentedData, documentedWebhookSignature)
	client := NewClient(config.PaymentConfig{PayOSChecksumKey: checksumKey}, nil)

	in, err := client.VerifyWebhook(context.Background(), body)
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
	if in.OrderCode != 123 || in.Amount != 3000 {
		t.Fatalf("order code/amount = %d/%d, want 123/3000", in.OrderCode, in.Amount)
	}
	if in.Reference != "TF230204212323" || in.Currency != "VND" {
		t.Fatalf("reference/currency = %q/%q, want TF230204212323/VND", in.Reference, in.Currency)
	}
	if in.PaymentLinkID != "124c33293c43417ab7879e14c8d9eb18" {
		t.Fatalf("payment link id = %q", in.PaymentLinkID)
	}
}

// research D3: a body whose signature does not verify is refused and nothing is
// trusted.
func TestVerifyWebhookRefusesATamperedBody(t *testing.T) {
	body := webhookBody(t, documentedData, documentedWebhookSignature)
	client := NewClient(config.PaymentConfig{PayOSChecksumKey: checksumKey}, nil)

	// Flip one byte of the signature.
	tampered := make([]byte, len(body))
	copy(tampered, body)
	for i := range tampered {
		if tampered[i] == '6' {
			tampered[i] = '7'
			break
		}
	}
	if _, err := client.VerifyWebhook(context.Background(), tampered); !errors.Is(err, domainerr.ErrWebhookInvalidSignature) {
		t.Fatalf("a tampered body must be refused, got %v", err)
	}
}

// research D3: a null/undefined data field is rendered as an empty string.
func TestWebhookSignatureRendersNullFieldsAsEmpty(t *testing.T) {
	data := map[string]any{"orderCode": 123, "amount": 3000, "code": nil}

	const documented = "amount=3000&code=&orderCode=123"
	if got, want := signWebhook(checksumKey, data), testHMAC(checksumKey, documented); got != want {
		t.Fatalf("signature = %s, want %s (nulls rendered as empty strings)", got, want)
	}
}

// research D2, D1: CreateLink signs the body and calls POST /v2/payment-requests
// with the merchant headers, and returns the link the provider answered.
func TestCreateLinkPostsSignedAndReturnsTheLink(t *testing.T) {
	var gotPath, gotClientID, gotAPIKey string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotClientID = r.Header.Get("x-client-id")
		gotAPIKey = r.Header.Get("x-api-key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"00","desc":"success","data":{"paymentLinkId":"pl-1","checkoutUrl":"https://pay.example/pl-1","qrCode":"qr-1"}}`))
	}))
	defer server.Close()

	cfg := config.PaymentConfig{PayOSBaseURL: server.URL, PayOSClientID: "cid", PayOSAPIKey: "key", PayOSChecksumKey: checksumKey}
	client := NewClient(cfg, server.Client())

	in := dto.CreateLinkInput{OrderCode: 123, Amount: 3000, Description: "VQRIO123", CancelURL: "https://shop.example/cancel", ReturnURL: "https://shop.example/return"}
	link, err := client.CreateLink(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	if gotPath != "/v2/payment-requests" {
		t.Errorf("path = %q, want /v2/payment-requests", gotPath)
	}
	if gotClientID != "cid" || gotAPIKey != "key" {
		t.Errorf("headers = %q/%q, want cid/key", gotClientID, gotAPIKey)
	}
	if gotBody["signature"] != signCreateLink(checksumKey, in) {
		t.Errorf("body signature = %v, want the computed one", gotBody["signature"])
	}
	if link.PaymentLinkID != "pl-1" || link.CheckoutURL != "https://pay.example/pl-1" || link.QRCode != "qr-1" {
		t.Fatalf("link = %+v, want the provider's data", link)
	}
}

// The provider-side link expires with the order's own payment deadline: a set
// ExpiredAt is sent as PayOS `expiredAt` (Unix seconds).
func TestCreateLinkSendsExpiredAtWhenSet(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"00","desc":"success","data":{"paymentLinkId":"pl-1","checkoutUrl":"https://pay.example/pl-1","qrCode":"qr-1"}}`))
	}))
	defer server.Close()

	cfg := config.PaymentConfig{PayOSBaseURL: server.URL, PayOSClientID: "cid", PayOSAPIKey: "key", PayOSChecksumKey: checksumKey}
	client := NewClient(cfg, server.Client())

	in := dto.CreateLinkInput{OrderCode: 123, Amount: 3000, Description: "VQRIO123", CancelURL: "https://shop.example/cancel", ReturnURL: "https://shop.example/return", ExpiredAt: 1790000000}
	if _, err := client.CreateLink(context.Background(), in); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if gotBody["expiredAt"] != float64(1790000000) {
		t.Errorf("expiredAt = %v, want 1790000000", gotBody["expiredAt"])
	}
}

// A zero ExpiredAt omits the field, so a link without a known order deadline
// keeps the previous behaviour.
func TestCreateLinkOmitsExpiredAtWhenZero(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"00","desc":"success","data":{"paymentLinkId":"pl-1","checkoutUrl":"https://pay.example/pl-1","qrCode":"qr-1"}}`))
	}))
	defer server.Close()

	cfg := config.PaymentConfig{PayOSBaseURL: server.URL, PayOSClientID: "cid", PayOSAPIKey: "key", PayOSChecksumKey: checksumKey}
	client := NewClient(cfg, server.Client())

	in := dto.CreateLinkInput{OrderCode: 123, Amount: 3000, Description: "VQRIO123", CancelURL: "https://shop.example/cancel", ReturnURL: "https://shop.example/return"}
	if _, err := client.CreateLink(context.Background(), in); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if _, ok := gotBody["expiredAt"]; ok {
		t.Errorf("expiredAt = %v, want the field omitted when zero", gotBody["expiredAt"])
	}
}

// research D2: ExpiredAt must not leak into the create-link signature, which the
// PayOS documentation fixes to the five fields amount/cancelUrl/description/
// orderCode/returnUrl.
func TestCreateLinkSignatureIgnoresExpiredAt(t *testing.T) {
	base := dto.CreateLinkInput{
		OrderCode:   123,
		Amount:      3000,
		Description: "VQRIO123",
		CancelURL:   "https://shop.example/cancel",
		ReturnURL:   "https://shop.example/return",
	}
	withExpiry := base
	withExpiry.ExpiredAt = 1790000000

	if got, want := signCreateLink(checksumKey, withExpiry), signCreateLink(checksumKey, base); got != want {
		t.Fatalf("signature with expiredAt = %s, want the same %s without it", got, want)
	}
	if got, want := signCreateLink(checksumKey, withExpiry), "60224b08adffbddddfc1bf4ab0f94111aa450d00b860c5acd27aa9db002592c3"; got != want {
		t.Fatalf("signature = %s, want the documented vector %s", got, want)
	}
}

// research D1: a provider refusal (a non-"00" code) is reported as a provider
// failure, so the use case can answer PAYMENT_PROVIDER_ERROR.
func TestCreateLinkReportsAProviderRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":"231","desc":"amount must be a number"}`))
	}))
	defer server.Close()

	client := NewClient(config.PaymentConfig{PayOSBaseURL: server.URL}, server.Client())
	if _, err := client.CreateLink(context.Background(), dto.CreateLinkInput{OrderCode: 1, Amount: 1}); !errors.Is(err, domainerr.ErrProviderFailure) {
		t.Fatalf("a provider refusal must be a provider failure, got %v", err)
	}
}

// research D1, D9: CancelLink calls POST /v2/payment-requests/{id}/cancel.
func TestCancelLinkPostsToTheCancelEndpoint(t *testing.T) {
	var gotPath, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"code":"00","desc":"success"}`))
	}))
	defer server.Close()

	client := NewClient(config.PaymentConfig{PayOSBaseURL: server.URL}, server.Client())
	if err := client.CancelLink(context.Background(), dto.CancelLinkInput{PaymentLinkID: "pl-1", Reason: "order cancelled"}); err != nil {
		t.Fatalf("CancelLink: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v2/payment-requests/pl-1/cancel" {
		t.Fatalf("cancel called %s %s, want POST /v2/payment-requests/pl-1/cancel", gotMethod, gotPath)
	}
}

// research D11: with no settings the log provider stands in; it returns a
// synthetic link so every non-provider path still runs, and it refuses to verify a
// webhook it cannot check.
func TestLogProviderStandsInWithoutCredentials(t *testing.T) {
	provider := NewLogProvider(nil)

	link, err := provider.CreateLink(context.Background(), dto.CreateLinkInput{OrderCode: 7, Amount: 1000})
	if err != nil || link.CheckoutURL == "" {
		t.Fatalf("the log provider must return a synthetic link, got %+v / %v", link, err)
	}
	if err := provider.CancelLink(context.Background(), dto.CancelLinkInput{OrderCode: 7}); err != nil {
		t.Fatalf("the log provider cancel must be a no-op, got %v", err)
	}
	if _, err := provider.VerifyWebhook(context.Background(), []byte(`{}`)); !errors.Is(err, domainerr.ErrWebhookInvalidSignature) {
		t.Fatalf("the log provider must refuse an unverifiable webhook, got %v", err)
	}
}

// webhookBody builds the JSON envelope {code, desc, success, data, signature}.
func webhookBody(t *testing.T, data map[string]any, signature string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"code":      "00",
		"desc":      "success",
		"success":   true,
		"data":      data,
		"signature": signature,
	})
	if err != nil {
		t.Fatalf("marshal webhook body: %v", err)
	}
	return body
}
