// Package sse is the shared live-event fan-out used by modules that need to
// push server-sent events (orders today, chat and payment later). It is
// transport-agnostic and storage-free: events are live-only, a slow subscriber
// is dropped instead of blocking the publisher, and a client that reconnects
// recovers through REST rather than a server replay (specs/020-order-sse-stream).
package sse

import (
	"log/slog"
	"sync"
)

// subscriberBuffer is how many undelivered events one subscriber may hold.
// Publishing never blocks, so a client that reads slower than this loses
// events instead of slowing the business operation that produced them.
const subscriberBuffer = 32

// Event is one live notification. Topic routes it: "user:<uuid>" reaches one
// customer's stream, "admins" reaches every administrator stream.
type Event struct {
	// Type is the SSE event name (e.g. "order.status_changed").
	Type string
	// Data is the already-encoded JSON payload, one line, with no PII beyond
	// what the owning module's data-model allows.
	Data []byte
}

// Hub fans events out to the subscribers of each topic. It is safe for
// concurrent use and holds no business rule: it only routes.
type Hub struct {
	mu     sync.RWMutex
	subs   map[string]map[chan Event]struct{}
	logger *slog.Logger

	muDrop sync.Mutex
	drops  map[string]int64
}

// New creates a Hub. A nil logger falls back to the default so construction
// cannot panic on a notification path.
func New(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		subs:   make(map[string]map[chan Event]struct{}),
		logger: logger,
		drops:  make(map[string]int64),
	}
}

// Subscribe registers a subscriber for the topic and returns its channel plus
// an unsubscribe function the caller MUST run when the stream ends, or the
// subscriber leaks.
func (h *Hub) Subscribe(topic string) (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	h.mu.Lock()
	set, ok := h.subs[topic]
	if !ok {
		set = make(map[chan Event]struct{})
		h.subs[topic] = set
	}
	set[ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() { h.unsubscribe(topic, ch) }
}

func (h *Hub) unsubscribe(topic string, ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.subs[topic]; ok {
		delete(set, ch)
		if len(set) == 0 {
			delete(h.subs, topic)
		}
	}
}

// Publish delivers the event to every subscriber of the topic without ever
// blocking: a full subscriber buffer drops the event and counts it, so a slow
// client cannot slow the order operation that produced the event.
func (h *Hub) Publish(topic, eventType string, data []byte) {
	h.mu.RLock()
	set, ok := h.subs[topic]
	if !ok || len(set) == 0 {
		h.mu.RUnlock()
		return
	}
	// Copy the subscriber set so the send happens outside the lock.
	targets := make([]chan Event, 0, len(set))
	for ch := range set {
		targets = append(targets, ch)
	}
	h.mu.RUnlock()

	evt := Event{Type: eventType, Data: data}
	for _, ch := range targets {
		select {
		case ch <- evt:
		default:
			h.muDrop.Lock()
			h.drops[topic]++
			dropped := h.drops[topic]
			h.muDrop.Unlock()
			h.logger.Warn("sse subscriber is slow, event dropped",
				slog.String("topic", topic),
				slog.String("type", eventType),
				slog.Int64("droppedTotal", dropped),
			)
		}
	}
}

// SubscriberCount reports how many subscribers currently listen on the topic.
// Tests use it to prove a disconnected stream leaves nothing behind.
func (h *Hub) SubscriberCount(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[topic])
}

// DroppedCount reports how many events were dropped for the topic because a
// subscriber buffer was full.
func (h *Hub) DroppedCount(topic string) int64 {
	h.muDrop.Lock()
	defer h.muDrop.Unlock()
	return h.drops[topic]
}
