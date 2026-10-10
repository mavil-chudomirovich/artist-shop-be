package implement

import (
	"context"
	"fmt"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
)

// This file composes and sends the order module's notifications (US5). Every
// helper is best-effort and is called AFTER the transaction has committed, never
// from inside a WithinTx closure: the Notifier adapter logs a delivery failure
// instead of returning it, and these helpers ignore the returned error anyway, so
// email can never fail or roll back the order operation it describes (FR-019,
// FR-020, FR-021, research D9). The email content is factual — the order's
// identifier, its status and the relevant next step — and lives here in the
// application layer, never in the domain.

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
