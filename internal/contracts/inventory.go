package contracts

import (
	"context"
	"time"

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
// It is the first contract in this file. It is distinct from
// InventoryReservation below: this is a read, and it neither uses nor closes that
// write contract (research D2).
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

// InventoryReservation exposes the reservation capability module 05 delivered to
// the order module (07), so checkout holds goods, a confirmed payment turns the
// hold into a sale, and a cancelling or expiring order returns it — all without
// the order storing a quantity of its own (Constitution II, research D4). The
// inventory module supplies an adapter at the composition root, which is exactly
// the condition feature 007's deferred D1 recorded.
//
// It is a separate interface from InventoryAvailability: this is the write side
// of the hold lifecycle, while InventoryAvailability is a read used by the cart.
// Each method joins the caller's transaction, so a hold and the order that caused
// it commit together or not at all (research D4).
//
// The methods are deliberately sentinel-free on the contract side: the provider's
// own errors pass through, exactly as module 05's use cases return them, so the
// consumer can classify a refusal without the contracts package owning a second
// error vocabulary (research D4).
type InventoryReservation interface {
	// Reserve sets a quantity of a product aside for one order while it is being
	// paid. Reserving the same order and product twice is a no-op rather than an
	// error, so a retried checkout never holds twice (FR-013, FR-017).
	Reserve(ctx context.Context, orderID, productID uuid.UUID, quantity int64) error

	// Release returns an order's active hold on a product to availability, with
	// no change to physical stock. An order with nothing left to release is a
	// no-op rather than an error (FR-015).
	Release(ctx context.Context, orderID, productID uuid.UUID) error

	// ApplySale consumes an order's hold on a product: the physical stock falls
	// by the held quantity and the hold closes, applied at most once. The
	// sourceReference is the payment event's per-line identity, so replaying the
	// same event changes stock at most once (FR-014, research D5).
	ApplySale(ctx context.Context, orderID, productID uuid.UUID, sourceReference string) error

	// HoldWindow returns the fixed window module 05 sets goods aside for an order
	// (its HoldTTL). The order derives its own expires_at from it, so the window
	// has a single owner and the order never repeats the fifteen minutes
	// (FR-016, research D4, D6).
	HoldWindow() time.Duration
}
