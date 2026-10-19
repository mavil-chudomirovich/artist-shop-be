package sse

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func receive(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

func TestPublishReachesOnlyItsTopic(t *testing.T) {
	h := New(testLogger())
	a, unsubA := h.Subscribe("user:a")
	defer unsubA()
	b, unsubB := h.Subscribe("user:b")
	defer unsubB()

	h.Publish("user:a", "order.status_changed", []byte(`{"orderId":"1"}`))

	if got := receive(t, a); got.Type != "order.status_changed" {
		t.Fatalf("wrong type: %q", got.Type)
	}
	select {
	case evt := <-b:
		t.Fatalf("leaked event to other topic: %+v", evt)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAdminTopicFansOutToEveryAdmin(t *testing.T) {
	h := New(testLogger())
	a, unsubA := h.Subscribe("admins")
	defer unsubA()
	b, unsubB := h.Subscribe("admins")
	defer unsubB()

	h.Publish("admins", "order.needs_confirmation", []byte(`{}`))

	receive(t, a)
	receive(t, b)
}

func TestPublishWithNoSubscriberIsNoop(_ *testing.T) {
	h := New(testLogger())
	h.Publish("user:nobody", "order.status_changed", []byte(`{}`))
}

func TestUnsubscribeCleansUp(t *testing.T) {
	h := New(testLogger())
	ch, unsub := h.Subscribe("user:a")
	if got := h.SubscriberCount("user:a"); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
	unsub()
	if got := h.SubscriberCount("user:a"); got != 0 {
		t.Fatalf("count after unsubscribe = %d, want 0", got)
	}
	// Publishing after unsubscribe must not block or panic.
	h.Publish("user:a", "order.status_changed", []byte(`{}`))
	_ = ch
}

func TestSlowSubscriberIsDroppedNotBlocking(t *testing.T) {
	h := New(testLogger())
	ch, unsub := h.Subscribe("user:slow")
	defer unsub()

	// Fill the buffer and overflow it; Publish must return without blocking.
	for i := 0; i < subscriberBuffer+10; i++ {
		h.Publish("user:slow", "order.status_changed", []byte(`{}`))
	}
	if got := h.DroppedCount("user:slow"); got == 0 {
		t.Fatal("expected drops for a slow subscriber, got 0")
	}
	// The subscriber still gets the buffered prefix.
	select {
	case <-ch:
	default:
		t.Fatal("expected buffered events to remain readable")
	}
}
