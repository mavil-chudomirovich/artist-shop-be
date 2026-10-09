// Package dto defines the inventory module's use-case input/output types.
//
// They are separate from the HTTP shapes: these carry what a use case computes,
// while presentation owns what a response looks like. Quantities are whole numbers
// (int64); money is not part of this module. The manual-operation inputs each carry
// an optional free-text note, and the hold-operation inputs carry the order a hold
// belongs to (spec Key Entities).
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
)

// RestockInput records goods arriving. The acting administrator is taken from the
// session, never from this input (FR-005).
type RestockInput struct {
	// ProductID is the product whose stock rises.
	ProductID uuid.UUID
	// Quantity is the positive whole number of units received.
	Quantity int64
	// Note is an optional free-text note for the change.
	Note *string
}

// DamageInput records goods lost.
type DamageInput struct {
	// ProductID is the product whose stock falls.
	ProductID uuid.UUID
	// Quantity is the positive whole number of units lost.
	Quantity int64
	// Note is an optional free-text note for the change.
	Note *string
}

// AdjustmentInput corrects a product's stock to a recounted value.
type AdjustmentInput struct {
	// ProductID is the product being corrected.
	ProductID uuid.UUID
	// Quantity is the counted value; zero is valid. Only the signed difference
	// from the stored quantity is recorded, and correcting to the stored value
	// writes no movement (FR-003, research D9).
	Quantity int64
	// Note is an optional free-text note for the correction.
	Note *string
}

// StockRefInput addresses one product's stock.
type StockRefInput struct {
	// ProductID is the product being read.
	ProductID uuid.UUID
}

// StockOutput is one product's stock: what is on the shelf, what is set aside, and
// what therefore remains available. All three are whole numbers and none is
// negative (FR-007).
type StockOutput struct {
	// ProductID is the product the stock belongs to.
	ProductID uuid.UUID
	// PhysicalQuantity is what is on the shelf.
	PhysicalQuantity int64
	// HeldQuantity is what active holds have set aside.
	HeldQuantity int64
	// AvailableQuantity is the difference: what a customer can take.
	AvailableQuantity int64
}

// HistoryInput pages through one product's movement history.
type HistoryInput struct {
	// ProductID is the product whose history is read.
	ProductID uuid.UUID
	// Page is 1-based.
	Page int
	// PageSize is the number of movements per page.
	PageSize int
}

// MovementOutput is one physical change as the administrator sees it. It carries
// the source reference when an outside event caused it and the actor when an
// administrator did, so the two paths stay distinguishable (FR-008, research D13).
type MovementOutput struct {
	// ID identifies the movement.
	ID uuid.UUID
	// ProductID is the product whose quantity changed.
	ProductID uuid.UUID
	// Kind is the kind of change.
	Kind constant.MovementKind
	// Delta is the signed change.
	Delta int64
	// ResultingQuantity is the physical quantity after the change.
	ResultingQuantity int64
	// SourceReference is the outside event's identity, or nil for a manual change.
	SourceReference *string
	// ActorID is the administrator who made a manual change, or nil for a
	// system-caused sale.
	ActorID *uuid.UUID
	// Note is the optional note a manual change carries, or nil.
	Note *string
	// CreatedAt is when the movement was written.
	CreatedAt time.Time
}

// MovementPage is one page of a product's movement history.
type MovementPage struct {
	// Movements holds the page's movements, oldest first.
	Movements []MovementOutput
	// Page and PageSize echo the requested window.
	Page     int
	PageSize int
	// Total is the number of movements the product has.
	Total int64
}

// ReserveInput holds a quantity for one order while it is being paid. It is the
// internal capability module 07 will call; no HTTP surface exposes it (FR-014,
// research D7).
type ReserveInput struct {
	// OrderID is the order the hold belongs to.
	OrderID uuid.UUID
	// ProductID is the product set aside.
	ProductID uuid.UUID
	// Quantity is the positive whole number of units to hold.
	Quantity int64
}

// ReleaseInput returns an active hold's quantity to availability, for example when
// an unpaid order is cancelled (FR-017).
type ReleaseInput struct {
	// OrderID is the order whose hold is released.
	OrderID uuid.UUID
	// ProductID is the product the hold set aside.
	ProductID uuid.UUID
}

// SaleInput turns a held order into a sale, idempotently keyed by the payment
// event's source reference (FR-016, FR-020).
type SaleInput struct {
	// OrderID is the order being paid.
	OrderID uuid.UUID
	// ProductID is the product whose hold is consumed.
	ProductID uuid.UUID
	// SourceReference is the identity of the outside payment event. Applying the
	// same reference more than once changes stock at most once.
	SourceReference string
}
