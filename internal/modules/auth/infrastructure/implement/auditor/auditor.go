// Package auditor adapts the shared audit emitter to the auth domain Auditor
// port.
package auditor

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// Auditor records auth security events.
type Auditor struct {
	emitter audit.Emitter
}

// New creates an Auditor.
func New(emitter audit.Emitter) *Auditor { return &Auditor{emitter: emitter} }

// Record emits an audit event for the action.
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
