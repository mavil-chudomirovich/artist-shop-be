package ssepublisher

import (
	"context"
	"testing"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/sse"
)

func TestPublishReachesSubscriber(t *testing.T) {
	hub := sse.New(nil)
	p := New(hub)
	ch, unsub := hub.Subscribe("admins")
	defer unsub()

	p.Publish(context.Background(), "admins", "order.needs_confirmation", []byte(`{}`))

	select {
	case evt := <-ch:
		if evt.Type != "order.needs_confirmation" {
			t.Fatalf("type = %q", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestNilHubIsNoop(_ *testing.T) {
	p := New(nil)
	p.Publish(context.Background(), "admins", "order.needs_confirmation", []byte(`{}`))
	var nilPublisher *Publisher
	nilPublisher.Publish(context.Background(), "admins", "order.needs_confirmation", []byte(`{}`))
}
