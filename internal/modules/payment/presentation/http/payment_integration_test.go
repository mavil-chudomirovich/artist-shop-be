//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

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

// This file drives US1 end to end against a real PostgreSQL container: the real
// payment repository and the real transaction boundary, the real HTTP surface,
// over an in-memory OrderPayment contract and a deterministic provider. Its
// centre of gravity is SC-001: starting a payment for an awaiting-payment order
// writes exactly one row whose amount equals the order total, and a second start
// adds no row — it reuses the one live link.
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

// fixedProvider returns a deterministic link, so the reuse path can be asserted on
// a stable checkout URL.
type fixedProvider struct {
	link dto.PaymentLink
}

func (p fixedProvider) CreateLink(context.Context, dto.CreateLinkInput) (dto.PaymentLink, error) {
	return p.link, nil
}

func (fixedProvider) CancelLink(context.Context, dto.CancelLinkInput) error { return nil }

func (fixedProvider) VerifyWebhook(context.Context, []byte) (dto.WebhookInput, error) {
	return dto.WebhookInput{}, nil
}

// newPaymentIntegrationFixture boots one migrated container and returns the real
// HTTP surface over the real payment repository.
func newPaymentIntegrationFixture(t *testing.T, target contracts.OrderPaymentTarget) (*pgxpool.Pool, http.HandlerFunc) {
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
