package email

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

func TestNewSenderFallsBackToLogSender(t *testing.T) {
	sender := NewSender(config.AuthConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, ok := sender.(*LogSender); !ok {
		t.Fatalf("expected LogSender, got %T", sender)
	}
	if err := sender.Send(context.Background(), Message{To: "a@example.com", Subject: "s", Body: "b"}); err != nil {
		t.Fatalf("LogSender.Send: %v", err)
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
