// Package notifier adapts the order module's Notifier port onto the shared
// mailer. Sending is best-effort: it runs after the transaction has committed, and
// a failure is logged here and swallowed, never returned, so email can never fail
// a business operation (FR-021, research D9).
package notifier

import (
	"context"
	"log/slog"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/mailer"
)

// Notifier implements the order module's Notifier port over an email Sender.
type Notifier struct {
	sender mailer.Sender
	logger *slog.Logger
}

var _ appinterface.Notifier = (*Notifier)(nil)

// New creates the notifier over the given sender. A nil logger falls back to the
// default so a construction cannot panic on a notification.
func New(sender mailer.Sender, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{sender: sender, logger: logger}
}

// Send delivers one message. A delivery failure is logged and swallowed — Send
// always returns nil — because an email that could not be sent must never fail the
// order operation that produced it (FR-021, research D9).
func (n *Notifier) Send(ctx context.Context, to, subject, body string) error {
	if err := n.sender.Send(ctx, to, subject, body); err != nil {
		n.logger.ErrorContext(ctx, "order notification could not be sent",
			slog.String("to", to),
			slog.String("subject", subject),
			slog.String("error", err.Error()),
		)
	}
	return nil
}
