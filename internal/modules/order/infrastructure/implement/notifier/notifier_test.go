package notifier

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// fakeSender records the messages it was asked to send and can be made to fail,
// so a test can prove both that a message reaches the sender and that a failure is
// swallowed (FR-021).
type fakeSender struct {
	sent []sentMessage
	err  error
}

type sentMessage struct {
	to      string
	subject string
	body    string
}

func (f *fakeSender) Send(_ context.Context, to, subject, body string) error {
	f.sent = append(f.sent, sentMessage{to: to, subject: subject, body: body})
	return f.err
}

// FR-019 to FR-021: a message is handed to the sender, and a successful send
// returns no error.
func TestSendDeliversThroughTheSender(t *testing.T) {
	sender := &fakeSender{}
	if err := New(sender, nil).Send(context.Background(), "artist@example.com", "New order", "needs confirmation"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected one message sent, got %d", len(sender.sent))
	}
	got := sender.sent[0]
	if got.to != "artist@example.com" || got.subject != "New order" || got.body != "needs confirmation" {
		t.Fatalf("the message did not reach the sender: %+v", got)
	}
}

// FR-021: a delivery failure is logged and swallowed — Send returns nil — so email
// never fails a business operation.
func TestSendSwallowsADeliveryFailure(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))
	sender := &fakeSender{err: errors.New("smtp refused")}

	if err := New(sender, logger).Send(context.Background(), "customer@example.com", "Order confirmed", "body"); err != nil {
		t.Fatalf("a failed send must not return an error, got %v", err)
	}
	if !strings.Contains(logged.String(), "order notification could not be sent") {
		t.Fatalf("the failure must be logged, got %q", logged.String())
	}
	if !strings.Contains(logged.String(), "smtp refused") {
		t.Fatalf("the log must carry the failure, got %q", logged.String())
	}
}
