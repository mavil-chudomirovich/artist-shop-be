package contracts

import (
	"context"

	"github.com/google/uuid"
)

// InventoryAvailability answers how many units of a product a customer can take,
// which is the product's physical stock minus the quantity its active holds have
// set aside. An inventory module adapter is supplied at the composition root, so
// the cart never reads module 05's tables (Constitution I, research D2).
//
// The instant an answer is computed is deliberately absent from the signature:
// availability depends on which holds have expired, and module 05 judges expiry
// with its own injected clock, so the expiry rule keeps one owner and a caller
// cannot pass a time that disagrees with the sweeper (research D2).
//
// It is the first contract in this file. It is distinct from the reservation
// contract module 05's deferred.md D1 withholds until the order flow exists: this
// is a read, and it neither uses nor closes that contract (research D2).
type InventoryAvailability interface {
	// AvailableQuantity returns one entry per requested identifier, in the same
	// order. A product with no stock row answers zero, so the result always has
	// exactly len(productIDs) entries (research D2).
	AvailableQuantity(ctx context.Context, productIDs []uuid.UUID) ([]Availability, error)
}

// Availability is the contracted available quantity of one product.
type Availability struct {
	// ProductID identifies the product the quantity belongs to.
	ProductID uuid.UUID
	// Available is the quantity a customer can take right now. It is never
	// negative.
	Available int64
}
