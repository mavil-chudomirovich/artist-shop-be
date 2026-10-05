package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/reqctx"
)

type fakeStore struct {
	mu        sync.Mutex
	calls     int
	failFirst int
	succeeded []Event
}

func (f *fakeStore) Insert(_ context.Context, e Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failFirst {
		return errors.New("temporary failure")
	}
	f.succeeded = append(f.succeeded, e)
	return nil
}

func (f *fakeStore) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeStore) successCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.succeeded)
}

func (f *fakeStore) firstEvent() (Event, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.succeeded) == 0 {
		return Event{}, false
	}
	return f.succeeded[0], true
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func newTestWriter(store Store, queueSize, retries int) (*Writer, context.CancelFunc) {
	writer := NewWriter(store, discardLogger(), queueSize, retries, 1)
	ctx, cancel := context.WithCancel(context.Background())
	writer.Start(ctx)
	return writer, cancel
}

func TestEmitPersistsEvent(t *testing.T) {
	store := &fakeStore{}
	writer, cancel := newTestWriter(store, 16, 3)
	defer cancel()

	ctx := reqctx.WithCorrelation(context.Background(), "corr-audit")
	writer.Emit(ctx, Event{Action: "PRODUCT_UPDATE", Outcome: OutcomeSuccess})

	waitFor(t, func() bool { return store.successCount() == 1 })

	event, _ := firstEvent(t, store)
	if event.EventID == uuid.Nil {
		t.Fatal("expected generated event id")
	}
	if event.CorrelationID != "corr-audit" {
		t.Fatalf("expected correlation propagated, got %q", event.CorrelationID)
	}
	if event.OccurredAt.IsZero() {
		t.Fatal("expected occurred_at populated")
	}
}

func firstEvent(t *testing.T, store *fakeStore) (Event, bool) {
	t.Helper()
	return store.firstEvent()
}

func TestRetriesAreIdempotent(t *testing.T) {
	store := &fakeStore{failFirst: 2}
	writer, cancel := newTestWriter(store, 16, 5)
	defer cancel()

	writer.Emit(context.Background(), Event{Action: "PAYMENT_CAPTURED", Outcome: OutcomeSuccess})

	waitFor(t, func() bool { return store.successCount() == 1 })

	if got := store.callCount(); got != 3 {
		t.Fatalf("expected 3 attempts (2 failures + 1 success), got %d", got)
	}
	event, _ := firstEvent(t, store)
	if event.EventID == uuid.Nil {
		t.Fatal("expected the same event id across retries")
	}
}

func TestEmitNeverBlocksWhenQueueFull(t *testing.T) {
	store := &fakeStore{}
	writer := NewWriter(store, discardLogger(), 1, 3, 1)
	// Workers are intentionally not started; the queue holds one event.

	writer.Emit(context.Background(), Event{Action: "FIRST", Outcome: OutcomeSuccess})

	done := make(chan struct{})
	go func() {
		writer.Emit(context.Background(), Event{Action: "SECOND", Outcome: OutcomeSuccess})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Emit blocked when the queue was full")
	}
}
