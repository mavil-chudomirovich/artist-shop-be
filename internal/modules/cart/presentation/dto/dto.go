package dto

import (
	"encoding/json"

	"github.com/google/uuid"
)

// MoneyResponse is money as an integer amount in the currency's minor unit plus
// its currency, the shape the contract shows (FR-009).
type MoneyResponse struct {
	// Amount is the amount in the currency's minor unit.
	Amount int64 `json:"amount"`
	// Currency is the currency code.
	Currency string `json:"currency"`
}

// CartLineResponse is one cart line as a client reads it (contracts/openapi.yaml,
// CartLine). The unit price is the one captured when the product was added; the
// name and slug are the product's current values and are null when the product is
// gone (FR-004). The buyable flag and the optional available quantity are US2's.
type CartLineResponse struct {
	// ProductID is the product the line is for.
	ProductID uuid.UUID `json:"productId"`
	// Name is the product's current name, or null when the product is gone.
	Name *string `json:"name"`
	// Slug is the product's current link segment, or null when the product is
	// gone.
	Slug *string `json:"slug"`
	// Quantity is the whole number the customer chose.
	Quantity int64 `json:"quantity"`
	// UnitPrice is the captured price.
	UnitPrice MoneyResponse `json:"unitPrice"`
	// LineTotal is quantity times the captured price.
	LineTotal MoneyResponse `json:"lineTotal"`
}

// CartResponse is the caller's whole cart (contracts/openapi.yaml, Cart). Subtotal
// is null when the cart is empty, because an empty cart carries no money.
type CartResponse struct {
	// Lines is one entry per product the customer added; an empty cart is an
	// empty array.
	Lines []CartLineResponse `json:"lines"`
	// Subtotal is the cart total, or null when the cart is empty.
	Subtotal *MoneyResponse `json:"subtotal"`
}

// AddItemRequest is the add body (contracts/openapi.yaml, AddItemRequest).
// ProductID is a string so a malformed identifier can be reported as
// VALIDATION_ERROR naming `productId` rather than lost to a generic decode
// failure. Quantity is a json.Number so a fractional value is a field error, as
// module 05 does.
type AddItemRequest struct {
	// ProductID is the product to add.
	ProductID string `json:"productId"`
	// Quantity is the positive whole number of units to add.
	Quantity json.Number `json:"quantity" swaggertype:"integer"`
}

// QuantityRequest is the change body (contracts/openapi.yaml, QuantityRequest).
type QuantityRequest struct {
	// Quantity is the new positive whole number for the line.
	Quantity json.Number `json:"quantity" swaggertype:"integer"`
}
