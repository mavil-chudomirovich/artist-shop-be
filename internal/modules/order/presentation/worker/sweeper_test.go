package worker

import (
	"context"
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
// its own: it calls the expiry use case every interval and stops when its context
// is cancelled. The interval is injected, so the scheduler tests drive real ticks
// in milliseconds; the expiry test drives the real use case over in-memory fakes
// with an injected clock, so an awaiting-payment order past its window is
// cancelled and its goods released exactly once, an order awaiting the artist is
// never selected, and a paid order is left untouched (FR-008, FR-009, research
// D5).

// fakeExpire is a stand-in for the expiry use case. It records the call count and
// the peak concurrency, and can hold a call open so a second tick has the chance
// to overlap it.
type fakeExpire struct {
	calls      atomic.Int32
	concurrent atomic.Int32
	peak       atomic.Int32

	entered     chan struct{}
	enteredOnce sync.Once
	release     chan struct{}
}

func (f *fakeExpire) ExpireOrders(ctx context.Context) error {
	current := f.concurrent.Add(1)
	for {
		peak := f.peak.Load()
		if current <= peak || f.peak.CompareAndSwap(peak, current) {
			break
		}
	}
	f.calls.Add(1)
	if f.entered != nil {
		f.enteredOnce.Do(func() { close(f.entered) })
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
		}
	}
	f.concurrent.Add(-1)
	return nil
}

// The sweeper calls the expiry use case on each tick.
func TestSweeperCallsExpireOnEachTick(t *testing.T) {
	fake := &fakeExpire{entered: make(chan struct{})}
	sweeper := NewWithInterval(fake, 2*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sweeper.Start(ctx)
		close(done)
	}()

	select {
	case <-fake.entered:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("the sweeper did not call the expiry use case")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the sweeper did not stop after its context was cancelled")
	}
	if got := fake.calls.Load(); got < 1 {
		t.Fatalf("expected at least one sweep, got %d", got)
	}
}

// The sweeper returns when its context is cancelled, so a shutdown cancels it
// with everything else in the composition root.
func TestSweeperStopsOnContextCancellation(t *testing.T) {
	sweeper := NewWithInterval(&fakeExpire{}, time.Hour, nil)

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

// A slow sweep never overlaps the next tick: the loop runs one sweep at a time.
func TestSweeperNeverRunsTwoSweepsAtOnce(t *testing.T) {
	release := make(chan struct{})
	fake := &fakeExpire{entered: make(chan struct{}), release: release}
	sweeper := NewWithInterval(fake, 2*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sweeper.Start(ctx)

	select {
	case <-fake.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the first sweep never started")
	}

	time.Sleep(50 * time.Millisecond)
	if got := fake.calls.Load(); got != 1 {
		t.Fatalf("expected exactly one sweep while the first was blocked, got %d", got)
	}
	if got := fake.peak.Load(); got > 1 {
		t.Fatalf("the sweeper ran %d sweeps at once", got)
	}

	close(release)
	cancel()
	sweeper.Stop()
}

// sweepOrders is a minimal OrderRepository for the expiry test. It reports the
// orders awaiting payment whose deadline has passed at the instant it is handed,
// records the row locks, and persists a state write.
type sweepOrders struct {
	appinterface.OrderRepository
	orders  []*model.Order
	locked  []uuid.UUID
	updates []constant.Status
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

func (r *sweepOrders) LockByID(_ context.Context, id uuid.UUID) (*model.Order, error) {
	for _, order := range r.orders {
		if order.ID == id {
			r.locked = append(r.locked, id)
			return order, nil
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

// sweepClock is the injected clock the expiry test drives.
type sweepClock struct{ at time.Time }

func (c sweepClock) Now() time.Time { return c.at }

// sweepTx runs the function without a real transaction: the expiry path writes
// through the in-memory repository, so there is nothing to roll back.
type sweepTx struct{}

func (sweepTx) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

// FR-008, FR-009, research D5: an awaiting-payment order past its window is
// cancelled and its goods released exactly once; an order awaiting the artist
// (which has no deadline) and a paid order are never selected.
func TestExpireOrdersCancelsOnlyTheExpiredUnpaidOrder(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	lines := func() []model.OrderLine {
		return []model.OrderLine{{
			ProductID: uuid.New(), Name: "Tranh", Slug: "tranh",
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

	if err := svc.ExpireOrders(context.Background()); err != nil {
		t.Fatalf("ExpireOrders: %v", err)
	}
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

	// A second sweep reports the same expired identifier, but its state now
	// refuses the move, so nothing is released twice (FR-008).
	if err := svc.ExpireOrders(context.Background()); err != nil {
		t.Fatalf("a retried ExpireOrders must not fail: %v", err)
	}
	if len(reservations.releases) != len(expired.Lines) {
		t.Fatalf("a retried sweep must not release again, got %+v", reservations.releases)
	}
}
