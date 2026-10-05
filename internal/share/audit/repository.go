package audit

import (
	"context"
	"fmt"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/database"
)

// Repository persists audit events. The unique constraint on event_id makes
// retries idempotent.
type Repository struct {
	q database.Querier
}

// NewRepository creates an audit repository backed by the given querier.
func NewRepository(q database.Querier) *Repository {
	return &Repository{q: q}
}

// Insert stores an event, ignoring duplicates with the same event_id.
func (r *Repository) Insert(ctx context.Context, e Event) error {
	const query = `
		INSERT INTO audit_logs (
			event_id, actor_id, actor_role, action, target_type, target_id,
			outcome, metadata, correlation_id, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (event_id) DO NOTHING`

	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}

	if _, err := r.q.Exec(ctx, query,
		e.EventID,
		e.ActorID,
		e.ActorRole,
		e.Action,
		e.TargetType,
		e.TargetID,
		string(e.Outcome),
		e.Metadata,
		e.CorrelationID,
		e.OccurredAt,
	); err != nil {
		return fmt.Errorf("insert audit record: %w", err)
	}
	return nil
}
