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

// ProductCatalog answers the facts about products that the cart needs and cannot
// read itself: the product's current name and slug, whether it may be sold, and
// its current price. A product module adapter is supplied at the composition root,
// so the cart never reads module 04's table (Constitution I, research D1).
//
// It is a separate interface from ProductLookup and ProductAvailability rather
// than one widened interface: ProductLookup answers a one-bit existence question
// and ProductAvailability is a write signal, while this is a richer read. Merging
// them would force every existing consumer and its fake to grow methods it never
// uses.
type ProductCatalog interface {
	// Products returns the facts of every requested product that exists, in one
	// bulk read. An identifier no product carries is absent from the result
	// rather than an error, so the cart can tell a removed product from an
	// available one (research D1).
	Products(ctx context.Context, productIDs []uuid.UUID) ([]ProductSummary, error)
}

// ProductSummary is the contract DTO for one product. It carries only what a
// consumer named in the contract needs, never module 04's domain model
// (docs/system-design/contract-purity.md). OnSale is a fact about whether the
// product may be sold, deliberately not module 04's four-state sell enum:
// exposing the enum would put that state machine inside every consumer.
type ProductSummary struct {
	// ID identifies the product.
	ID uuid.UUID
	// Name is the product's current name.
	Name string
	// Slug is the product's current public link segment.
	Slug string
	// OnSale reports whether the product may currently be sold. It is true only
	// for a product that is on sale.
	OnSale bool
	// Price is the product's current price.
	Price ProductPrice
}

// ProductPrice is money as an integer amount in the currency's minor unit plus
// the currency, mirroring the shape the product module stores (Constitution II).
type ProductPrice struct {
	// Amount is the amount in the currency's minor unit. Never a float.
	Amount int64
	// Currency is the currency code the amount is denominated in.
	Currency string
}
