package auditor

import (
	"context"

	"github.com/google/uuid"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/admin/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
)

// Auditor records the admin module's single dashboard-view event. It delegates to
// the foundation's shared writer through audit.Emitter rather than opening a second
// writer, so the whole service has one audit trail and one set of retry and drop
// semantics (FR-030, Constitution VI).
type Auditor struct {
	emitter audit.Emitter
}

// New creates an Auditor over the shared audit emitter.
func New(emitter audit.Emitter) *Auditor { return &Auditor{emitter: emitter} }

// Record emits an audit event. The service supplies action
// ADMIN_DASHBOARD_VIEWED, outcome SUCCESS, target dashboard/overview and metadata
// {days, lowStockThreshold, recentOrderCount}; this adapter only forwards it, so the
// event never contains customer names, emails or order content (research D11).
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

// The adapter must satisfy the admin module's Auditor port.
var _ appinterface.Auditor = (*Auditor)(nil)
