package constant

// Stable, machine-readable error codes of the inventory module. They mirror
// specs/007-inventory-tracking/contracts/error-codes.md and are mapped to HTTP
// status codes by presentation/http/errors.go. Clients branch on error.code,
// never on error.message.
//
// A code is part of the module's public contract: renaming one breaks every
// client that already handles it.
const (
	// CodeInsufficientStock reports that a change would take the product's
	// physical quantity below zero, or below the quantity currently held for
	// orders being paid (FR-009). It is a conflict and not a validation failure:
	// the value is well formed and the current state of the shelf is what makes it
	// impossible (error-codes.md).
	CodeInsufficientStock = "INVENTORY_INSUFFICIENT_STOCK"
)
