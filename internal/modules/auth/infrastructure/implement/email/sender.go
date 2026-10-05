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

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

// Message is an outbound email.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers a message.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// LogSender logs messages instead of sending them (local/dev).
type LogSender struct {
	logger *slog.Logger
}

// Send logs the message.
func (s *LogSender) Send(ctx context.Context, m Message) error {
	s.logger.InfoContext(ctx, "email (log sender)",
		slog.String("to", m.To),
		slog.String("subject", m.Subject),
		slog.String("body", m.Body),
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

// Send sends the message via SMTP.
func (s *SMTPSender) Send(_ context.Context, m Message) error {
	addr := s.host + ":" + strconv.Itoa(s.port)
	var auth smtp.Auth
	if s.username != "" {
		auth = smtp.PlainAuth("", s.username, s.password, s.host)
	}
	body := strings.Join([]string{
		"To: " + m.To,
		"From: " + s.from,
		"Subject: " + m.Subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=\"utf-8\"",
		"",
		m.Body,
	}, "\r\n")
	if err := smtp.SendMail(addr, auth, s.from, []string{m.To}, []byte(body)); err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	return nil
}

// NewSender returns an SMTP sender when configured, otherwise a log sender.
func NewSender(cfg config.AuthConfig, logger *slog.Logger) Sender {
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
