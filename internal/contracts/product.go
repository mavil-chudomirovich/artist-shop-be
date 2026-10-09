package contracts

import (
	"context"

	"github.com/google/uuid"
)

// ProductAvailability lets the inventory module signal a product's availability
// to the module that owns the sell state. The product module supplies the adapter
// at the composition root, so inventory never learns product's states and product
// never reads inventory's tables (research D4, Constitution I).
//
// The methods are a signal, not a state write: the product module maps each one
// onto the two edges its own state machine exposes (on sale ↔ out of stock). They
// run in the caller's transaction, so a stock change and the sell-state change it
// causes commit together or not at all (FR-026). They are idempotent and
// tolerant: a signal for the state the product is already in, or one aimed at an
// announced or retired product, is a no-op rather than an error, so a correct
// stock operation is never failed by a signal that changes nothing.
type ProductAvailability interface {
	// MarkOutOfStock moves an on-sale product to out of stock. It is a no-op when
	// the product is already out of stock, and it leaves an announced or retired
	// product untouched. An unknown product is reported as the product module's
	// not-found error (FR-024, FR-027).
	MarkOutOfStock(ctx context.Context, productID uuid.UUID) error

	// MarkOnSale moves an out-of-stock product back on sale. It is a no-op when
	// the product is already on sale, and it leaves an announced or retired
	// product untouched. An unknown product is reported as the product module's
	// not-found error (FR-025, FR-027).
	MarkOnSale(ctx context.Context, productID uuid.UUID) error
}

// ProductLookup answers whether a product exists, so inventory can tell an
// unknown product (reported as not-found) from one that has never been stocked
// (understood as quantity zero) without reading module 04's table (research D4,
// D12).
//
// Existence cannot be answered by the foreign key inventory owns: it only fires
// on an insert, so it tells the truth for a restock but says nothing for a read
// or a conditional decrease, neither of which writes a row. This interface is the
// arrangement Constitution I prescribes for a question one module must ask
// another.
type ProductLookup interface {
	// ProductExists reports whether a product carries the identifier. It reports
	// an error only when the lookup itself failed; a product that does not exist
	// answers (false, nil).
	ProductExists(ctx context.Context, productID uuid.UUID) (bool, error)
}
