package worker

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	orderimplement "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/implement"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/mapper"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file is the sweeper's contract. The sweeper carries no business rule of
// its own: on each tick it calls both of the module's sweeps — expiry and
// completion — and stops when its context is cancelled. The interval is injected,
// so the scheduler tests drive real ticks in milliseconds; the end-to-end tests
// drive the real use cases over in-memory fakes with an injected clock, so an
// awaiting-payment order past its window is cancelled and its goods released
// exactly once, and a delivered order past its five-day window is completed with
// no actor exactly once, while an order that is not in either state is left
// untouched (FR-008, FR-009, FR-012, FR-013, research D5, D6).

// fakeSweeps is a stand-in for the application capability the sweeper drives. It
// records each sweep's call count, can make either sweep fail, and can hold the
// first sweep open so a second tick has the chance to overlap it.
type fakeSweeps struct {
	expireCalls   atomic.Int32
	completeCalls atomic.Int32
	concurrent    atomic.Int32
	peak          atomic.Int32

	expireEntered     chan struct{}
	expireEnterOnce   sync.Once
	completeEntered   chan struct{}
	completeEnterOnce sync.Once
	release           chan struct{}

	failExpire   bool
	failComplete bool
}

func (f *fakeSweeps) enter() {
	current := f.concurrent.Add(1)
	for {
		peak := f.peak.Load()
		if current <= peak || f.peak.CompareAndSwap(peak, current) {
			break
		}
	}
}

func (f *fakeSweeps) leave() { f.concurrent.Add(-1) }

func (f *fakeSweeps) ExpireOrders(ctx context.Context) error {
	f.enter()
	defer f.leave()
	f.expireCalls.Add(1)
	if f.expireEntered != nil {
		f.expireEnterOnce.Do(func() { close(f.expireEntered) })
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
		}
	}
	if f.failExpire {
		return errors.New("expiry unavailable")
	}
	return nil
}

func (f *fakeSweeps) AutoCompleteDelivered(ctx context.Context) error {
	f.enter()
	defer f.leave()
	f.completeCalls.Add(1)
	if f.completeEntered != nil {
		f.completeEnterOnce.Do(func() { close(f.completeEntered) })
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
		}
	}
	if f.failComplete {
		return errors.New("completion unavailable")
	}
	return nil
}

// FR-008, FR-012: one tick runs both sweeps.
func TestSweeperRunsBothSweepsOnEachTick(t *testing.T) {
	fake := &fakeSweeps{expireEntered: make(chan struct{}), completeEntered: make(chan struct{})}
	sweeper := NewWithInterval(fake, 2*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sweeper.Start(ctx)
		close(done)
	}()

	for name, entered := range map[string]chan struct{}{
		"expiry":     fake.expireEntered,
		"completion": fake.completeEntered,
	} {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatalf("the sweeper did not call the %s sweep", name)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the sweeper did not stop after its context was cancelled")
	}
	if got := fake.expireCalls.Load(); got < 1 {
		t.Fatalf("expected at least one expiry sweep, got %d", got)
	}
	if got := fake.completeCalls.Load(); got < 1 {
		t.Fatalf("expected at least one completion sweep, got %d", got)
	}
}

// FR-012: a failure of one sweep does not stop the other — the tick still runs the
// completion sweep after the expiry sweep fails, and the loop keeps ticking so a
// failing sweep cannot starve a healthy one.
func TestSweeperKeepsGoingWhenASweepFails(t *testing.T) {
	t.Run("an expiry failure still runs the completion sweep", func(t *testing.T) {
		fake := &fakeSweeps{
			failExpire:      true,
			expireEntered:   make(chan struct{}),
			completeEntered: make(chan struct{}),
		}
		sweeper := NewWithInterval(fake, 2*time.Millisecond, nil)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go sweeper.Start(ctx)

		select {
		case <-fake.completeEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("the completion sweep must run even when the expiry sweep fails")
		}
		// And the loop survives, so a later tick runs both again.
		deadline := time.Now().Add(2 * time.Second)
		for fake.expireCalls.Load() < 2 {
			if time.Now().After(deadline) {
				t.Fatalf("the loop stopped after a failed sweep: %d expiry sweeps", fake.expireCalls.Load())
			}
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
		sweeper.Stop()
	})

	t.Run("a completion failure does not stop the next tick", func(t *testing.T) {
		fake := &fakeSweeps{
			failComplete:  true,
			expireEntered: make(chan struct{}),
		}
		sweeper := NewWithInterval(fake, 2*time.Millisecond, nil)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go sweeper.Start(ctx)

		select {
		case <-fake.expireEntered:
		case <-time.After(2 * time.Second):
			t.Fatal("the expiry sweep never ran")
		}
		deadline := time.Now().Add(2 * time.Second)
		for fake.expireCalls.Load() < 2 {
			if time.Now().After(deadline) {
				t.Fatalf("the loop stopped after a failed completion sweep: %d expiry sweeps", fake.expireCalls.Load())
			}
			time.Sleep(5 * time.Millisecond)
		}
		if got := fake.completeCalls.Load(); got < 1 {
			t.Fatalf("expected the completion sweep to have run at least once, got %d", got)
		}
		cancel()
		sweeper.Stop()
	})
}

// The sweeper returns when its context is cancelled, so a shutdown cancels it
// with everything else in the composition root.
func TestSweeperStopsOnContextCancellation(t *testing.T) {
	sweeper := NewWithInterval(&fakeSweeps{}, time.Hour, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sweeper.Start(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond) // let Start reach its loop
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancellation")
	}
	sweeper.Stop() // the loop has ended, so this returns at once
}

// A slow sweep never overlaps the next tick: the loop runs one tick at a time, so
// the completion sweep cannot start while the expiry sweep is still running.
func TestSweeperNeverRunsTwoSweepsAtOnce(t *testing.T) {
	release := make(chan struct{})
	fake := &fakeSweeps{expireEntered: make(chan struct{}), release: release}
	sweeper := NewWithInterval(fake, 2*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sweeper.Start(ctx)

	select {
	case <-fake.expireEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("the first sweep never started")
	}

	time.Sleep(50 * time.Millisecond)
	if got := fake.expireCalls.Load(); got != 1 {
		t.Fatalf("expected exactly one sweep while the first was blocked, got %d", got)
	}
	if got := fake.completeCalls.Load(); got != 0 {
		t.Fatalf("the next sweep must not start while the first is blocked, got %d", got)
	}
	if got := fake.peak.Load(); got > 1 {
		t.Fatalf("the sweeper ran %d sweeps at once", got)
	}

	close(release)
	cancel()
	sweeper.Stop()
}

// sweepOrders is a minimal OrderRepository for the end-to-end sweep tests. It
// reports the orders awaiting payment whose deadline has passed at the instant it
// is handed and the delivered orders past their completion cutoff, records the row
// locks, and persists the state writes.
type sweepOrders struct {
	appinterface.OrderRepository
	orders      []*model.Order
	locked      []uuid.UUID
	updates     []constant.Status
	completions []uuid.UUID
}

func (r *sweepOrders) ListExpiredPending(_ context.Context, now time.Time) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0)
	for _, order := range r.orders {
		if order.Status == constant.StatusPaymentPending && order.PaymentExpiresAt != nil && !order.PaymentExpiresAt.After(now) {
			out = append(out, order.ID)
		}
	}
	return out, nil
}

func (r *sweepOrders) ListAwaitingCompletion(_ context.Context, cutoff time.Time) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0)
	for _, order := range r.orders {
		if order.Status == constant.StatusDelivered && order.DeliveredAt != nil && !order.DeliveredAt.After(cutoff) {
			out = append(out, order.ID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}

func (r *sweepOrders) LockByID(_ context.Context, id uuid.UUID) (*model.Order, error) {
	for _, order := range r.orders {
		if order.ID == id {
			r.locked = append(r.locked, id)
			clone := *order
			return &clone, nil
		}
	}
	return nil, domainerr.ErrNotFound
}

func (r *sweepOrders) UpdateStatus(_ context.Context, id uuid.UUID, status constant.Status, _ time.Time) error {
	r.updates = append(r.updates, status)
	for _, order := range r.orders {
		if order.ID == id {
			order.Status = status
		}
	}
	return nil
}

func (r *sweepOrders) SaveCompleted(_ context.Context, order *model.Order, _ time.Time) error {
	r.completions = append(r.completions, order.ID)
	for _, stored := range r.orders {
		if stored.ID == order.ID {
			stored.Status = order.Status
			stored.CompletedAt = order.CompletedAt
			stored.CompletedBy = order.CompletedBy
		}
	}
	return nil
}

// sweepReservations records the holds the sweep returns. Only Release is
// exercised by the expiry path; the embedded interface makes any other call
// panic loudly.
type sweepReservations struct {
	appinterface.InventoryReservation
	releases []uuid.UUID
}

func (r *sweepReservations) Release(_ context.Context, _, productID uuid.UUID) error {
	r.releases = append(r.releases, productID)
	return nil
}

// sweepClock is the injected clock the end-to-end tests drive.
type sweepClock struct{ at time.Time }

func (c sweepClock) Now() time.Time { return c.at }

// sweepTx runs the function without a real transaction: the sweep paths write
// through the in-memory repository, so there is nothing to roll back.
type sweepTx struct{}

func (sweepTx) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

// FR-008, FR-009, research D5: through the sweeper's own tick, an awaiting-payment
// order past its window is cancelled and its goods released exactly once; an order
// awaiting the artist (which has no deadline) and a paid order are never selected.
func TestSweepCancelsOnlyTheExpiredUnpaidOrder(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	lines := func() []model.OrderLine {
		return []model.OrderLine{{
			ProductID: uuid.New(), ProductName: "Tranh",
			UnitPrice: model.Price{Amount: 1000, Currency: "VND"}, Quantity: 2,
		}}
	}
	address := model.Address{RecipientName: "Nguyễn Văn A"}
	past := now.Add(-time.Second)
	expired := model.NewOrder(uuid.New(), address, lines(), now.Add(-time.Minute))
	expired.Status = constant.StatusPaymentPending
	expired.PaymentExpiresAt = &past
	paid := model.NewOrder(uuid.New(), address, lines(), now.Add(-time.Minute))
	paid.Status = constant.StatusPaid
	// A fresh order awaits the artist: PENDING with no deadline (FR-009).
	awaitingArtist := model.NewOrder(uuid.New(), address, lines(), now.Add(-time.Minute))

	orders := &sweepOrders{orders: []*model.Order{expired, paid, awaitingArtist}}
	reservations := &sweepReservations{}
	svc := orderimplement.New(orderimplement.Service{
		Orders:       orders,
		Reservations: reservations,
		Tx:           sweepTx{},
		Clock:        sweepClock{at: now},
		Mapper:       mapper.New(),
	})
	sweeper := NewWithInterval(svc, time.Hour, nil)

	sweeper.sweep(context.Background())
	if expired.Status != constant.StatusCancelled {
		t.Fatalf("the expired unpaid order must be cancelled, got %s", expired.Status)
	}
	if len(reservations.releases) != len(expired.Lines) {
		t.Fatalf("expected the expired order's holds to be released, got %+v", reservations.releases)
	}
	if paid.Status != constant.StatusPaid {
		t.Fatalf("a paid order must be untouched, got %s", paid.Status)
	}
	if awaitingArtist.Status != constant.StatusPending {
		t.Fatalf("an order awaiting the artist must never expire, got %s", awaitingArtist.Status)
	}

	// A second tick reports the same expired identifier, but its state now
	// refuses the move, so nothing is released twice (FR-008).
	sweeper.sweep(context.Background())
	if len(reservations.releases) != len(expired.Lines) {
		t.Fatalf("a retried sweep must not release again, got %+v", reservations.releases)
	}
}

// FR-012, FR-013, research D6: through the sweeper's own tick, a delivered order
// past delivered_at + 5 days is completed with no acting account exactly once
// across repeats, while an in-transit order left long, an order inside the window
// and a paid order are never completed by time.
func TestSweepCompletesOnlyTheDeliveredOrderPastItsWindow(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	lines := func() []model.OrderLine {
		return []model.OrderLine{{
			ProductID: uuid.New(), ProductName: "Tranh",
			UnitPrice: model.Price{Amount: 1000, Currency: "VND"}, Quantity: 1,
		}}
	}
	address := model.Address{RecipientName: "Nguyễn Văn A"}
	oldDelivery := now.Add(-model.DeliveryCompletionWindow - time.Hour)

	delivered := model.NewOrder(uuid.New(), address, lines(), now.Add(-time.Hour))
	delivered.Status = constant.StatusDelivered
	stamp := oldDelivery
	delivered.DeliveredAt = &stamp
	// Delivered, but still inside the window: the customer has time to confirm.
	inside := model.NewOrder(uuid.New(), address, lines(), now.Add(-time.Hour))
	inside.Status = constant.StatusDelivered
	insideStamp := now.Add(-model.DeliveryCompletionWindow + time.Minute)
	inside.DeliveredAt = &insideStamp
	// In transit for a long time and paid: neither is ever completed by time.
	shipping := model.NewOrder(uuid.New(), address, lines(), now.Add(-48*time.Hour))
	shipping.Status = constant.StatusShipping
	paid := model.NewOrder(uuid.New(), address, lines(), now.Add(-48*time.Hour))
	paid.Status = constant.StatusPaid

	orders := &sweepOrders{orders: []*model.Order{delivered, inside, shipping, paid}}
	svc := orderimplement.New(orderimplement.Service{
		Orders: orders,
		Tx:     sweepTx{},
		Clock:  sweepClock{at: now},
		Mapper: mapper.New(),
	})
	sweeper := NewWithInterval(svc, time.Hour, nil)

	sweeper.sweep(context.Background())
	if delivered.Status != constant.StatusCompleted {
		t.Fatalf("the delivered order past its window must complete, got %s", delivered.Status)
	}
	if delivered.CompletedAt == nil || !delivered.CompletedAt.Equal(now) {
		t.Fatalf("the sweep must stamp the completion instant, got %v", delivered.CompletedAt)
	}
	if delivered.CompletedBy != nil {
		t.Fatalf("the sweep completes with no acting account, got %v", delivered.CompletedBy)
	}
	if len(orders.completions) != 1 {
		t.Fatalf("exactly one order may complete, got %d", len(orders.completions))
	}
	for _, order := range []*model.Order{inside, shipping, paid} {
		if order.CompletedAt != nil || order.Status == constant.StatusCompleted {
			t.Fatalf("an order in state %s must never complete by time, got %s", order.Status, order.Status)
		}
	}

	// A repeated tick changes nothing: the order is no longer DELIVERED, so the
	// completion edge refuses it and the sweep skips it (FR-012).
	sweeper.sweep(context.Background())
	if len(orders.completions) != 1 {
		t.Fatalf("a repeated tick must not complete again, got %d", len(orders.completions))
	}
}
