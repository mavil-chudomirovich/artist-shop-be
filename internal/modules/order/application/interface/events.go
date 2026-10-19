package appinterface

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
)

// SSE topics and event names for the order live stream
// (specs/020-order-sse-stream/data-model.md). The wire format is documented in
// specs/020-order-sse-stream/contracts/sse-events.md.
const (
	// TopicAdmins reaches every administrator stream.
	TopicAdmins = "admins"
	// EventNeedsConfirmation tells admins an order entered PENDING.
	EventNeedsConfirmation = "order.needs_confirmation"
	// EventStatusChanged tells a customer one of their orders moved.
	EventStatusChanged = "order.status_changed"
	// EventFeedbackReceived tells admins an order carries feedback.
	EventFeedbackReceived = "order.feedback_received"
)

// TopicForUser routes an event to exactly one customer's stream. The topic is
// derived from the session's subject, never from client input (FR-004).
func TopicForUser(userID uuid.UUID) string {
	return "user:" + userID.String()
}

// EventPublisher fans live order events out to SSE subscribers. Publishing is
// best-effort and runs after the transaction commits, exactly like the
// Notifier: a failure never fails the business operation (FR-005). A nil
// publisher is tolerated and skips silently.
type EventPublisher interface {
	// Publish delivers one event to the topic without blocking. The payload is
	// the already-encoded JSON object, one line, with no PII beyond identifiers.
	Publish(ctx context.Context, topic, eventType string, payload []byte)
}

// StatusChangedPayload is the data of EventStatusChanged.
type StatusChangedPayload struct {
	OrderID string          `json:"orderId"`
	From    constant.Status `json:"from"`
	To      constant.Status `json:"to"`
	Version int64           `json:"version"`
	At      time.Time       `json:"at"`
}

// NeedsConfirmationPayload is the data of EventNeedsConfirmation.
type NeedsConfirmationPayload struct {
	OrderID string    `json:"orderId"`
	Version int64     `json:"version"`
	At      time.Time `json:"at"`
}

// FeedbackReceivedPayload is the data of EventFeedbackReceived.
type FeedbackReceivedPayload struct {
	OrderID string    `json:"orderId"`
	At      time.Time `json:"at"`
}

// EncodeStatusChanged builds the payload of EventStatusChanged.
func EncodeStatusChanged(orderID uuid.UUID, from, to constant.Status, version int64, at time.Time) []byte {
	raw, err := json.Marshal(StatusChangedPayload{
		OrderID: orderID.String(),
		From:    from,
		To:      to,
		Version: version,
		At:      at.UTC(),
	})
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}

// EncodeNeedsConfirmation builds the payload of EventNeedsConfirmation.
func EncodeNeedsConfirmation(orderID uuid.UUID, version int64, at time.Time) []byte {
	raw, err := json.Marshal(NeedsConfirmationPayload{
		OrderID: orderID.String(),
		Version: version,
		At:      at.UTC(),
	})
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}

// EncodeFeedbackReceived builds the payload of EventFeedbackReceived.
func EncodeFeedbackReceived(orderID uuid.UUID, at time.Time) []byte {
	raw, err := json.Marshal(FeedbackReceivedPayload{
		OrderID: orderID.String(),
		At:      at.UTC(),
	})
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}
