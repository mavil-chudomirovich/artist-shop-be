package reqctx

import (
	"context"
	"testing"
)

func TestCorrelationIDRoundTrip(t *testing.T) {
	ctx := WithCorrelation(context.Background(), "corr-123")
	if got := CorrelationID(ctx); got != "corr-123" {
		t.Fatalf("expected corr-123, got %q", got)
	}
}

func TestCorrelationIDAbsentReturnsEmpty(t *testing.T) {
	if got := CorrelationID(context.Background()); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestCorrelationIDIsolatedBetweenRequests(t *testing.T) {
	a := WithCorrelation(context.Background(), "a")
	b := WithCorrelation(context.Background(), "b")
	if CorrelationID(a) == CorrelationID(b) {
		t.Fatal("correlation IDs must not leak between contexts")
	}
	if got := CorrelationID(context.Background()); got != "" {
		t.Fatalf("parent context must stay clean, got %q", got)
	}
}
