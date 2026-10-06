package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/textproto"
	"strings"
	"testing"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
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

// TestSendErrorsAreClassifiedFromTheReplyCodeAlone covers the decision the retry
// rule depends on: the classification comes from the status category the provider
// answered with, and never from the sentence it answered with. The last case is
// the one that matters most - the provider can reword a permanent failure and the
// classification must not move.
func TestSendErrorsAreClassifiedFromTheReplyCodeAlone(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"temporary", &textproto.Error{Code: 421, Msg: "service not available, closing transmission channel"}, domainerr.ErrDeliveryTransient},
		{"mailbox busy", &textproto.Error{Code: 450, Msg: "mailbox busy"}, domainerr.ErrDeliveryTransient},
		{"permanent", &textproto.Error{Code: 550, Msg: "unauthorized IP address"}, domainerr.ErrDeliveryConfiguration},
		{"authentication rejected", &textproto.Error{Code: 535, Msg: "535 5.7.8 Bad credentials"}, domainerr.ErrDeliveryConfiguration},
		{"unreachable", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, domainerr.ErrDeliveryUnreachable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifySendError(c.err)
			if !errors.Is(got, c.want) {
				t.Fatalf("expected %v, got %v", c.want, got)
			}
			if strings.Contains(got.Error(), "unauthorized") || strings.Contains(got.Error(), "closing") ||
				strings.Contains(got.Error(), "busy") || strings.Contains(got.Error(), "Bad credentials") {
				t.Fatalf("the classification carried the provider's wording: %v", got)
			}
		})
	}
}

// TestARephrasedProviderFailureKeepsItsClassification is the guarantee FR-017
// rests on: a copy edit between provider releases cannot turn a permanent
// failure into a retried one, because the same code with different words lands in
// the same class.
func TestARephrasedProviderFailureKeepsItsClassification(t *testing.T) {
	before := classifySendError(&textproto.Error{Code: 550, Msg: "unauthorized IP address"})
	after := classifySendError(&textproto.Error{Code: 550, Msg: "we could not verify the sending host, sorry"})

	if !errors.Is(before, domainerr.ErrDeliveryConfiguration) {
		t.Fatalf("expected a configuration failure, got %v", before)
	}
	if errors.Is(after, domainerr.ErrDeliveryTransient) || errors.Is(after, domainerr.ErrDeliveryUnreachable) {
		t.Fatalf("rephrasing turned a permanent failure into a retried one: %v", after)
	}
	if !errors.Is(after, domainerr.ErrDeliveryConfiguration) {
		t.Fatalf("expected the classification to be unchanged, got %v", after)
	}
}

// TestACancelledSendIsNotClassifiedAndNeverRetried: once the request itself has
// ended there is nobody to answer, so the failure must not look like one a retry
// could resolve.
func TestACancelledSendIsNotClassifiedAndNeverRetried(t *testing.T) {
	got := classifySendError(fmt.Errorf("send email: %w", context.Canceled))

	for _, sentinel := range []error{domainerr.ErrDeliveryTransient, domainerr.ErrDeliveryUnreachable,
		domainerr.ErrDeliveryConfiguration, domainerr.ErrDeliveryRefused} {
		if errors.Is(got, sentinel) {
			t.Fatalf("a cancelled send must carry no classification, got %v", got)
		}
	}
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("expected the cause to be preserved, got %v", got)
	}
}
