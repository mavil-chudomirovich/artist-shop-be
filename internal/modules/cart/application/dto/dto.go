// Package dto defines the cart module's use-case input/output types.
//
// They are separate from the HTTP shapes: these carry what a use case computes,
// while presentation owns what a response looks like. Money is an integer amount
// in the currency's minor unit plus its currency, never a float. The product
// identifier in an input is the product the request names; the acting account is
// never part of any input (FR-010).
package dto

import "github.com/google/uuid"

// GetCartInput carries no member: the cart is the caller's own, addressed with no
// identifier, so the acting account is the only thing that identifies it
// (research D7).
type GetCartInput struct{}

// AddItemInput adds a variant to the caller's cart. The owner is taken from the
// session, never from this input (FR-010).
type AddItemInput struct {
	// VariantID is the variant to add. A cart line is identified by its variant,
	// so adding a product without a variant is impossible (FR-006, research D7).
	VariantID uuid.UUID
	// Quantity is the positive whole number of units to add to the line.
	Quantity int64
}

// ChangeQuantityInput sets the quantity of the line for one variant.
type ChangeQuantityInput struct {
	// VariantID is the variant whose line changes.
	VariantID uuid.UUID
	// Quantity is the new positive whole number for the line.
	Quantity int64
}

// RemoveItemInput removes the line for one variant.
type RemoveItemInput struct {
	// VariantID is the variant whose line is removed.
	VariantID uuid.UUID
}

// MoneyView is money as an integer amount in the currency's minor unit plus the
// currency, the shape a response shows (FR-009).
type MoneyView struct {
	// Amount is the amount in the currency's minor unit.
	Amount int64
	// Currency is the currency code.
	Currency string
}

// LineView is one cart line as a use case answers it. The unit price is the one
// captured when the variant was added; the product name and the variant name are
// the current values and are nil when the variant is gone. Buyable reports whether
// the line can be bought as it stands, and AvailableQuantity is present only when
// the variant is on sale but short (FR-004, FR-012, research D10).
type LineView struct {
	// ProductID is the product the variant belongs to.
	ProductID uuid.UUID
	// ProductCode is the link segment of the product.
	ProductCode string
	// ProductName is the product's current name, or nil when the variant is gone.
	ProductName *string
	// VariantID is the variant the line is for.
	VariantID uuid.UUID
	// VariantName is the variant's current display name, or nil when the variant
	// is gone.
	VariantName *string
	// ImageURL is the variant picture or product fallback.
	ImageURL string
	// Quantity is the whole number the customer chose.
	Quantity int64
	// UnitPrice is the captured price.
	UnitPrice MoneyView
	// LineTotal is quantity times the captured price.
	LineTotal MoneyView
	// Buyable reports whether the line can be bought as it stands.
	Buyable bool
	// AvailableQuantity is the currently available amount, present only when the
	// line is on sale but short.
	AvailableQuantity *int64
}

// CartView is the caller's whole cart. Subtotal is absent (nil) when the cart is
// empty, because an empty cart carries no money (FR-004, spec Assumptions).
type CartView struct {
	// Lines is one entry per variant the customer added; an empty cart is an
	// empty slice.
	Lines []LineView
	// Subtotal is the cart total, or nil when the cart is empty.
	Subtotal *MoneyView
}
