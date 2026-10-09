package dto

import "encoding/json"

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
