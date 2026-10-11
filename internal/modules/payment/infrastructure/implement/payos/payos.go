package payos

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

// providerCodeSuccess is PayOS's success code: every other value is a refusal
// (research D1).
const providerCodeSuccess = "00"

// createLinkPath is the PayOS Merchant API path that creates a payment request
// (research D1).
const createLinkPath = "/v2/payment-requests"

// Client is the PayOS provider adapter. It is written with the standard library
// only — net/http for the calls and crypto/hmac + crypto/sha256 for the signatures
// — so no dependency is added (ADR 015 §5 D4).
type Client struct {
	baseURL     string
	clientID    string
	apiKey      string
	checksumKey string
	httpClient  *http.Client
}

var _ appinterface.Provider = (*Client)(nil)

// NewClient creates the PayOS client. A nil http.Client becomes a default one with
// a bounded timeout, so a slow provider cannot hold a request open indefinitely.
func NewClient(cfg config.PaymentConfig, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:     strings.TrimRight(cfg.PayOSBaseURL, "/"),
		clientID:    cfg.PayOSClientID,
		apiKey:      cfg.PayOSAPIKey,
		checksumKey: cfg.PayOSChecksumKey,
		httpClient:  httpClient,
	}
}

// CreateLink signs the request and calls POST /v2/payment-requests. A refusal or an
// unreachable provider is reported as domainerr.ErrProviderFailure; nothing is
// written when it fails (FR-002, research D1, D2).
func (c *Client) CreateLink(ctx context.Context, in dto.CreateLinkInput) (dto.PaymentLink, error) {
	body := map[string]any{
		"orderCode":   in.OrderCode,
		"amount":      in.Amount,
		"description": in.Description,
		"cancelUrl":   in.CancelURL,
		"returnUrl":   in.ReturnURL,
		"signature":   signCreateLink(c.checksumKey, in),
	}

	var resp struct {
		Code string `json:"code"`
		Desc string `json:"desc"`
		Data struct {
			PaymentLinkID string `json:"paymentLinkId"`
			CheckoutURL   string `json:"checkoutUrl"`
			QRCode        string `json:"qrCode"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, createLinkPath, body, &resp); err != nil {
		return dto.PaymentLink{}, err
	}
	if resp.Code != providerCodeSuccess {
		return dto.PaymentLink{}, domainerr.ProviderFailure(fmt.Errorf("payos refused the link: %s %s", resp.Code, resp.Desc))
	}
	return dto.PaymentLink{
		PaymentLinkID: resp.Data.PaymentLinkID,
		CheckoutURL:   resp.Data.CheckoutURL,
		QRCode:        resp.Data.QRCode,
	}, nil
}

// CancelLink calls POST /v2/payment-requests/{id}/cancel. The {id} is the payment
// link handle when present, otherwise the order code (research D1, D9).
func (c *Client) CancelLink(ctx context.Context, in dto.CancelLinkInput) error {
	id := in.PaymentLinkID
	if id == "" {
		id = strconv.FormatInt(in.OrderCode, 10)
	}
	path := fmt.Sprintf("/v2/payment-requests/%s/cancel", id)

	var resp struct {
		Code string `json:"code"`
		Desc string `json:"desc"`
	}
	if err := c.do(ctx, http.MethodPost, path, map[string]any{"reason": in.Reason}, &resp); err != nil {
		return err
	}
	if resp.Code != providerCodeSuccess {
		return domainerr.ProviderFailure(fmt.Errorf("payos refused the cancel: %s %s", resp.Code, resp.Desc))
	}
	return nil
}

// VerifyWebhook verifies the signature on a raw webhook body before anything is
// trusted, and returns the verified data fields. A body whose signature does not
// verify is refused with domainerr.ErrWebhookInvalidSignature; a body that does not
// parse returns a plain error presentation maps to MALFORMED_REQUEST (FR-019,
// research D3).
func (c *Client) VerifyWebhook(_ context.Context, body []byte) (dto.WebhookInput, error) {
	var payload struct {
		Code      string         `json:"code"`
		Desc      string         `json:"desc"`
		Success   bool           `json:"success"`
		Data      map[string]any `json:"data"`
		Signature string         `json:"signature"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	// UseNumber keeps an order code exact: a float64 would lose the precision of a
	// 53-bit integer before it could be compared.
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return dto.WebhookInput{}, fmt.Errorf("malformed payos webhook: %w", err)
	}
	if payload.Data == nil || payload.Signature == "" {
		return dto.WebhookInput{}, domainerr.ErrWebhookInvalidSignature
	}

	expected := signWebhook(c.checksumKey, payload.Data)
	if !hmac.Equal([]byte(expected), []byte(payload.Signature)) {
		return dto.WebhookInput{}, domainerr.ErrWebhookInvalidSignature
	}
	return toWebhookInput(payload.Data), nil
}

// do performs one authenticated request and decodes the JSON response into out.
// Every transport failure and non-2xx status is reported as a provider failure, so
// the use case answers PAYMENT_PROVIDER_ERROR without inspecting the provider's own
// error (FR-002).
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return domainerr.ProviderFailure(err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return domainerr.ProviderFailure(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-client-id", c.clientID)
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domainerr.ProviderFailure(fmt.Errorf("payos unreachable: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return domainerr.ProviderFailure(fmt.Errorf("payos status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail))))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return domainerr.ProviderFailure(fmt.Errorf("decode payos response: %w", err))
	}
	return nil
}

// signCreateLink computes the create-link signature: HMAC_SHA256 over exactly
// amount, cancelUrl, description, orderCode and returnUrl, in that (alphabetical)
// order, formatted key=value&... (research D2).
func signCreateLink(checksumKey string, in dto.CreateLinkInput) string {
	data := fmt.Sprintf("amount=%d&cancelUrl=%s&description=%s&orderCode=%d&returnUrl=%s",
		in.Amount, in.CancelURL, in.Description, in.OrderCode, in.ReturnURL)
	return hmacSHA256Hex(checksumKey, data)
}

// signWebhook computes the webhook signature: HMAC_SHA256 over the data object's
// fields sorted alphabetically, formatted key=value&..., with null/undefined
// rendered as an empty string (research D3).
func signWebhook(checksumKey string, data map[string]any) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+stringify(data[key]))
	}
	return hmacSHA256Hex(checksumKey, strings.Join(parts, "&"))
}

// hmacSHA256Hex is the one HMAC used by both signatures.
func hmacSHA256Hex(key, data string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// stringify renders one webhook data value the way the documented algorithm does:
// null/undefined as the empty string, scalars as their text, and anything else as
// its JSON (encoding/json sorts a nested object's keys, so nested content is
// deterministic too).
func stringify(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}

// toWebhookInput reads the verified data object into the module's DTO. A missing
// field is its zero value.
func toWebhookInput(data map[string]any) dto.WebhookInput {
	return dto.WebhookInput{
		OrderCode:           toInt64(data["orderCode"]),
		Amount:              toInt64(data["amount"]),
		Description:         toString(data["description"]),
		Reference:           toString(data["reference"]),
		TransactionDateTime: toString(data["transactionDateTime"]),
		Currency:            toString(data["currency"]),
		PaymentLinkID:       toString(data["paymentLinkId"]),
		AccountNumber:       toString(data["accountNumber"]),
		Code:                toString(data["code"]),
		Desc:                toString(data["desc"]),
	}
}

// toInt64 reads an integer from a decoded JSON value. json.Number is used when
// present; anything else is coerced from a string or a float.
func toInt64(value any) int64 {
	switch typed := value.(type) {
	case json.Number:
		n, _ := typed.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(typed, 10, 64)
		return n
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

// toString reads a string from a decoded JSON value; anything else is its text.
func toString(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return stringify(value)
}

// LogProvider is the fallback provider used when the PayOS settings are empty. It
// returns a synthetic link so every non-provider path still runs without
// credentials, and it refuses to verify a webhook it cannot check, because there is
// no checksum key to check against (FR-019, research D11).
type LogProvider struct {
	logger *slog.Logger
}

var _ appinterface.Provider = (*LogProvider)(nil)

// NewLogProvider creates the log-only provider. A nil logger falls back to the
// default so a construction cannot panic.
func NewLogProvider(logger *slog.Logger) *LogProvider {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogProvider{logger: logger}
}

// CreateLink returns a synthetic link and logs it. A developer without PayOS
// credentials can therefore run scenario 1 of the quickstart.
func (p *LogProvider) CreateLink(ctx context.Context, in dto.CreateLinkInput) (dto.PaymentLink, error) {
	p.logger.InfoContext(ctx, "payos (log provider): create link",
		slog.Int64("orderCode", in.OrderCode),
		slog.Int64("amount", in.Amount),
	)
	return dto.PaymentLink{
		PaymentLinkID: fmt.Sprintf("log-%d", in.OrderCode),
		CheckoutURL:   fmt.Sprintf("https://log.payos.local/payment/%d", in.OrderCode),
		QRCode:        fmt.Sprintf("log-qr-%d", in.OrderCode),
	}, nil
}

// CancelLink logs the cancel and reports success; there is no live link to cancel.
func (p *LogProvider) CancelLink(ctx context.Context, in dto.CancelLinkInput) error {
	p.logger.InfoContext(ctx, "payos (log provider): cancel link",
		slog.Int64("orderCode", in.OrderCode),
		slog.String("reason", in.Reason),
	)
	return nil
}

// VerifyWebhook refuses every webhook: without a checksum key the log provider
// cannot check a signature, and an unverified event is never trusted.
func (p *LogProvider) VerifyWebhook(ctx context.Context, _ []byte) (dto.WebhookInput, error) {
	p.logger.WarnContext(ctx, "payos (log provider): refusing an unverifiable webhook")
	return dto.WebhookInput{}, domainerr.ErrWebhookInvalidSignature
}

// NewProvider returns the PayOS client when the credentials are set, otherwise the
// log-only provider, so a missing configuration disables the live provider and
// nothing else (research D11).
func NewProvider(cfg config.PaymentConfig, httpClient *http.Client, logger *slog.Logger) appinterface.Provider {
	if !cfg.IsConfigured() {
		return NewLogProvider(logger)
	}
	return NewClient(cfg, httpClient)
}
