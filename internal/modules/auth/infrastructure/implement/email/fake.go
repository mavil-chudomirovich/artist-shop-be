package email

import (
	"context"
	"sync"
)

// FakeSender captures messages in memory for tests.
type FakeSender struct {
	mu   sync.Mutex
	sent []Message
}

// Send records the message.
func (f *FakeSender) Send(_ context.Context, m Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

// All returns a copy of all sent messages.
func (f *FakeSender) All() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Message, len(f.sent))
	copy(out, f.sent)
	return out
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

// Reset clears recorded messages.
func (f *FakeSender) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = nil
}
