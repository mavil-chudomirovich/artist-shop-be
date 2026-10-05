package email

import (
	"context"
	"sync"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
)

// Message is a captured outbound email.
type Message struct {
	To      string
	Subject string
	Body    string
}

// FakeSender captures messages in memory for integration tests.
type FakeSender struct {
	mu   sync.Mutex
	sent []Message
}

var _ appinterface.EmailSender = (*FakeSender)(nil)

// Send records the message.
func (f *FakeSender) Send(_ context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, Message{To: to, Subject: subject, Body: body})
	return nil
}

// Last returns the most recent message.
func (f *FakeSender) Last() (Message, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return Message{}, false
	}
	return f.sent[len(f.sent)-1], true
}
