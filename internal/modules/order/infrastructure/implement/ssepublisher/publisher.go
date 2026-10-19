// Package ssepublisher adapts the order module's EventPublisher port onto the
// shared SSE hub. Publishing is best-effort: it runs after the transaction
// committed, and a hub that drops a slow subscriber never fails the order
// operation that produced the event (specs/020-order-sse-stream FR-005).
package ssepublisher

import (
	"context"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/sse"
)

// Publisher implements the order module's EventPublisher port over a Hub.
type Publisher struct {
	hub *sse.Hub
}

var _ appinterface.EventPublisher = (*Publisher)(nil)

// New creates the publisher over the given hub. A nil hub is tolerated: every
// Publish becomes a no-op, so a construction without SSE wired cannot panic.
func New(hub *sse.Hub) *Publisher { return &Publisher{hub: hub} }

// Publish fans one event out. It never returns an error and never blocks the
// caller: the hub drops slow subscribers instead.
func (p *Publisher) Publish(_ context.Context, topic, eventType string, payload []byte) {
	if p == nil || p.hub == nil {
		return
	}
	p.hub.Publish(topic, eventType, payload)
}
