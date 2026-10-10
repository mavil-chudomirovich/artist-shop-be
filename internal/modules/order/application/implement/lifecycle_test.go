package implement

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is the use-case contract of US2: the order's selling life. MarkPaid
// turns the hold into a sale, once per line and keyed by the payment event's
// per-line source reference; Cancel returns the hold; Ship and Complete drive
// their transitions; and every move the current state does not allow is refused
// naming that state and leaves the order untouched (FR-009 to FR-015, research
// D5, D13). It runs the real transitions over in-memory fakes of the repository
// and the inventory reservation contract, so the answers asserted are the
// service's own.

// statusWrite records one persisted state change.
type statusWrite struct {
	id     uuid.UUID
	status constant.Status
}

// lifecycleOrders is the in-memory OrderRepository the lifecycle uses. It holds
// orders by identifier, answers the row lock, records every state write, and
// answers the expiry read from a fixed set. Embedding the interface makes any
// method the use cases drift into panic loudly rather than silently pass.
type lifecycleOrders struct {
	appinterface.OrderRepository
	orders  map[uuid.UUID]*model.Order
	locked  []uuid.UUID
	updates []statusWrite
	expired []uuid.UUID
}

func (r *lifecycleOrders) LockByID(_ context.Context, id uuid.UUID) (*model.Order, error) {
	order, ok := r.orders[id]
	if !ok {
		return nil, domainerr.ErrNotFound
	}
	r.locked = append(r.locked, id)
	return order, nil
}

func (r *lifecycleOrders) UpdateStatus(_ context.Context, id uuid.UUID, status constant.Status, _ time.Time) error {
	r.updates = append(r.updates, statusWrite{id: id, status: status})
	if order, ok := r.orders[id]; ok {
		order.Status = status
	}
	return nil
}

func (r *lifecycleOrders) ListExpiredPending(_ context.Context, _ time.Time) ([]uuid.UUID, error) {
	return r.expired, nil
}

// passthroughTx is the in-memory UnitOfWork. It runs the function without a real
// transaction: the lifecycle tests refuse a move before any write, so there is
// nothing to roll back, and the point under test is which transition ran and
// which contract call it produced.
type passthroughTx struct{}

func (passthroughTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// lifecycleFixture is the real lifecycle use cases over in-memory fakes.
type lifecycleFixture struct {
	svc          *Service
	orders       *lifecycleOrders
	reservations *fakeReservation
}

func newLifecycleFixture(orders ...*model.Order) *lifecycleFixture {
	byID := make(map[uuid.UUID]*model.Order, len(orders))
	for _, order := range orders {
		byID[order.ID] = order
	}
	store := &lifecycleOrders{orders: byID}
	reservations := &fakeReservation{}
	svc := New(Service{
		Orders:       store,
		Reservations: reservations,
		Tx:           passthroughTx{},
		Clock:        memoryClock{},
		Mapper:       mapper.New(),
	})
	return &lifecycleFixture{svc: svc, orders: store, reservations: reservations}
}

// paymentPendingOrder builds a confirmed, awaiting-payment order over the given
// lines, using the domain constructor so its identifier, line links and total are
// the same ones a checkout-and-confirm would produce.
func paymentPendingOrder(owner uuid.UUID, lines ...model.OrderLine) *model.Order {
	order := model.NewOrder(owner, model.Address{RecipientName: "Nguyễn Văn A"}, lines, fixedNow)
	order.Status = constant.StatusPaymentPending
	return order
}

// line builds one snapshot line for a lifecycle test.
func line(productID uuid.UUID, amount, quantity int64) model.OrderLine {
	return model.OrderLine{
		ProductID: productID,
		Name:      "Tranh",
		Slug:      "tranh",
		UnitPrice: model.Price{Amount: amount, Currency: "VND"},
		Quantity:  quantity,
	}
}

// FR-009, FR-014, research D5, D13: confirming payment moves the order to paid
// and turns every line's hold into a sale, each with its own source reference
// derived from the payment event and the product. Replaying the same payment must
// not sell a second time.
func TestMarkPaidTurnsTheHoldIntoASaleExactlyOnce(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	order := paymentPendingOrder(uuid.New(), line(first, 120000, 2), line(second, 33333, 1))
	f := newLifecycleFixture(order)

	const event = "payment-event-1"
	if err := f.svc.MarkPaid(context.Background(), order.ID, event); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}

	if order.Status != constant.StatusPaid {
		t.Fatalf("status = %s, want PAID", order.Status)
	}
	if len(f.orders.updates) != 1 || f.orders.updates[0].status != constant.StatusPaid {
		t.Fatalf("the paid state must be persisted once, got %+v", f.orders.updates)
	}
	if len(f.reservations.sales) != 2 {
		t.Fatalf("expected one sale per line, got %+v", f.reservations.sales)
	}

	references := make(map[string]bool, len(f.reservations.sales))
	for _, sale := range f.reservations.sales {
		if sale.orderID != order.ID {
			t.Errorf("sale names order %s, want %s", sale.orderID, order.ID)
		}
		if !strings.Contains(sale.reference, event) || !strings.HasSuffix(sale.reference, sale.productID.String()) {
			t.Errorf("source reference %q must combine the event %q and the product %s", sale.reference, event, sale.productID)
		}
		references[sale.reference] = true
	}
	if len(references) != 2 {
		t.Fatalf("each line's sale must carry a distinct source reference, got %+v", f.reservations.sales)
	}

	// Replaying the same payment event is a no-op success: the sale is already
	// applied and must not be applied again (FR-014, idempotent-success).
	if err := f.svc.MarkPaid(context.Background(), order.ID, event); err != nil {
		t.Fatalf("replaying the payment must be a no-op success, got %v", err)
	}
	if len(f.reservations.sales) != 2 {
		t.Fatalf("a replayed payment must not sell again, got %+v", f.reservations.sales)
	}
	if order.Status != constant.StatusPaid {
		t.Fatalf("the replay must leave the order paid, got %s", order.Status)
	}
}

// FR-014: once the order is past the awaiting-payment edge — paid, shipped or
// completed — a payment event is a no-op success, matching module 05's
// already-applied outcomes. It neither transitions the order nor sells anything.
func TestMarkPaidIsANoOpOnceTheOrderIsPastAwaitingPayment(t *testing.T) {
	for _, status := range []constant.Status{constant.StatusPaid, constant.StatusShipped, constant.StatusCompleted} {
		order := paymentPendingOrder(uuid.New(), line(uuid.New(), 1000, 1))
		order.Status = status
		f := newLifecycleFixture(order)

		if err := f.svc.MarkPaid(context.Background(), order.ID, "payment-event-1"); err != nil {
			t.Fatalf("status %s: MarkPaid must be a no-op success, got %v", status, err)
		}
		if got := order.Status; got != status {
			t.Fatalf("status %s: a replay must leave the state as it was, got %s", status, got)
		}
		if len(f.reservations.sales) != 0 {
			t.Fatalf("status %s: a replay must not sell, got %+v", status, f.reservations.sales)
		}
		if len(f.orders.updates) != 0 {
			t.Fatalf("status %s: a replay must persist nothing, got %+v", status, f.orders.updates)
		}
	}
}

// FR-009, FR-011: a payment arriving after cancellation is a real anomaly, not a
// replay, so it is refused naming the cancelled state and sells nothing.
func TestMarkPaidRefusesACancelledOrder(t *testing.T) {
	order := paymentPendingOrder(uuid.New(), line(uuid.New(), 1000, 1))
	order.Status = constant.StatusCancelled
	f := newLifecycleFixture(order)

	err := f.svc.MarkPaid(context.Background(), order.ID, "payment-event-1")
	var refusal *domainerr.StateTransitionError
	if !errors.As(err, &refusal) {
		t.Fatalf("a payment after cancellation must be refused, got %v", err)
	}
	if refusal.From != constant.StatusCancelled || refusal.To != constant.StatusPaid {
		t.Fatalf("the refusal must name CANCELLED, got %+v", refusal)
	}
	if len(f.reservations.sales) != 0 {
		t.Fatalf("a refused payment must sell nothing, got %+v", f.reservations.sales)
	}
	if len(f.orders.updates) != 0 {
		t.Fatalf("a refused payment must persist nothing, got %+v", f.orders.updates)
	}
}

// FR-009, FR-015: cancelling an awaiting-payment order returns the hold of every
// line exactly once and is refused on replay because the order is already
// cancelled.
func TestCancelReturnsTheHoldExactlyOnce(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	order := paymentPendingOrder(uuid.New(), line(first, 1000, 1), line(second, 2000, 3))
	f := newLifecycleFixture(order)

	if err := f.svc.Cancel(context.Background(), order.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if order.Status != constant.StatusCancelled {
		t.Fatalf("status = %s, want CANCELLED", order.Status)
	}
	if len(f.reservations.releases) != 2 {
		t.Fatalf("expected one release per line, got %+v", f.reservations.releases)
	}
	if len(f.reservations.sales) != 0 {
		t.Fatalf("cancelling must not sell anything, got %+v", f.reservations.sales)
	}
	for _, release := range f.reservations.releases {
		if release.orderID != order.ID {
			t.Errorf("release names order %s, want %s", release.orderID, order.ID)
		}
	}

	err := f.svc.Cancel(context.Background(), order.ID)
	var refusal *domainerr.StateTransitionError
	if !errors.As(err, &refusal) {
		t.Fatalf("a second cancel must be refused with a state transition error, got %v", err)
	}
	if refusal.From != constant.StatusCancelled {
		t.Fatalf("the refusal must name the current state CANCELLED, got %+v", refusal)
	}
	if len(f.reservations.releases) != 2 {
		t.Fatalf("a replayed cancel must not release again, got %+v", f.reservations.releases)
	}
}

// FR-009, FR-011, FR-022: ship and complete drive their transitions, and a move
// the current state does not allow is refused naming that state and leaves the
// order exactly as it was.
func TestShipAndCompleteDriveTheTransitionsAndRefuseAnIllegalMove(t *testing.T) {
	order := paymentPendingOrder(uuid.New(), line(uuid.New(), 1000, 1))
	f := newLifecycleFixture(order)
	ctx := context.Background()

	// Shipping an awaiting-payment order is refused, naming PENDING_PAYMENT.
	err := f.svc.Ship(ctx, order.ID)
	var refusal *domainerr.StateTransitionError
	if !errors.As(err, &refusal) {
		t.Fatalf("shipping an unpaid order must be refused with a state transition error, got %v", err)
	}
	if refusal.From != constant.StatusPaymentPending || refusal.To != constant.StatusShipped {
		t.Fatalf("the refusal must name PAYMENT_PENDING, got %+v", refusal)
	}
	if order.Status != constant.StatusPaymentPending {
		t.Fatalf("a refused move must leave the order untouched, got %s", order.Status)
	}
	if len(f.orders.updates) != 0 {
		t.Fatalf("a refused move must persist nothing, got %+v", f.orders.updates)
	}

	// A paid order can be shipped and then completed.
	order.Status = constant.StatusPaid
	if err := f.svc.Ship(ctx, order.ID); err != nil {
		t.Fatalf("Ship: %v", err)
	}
	if order.Status != constant.StatusShipped {
		t.Fatalf("status = %s, want SHIPPED", order.Status)
	}
	if err := f.svc.Complete(ctx, order.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if order.Status != constant.StatusCompleted {
		t.Fatalf("status = %s, want COMPLETED", order.Status)
	}

	// Completing a completed order is refused, naming COMPLETED.
	err = f.svc.Complete(ctx, order.ID)
	if !errors.As(err, &refusal) {
		t.Fatalf("completing a completed order must be refused, got %v", err)
	}
	if refusal.From != constant.StatusCompleted {
		t.Fatalf("the refusal must name COMPLETED, got %+v", refusal)
	}
	if order.Status != constant.StatusCompleted {
		t.Fatalf("a refused move must leave the order untouched, got %s", order.Status)
	}
}

// FR-012, research D6: the expiry use case cancels every awaiting-payment order
// its repository reports past the window, and a retried sweep never releases a
// second time.
func TestExpireOrdersCancelsExpiredOrdersOnce(t *testing.T) {
	expired := paymentPendingOrder(uuid.New(), line(uuid.New(), 1000, 1))
	f := newLifecycleFixture(expired)
	f.orders.expired = []uuid.UUID{expired.ID}

	if err := f.svc.ExpireOrders(context.Background()); err != nil {
		t.Fatalf("ExpireOrders: %v", err)
	}
	if expired.Status != constant.StatusCancelled {
		t.Fatalf("an expired unpaid order must be cancelled, got %s", expired.Status)
	}
	if len(f.reservations.releases) != 1 {
		t.Fatalf("expected the order's hold to be released once, got %+v", f.reservations.releases)
	}

	// The next sweep reports the same order, but its state now refuses the move,
	// so nothing is released a second time.
	if err := f.svc.ExpireOrders(context.Background()); err != nil {
		t.Fatalf("a retried ExpireOrders must not fail: %v", err)
	}
	if len(f.reservations.releases) != 1 {
		t.Fatalf("a retried sweep must not release again, got %+v", f.reservations.releases)
	}
}
