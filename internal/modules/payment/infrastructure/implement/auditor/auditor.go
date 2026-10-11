package auditor

import (
	"context"

	"github.com/google/uuid"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// Auditor records the payment module's four events: created, applied, cancelled and
// unmatched, each naming the payment and the order (FR-022, Constitution VI). The
// webhook's events carry no acting account, which the shared writer represents as a
// nil actor.
//
// It delegates to the foundation's shared writer through audit.Emitter rather than
// opening a second writer, so the whole service has one audit trail and one set of
// retry and drop semantics.
type Auditor struct {
	emitter audit.Emitter
}

// New creates an Auditor over the shared audit emitter.
func New(emitter audit.Emitter) *Auditor { return &Auditor{emitter: emitter} }

// Record emits an audit event. Emission never blocks the business operation and
// never fails it: the shared writer queues, retries and reports a dropped event
// through the logger instead.
func (a *Auditor) Record(ctx context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any) {
	a.emitter.Emit(ctx, audit.Event{
		ActorID:    actorID,
		ActorRole:  actorRole,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Outcome:    audit.Outcome(outcome),
		Metadata:   metadata,
	})
}

var _ appinterface.Auditor = (*Auditor)(nil)
