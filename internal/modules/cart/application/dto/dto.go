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

// AddItemInput adds a product to the caller's cart. The owner is taken from the
// session, never from this input (FR-010).
type AddItemInput struct {
	// ProductID is the product to add.
	ProductID uuid.UUID
	// Quantity is the positive whole number of units to add to the line.
	Quantity int64
}

// ChangeQuantityInput sets the quantity of the line for one product.
type ChangeQuantityInput struct {
	// ProductID is the product whose line changes.
	ProductID uuid.UUID
	// Quantity is the new positive whole number for the line.
	Quantity int64
}

// RemoveItemInput removes the line for one product.
type RemoveItemInput struct {
	// ProductID is the product whose line is removed.
	ProductID uuid.UUID
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
// captured when the product was added; the name and slug are the product's
// current values and are nil when the product is gone. Buyable reports whether
// the line can be bought as it stands, and AvailableQuantity is present only when
// the product is on sale but short (FR-004, FR-012, research D10).
type LineView struct {
	// ProductID is the product the line is for.
	ProductID uuid.UUID
	// Name is the product's current name, or nil when the product is gone.
	Name *string
	// Slug is the product's current link segment, or nil when the product is
	// gone.
	Slug *string
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
	// Lines is one entry per product the customer added; an empty cart is an
	// empty slice.
	Lines []LineView
	// Subtotal is the cart total, or nil when the cart is empty.
	Subtotal *MoneyView
}
