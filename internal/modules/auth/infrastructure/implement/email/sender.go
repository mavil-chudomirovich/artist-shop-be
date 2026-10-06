// Package email delivers auth messages (OTP, password reset). The default
// sender logs messages; an SMTP sender is used when configured.
package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
)

// LogSender logs messages instead of sending them (local/dev). It implements
// the application EmailSender port directly; adapters MUST NOT declare their
// own interfaces (Constitution I).
type LogSender struct {
	logger *slog.Logger
}

var _ appinterface.EmailSender = (*LogSender)(nil)

// Send logs the recipient and subject. The body is never logged because it
// carries the confirmation code or the password-reset token (Constitution VI);
// use an SMTP sink such as Mailpit to read them locally.
func (s *LogSender) Send(ctx context.Context, to, subject, _ string) error {
	logging.WithCorrelation(ctx, s.logger).InfoContext(ctx, "email (log sender)",
		slog.String("to", to),
		slog.String("subject", subject),
	)
	return nil
}

// SMTPSender delivers messages through an SMTP server.
type SMTPSender struct {
	host     string
	port     int
	username string
	password string
	from     string
}

var _ appinterface.EmailSender = (*SMTPSender)(nil)

// Send sends the message via SMTP.
func (s *SMTPSender) Send(_ context.Context, to, subject, body string) error {
	addr := s.host + ":" + strconv.Itoa(s.port)
	var auth smtp.Auth
	if s.username != "" {
		auth = smtp.PlainAuth("", s.username, s.password, s.host)
	}
	message := strings.Join([]string{
		"To: " + to,
		"From: " + s.from,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=\"utf-8\"",
		"",
		body,
	}, "\r\n")
	if err := smtp.SendMail(addr, auth, s.from, []string{to}, []byte(message)); err != nil {
		return classifySendError(err)
	}
	return nil
}

// classifySendError maps an SMTP failure onto the module's own classification.
//
// The decision reads the reply code class and nothing else. SMTP defines that
// class as the stable part of a reply: a 4xx is a temporary failure the server
// asks the client to repeat, a 5xx is permanent. The server's sentence is
// deliberately not inspected - it is provider copy, it changes between releases,
// and keying on it would silently turn a permanent failure into a retried one
// (FR-017). Only the code number is carried, never the sentence, so the recorded
// diagnostic stays free of provider wording (FR-022).
func classifySendError(err error) error {
	var reply *textproto.Error
	if errors.As(err, &reply) {
		switch code := reply.Code; {
		case code >= 400 && code < 500:
			return fmt.Errorf("%w: the provider reported status %d", domainerr.ErrDeliveryTransient, code)
		case code >= 500 && code < 600:
			// A permanent rejection of a message this deployment sends is, in
			// practice, a deployment that is not set up correctly - credentials
			// refused, a sender or an IP the provider has not authorised. SMTP
			// exposes no status-level way to separate that from a rejection aimed
			// at this one message, and reading the sentence to tell them apart is
			// what FR-017 forbids, so both land here and both are not retried.
			return fmt.Errorf("%w: the provider reported status %d", domainerr.ErrDeliveryConfiguration, code)
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// The request itself ended. There is nobody left to answer, so this is
		// returned unclassified and never retried.
		return fmt.Errorf("send email: %w", err)
	}
	return fmt.Errorf("%w: the provider could not be contacted", domainerr.ErrDeliveryUnreachable)
}

// NewSender returns an SMTP sender when configured, otherwise a log sender.
func NewSender(cfg config.AuthConfig, logger *slog.Logger) appinterface.EmailSender {
	if cfg.SMTPHost == "" {
		return &LogSender{logger: logger}
	}
	return &SMTPSender{
		host:     cfg.SMTPHost,
		port:     cfg.SMTPPort,
		username: cfg.SMTPUsername,
		password: cfg.SMTPPassword,
		from:     cfg.SMTPFrom,
	}
}
