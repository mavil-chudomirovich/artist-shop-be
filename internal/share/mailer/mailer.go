// Package mailer is a minimal, module-agnostic email sender. It is shared code
// (Constitution I) used by the order notifier so the order module can send email
// without importing another module's internals or depending on a module that does
// not exist yet (research D9).
//
// The default sender logs messages instead of sending them; an SMTP sender is used
// when a host is configured. This package deliberately depends on no module's
// configuration type: NewSender takes the connection values directly, so a caller
// maps its own configuration onto them and the mailer stays reusable.
package mailer

import (
	"bytes"
	"context"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
)

// Sender delivers one email message. It is the seam the order notifier depends on,
// so a test can supply a fake and never touch a real SMTP server.
type Sender interface {
	// Send delivers one message to `to`. A caller that treats delivery as
	// best-effort ignores the error rather than failing its own operation.
	Send(ctx context.Context, to, subject, body string) error
}

// LogSender logs messages instead of sending them (local/dev). It implements
// Sender directly; adapters MUST NOT declare their own interfaces (Constitution
// I).
type LogSender struct {
	logger *slog.Logger
}

var _ Sender = (*LogSender)(nil)

// Send logs the recipient and the subject. The body is never logged: it may carry
// account or order detail, and logging it would put that PII into the log stream
// (Constitution VI). Use an SMTP sink such as Mailpit to read a message locally.
func (s *LogSender) Send(ctx context.Context, to, subject, _ string) error {
	logging.WithCorrelation(ctx, s.logger).InfoContext(ctx, "email (log sender)",
		slog.String("to", to),
		slog.String("subject", subject),
	)
	return nil
}

// SMTPSender delivers messages through an SMTP server using the standard library,
// mirroring module 01's sender without depending on it.
type SMTPSender struct {
	host     string
	port     int
	username string
	password string
	from     string
}

var _ Sender = (*SMTPSender)(nil)

// Send sends the message via SMTP.
func (s *SMTPSender) Send(_ context.Context, to, subject, body string) error {
	addr := s.host + ":" + strconv.Itoa(s.port)
	var auth smtp.Auth
	if s.username != "" {
		auth = smtp.PlainAuth("", s.username, s.password, s.host)
	}
	if err := smtp.SendMail(addr, auth, sanitizeHeader(s.from), []string{to}, BuildMessage(to, s.from, subject, body)); err != nil {
		return err
	}
	return nil
}

// BuildMessage assembles an RFC 5322 message: the subject is encoded so a
// Vietnamese subject survives every client, and the body is quoted-printable so
// the HTML's non-ASCII text survives servers that are not 8BITMIME.
func BuildMessage(to, from, subject, body string) []byte {
	contentType := "text/plain; charset=\"utf-8\""
	if strings.Contains(body, "<html") || strings.Contains(body, "<!DOCTYPE") {
		contentType = "text/html; charset=\"utf-8\""
	}

	headers := []string{
		"From: " + sanitizeHeader(from),
		"To: " + sanitizeHeader(to),
		"Subject: " + mime.QEncoding.Encode("utf-8", sanitizeHeader(subject)),
		"MIME-Version: 1.0",
		"Content-Type: " + contentType,
		"Content-Transfer-Encoding: quoted-printable",
	}

	var buf bytes.Buffer
	buf.WriteString(strings.Join(headers, "\r\n"))
	buf.WriteString("\r\n\r\n")
	w := quotedprintable.NewWriter(&buf)
	_, _ = w.Write([]byte(body))
	_ = w.Close()
	return buf.Bytes()
}

// sanitizeHeader removes the line breaks that would let a caller inject a header.
func sanitizeHeader(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	return strings.TrimSpace(value)
}

// NewSender returns an SMTP sender when a host is set, otherwise a log sender. The
// connection values are passed directly rather than as a configuration struct, so
// this shared package depends on no module's configuration (research D9).
func NewSender(host string, port int, username, password, from string, logger *slog.Logger) Sender {
	if host == "" {
		if logger == nil {
			logger = slog.Default()
		}
		return &LogSender{logger: logger}
	}
	return &SMTPSender{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
	}
}
