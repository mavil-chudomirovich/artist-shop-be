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
	// variant, in a stable order. A customer with no cart returns an empty
	// slice, never an error (FR-001, FR-006). A cart can hold two variants of one
	// product as two lines, so the line identity is the variant (FR-006).
	CartLines(ctx context.Context, userID uuid.UUID) ([]CartLine, error)

	// ClearCart empties the caller's cart inside whatever transaction the
	// context carries. A customer with no cart is a no-op (FR-007).
	ClearCart(ctx context.Context, userID uuid.UUID) error
}

// CartLine is one line of a cart at the moment of checkout, as the order module
// needs it: the variant chosen, the quantity, and the price the customer was
// shown. It is the contract DTO rather than the cart's domain model, so a storage
// change on the provider side cannot silently break the consumer
// (docs/system-design/contract-purity.md).
//
// It carries no product or variant snapshots. The order re-reads the variant's
// live facts through ProductVariants for its own re-check — on-sale state, current
// effective price and availability — and snapshots the product name, code, variant
// name and SKU from that read, so a second copy on the cart line would be a value
// the order never uses (research D8).
type CartLine struct {
	// VariantID identifies the variant the line is for. It is the line identity:
	// a cart holds at most one line per variant, and two variants of one product
	// are two lines (FR-006, research D7).
	VariantID uuid.UUID
	// Quantity is the whole number the customer chose, at least 1.
	Quantity int64
	// UnitPrice is the price captured when the variant was added, in the
	// currency's minor unit. It is an integer, never a float (Constitution II).
	UnitPrice ProductPrice
}
