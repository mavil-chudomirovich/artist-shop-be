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
// CartLine). The unit price is the one captured when the variant was added; the
// product name and the variant name are the current values and are null when the
// variant is gone (FR-004). Buyable reports whether the line can be bought as it
// stands, and it is always carried; AvailableQuantity is carried only when the
// line is on sale but short of its quantity, so a client knows how far to reduce
// it (FR-012, research D10). The SKU is never exposed to a customer.
type CartLineResponse struct {
	// ProductID is the product the variant belongs to.
	ProductID uuid.UUID `json:"productId"`
	// ProductName is the product's current name, or null when the variant is
	// gone.
	ProductName *string `json:"productName"`
	// VariantID is the variant the line is for.
	VariantID uuid.UUID `json:"variantId"`
	// VariantName is the variant's current display name, or null when the variant
	// is gone.
	VariantName *string `json:"variantName"`
	// Quantity is the whole number the customer chose.
	Quantity int64 `json:"quantity"`
	// UnitPrice is the captured price.
	UnitPrice MoneyResponse `json:"unitPrice"`
	// LineTotal is quantity times the captured price.
	LineTotal MoneyResponse `json:"lineTotal"`
	// Buyable reports whether the line can be bought as it stands.
	Buyable bool `json:"buyable"`
	// AvailableQuantity is the currently available amount, present only when the
	// line is on sale but short of its quantity.
	AvailableQuantity *int64 `json:"availableQuantity,omitempty"`
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
// VariantID is a string so a malformed identifier can be reported as
// VALIDATION_ERROR naming `variantId` rather than lost to a generic decode
// failure. Quantity is a json.Number so a fractional value is a field error, as
// module 05 does.
type AddItemRequest struct {
	// VariantID is the variant to add (a product without a variant is refused).
	VariantID string `json:"variantId"`
	// Quantity is the positive whole number of units to add.
	Quantity json.Number `json:"quantity" swaggertype:"integer"`
}

// QuantityRequest is the change body (contracts/openapi.yaml, QuantityRequest).
type QuantityRequest struct {
	// Quantity is the new positive whole number for the line.
	Quantity json.Number `json:"quantity" swaggertype:"integer"`
}
