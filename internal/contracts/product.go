package contracts

import (
	"context"

	"github.com/google/uuid"
)

// ProductAvailability lets the inventory module signal a variant's availability
// to the module that owns the sell state. The product module supplies the adapter
// at the composition root, so inventory never learns product's states and product
// never reads inventory's tables (research D4, D6, Constitution I).
//
// The methods are a signal, not a state write: the product module maps each one
// onto the two edges its own variant state machine exposes (on sale ↔ out of
// stock). They run in the caller's transaction, so a stock change and the
// sell-state change it causes commit together or not at all (FR-026). They are
// idempotent and tolerant: a signal for the state the variant is already in, or
// one aimed at an announced or retired variant, is a no-op rather than an error,
// so a correct stock operation is never failed by a signal that changes nothing.
type ProductAvailability interface {
	// MarkVariantOutOfStock moves an on-sale variant to out of stock. It is a
	// no-op when the variant is already out of stock, and it leaves an announced
	// or retired variant untouched. An unknown variant is reported as the product
	// module's not-found error (FR-015, FR-027, research D4).
	MarkVariantOutOfStock(ctx context.Context, variantID uuid.UUID) error

	// MarkVariantOnSale moves an out-of-stock variant back on sale. It is a
	// no-op when the variant is already on sale, and it leaves an announced or
	// retired variant untouched. An unknown variant is reported as the product
	// module's not-found error (FR-015, FR-027, research D4).
	MarkVariantOnSale(ctx context.Context, variantID uuid.UUID) error
}

// ProductLookup answers whether a variant exists, so inventory can tell an
// unknown variant (reported as not-found) from one that has never been stocked
// (understood as quantity zero) without reading module 04's table (research D4,
// D6).
//
// Existence cannot be answered by the foreign key inventory owns: it only fires
// on an insert, so it tells the truth for a restock but says nothing for a read
// or a conditional decrease, neither of which writes a row. This interface is the
// arrangement Constitution I prescribes for a question one module must ask
// another.
type ProductLookup interface {
	// VariantExists reports whether a variant carries the identifier. It reports
	// an error only when the lookup itself failed; a variant that does not exist
	// answers (false, nil).
	VariantExists(ctx context.Context, variantID uuid.UUID) (bool, error)
}

// ProductCategoryVariants answers which variants belong to given categories,
// so the category hide guard can resolve "my variants" without reading the
// product module's tables (Constitution I, specs/016-hide-guard research D2).
//
// The provider module (product, 04) supplies an adapter at the composition
// root. The answer is a flat variant-ID set in one bounded bulk read — all the
// check and the cleanup need — and no product internals cross the boundary.
type ProductCategoryVariants interface {
	// VariantIDsByCategories returns the identifiers of every variant whose
	// product belongs to one of the given categories, in a stable order. An
	// empty input answers an empty slice, never an error.
	VariantIDsByCategories(ctx context.Context, categoryIDs []uuid.UUID) ([]uuid.UUID, error)
}

// ProductVariants answers the facts about variants that the cart needs and cannot
// read itself: the product a variant belongs to, the product's current name and
// code, the variant's current name and SKU, whether it may be sold, and its
// current effective price. A product module adapter is supplied at the composition
// root, so the consumers never read module 04's table (Constitution I, research
// D6).
//
// It replaces the former ProductCatalog, which answered facts about a product: the
// unit of sale is now the variant, so the DTO that travels across the boundary
// names the variant and the facts about it rather than a product whose variant the
// consumer would have to guess (research D6).
//
// It is a separate interface from ProductLookup and ProductAvailability rather
// than one widened interface: ProductLookup answers a one-bit existence question
// and ProductAvailability is a write signal, while this is a richer read. Merging
// them would force every existing consumer and its fake to grow methods it never
// uses.
type ProductVariants interface {
	// Variants returns the facts of every requested variant that exists, in one
	// bulk read. An identifier no variant carries is absent from the result
	// rather than an error, so the cart can tell a removed variant from an
	// available one (research D6).
	Variants(ctx context.Context, variantIDs []uuid.UUID) ([]VariantSummary, error)
}

// VariantSummary is the contract DTO for one variant. It carries only what a
// consumer named in the contract needs, never module 04's domain model
// (docs/system-design/contract-purity.md). OnSale is a fact about whether the
// variant may be sold, deliberately not module 04's four-state sell enum:
// exposing the enum would put that state machine inside every consumer.
//
// ProductName, ProductCode, VariantName and SKU are the snapshots the order keeps
// when it records the sale (FR-019); Price is the variant's effective price, its
// own amount or the product's default, in the product's currency (FR-017, FR-024).
type VariantSummary struct {
	// VariantID identifies the variant.
	VariantID uuid.UUID
	// ProductID identifies the product the variant belongs to.
	ProductID uuid.UUID
	// ProductName is the product's current name, snapped onto an order line.
	ProductName string
	// ProductCode is the product code, snapped onto an order line. It is both the
	// operator-facing code and the product's link segment.
	ProductCode string
	// VariantName is the variant's current display name, snapped onto an order
	// line.
	VariantName string
	// SKU is the variant's current SKU, snapped onto an order line. It is stored
	// but never shown to a customer.
	SKU string
	// OnSale reports whether the variant may currently be sold. It is true only
	// while the variant is ACTIVE and its product flag is on and its category
	// is visible; a hidden variant is absent from the result rather than
	// reported with OnSale=false, exactly like a removed variant.
	OnSale bool
	// Price is the variant's current effective price: its own amount, or the
	// product's default when the variant carries none.
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
