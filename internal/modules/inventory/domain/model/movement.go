package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
)

// FieldDelta is the movement member named when a change carries no amount. It is
// not a request member: the delta is derived from the operation, so the name is
// internal vocabulary rather than contract vocabulary.
const FieldDelta = "delta"

// FieldSourceReference is the outside-event identity named when an event arrives
// without one. It is not a request member: the reference is what makes a physical
// change idempotent, so a missing one is refused rather than stored as an empty
// key on the ledger.
const FieldSourceReference = "sourceReference"

// Movement is one immutable record of a single change to a product's physical
// stock. It is append-only: nothing edits or removes a row, so the ledger is the
// traceable history of how a quantity came to be (FR-004, research D1).
//
// A hold is not a movement; only turning a hold into a sale is.
type Movement struct {
	// ID identifies the movement.
	ID uuid.UUID
	// ProductID is the product whose quantity changed.
	ProductID uuid.UUID
	// Kind is the kind of change.
	Kind constant.MovementKind
	// Delta is the signed change: positive for a restock and a positive
	// adjustment, negative for damage, a sale and a negative adjustment. Never
	// zero.
	Delta int64
	// ResultingQuantity is the physical quantity after this change. It is never
	// negative.
	ResultingQuantity int64
	// SourceReference is the identity of the outside event that caused the change,
	// or nil for a manual one. It is unique across movements (FR-020).
	SourceReference *string
	// ActorID is the administrator who made a manual change, or nil for a
	// system-caused sale (research D13).
	ActorID *uuid.UUID
	// Note is an optional free-text note a manual change carries.
	Note *string
	// CreatedAt is when the movement was written. Set once.
	CreatedAt time.Time
}

// NewMovement builds one ledger row, refusing a delta of zero (a change that
// changes nothing is not a change), a negative resulting quantity and a kind the
// module does not know (FR-004, FR-009, research D9).
func NewMovement(productID uuid.UUID, kind constant.MovementKind, delta, resultingQuantity int64, actorID *uuid.UUID, sourceReference, note *string, now time.Time) (*Movement, error) {
	if !constant.IsValidMovementKind(kind) {
		return nil, domainerr.InvalidValue(FieldDelta, "is not a known movement kind")
	}
	if delta == 0 {
		return nil, domainerr.InvalidValue(FieldQuantity, "a movement must change the quantity")
	}
	if resultingQuantity < 0 {
		return nil, domainerr.InvalidValue(FieldQuantity, "the resulting quantity must not be negative")
	}
	return &Movement{
		ID:                uuid.New(),
		ProductID:         productID,
		Kind:              kind,
		Delta:             delta,
		ResultingQuantity: resultingQuantity,
		SourceReference:   sourceReference,
		ActorID:           actorID,
		Note:              note,
		CreatedAt:         now,
	}, nil
}

// AdjustmentDelta returns the signed difference an adjustment records: the counted
// value minus the current quantity (FR-003, research D9). A zero difference is a
// valid result — the use case omits the movement rather than writing an empty row —
// but a negative counted value is refused (FR-012).
func AdjustmentDelta(current, counted int64) (int64, error) {
	if counted < 0 {
		return 0, domainerr.InvalidValue(FieldQuantity, "must not be negative")
	}
	return counted - current, nil
}
