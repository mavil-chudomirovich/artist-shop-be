package sse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServeStreamWritesHeadersAndEvent(t *testing.T) {
	h := New(testLogger())
	ch, unsub := h.Subscribe("user:a")
	defer unsub()

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ServeStream(rec, req, ch, testLogger())
	}()

	// Wait for the initial headers/flush, then publish and cancel.
	time.Sleep(50 * time.Millisecond)
	h.Publish("user:a", "order.status_changed", []byte(`{"orderId":"1"}`))
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeStream did not return after client cancel")
	}

	body := rec.Body.String()
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if !strings.Contains(body, "event: order.status_changed") {
		t.Fatalf("body misses event line: %q", body)
	}
	if !strings.Contains(body, `data: {"orderId":"1"}`) {
		t.Fatalf("body misses data line: %q", body)
	}
	if got := h.SubscriberCount("user:a"); got != 1 {
		t.Fatalf("hub still holds subscriber during test: %d", got)
	}
}
