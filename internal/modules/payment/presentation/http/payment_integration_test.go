//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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
// and module 05's stock: driving MarkPaid twice would be two sales. A markErr
// stands in for module 07's version/state refusal (US3), so a stale or late
// confirmation can be driven without a live order.
type recordingOrderPayment struct {
	target contracts.OrderPaymentTarget

	mu            sync.Mutex
	markPaidCalls int
	lastSource    string
	lastVersion   int64
	markErr       error
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
	return o.markErr
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

// unmatchedEvents counts the PAYMENT_UNMATCHED rows the order code's payment
// carries â€” the operator's reconciliation trail (FR-014, research D8).
func unmatchedEvents(t *testing.T, pool *pgxpool.Pool, orderCode int64) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM payment_events e
		JOIN payments p ON p.id = e.payment_id
		WHERE p.order_code = $1 AND e.action = 'PAYMENT_UNMATCHED'`, orderCode).Scan(&count); err != nil {
		t.Fatalf("count PAYMENT_UNMATCHED events: %v", err)
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
	// readable, not 404 â€” the customer sees the order's payment state.
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
// sale is applied once â€” the row lock serialises them, and the second finds the
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

// SC-003: a stale confirmation â€” the order was edited and re-confirmed after the
// link was made, so its content version moved â€” is recorded UNMATCHED with a
// reason and never applied: the order is not paid and the goods stay unsold. A
// re-delivered confirmation records no duplicate (FR-012, FR-014, research D8).
func TestWebhookRecordsAStaleConfirmationWithoutApplying(t *testing.T) {
	const orderCode = 910003
	orderID := uuid.New()
	webhook := dto.WebhookInput{OrderCode: orderCode, Amount: 120000, Reference: "TF-INT-3", Currency: "VND"}
	pool, orders, handler := newWebhookIntegrationFixture(t, webhook)
	// The order's content changed after this payment link was made: module 07's
	// adapter reports contracts.ErrOrderStale.
	orders.markErr = contracts.ErrOrderStale
	insertPendingPayment(t, pool, orderID, testCustomerID, orderCode, 120000, 2)

	rec := deliverWebhook(handler)
	if rec.Code != http.StatusOK {
		t.Fatalf("stale confirmation status = %d, want 200 (the provider stops retrying); body: %s", rec.Code, rec.Body.String())
	}

	var status string
	var reason *string
	if err := pool.QueryRow(context.Background(),
		`SELECT status, unmatched_reason FROM payments WHERE order_code = $1`, orderCode).Scan(&status, &reason); err != nil {
		t.Fatalf("read payment: %v", err)
	}
	if status != "UNMATCHED" {
		t.Fatalf("status = %q, want UNMATCHED", status)
	}
	if reason == nil || *reason == "" {
		t.Fatalf("unmatched_reason = %v, want a reason naming the mismatch", reason)
	}
	if got := appliedEvents(t, pool, orderCode); got != 0 {
		t.Fatalf("PAYMENT_APPLIED events = %d, want none (the order is untouched)", got)
	}
	if got := unmatchedEvents(t, pool, orderCode); got != 1 {
		t.Fatalf("PAYMENT_UNMATCHED events = %d, want exactly 1", got)
	}
	// The paid transition was driven once and refused, so the goods stay unsold.
	if calls, _, _ := orders.calls(); calls != 1 {
		t.Fatalf("the sale was driven %d times, want once (then refused)", calls)
	}

	// A re-delivery is a no-op: still one UNMATCHED event, no second one.
	again := deliverWebhook(handler)
	if again.Code != http.StatusOK {
		t.Fatalf("re-delivered stale confirmation status = %d, want 200; body: %s", again.Code, again.Body.String())
	}
	if got := unmatchedEvents(t, pool, orderCode); got != 1 {
		t.Fatalf("a re-delivery wrote %d PAYMENT_UNMATCHED events, want still 1", got)
	}
	if calls, _, _ := orders.calls(); calls != 1 {
		t.Fatalf("a re-delivery drove the sale %d times, want still 1", calls)
	}
}

// cancelProvider is the integration provider for US4. It answers create and verify
// with nothing, and records the cancel input, so a test can prove which link the
// module asked to cancel and can refuse the cancel to exercise the best-effort
// path.
type cancelProvider struct {
	cancelErr  error
	cancels    int
	lastCancel dto.CancelLinkInput
}

func (p *cancelProvider) CreateLink(context.Context, dto.CreateLinkInput) (dto.PaymentLink, error) {
	return dto.PaymentLink{}, nil
}

func (p *cancelProvider) CancelLink(_ context.Context, in dto.CancelLinkInput) error {
	p.cancels++
	p.lastCancel = in
	return p.cancelErr
}

func (p *cancelProvider) VerifyWebhook(context.Context, []byte) (dto.WebhookInput, error) {
	return dto.WebhookInput{}, nil
}

// newCancelIntegrationFixture returns the real payment use cases over the real
// repository and a provider whose cancel can be refused, so US4's cancel path is
// proven against real PostgreSQL.
func newCancelIntegrationFixture(t *testing.T, provider *cancelProvider) (*pgxpool.Pool, *paymentimplement.Service) {
	t.Helper()
	pool := bootPaymentPool(t)
	service := paymentimplement.New(paymentimplement.Service{
		Payments: paymentpostgres.NewPaymentRepository(pool),
		Provider: provider,
		Tx:       &database.DB{Pool: pool},
		Clock:    integrationClock{},
		Mapper:   paymentmapper.New(),
	})
	return pool, service
}

// paymentStatusOf reads one order's payment status.
func paymentStatusOf(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM payments WHERE order_id = $1`, orderID).Scan(&status); err != nil {
		t.Fatalf("read payment status: %v", err)
	}
	return status
}

// cancelEvents counts the PAYMENT_CANCELLED rows the order's payment carries.
func cancelEvents(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM payment_events e
		JOIN payments p ON p.id = e.payment_id
		WHERE p.order_id = $1 AND e.action = 'PAYMENT_CANCELLED'`, orderID).Scan(&count); err != nil {
		t.Fatalf("count PAYMENT_CANCELLED events: %v", err)
	}
	return count
}

// SC-004, FR-015, research D9: cancelling an order cancels its live payment â€” the
// provider is asked to cancel the link, the payment row becomes CANCELLED and the
// PAYMENT_CANCELLED event is recorded, all against real PostgreSQL. The goods
// release is module 07's own move and is proven by its lifecycle integration
// test; here the payment half of SC-004 is what is asserted.
func TestCancellingAnOrderCancelsItsPaymentAndRecordsTheEvent(t *testing.T) {
	provider := &cancelProvider{}
	pool, service := newCancelIntegrationFixture(t, provider)
	orderID := uuid.New()
	insertPendingPayment(t, pool, orderID, testCustomerID, 920001, 120000, 2)

	service.CancelForOrder(context.Background(), orderID)

	if provider.cancels != 1 {
		t.Fatalf("the provider cancel was called %d times, want exactly 1", provider.cancels)
	}
	if provider.lastCancel.OrderCode != 920001 {
		t.Errorf("provider cancel order code = %d, want the stored 920001", provider.lastCancel.OrderCode)
	}
	if got := paymentStatusOf(t, pool, orderID); got != "CANCELLED" {
		t.Fatalf("payment status = %q, want CANCELLED", got)
	}
	if got := cancelEvents(t, pool, orderID); got != 1 {
		t.Fatalf("PAYMENT_CANCELLED events = %d, want exactly 1", got)
	}
}

// SC-004, FR-016, research D9: a provider that refuses the cancel does not fail â€”
// the payment is left live for reconciliation and no PAYMENT_CANCELLED event is
// recorded, so the order's already-committed cancellation (and its released
// goods) is never affected.
func TestCancellingAnOrderWithAProviderFailureLeavesThePaymentLive(t *testing.T) {
	provider := &cancelProvider{cancelErr: errors.New("provider refused the cancel")}
	pool, service := newCancelIntegrationFixture(t, provider)
	orderID := uuid.New()
	insertPendingPayment(t, pool, orderID, testCustomerID, 920002, 120000, 2)

	service.CancelForOrder(context.Background(), orderID)

	if provider.cancels != 1 {
		t.Fatalf("the provider cancel was called %d times, want exactly 1", provider.cancels)
	}
	if got := paymentStatusOf(t, pool, orderID); got != "PENDING" {
		t.Fatalf("payment status = %q, want PENDING â€” left live for reconciliation on a provider failure", got)
	}
	if got := cancelEvents(t, pool, orderID); got != 0 {
		t.Fatalf("PAYMENT_CANCELLED events = %d, want none when the provider refused", got)
	}
}

// FR-015: an order with no live payment is a no-op â€” the provider is never called
// and nothing is written.
func TestCancellingAnOrderWithNoLivePaymentIsANoOp(t *testing.T) {
	provider := &cancelProvider{}
	_, service := newCancelIntegrationFixture(t, provider)

	service.CancelForOrder(context.Background(), uuid.New())

	if provider.cancels != 0 {
		t.Fatalf("the provider cancel was called %d times for an order with no live payment, want none", provider.cancels)
	}
}

// newAdminListIntegrationFixture returns the real operator list over the real
// payment repository, mounted the way the composition root mounts it â€” once at
// /admin/payments behind the administrator role guard.
func newAdminListIntegrationFixture(t *testing.T) (*pgxpool.Pool, http.Handler) {
	t.Helper()
	pool := bootPaymentPool(t)
	service := paymentimplement.New(paymentimplement.Service{
		Payments: paymentpostgres.NewPaymentRepository(pool),
		Tx:       &database.DB{Pool: pool},
		Clock:    integrationClock{},
		Mapper:   paymentmapper.New(),
	})
	handler := New(service, testLogger)
	root := chi.NewRouter()
	root.Mount(adminPaymentsPath, handler.AdminRouter(paymentAdminHooks()))
	return pool, root
}

// insertPayment writes one payment directly with an explicit status, reason and
// creation instant, so the operator list's ordering and its status filter can be
// asserted against real rows. A PENDING/PAID/CANCELLED row carries no reason and
// an UNMATCHED row carries one, satisfying the storage check.
func insertPayment(t *testing.T, pool *pgxpool.Pool, orderID, userID uuid.UUID, orderCode, amount int64, status, reason string, createdAt time.Time) {
	t.Helper()
	var reasonArg any
	if reason != "" {
		reasonArg = reason
	}
	_, err := pool.Exec(context.Background(), `
		INSERT INTO payments
			(id, order_id, user_id, order_code, amount, currency, description,
			 order_version, status, provider, unmatched_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'VND', 'DH0001', 2, $6, 'payos', $7, $8, $8)`,
		uuid.New(), orderID, userID, orderCode, amount, status, reasonArg, createdAt)
	if err != nil {
		t.Fatalf("insert payment: %v", err)
	}
}

// SC-005, FR-018: the operator's list returns every payment, newest first, and
// status=UNMATCHED returns only the reconciliation queue â€” proven against real
// PostgreSQL, where the repository owns the ordering and the filter.
func TestAdminListReturnsEveryPaymentAndFiltersTheReconciliationQueue(t *testing.T) {
	pool, handler := newAdminListIntegrationFixture(t)
	base := time.Now().UTC()

	// The newest is UNMATCHED (the queue), then a PAID and a PENDING; the older
	// two belong to other customers, so the list is unfiltered by owner.
	insertPayment(t, pool, uuid.New(), testCustomerID, 930003, 120000, "UNMATCHED", "amount mismatch", base.Add(2*time.Minute))
	insertPayment(t, pool, uuid.New(), testCustomerTwoID, 930002, 120000, "PAID", "", base.Add(time.Minute))
	insertPayment(t, pool, uuid.New(), testCustomerTwoID, 930001, 120000, "PENDING", "", base)

	all := perform(handler, http.MethodGet, adminPaymentsPath, "admin-token")
	if all.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body: %s", all.Code, all.Body.String())
	}
	var body adminPaymentListBody
	if err := json.Unmarshal(all.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode the list body %q: %v", all.Body.String(), err)
	}
	if len(body.Data) != 3 || body.Meta.Total != 3 {
		t.Fatalf("the list must return every payment, got %d rows (total %d)", len(body.Data), body.Meta.Total)
	}
	if body.Data[0].OrderCode != 930003 || body.Data[0].Status != "UNMATCHED" {
		t.Fatalf("the first row must be the newest payment, got %+v", body.Data[0])
	}
	if body.Data[0].UserID != testCustomerID || body.Data[0].UnmatchedReason != "amount mismatch" {
		t.Fatalf("the row must carry its owner and reason, got %+v", body.Data[0])
	}
	if body.Data[2].OrderCode != 930001 || body.Data[2].Status != "PENDING" {
		t.Fatalf("the last row must be the oldest payment, got %+v", body.Data[2])
	}

	// status=UNMATCHED is the reconciliation queue: exactly the one row.
	queue := perform(handler, http.MethodGet, adminPaymentsPath+"?status=UNMATCHED", "admin-token")
	if queue.Code != http.StatusOK {
		t.Fatalf("queue status = %d, want 200; body: %s", queue.Code, queue.Body.String())
	}
	var queueBody adminPaymentListBody
	if err := json.Unmarshal(queue.Body.Bytes(), &queueBody); err != nil {
		t.Fatalf("decode the queue body %q: %v", queue.Body.String(), err)
	}
	if len(queueBody.Data) != 1 || queueBody.Meta.Total != 1 {
		t.Fatalf("the reconciliation queue must hold exactly one payment, got %d (total %d)", len(queueBody.Data), queueBody.Meta.Total)
	}
	if queueBody.Data[0].Status != "UNMATCHED" || queueBody.Data[0].OrderCode != 930003 {
		t.Fatalf("the queue row = %+v, want the UNMATCHED 930003", queueBody.Data[0])
	}
}
