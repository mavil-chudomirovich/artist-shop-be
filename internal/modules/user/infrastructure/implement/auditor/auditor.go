// Package auditor adapts the shared audit emitter to the user module's Auditor
// port.
package auditor

import (
	"context"

	"github.com/google/uuid"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/user/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// Auditor records the user module's audit events: every profile, avatar and
// address change, and every administrator read of customer contact details
// (FR-019, FR-022a).
type Auditor struct {
	emitter audit.Emitter
}

// New creates an Auditor.
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
