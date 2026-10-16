package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is the unit contract of module 07's OrderPayment adapter. It proves
// the adapter answers the order's payment-relevant facts, translates a missing
// order and the version/state refusals onto the contract's sentinels, and
// delegates the paid transition with the version it was given — the boundary the
// payment module (08) depends on (research D5).

// fakeOrders answers the repository read the adapter needs. Embedding the
// interface makes any method the adapter drifts into panic loudly rather than
// silently pass.
type fakeOrders struct {
	appinterface.OrderRepository
	order *model.Order
	err   error
}

func (f *fakeOrders) FindByID(_ context.Context, _ uuid.UUID) (*model.Order, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.order, nil
}

// fakeService records the paid transition the adapter delegates, so a test can
// prove the version it was handed crosses the boundary unchanged.
type fakeService struct {
	appinterface.OrderService
	called          bool
	orderID         uuid.UUID
	sourceReference string
	expectedVersion int64
	err             error
}

func (f *fakeService) MarkPaid(_ context.Context, orderID uuid.UUID, sourceReference string, expectedVersion int64) error {
	f.called = true
	f.orderID = orderID
	f.sourceReference = sourceReference
	f.expectedVersion = expectedVersion
	return f.err
}

func awaitingPaymentOrder(owner uuid.UUID) *model.Order {
	deadline := time.Date(2026, time.October, 11, 13, 0, 0, 0, time.UTC)
	order := model.NewOrder(owner, model.Address{RecipientName: "Nguyễn Văn A"}, []model.OrderLine{{
		ProductID: uuid.New(), ProductName: "Tranh", Slug: "tranh",
		UnitPrice: model.Price{Amount: 120000, Currency: "VND"}, Quantity: 2,
	}}, time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC))
	order.Status = constant.StatusPaymentPending
	order.Version = 2
	order.PaymentExpiresAt = &deadline
	return order
}

// research D5: PaymentTarget answers the owner, the total (no shipping fee), the
// content version, the deadline and the awaiting-payment flag.
func TestPaymentTargetAnswersTheOrdersPaymentFacts(t *testing.T) {
	owner := uuid.New()
	order := awaitingPaymentOrder(owner)
	adapter := New(&fakeOrders{order: order}, &fakeService{})

	target, err := adapter.PaymentTarget(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("PaymentTarget: %v", err)
	}
	if target.UserID != owner {
		t.Errorf("UserID = %s, want %s", target.UserID, owner)
	}
	if target.Amount != order.Total.Amount || target.Currency != order.Total.Currency {
		t.Errorf("amount = %d %s, want the order total %d %s",
			target.Amount, target.Currency, order.Total.Amount, order.Total.Currency)
	}
	if target.Version != order.Version {
		t.Errorf("Version = %d, want %d", target.Version, order.Version)
	}
	if !target.PaymentExpiresAt.Equal(*order.PaymentExpiresAt) {
		t.Errorf("PaymentExpiresAt = %v, want %v", target.PaymentExpiresAt, *order.PaymentExpiresAt)
	}
	if !target.AwaitingPayment {
		t.Error("AwaitingPayment = false, want true for a PAYMENT_PENDING order")
	}
}

// research D5: an order that is not awaiting payment carries the flag false, so
// the payment module never starts a payment for it.
func TestPaymentTargetFlagsAnOrderNotAwaitingPayment(t *testing.T) {
	order := awaitingPaymentOrder(uuid.New())
	order.Status = constant.StatusPending
	adapter := New(&fakeOrders{order: order}, &fakeService{})

	target, err := adapter.PaymentTarget(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("PaymentTarget: %v", err)
	}
	if target.AwaitingPayment {
		t.Fatal("AwaitingPayment = true, want false for a PENDING order")
	}
}

// research D5: a missing order answers the contract's not-found sentinel.
func TestPaymentTargetTranslatesNotFound(t *testing.T) {
	adapter := New(&fakeOrders{err: domainerr.ErrNotFound}, &fakeService{})

	_, err := adapter.PaymentTarget(context.Background(), uuid.New())
	if !errors.Is(err, contracts.ErrOrderNotFound) {
		t.Fatalf("a missing order must answer contracts.ErrOrderNotFound, got %v", err)
	}
}

// research D5: MarkPaid delegates to the order's version-aware transition with the
// order id, the source reference and the expected version unchanged.
func TestMarkPaidDelegatesTheVersionAwareTransition(t *testing.T) {
	order := awaitingPaymentOrder(uuid.New())
	service := &fakeService{}
	adapter := New(&fakeOrders{order: order}, service)

	if err := adapter.MarkPaid(context.Background(), order.ID, "payos:123", 2); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if !service.called {
		t.Fatal("MarkPaid must delegate to the order use case")
	}
	if service.orderID != order.ID || service.sourceReference != "payos:123" || service.expectedVersion != 2 {
		t.Fatalf("delegation carried %s/%q/%d, want %s/payos:123/2",
			service.orderID, service.sourceReference, service.expectedVersion, order.ID)
	}
}

// research D5: module 07's refusals cross the boundary as the contract's sentinels,
// so the payment module classifies them without importing the order module.
func TestMarkPaidTranslatesModuleSevenRefusals(t *testing.T) {
	order := awaitingPaymentOrder(uuid.New())
	cases := []struct {
		name string
		in   error
		want error
	}{
		{name: "stale version", in: domainerr.StalePayment(3, 2), want: contracts.ErrOrderStale},
		{name: "state refusal", in: domainerr.StateTransitionInvalid(constant.StatusCancelled, constant.StatusPaid), want: contracts.ErrOrderNotPayable},
		{name: "missing order", in: domainerr.ErrNotFound, want: contracts.ErrOrderNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := New(&fakeOrders{order: order}, &fakeService{err: tc.in})

			err := adapter.MarkPaid(context.Background(), order.ID, "payos:123", 2)
			if !errors.Is(err, tc.want) {
				t.Fatalf("MarkPaid error = %v, want it to match %v", err, tc.want)
			}
		})
	}
}
