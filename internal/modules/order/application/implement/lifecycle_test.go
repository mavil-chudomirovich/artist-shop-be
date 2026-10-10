package implement

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	appdto "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/dto"
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
// orders by identifier, answers the row lock, records every state write and every
// confirmation, and answers the expiry read with the same predicate the adapter
// uses — awaiting payment with a deadline that has passed — so a test can prove
// which orders the sweep selects. Embedding the interface makes any method the
// use cases drift into panic loudly rather than silently pass.
type lifecycleOrders struct {
	appinterface.OrderRepository
	orders    map[uuid.UUID]*model.Order
	locked    []uuid.UUID
	updates   []statusWrite
	confirmed []*model.Order
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

func (r *lifecycleOrders) SaveConfirm(_ context.Context, order *model.Order, _ time.Time) error {
	r.confirmed = append(r.confirmed, order)
	if stored, ok := r.orders[order.ID]; ok {
		stored.Status = order.Status
		stored.Version = order.Version
		stored.ConfirmedAt = order.ConfirmedAt
		stored.PaymentExpiresAt = order.PaymentExpiresAt
	}
	return nil
}

// ListExpiredPending applies the adapter's predicate: only an order awaiting
// payment (PAYMENT_PENDING) whose deadline has passed at the given instant is
// selected. An order awaiting the artist has no deadline and is never selected
// (FR-008, FR-009).
func (r *lifecycleOrders) ListExpiredPending(_ context.Context, now time.Time) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0)
	for id, order := range r.orders {
		if order.Status == constant.StatusPaymentPending && order.PaymentExpiresAt != nil && !order.PaymentExpiresAt.After(now) {
			out = append(out, id)
		}
	}
	return out, nil
}

// snapshot copies the stored orders by value, so a rollback can put every field
// back the way it was.
func (r *lifecycleOrders) snapshot() map[uuid.UUID]model.Order {
	out := make(map[uuid.UUID]model.Order, len(r.orders))
	for id, order := range r.orders {
		out[id] = *order
	}
	return out
}

// restore puts each stored order back to its snapshot, discarding the mutations a
// failed transaction made before it rolled back.
func (r *lifecycleOrders) restore(snapshot map[uuid.UUID]model.Order) {
	for id, order := range snapshot {
		if stored, ok := r.orders[id]; ok {
			*stored = order
		}
	}
}

// passthroughTx is the in-memory UnitOfWork. It runs the function without a real
// transaction: the lifecycle tests refuse a move before any write, so there is
// nothing to roll back, and the point under test is which transition ran and
// which contract call it produced.
type passthroughTx struct{}

func (passthroughTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// rollbackTx is the in-memory UnitOfWork the all-or-nothing confirmation test
// uses. Like the real transaction the use case opens, it discards the holds taken
// and the order writes made when the function fails, so a shortage on one line
// leaves the order and every line exactly as they were (FR-005, FR-024, research
// D3).
type rollbackTx struct {
	orders       *lifecycleOrders
	reservations *fakeReservation
}

func (t rollbackTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	heldBefore := len(t.reservations.calls)
	confirmedBefore := len(t.orders.confirmed)
	updatesBefore := len(t.orders.updates)
	snapshot := t.orders.snapshot()
	if err := fn(ctx); err != nil {
		t.reservations.calls = t.reservations.calls[:heldBefore]
		t.orders.confirmed = t.orders.confirmed[:confirmedBefore]
		t.orders.updates = t.orders.updates[:updatesBefore]
		t.orders.restore(snapshot)
		return err
	}
	return nil
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

// newRollbackFixture is newLifecycleFixture over a UnitOfWork that rolls back on
// failure and an availability read the confirmation names the shortage with, so a
// test can prove that one line that cannot be held leaves no line held.
func newRollbackFixture(available map[uuid.UUID]int64, orders ...*model.Order) *lifecycleFixture {
	byID := make(map[uuid.UUID]*model.Order, len(orders))
	for _, order := range orders {
		byID[order.ID] = order
	}
	store := &lifecycleOrders{orders: byID}
	reservations := &fakeReservation{}
	svc := New(Service{
		Orders:       store,
		Availability: &fakeAvailability{available: available},
		Reservations: reservations,
		Tx:           rollbackTx{orders: store, reservations: reservations},
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

	// Shipping an awaiting-payment order is refused, naming PAYMENT_PENDING.
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

// FR-008, FR-009, research D5: the expiry use case cancels the awaiting-payment
// order whose payment deadline has passed and returns its goods exactly once,
// while an order still awaiting the artist — which has no deadline — is never
// selected; and a retried sweep never releases a second time.
func TestExpireOrdersCancelsExpiredOrdersOnce(t *testing.T) {
	expired := paymentPendingOrder(uuid.New(), line(uuid.New(), 1000, 1))
	past := fixedNow.Add(-time.Minute)
	expired.PaymentExpiresAt = &past
	awaitingArtist := model.NewOrder(uuid.New(), model.Address{RecipientName: "Nguyễn Văn A"},
		[]model.OrderLine{line(uuid.New(), 2000, 1)}, fixedNow)
	f := newLifecycleFixture(expired, awaitingArtist)

	if err := f.svc.ExpireOrders(context.Background()); err != nil {
		t.Fatalf("ExpireOrders: %v", err)
	}
	if expired.Status != constant.StatusCancelled {
		t.Fatalf("an expired unpaid order must be cancelled, got %s", expired.Status)
	}
	// One release proves only the expired order was cancelled: had the order
	// awaiting the artist been selected, its line would have added a second.
	if len(f.reservations.releases) != 1 {
		t.Fatalf("expected the expired order's hold to be released once, got %+v", f.reservations.releases)
	}
	if awaitingArtist.Status != constant.StatusPending {
		t.Fatalf("an order awaiting the artist must never expire, got %s", awaitingArtist.Status)
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

// FR-004, FR-005, FR-006, FR-010, FR-024, research D3: confirming an order
// awaiting the artist holds every line in the same transaction, opens the payment
// window from the injected clock, bumps the content version and reaches
// PAYMENT_PENDING.
func TestConfirmHoldsEveryLineAndOpensThePaymentWindow(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	order := model.NewOrder(uuid.New(), model.Address{RecipientName: "Nguyễn Văn A"},
		[]model.OrderLine{line(first, 1000, 2), line(second, 2000, 1)}, fixedNow)
	if order.Status != constant.StatusPending {
		t.Fatalf("a fresh order must await the artist, got %s", order.Status)
	}
	f := newLifecycleFixture(order)

	if _, err := f.svc.ConfirmByAdmin(actorContext(uuid.New()), appdto.OrderRefInput{OrderID: order.ID}); err != nil {
		t.Fatalf("ConfirmByAdmin: %v", err)
	}

	if order.Status != constant.StatusPaymentPending {
		t.Fatalf("status = %s, want PAYMENT_PENDING", order.Status)
	}
	if order.Version != 2 {
		t.Fatalf("confirmation must bump the content version, got %d", order.Version)
	}
	if order.ConfirmedAt == nil || !order.ConfirmedAt.Equal(fixedNow) {
		t.Fatalf("confirmation must stamp the confirmation instant, got %v", order.ConfirmedAt)
	}
	if order.PaymentExpiresAt == nil || !order.PaymentExpiresAt.Equal(fixedNow.Add(holdWindow)) {
		t.Fatalf("confirmation must open the payment window from the hold window, got %v", order.PaymentExpiresAt)
	}
	if len(f.orders.confirmed) != 1 {
		t.Fatalf("the confirmed order must be persisted once, got %d", len(f.orders.confirmed))
	}
	if len(f.reservations.calls) != 2 {
		t.Fatalf("expected a hold for every line, got %+v", f.reservations.calls)
	}
	held := map[uuid.UUID]int64{}
	for _, call := range f.reservations.calls {
		if call.orderID != order.ID {
			t.Errorf("a hold names order %s, want %s", call.orderID, order.ID)
		}
		held[call.productID] = call.quantity
	}
	if held[first] != 2 || held[second] != 1 {
		t.Fatalf("every line must be held for its quantity, got %+v", held)
	}
	if len(f.orders.updates) != 0 {
		t.Fatalf("confirmation must persist through SaveConfirm, not UpdateStatus, got %+v", f.orders.updates)
	}
}

// FR-010, FR-011: confirming an order that is not awaiting the artist is refused
// naming the current state, holds nothing and persists nothing.
func TestConfirmRefusesAnOrderNotAwaitingTheArtist(t *testing.T) {
	order := paymentPendingOrder(uuid.New(), line(uuid.New(), 1000, 1))
	f := newLifecycleFixture(order)

	_, err := f.svc.ConfirmByAdmin(actorContext(uuid.New()), appdto.OrderRefInput{OrderID: order.ID})
	var refusal *domainerr.StateTransitionError
	if !errors.As(err, &refusal) {
		t.Fatalf("confirming a confirmed order must be refused, got %v", err)
	}
	if refusal.From != constant.StatusPaymentPending || refusal.To != constant.StatusPaymentPending {
		t.Fatalf("the refusal must name the current state, got %+v", refusal)
	}
	if order.Status != constant.StatusPaymentPending {
		t.Fatalf("a refused confirm must leave the order untouched, got %s", order.Status)
	}
	if len(f.reservations.calls) != 0 || len(f.orders.confirmed) != 0 {
		t.Fatalf("a refused confirm must hold and persist nothing, got %+v / %d",
			f.reservations.calls, len(f.orders.confirmed))
	}
}

// FR-011: cancel is allowed from PENDING (no goods held) and PAYMENT_PENDING
// (releasing the hold exactly once), and refused from PAID.
func TestCancelFromPendingHoldsNothingAndFromPaidIsRefused(t *testing.T) {
	product := uuid.New()
	pending := model.NewOrder(uuid.New(), model.Address{RecipientName: "Nguyễn Văn A"},
		[]model.OrderLine{line(product, 1000, 1)}, fixedNow)
	f := newLifecycleFixture(pending)

	if err := f.svc.Cancel(context.Background(), pending.ID); err != nil {
		t.Fatalf("Cancel from PENDING: %v", err)
	}
	if pending.Status != constant.StatusCancelled {
		t.Fatalf("a cancelled order must be CANCELLED, got %s", pending.Status)
	}
	// The use case asks module 05 to release each line; a line that was never
	// held has no hold to return, and module 05's release is idempotent, so no
	// goods move. What must never happen is a sale.
	if len(f.reservations.sales) != 0 {
		t.Fatalf("cancelling an order awaiting the artist must not sell anything, got %+v", f.reservations.sales)
	}

	paid := paymentPendingOrder(uuid.New(), line(product, 1000, 1))
	paid.Status = constant.StatusPaid
	f2 := newLifecycleFixture(paid)
	err := f2.svc.Cancel(context.Background(), paid.ID)
	var refusal *domainerr.StateTransitionError
	if !errors.As(err, &refusal) {
		t.Fatalf("cancelling a paid order must be refused, got %v", err)
	}
	if refusal.From != constant.StatusPaid {
		t.Fatalf("the refusal must name PAID, got %+v", refusal)
	}
	if paid.Status != constant.StatusPaid {
		t.Fatalf("a refused cancel must leave the order untouched, got %s", paid.Status)
	}
	if len(f2.reservations.releases) != 0 {
		t.Fatalf("a refused cancel must release nothing, got %+v", f2.reservations.releases)
	}
}

// FR-005, FR-024, SC-002, SC-005, research D3: confirming a multi-line order
// whose one line cannot be held is refused with the shortage naming that line;
// the order stays awaiting the artist, and no line is left held because the
// transaction rolled the partial hold back. This is the all-or-nothing guarantee
// the storage-level integration test proves against real PostgreSQL; here the
// rollback is the fake transaction's, so the use case's own answer is asserted.
func TestConfirmRefusesAndHoldsNothingWhenAnyLineCannotBeHeld(t *testing.T) {
	held := uuid.New()
	short := uuid.New()
	order := model.NewOrder(uuid.New(), model.Address{RecipientName: "Nguyễn Văn A"},
		[]model.OrderLine{line(held, 1000, 2), line(short, 2000, 1)}, fixedNow)
	if order.Status != constant.StatusPending {
		t.Fatalf("a fresh order must await the artist, got %s", order.Status)
	}
	f := newRollbackFixture(map[uuid.UUID]int64{short: 0}, order)
	// Module 05 refuses the second line: the last unit was taken by a competing
	// hold, so the confirmation must take nothing at all (FR-024).
	f.reservations.failOn = map[uuid.UUID]error{short: errors.New("insufficient stock")}

	_, err := f.svc.ConfirmByAdmin(actorContext(uuid.New()), appdto.OrderRefInput{OrderID: order.ID})

	var refusal *domainerr.QuantityExceedsAvailableError
	if !errors.As(err, &refusal) {
		t.Fatalf("a line that cannot be held must refuse the confirmation, got %v", err)
	}
	if !errors.Is(err, domainerr.ErrQuantityExceedsAvailable) {
		t.Fatalf("the refusal must map to ORDER_QUANTITY_EXCEEDS_AVAILABLE, got %v", err)
	}
	if refusal.ProductID != short {
		t.Fatalf("the refusal must name the short line %s, got %s", short, refusal.ProductID)
	}

	// The order is exactly as it was: still awaiting the artist, unversioned and
	// without a confirmation instant or a payment deadline (FR-005).
	if order.Status != constant.StatusPending {
		t.Fatalf("a refused confirmation must leave the order awaiting the artist, got %s", order.Status)
	}
	if order.Version != 1 || order.ConfirmedAt != nil || order.PaymentExpiresAt != nil {
		t.Fatalf("a refused confirmation must not version or stamp the order, got v%d %v/%v",
			order.Version, order.ConfirmedAt, order.PaymentExpiresAt)
	}

	// No line is held — the successful hold on the first line was rolled back —
	// and nothing was persisted (FR-005, SC-002).
	if len(f.reservations.calls) != 0 {
		t.Fatalf("no line may stay held when a sibling line is short, got %+v", f.reservations.calls)
	}
	if len(f.orders.confirmed) != 0 || len(f.orders.updates) != 0 {
		t.Fatalf("a refused confirmation must persist nothing, got confirmed=%d updates=%d",
			len(f.orders.confirmed), len(f.orders.updates))
	}
}
