package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// StockResponse is one product's stock as an administrator reads it
// (contracts/openapi.yaml, StockView): what is on the shelf, what active holds
// have set aside, and what therefore remains available. All three are whole
// numbers and none is negative. The product identifier is carried by the path and
// is deliberately not repeated in the body (FR-007).
type StockResponse struct {
	// PhysicalQuantity is what is on the shelf.
	PhysicalQuantity int64 `json:"physicalQuantity"`
	// HeldQuantity is what active holds have set aside.
	HeldQuantity int64 `json:"heldQuantity"`
	// AvailableQuantity is the difference: what a customer can take.
	AvailableQuantity int64 `json:"availableQuantity"`
}

// MovementResponse is one physical change as an administrator reads it
// (contracts/openapi.yaml, StockMovement). Delta is signed and ResultingQuantity
// is the physical quantity after the change, so the count can be followed forward
// without re-summing. SourceReference is present when an outside event caused the
// change; ActorID is present when an administrator did. The two are mutually
// exclusive, which is what keeps a manual change distinguishable from a sale
// (FR-008, research D13).
type MovementResponse struct {
	// ID identifies the movement.
	ID uuid.UUID `json:"id"`
	// ProductID is the product whose quantity changed.
	ProductID uuid.UUID `json:"productId"`
	// Kind is the kind of change: RESTOCK, DAMAGE, ADJUSTMENT or SALE.
	Kind string `json:"kind"`
	// Delta is the signed change.
	Delta int64 `json:"delta"`
	// ResultingQuantity is the physical quantity after the change.
	ResultingQuantity int64 `json:"resultingQuantity"`
	// SourceReference is the outside event's identity, or null for a manual
	// change.
	SourceReference *string `json:"sourceReference"`
	// ActorID is the administrator who made a manual change, or null for a
	// system-caused sale.
	ActorID *uuid.UUID `json:"actorId"`
	// Note is the optional note a manual change carries, or null.
	Note *string `json:"note"`
	// CreatedAt is when the movement was written.
	CreatedAt time.Time `json:"createdAt"`
}

// QuantityRequest is the restock and damage body (contracts/openapi.yaml,
// QuantityRequest). Quantity is a json.Number so the transport can tell a
// fractional value apart from a malformed body and answer VALIDATION_ERROR naming
// `quantity`, rather than losing the field to a generic decode failure. The
// positive-whole-number rule itself is the use case's (FR-012).
type QuantityRequest struct {
	// Quantity is the positive whole number received or lost.
	Quantity json.Number `json:"quantity"`
	// Note is an optional free-text note for the change.
	Note *string `json:"note"`
}

// AdjustmentRequest is the adjustment body (contracts/openapi.yaml,
// AdjustmentRequest). Zero is a valid counted value; the difference from what is
// stored is what gets recorded.
type AdjustmentRequest struct {
	// Quantity is the counted value; zero is valid.
	Quantity json.Number `json:"quantity"`
	// Note is an optional free-text note for the correction.
	Note *string `json:"note"`
}
