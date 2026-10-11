//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/dto"
	paymentimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/implement"
	paymentmapper "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/mapper"
	paymentpostgres "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/infrastructure/implement/postgres"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database/migrate"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/testsupport"
)

// This file drives the payment HTTP surface end to end against a real PostgreSQL
// container: the real payment repository and the real transaction boundary, the
// real HTTP surface, over an in-memory OrderPayment contract and a deterministic
// provider.
//
// US1's centre of gravity is SC-001: starting a payment for an awaiting-payment
// order writes exactly one row whose amount equals the order total, and a second
// start adds no row. US2's is SC-002: a verified webhook applies the sale once, a
// replay changes nothing, and two concurrent confirmations apply it once.
//
// Docker is required to run it; the file still compiles without a container so
// `go vet -tags integration ./...` covers it.

// integrationClock is the production-like clock for the integration run.
type integrationClock struct{}

func (integrationClock) Now() time.Time { return time.Now().UTC() }

// stubOrderPayment answers the OrderPayment contract with a fixed awaiting-payment
// target. US1 never drives the paid transition.
type stubOrderPayment struct {
	target contracts.OrderPaymentTarget
}

func (s stubOrderPayment) PaymentTarget(context.Context, uuid.UUID) (contracts.OrderPaymentTarget, error) {
	return s.target, nil
}

func (stubOrderPayment) MarkPaid(context.Context, uuid.UUID, string, int64) error { return nil }

// recordingOrderPayment answers the OrderPayment contract and records the paid
// transition it was asked to drive, so US2 can prove the sale is applied once even
// under concurrent confirmations. Its own state stands in for module 07's order
// and module 05's stock: driving MarkPaid twice would be two sales.
type recordingOrderPayment struct {
	target contracts.OrderPaymentTarget

	mu            sync.Mutex
	markPaidCalls int
	lastSource    string
	lastVersion   int64
}

func (o *recordingOrderPayment) PaymentTarget(context.Context, uuid.UUID) (contracts.OrderPaymentTarget, error) {
	return o.target, nil
}

func (o *recordingOrderPayment) MarkPaid(_ context.Context, _ uuid.UUID, source string, version int64) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.markPaidCalls++
	o.lastSource = source
	o.lastVersion = version
	return nil
}

// calls returns how many times the paid transition was driven, and with what.
func (o *recordingOrderPayment) calls() (int, string, int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.markPaidCalls, o.lastSource, o.lastVersion
}

// fixedProvider returns a deterministic link, so the reuse path can be asserted on
// a stable checkout URL, and a fixed verified webhook, so US2's apply path is
// driven without a live signature.
type fixedProvider struct {
	link    dto.PaymentLink
	webhook dto.WebhookInput
}

func (p fixedProvider) CreateLink(context.Context, dto.CreateLinkInput) (dto.PaymentLink, error) {
	return p.link, nil
}

func (fixedProvider) CancelLink(context.Context, dto.CancelLinkInput) error { return nil }

func (p fixedProvider) VerifyWebhook(context.Context, []byte) (dto.WebhookInput, error) {
	return p.webhook, nil
}

// bootPaymentPool boots one migrated PostgreSQL container and connects to it.
func bootPaymentPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := testsupport.PostgresDSN(t)
	ctx := context.Background()

	runner, err := migrate.New(dsn, 30*time.Second)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer runner.Close()
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newPaymentIntegrationFixture returns the real HTTP surface over the real payment
// repository for US1.
func newPaymentIntegrationFixture(t *testing.T, target contracts.OrderPaymentTarget) (*pgxpool.Pool, http.HandlerFunc) {
	t.Helper()
	pool := bootPaymentPool(t)

	service := paymentimplement.New(paymentimplement.Service{
		Payments: paymentpostgres.NewPaymentRepository(pool),
		Orders:   stubOrderPayment{target: target},
		Provider: fixedProvider{link: dto.PaymentLink{
			PaymentLinkID: "pl-fixed",
			CheckoutURL:   "https://pay.example/fixed",
			QRCode:        "qr-fixed",
		}},
		Tx:        &database.DB{Pool: pool},
		Clock:     integrationClock{},
		Mapper:    paymentmapper.New(),
		ReturnURL: "https://shop.example/payment/return",
		CancelURL: "https://shop.example/payment/cancel",
	})
	router := newPaymentRouter(service)
	return pool, func(w http.ResponseWriter, r *http.Request) { router.ServeHTTP(w, r) }
}

// newWebhookIntegrationFixture returns the public callback over the real payment
// repository and a recording order contract for US2.
func newWebhookIntegrationFixture(t *testing.T, webhook dto.WebhookInput) (*pgxpool.Pool, *recordingOrderPayment, http.Handler) {
	t.Helper()
	pool := bootPaymentPool(t)
	orders := &recordingOrderPayment{}
	service := paymentimplement.New(paymentimplement.Service{
		Payments: paymentpostgres.NewPaymentRepository(pool),
		Orders:   orders,
		Provider: fixedProvider{webhook: webhook},
		Tx:       &database.DB{Pool: pool},
		Clock:    integrationClock{},
		Mapper:   paymentmapper.New(),
	})
	handler := New(service, testLogger)
	root := chi.NewRouter()
	root.Mount("/api/v1/payments/payos/webhook", handler.WebhookRouter())
	return pool, orders, root
}

// insertPendingPayment writes one live payment directly, so the webhook lookup has
// a row to match by order code.
func insertPendingPayment(t *testing.T, pool *pgxpool.Pool, orderID, userID uuid.UUID, orderCode, amount, version int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO payments
			(id, order_id, user_id, order_code, amount, currency, description,
			 order_version, status, provider, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'VND', 'DH0001', $6, 'PENDING', 'payos', now(), now())`,
		uuid.New(), orderID, userID, orderCode, amount, version)
	if err != nil {
		t.Fatalf("insert pending payment: %v", err)
	}
}

// deliverWebhook posts a callback body to the public route. The fixture's provider
// is the signature check's stand-in, so the body's content is not inspected here.
func deliverWebhook(handler http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/payments/payos/webhook",
		strings.NewReader(`{"code":"00","desc":"success","success":true,"data":{},"signature":"fixed"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// appliedEvents counts the PAYMENT_APPLIED rows the order code's payment carries.
func appliedEvents(t *testing.T, pool *pgxpool.Pool, orderCode int64) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM payment_events e
		JOIN payments p ON p.id = e.payment_id
		WHERE p.order_code = $1 AND e.action = 'PAYMENT_APPLIED'`, orderCode).Scan(&count); err != nil {
		t.Fatalf("count PAYMENT_APPLIED events: %v", err)
	}
	return count
}

// SC-001: starting a payment writes one row whose amount equals the order total;
// a second start adds no row and returns the same link.
func TestStartPaymentWritesOneRowAndReusesTheLink(t *testing.T) {
	orderID := uuid.New()
	target := contracts.OrderPaymentTarget{
		UserID:           testCustomerID,
		Amount:           120000,
		Currency:         "VND",
		Version:          2,
		AwaitingPayment:  true,
		PaymentExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	pool, handler := newPaymentIntegrationFixture(t, target)

	first := perform(handler, "POST", paymentPath(orderID), "customer-token")
	if first.Code != 200 {
		t.Fatalf("first start status = %d, want 200; body: %s", first.Code, first.Body.String())
	}

	var count int
	var amount, orderVersion int64
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*), max(amount), max(order_version) FROM payments WHERE order_id = $1`,
		orderID).Scan(&count, &amount, &orderVersion); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if count != 1 {
		t.Fatalf("a start wrote %d payments, want exactly 1", count)
	}
	if amount != 120000 {
		t.Fatalf("amount = %d, want the order total 120000", amount)
	}
	if orderVersion != 2 {
		t.Fatalf("order_version = %d, want the order's 2", orderVersion)
	}

	// The PAYMENT_CREATED event was written beside the payment, in the same
	// transaction (data-model.md, FR-022).
	var events int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM payment_events WHERE action = 'PAYMENT_CREATED'`).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 1 {
		t.Fatalf("PAYMENT_CREATED events = %d, want exactly 1", events)
	}

	// A second start reuses the live payment: no second row, the same link.
	second := perform(handler, "POST", paymentPath(orderID), "customer-token")
	if second.Code != 200 {
		t.Fatalf("second start status = %d, want 200; body: %s", second.Code, second.Body.String())
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM payments WHERE order_id = $1`, orderID).Scan(&count); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if count != 1 {
		t.Fatalf("a repeat start wrote %d payments, want the one live row", count)
	}

	// The read returns the live payment.
	read := perform(handler, "GET", paymentPath(orderID), "customer-token")
	if read.Code != 200 {
		t.Fatalf("read status = %d, want 200; body: %s", read.Code, read.Body.String())
	}

	// FR-017: once the payment reaches a terminal state it must still be
	// readable, not 404 — the customer sees the order's payment state.
	if _, err := pool.Exec(context.Background(),
		`UPDATE payments SET status = 'PAID', provider_reference = 'ref-1' WHERE order_id = $1`,
		orderID); err != nil {
		t.Fatalf("mark the payment paid: %v", err)
	}
	terminal := perform(handler, "GET", paymentPath(orderID), "customer-token")
	if terminal.Code != 200 {
		t.Fatalf("terminal read status = %d, want 200; body: %s", terminal.Code, terminal.Body.String())
	}
	var body paymentBody
	if err := json.Unmarshal(terminal.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.Status != "PAID" {
		t.Fatalf("terminal read status = %q, want PAID", body.Data.Status)
	}
}

// SC-002: delivering a verified webhook applies the sale once and a replay changes
// nothing. The payment becomes PAID with the provider reference, one
// PAYMENT_APPLIED event is recorded, and module 07's paid transition is driven
// exactly once with the payment's own stable reference and the stored version.
func TestWebhookAppliesOnceAndReplayChangesNothing(t *testing.T) {
	const orderCode = 910001
	orderID := uuid.New()
	webhook := dto.WebhookInput{OrderCode: orderCode, Amount: 120000, Reference: "TF-INT-1", Currency: "VND"}
	pool, orders, handler := newWebhookIntegrationFixture(t, webhook)
	insertPendingPayment(t, pool, orderID, testCustomerID, orderCode, 120000, 2)

	first := deliverWebhook(handler)
	if first.Code != http.StatusOK {
		t.Fatalf("first delivery status = %d, want 200; body: %s", first.Code, first.Body.String())
	}

	var status string
	var reference *string
	if err := pool.QueryRow(context.Background(),
		`SELECT status, provider_reference FROM payments WHERE order_code = $1`, orderCode).Scan(&status, &reference); err != nil {
		t.Fatalf("read payment: %v", err)
	}
	if status != "PAID" {
		t.Fatalf("status = %q, want PAID", status)
	}
	if reference == nil || *reference != "TF-INT-1" {
		t.Fatalf("provider_reference = %v, want TF-INT-1", reference)
	}

	if got := appliedEvents(t, pool, orderCode); got != 1 {
		t.Fatalf("PAYMENT_APPLIED events = %d, want exactly 1", got)
	}
	if calls, source, version := orders.calls(); calls != 1 || source != "payos:910001" || version != 2 {
		t.Fatalf("the sale was driven %d times with %q/%d, want once with payos:910001/2", calls, source, version)
	}

	// A replay is a no-op success: nothing applies a second time.
	second := deliverWebhook(handler)
	if second.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want 200; body: %s", second.Code, second.Body.String())
	}
	if got := appliedEvents(t, pool, orderCode); got != 1 {
		t.Fatalf("a replay wrote %d PAYMENT_APPLIED events, want still 1", got)
	}
	if calls, _, _ := orders.calls(); calls != 1 {
		t.Fatalf("a replay drove the sale %d times, want still 1", calls)
	}
}

// SC-002: two concurrent confirmations of the same webhook both answer 200 and the
// sale is applied once — the row lock serialises them, and the second finds the
// payment already PAID.
func TestWebhookConcurrentConfirmationsApplyOnce(t *testing.T) {
	const orderCode = 910002
	orderID := uuid.New()
	webhook := dto.WebhookInput{OrderCode: orderCode, Amount: 120000, Reference: "TF-INT-2", Currency: "VND"}
	pool, orders, handler := newWebhookIntegrationFixture(t, webhook)
	insertPendingPayment(t, pool, orderID, testCustomerID, orderCode, 120000, 2)

	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			codes[idx] = deliverWebhook(handler).Code
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("confirmation %d status = %d, want 200", i, code)
		}
	}
	if got := appliedEvents(t, pool, orderCode); got != 1 {
		t.Fatalf("concurrent confirmations wrote %d PAYMENT_APPLIED events, want exactly 1", got)
	}
	if calls, _, _ := orders.calls(); calls != 1 {
		t.Fatalf("concurrent confirmations drove the sale %d times, want exactly 1", calls)
	}
}
