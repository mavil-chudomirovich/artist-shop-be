// Package audit provides an append-only audit trail for privileged actions and
// payment events. Writes are asynchronous and never block business operations
// (FR-014).
package audit

import (
	"time"

	"github.com/google/uuid"
)

// Outcome describes whether the audited action succeeded.
type Outcome string

// Supported outcomes.
const (
	OutcomeSuccess Outcome = "SUCCESS"
	OutcomeFailure Outcome = "FAILURE"
)

// Event is an immutable audit record.
type Event struct {
	EventID       uuid.UUID
	ActorID       *uuid.UUID
	ActorRole     string
	Action        string
	TargetType    string
	TargetID      string
	Outcome       Outcome
	Metadata      map[string]any
	CorrelationID string
	OccurredAt    time.Time
}
