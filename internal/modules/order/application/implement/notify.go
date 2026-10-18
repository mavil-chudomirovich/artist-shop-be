package implement

import (
	"context"
	"fmt"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file composes and sends the order module's notifications. Every helper is
// best-effort and is called AFTER the transaction has committed, never from inside
// a WithinTx closure: the Notifier adapter logs a delivery failure instead of
// returning it, and these helpers ignore the returned error anyway, so email can
// never fail or roll back the order operation it describes (FR-021, FR-022,
// FR-023, research D12). The email content is factual — the order's identifier,
// its status and the relevant next step — and lives here in the application layer,
// never in the domain.
//
// Feature 019 (US6) extends the notes this file already carried: the customer is
// emailed on each new status the delivery lifecycle introduces — SHIPPING,
// DELIVERED and COMPLETED (including the sweep's automatic completion) — through
// notifyCustomerStatusChange, and the artist is emailed when the customer submits
// order feedback through notifyArtistFeedbackReceived (FR-021, FR-022).

// notifyArtistConfirmationNeeded emails the shop operator that an order is
// waiting for confirmation, so the artist knows to work the queue. It is called
// on checkout (a fresh PENDING order) and on any edit that leaves the order
// awaiting confirmation (FR-019). A nil notifier or an unconfigured artist
// address is tolerated: the notification is skipped rather than panicking a
// read-only or unconfigured construction.
func (s *Service) notifyArtistConfirmationNeeded(ctx context.Context, order *model.Order) {
	if s.Notifier == nil || s.ArtistEmail == "" {
		return
	}
	subject := fmt.Sprintf("Order needs confirmation: %s", order.ID)
	body := fmt.Sprintf(
		"Order %s is awaiting your confirmation.\nStatus: %s\nNext step: confirm or decline the order.\n",
		order.ID, order.Status,
	)
	_ = s.Notifier.Send(ctx, s.ArtistEmail, subject, body)
}

// notifyCustomerStatusChange emails the order's owner that its status changed,
// reading the account's address through the CustomerLookupService contract so the
// order never reads another module's tables (FR-020, Constitution I). A nil
// notifier, a nil customer lookup, a lookup failure or an account with no address
// skips the notification silently: sending is best-effort and must never fail the
// order operation (FR-021).
func (s *Service) notifyCustomerStatusChange(ctx context.Context, order *model.Order) {
	if s.Notifier == nil || s.Customers == nil {
		return
	}
	customer, err := s.Customers.LookupCustomer(ctx, order.UserID)
	if err != nil || customer.Email == "" {
		return
	}
	subject := fmt.Sprintf("Order status updated: %s", order.ID)
	body := fmt.Sprintf("Your order %s is now %s.\n", order.ID, order.Status)
	_ = s.Notifier.Send(ctx, customer.Email, subject, body)
}

// notifyArtistFeedbackReceived emails the shop operator that an order carries the
// customer's feedback, so the artist knows to read it (FR-022). Like every note
// here it is best-effort and runs after the feedback's transaction committed: a nil
// notifier, an unconfigured artist address or an order with no feedback skips the
// notification, and a delivery failure is logged by the adapter rather than
// returned, so email never fails the feedback operation it describes (FR-023,
// research D12). The artist address is the same configured address the
// confirmation-needed note uses.
func (s *Service) notifyArtistFeedbackReceived(ctx context.Context, order *model.Order) {
	if s.Notifier == nil || s.ArtistEmail == "" || order == nil || order.Feedback == nil {
		return
	}
	subject := fmt.Sprintf("Order feedback received: %s", order.ID)
	body := fmt.Sprintf("Order %s received feedback.\nFeedback: %s\n", order.ID, order.Feedback.Content)
	_ = s.Notifier.Send(ctx, s.ArtistEmail, subject, body)
}
