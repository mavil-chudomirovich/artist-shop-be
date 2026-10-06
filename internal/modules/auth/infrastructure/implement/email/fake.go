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
	mu        sync.Mutex
	sent      []Message
	remaining int
	err       error
}

var _ appinterface.EmailSender = (*FakeSender)(nil)

// Send records the message. A refused send is never recorded: a message that did
// not leave the system is not one the recipient could hold.
func (f *FakeSender) Send(_ context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.remaining > 0 {
		f.remaining--
		return f.err
	}
	f.sent = append(f.sent, Message{To: to, Subject: subject, Body: body})
	return nil
}

// FailNext makes the next n sends fail with err, so a test can exercise a
// provider that refuses a message. Sends after them succeed again.
func (f *FakeSender) FailNext(n int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remaining, f.err = n, err
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

// Count returns how many messages left the system.
func (f *FakeSender) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}
