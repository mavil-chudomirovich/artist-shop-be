package contracts

import (
	"context"

	"github.com/google/uuid"
)

// CartCheckout exposes a customer's cart to the order module, so checkout can
// turn it into an order and empty it without reading the cart's tables
// (Constitution I, research D1). The cart module (06) supplies an adapter at the
// composition root; the order module (07) is the consumer that closes feature
// 006's deferred D1 and feature 007's D1.
//
// Both methods join the transaction the context already carries when there is
// one, so the order and the emptied cart commit together or not at all
// (Constitution II, research D1).
type CartCheckout interface {
	// CartLines returns the caller's cart lines for checkout: one entry per
	// product, in a stable order. A customer with no cart returns an empty
	// slice, never an error (FR-001, FR-006).
	CartLines(ctx context.Context, userID uuid.UUID) ([]CartLine, error)

	// ClearCart empties the caller's cart inside whatever transaction the
	// context carries. A customer with no cart is a no-op (FR-007).
	ClearCart(ctx context.Context, userID uuid.UUID) error
}

// CartLine is one line of a cart at the moment of checkout, as the order module
// needs it: the product, the quantity, and the price the customer was shown. It
// is the contract DTO rather than the cart's domain model, so a storage change
// on the provider side cannot silently break the consumer
// (docs/system-design/contract-purity.md).
type CartLine struct {
	// ProductID identifies the product the line is for. A cart holds at most one
	// line per product (FR-002).
	ProductID uuid.UUID
	// Quantity is the whole number the customer chose, at least 1.
	Quantity int64
	// UnitPriceAmount is the price captured when the product was added, in the
	// currency's minor unit. It is an integer, never a float (Constitution II).
	UnitPriceAmount int64
	// Currency is the currency the captured amount is denominated in.
	Currency string
}
