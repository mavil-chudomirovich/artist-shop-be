// Package email delivers auth messages (OTP, password reset). The default
// sender logs messages; an SMTP sender is used when configured.
package email

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strconv"
	"strings"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
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
		return fmt.Errorf("send email: %w", err)
	}
	return nil
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
