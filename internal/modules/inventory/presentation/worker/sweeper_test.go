package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This file is US3's sweeper contract. The sweeper carries no business rule of
// its own: it calls the expire use case every interval and stops when its context
// is cancelled. The interval is injected, so the tests drive real ticks in
// milliseconds; the fake expire use case records how many times it ran and how
// many ran at once, which is how "does not run two sweeps at once" is proven
// rather than asserted (research D6).

// fakeExpire is a stand-in for the expire use case. It records the call count and
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

func (f *fakeExpire) ExpireHolds(ctx context.Context) error {
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

// The sweeper calls the expire use case on each tick.
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
		t.Fatal("the sweeper did not call the expire use case")
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

	// Several intervals elapse while the first sweep is still running; a sweeper
	// that launched each tick would have started more than one.
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
