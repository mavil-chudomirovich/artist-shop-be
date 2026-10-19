package implement

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/mailer"
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
	if renderedSubject, renderedBody, err := mailer.RenderArtistOrderConfirmEmail(mailer.ArtistOrderConfirmParams{
		AppName:      s.appName(),
		OrderID:      order.ID.String(),
		OrderTime:    s.formatTime(order.CreatedAt),
		CustomerName: order.Address.RecipientName,
		ItemsSummary: s.itemsSummary(order),
		TotalAmount:  mailer.FormatMoney(order.Total.Amount, order.Total.Currency),
		ConfirmURL:   s.frontendURL("/admin/orders/" + order.ID.String()),
		SupportEmail: s.supportEmail(),
	}); err == nil {
		subject, body = renderedSubject, renderedBody
	}
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
	if renderedSubject, renderedBody, err := mailer.RenderOrderStatusEmail(mailer.OrderStatusParams{
		AppName:        s.appName(),
		CustomerName:   customer.DisplayName,
		OrderID:        order.ID.String(),
		RawStatus:      string(order.Status),
		OrderDate:      s.formatTime(order.UpdatedAt),
		OrderDateLabel: statusDateLabel(order.Status),
		Lines:          s.orderLines(order),
		TotalAmount:    mailer.FormatMoney(order.Total.Amount, order.Total.Currency),
		OrderURL:       s.frontendURL("/account/orders/" + order.ID.String()),
		SupportEmail:   s.supportEmail(),
	}); err == nil {
		subject, body = renderedSubject, renderedBody
	}
	_ = s.Notifier.Send(ctx, customer.Email, subject, body)
}

// notifyAdminPaymentReceived emails the shop operator that a customer paid an
// order, so the shop knows to prepare the artwork. A nil notifier or an
// unconfigured artist address skips the notification silently; a delivery
// failure never fails the operation that produced it.
func (s *Service) notifyAdminPaymentReceived(ctx context.Context, order *model.Order) {
	if s.Notifier == nil || s.ArtistEmail == "" || order == nil {
		return
	}
	subject := fmt.Sprintf("Order paid: %s", order.ID)
	body := fmt.Sprintf("Order %s has been paid.\n", order.ID)

	customerName, customerEmail := order.Address.RecipientName, ""
	if s.Customers != nil {
		if customer, err := s.Customers.LookupCustomer(ctx, order.UserID); err == nil {
			if customer.DisplayName != "" {
				customerName = customer.DisplayName
			}
			customerEmail = customer.Email
		}
	}
	if renderedSubject, renderedBody, err := mailer.RenderAdminPaymentReceivedEmail(mailer.AdminPaymentParams{
		AppName:       s.appName(),
		OrderID:       order.ID.String(),
		CustomerName:  customerName,
		CustomerEmail: customerEmail,
		PaidAt:        s.formatTime(order.UpdatedAt),
		Lines:         s.orderLines(order),
		TotalAmount:   mailer.FormatMoney(order.Total.Amount, order.Total.Currency),
		OrderURL:      s.frontendURL("/admin/orders/" + order.ID.String()),
		SupportEmail:  s.supportEmail(),
	}); err == nil {
		subject, body = renderedSubject, renderedBody
	}
	_ = s.Notifier.Send(ctx, s.ArtistEmail, subject, body)
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

	customerName := order.Address.RecipientName
	if s.Customers != nil {
		if customer, err := s.Customers.LookupCustomer(ctx, order.Feedback.AuthorID); err == nil && customer.DisplayName != "" {
			customerName = customer.DisplayName
		}
	}
	if renderedSubject, renderedBody, err := mailer.RenderArtistFeedbackEmail(mailer.ArtistFeedbackParams{
		AppName:         s.appName(),
		OrderID:         order.ID.String(),
		CustomerName:    customerName,
		FeedbackContent: order.Feedback.Content,
		FeedbackDate:    s.formatTime(order.Feedback.CreatedAt),
		OrderURL:        s.frontendURL("/admin/orders/" + order.ID.String()),
		SupportEmail:    s.supportEmail(),
	}); err == nil {
		subject, body = renderedSubject, renderedBody
	}
	_ = s.Notifier.Send(ctx, s.ArtistEmail, subject, body)
}

func (s *Service) appName() string {
	if strings.TrimSpace(s.AppName) != "" {
		return s.AppName
	}
	return mailer.DefaultAppName
}

func (s *Service) supportEmail() string {
	if strings.TrimSpace(s.SupportEmail) != "" {
		return s.SupportEmail
	}
	return s.ArtistEmail
}

func (s *Service) location() *time.Location {
	if s.Location != nil {
		return s.Location
	}
	return time.UTC
}

func (s *Service) formatTime(t time.Time) string {
	return t.In(s.location()).Format("02/01/2006 15:04")
}

func (s *Service) frontendURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(s.FrontendURL), "/")
	if base == "" {
		return ""
	}
	return base + path
}

func (s *Service) orderLines(order *model.Order) []mailer.OrderLineItem {
	lines := make([]mailer.OrderLineItem, 0, len(order.Lines))
	for _, line := range order.Lines {
		total := line.LineTotal()
		lines = append(lines, mailer.OrderLineItem{
			ProductName: line.ProductName,
			VariantName: line.VariantName,
			Quantity:    line.Quantity,
			LineTotal:   mailer.FormatMoney(total.Amount, total.Currency),
		})
	}
	return lines
}

func (s *Service) itemsSummary(order *model.Order) string {
	parts := make([]string, 0, len(order.Lines))
	for _, line := range order.Lines {
		name := line.ProductName
		if line.VariantName != "" {
			name += " (" + line.VariantName + ")"
		}
		parts = append(parts, fmt.Sprintf("%dx %s", line.Quantity, name))
	}
	return strings.Join(parts, "\n")
}

func statusDateLabel(status constant.Status) string {
	switch status {
	case constant.StatusPending, constant.StatusPaymentPending:
		return "Ngày đặt"
	case constant.StatusPaid:
		return "Thời gian thanh toán"
	case constant.StatusShipping, constant.StatusDelivered:
		return "Thời gian giao"
	case constant.StatusCompleted:
		return "Thời gian hoàn tất"
	case constant.StatusCancelled:
		return "Thời gian hủy"
	default:
		return "Cập nhật"
	}
}
