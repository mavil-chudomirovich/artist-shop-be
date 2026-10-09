// Package constant holds the cart module's business constants: the stable,
// machine-readable error codes the module can answer with.
package constant

// Stable, machine-readable error codes of the cart module. They mirror
// specs/008-cart/contracts/error-codes.md and are mapped to HTTP status codes by
// presentation/http/errors.go. Clients branch on error.code, never on
// error.message.
//
// A code is part of the module's public contract: renaming one breaks every
// client that already handles it.
const (
	// CodeProductNotPurchasable reports that a product exists but is not on sale
	// — announced, out of stock or retired — so it cannot be added or kept as a
	// line. It is a conflict and not a validation failure: the product is real
	// and the value is well formed; the product's state is what makes the
	// request impossible (FR-005, error-codes.md).
	CodeProductNotPurchasable = "CART_PRODUCT_NOT_PURCHASABLE"
	// CodeQuantityExceedsAvailable reports that the requested quantity, or the
	// quantity an add would produce, is greater than the amount currently
	// available. It is a conflict for the same reason as
	// CodeProductNotPurchasable: the value is well formed and the shelf is what
	// makes the request impossible (FR-007, error-codes.md).
	CodeQuantityExceedsAvailable = "CART_QUANTITY_EXCEEDS_AVAILABLE"
)
