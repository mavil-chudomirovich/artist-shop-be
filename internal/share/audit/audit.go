package audit

import "context"

// Emitter is the interface business modules use to record audit events.
type Emitter interface {
	Emit(ctx context.Context, e Event)
}
