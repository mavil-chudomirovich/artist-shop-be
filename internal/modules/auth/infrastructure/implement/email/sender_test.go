package email

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/logging"
)

func TestNewSenderFallsBackToLogSender(t *testing.T) {
	sender := NewSender(config.AuthConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, ok := sender.(*LogSender); !ok {
		t.Fatalf("expected LogSender, got %T", sender)
	}
	if err := sender.Send(context.Background(), "a@example.com", "Confirm your email", "Your confirmation code is 123456"); err != nil {
		t.Fatalf("LogSender.Send: %v", err)
	}
}

func TestLogSenderNeverLogsTheBody(t *testing.T) {
	var buf bytes.Buffer
	sender := NewSender(config.AuthConfig{}, logging.New("info", &buf))
	const secret = "Use this token to reset your password: topsecretresettoken"

	if err := sender.Send(context.Background(), "a@example.com", "Reset your password", secret); err != nil {
		t.Fatalf("LogSender.Send: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "topsecretresettoken") {
		t.Fatalf("log sender leaked the message body: %s", out)
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(out), &record); err != nil {
		t.Fatalf("decode log record: %v", err)
	}
	if record["subject"] != "Reset your password" {
		t.Fatalf("expected the subject to stay visible, got %v", record["subject"])
	}
}

func TestNewSenderUsesSMTPWhenConfigured(t *testing.T) {
	sender := NewSender(config.AuthConfig{
		SMTPHost: "smtp.example.com", SMTPPort: 587,
		SMTPUsername: "user", SMTPPassword: "pass", SMTPFrom: "no-reply@example.com",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	smtpSender, ok := sender.(*SMTPSender)
	if !ok {
		t.Fatalf("expected SMTPSender, got %T", sender)
	}
	if smtpSender.host != "smtp.example.com" || smtpSender.port != 587 || smtpSender.from != "no-reply@example.com" {
		t.Fatalf("unexpected config: %+v", smtpSender)
	}
}
