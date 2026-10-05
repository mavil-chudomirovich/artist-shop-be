package email

import "context"

// Port adapts a Sender to the domain EmailSender port.
type Port struct {
	sender Sender
}

// NewPort wraps a Sender.
func NewPort(sender Sender) *Port { return &Port{sender: sender} }

// Send delivers a message using the wrapped sender.
func (p *Port) Send(ctx context.Context, to, subject, body string) error {
	return p.sender.Send(ctx, Message{To: to, Subject: subject, Body: body})
}
