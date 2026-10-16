// Package httpdto defines the product module's HTTP request and response shapes.
//
// The public shapes are deliberately the members a customer reads (FR-003,
// FR-008); the administrator shapes, which carry the code, the SKU, the position
// and the sell state the public shape must not, are reached only behind the
// administrator role guard.
package httpdto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// PriceResponse is money as an integer amount in the currency's minor unit plus
// the currency. It is never a floating point number, so the amount a customer sees
// is exactly the amount the operator entered.
type PriceResponse struct {
	// Amount is the price in the currency's minor unit.
	Amount int64 `json:"amount"`
	// Currency is the currency code: exactly three uppercase letters.
	Currency string `json:"currency"`
}

// PublicProductResponse is exactly what a customer reads from the list: one entry
// per product, with the lowest on-sale variant price and the card image (FR-001,
// FR-002).
type PublicProductResponse struct {
	// ID is the product's stable public identifier.
	ID uuid.UUID `json:"id"`
	// Name is the name the operator wrote.
	Name string `json:"name"`
	// Slug is the segment a customer-facing link is built from.
	Slug string `json:"slug"`
	// Price is the lowest on-sale variant price with its currency.
	Price PriceResponse `json:"price"`
	// ImageURL is the card image's link, or null when the product has none.
	ImageURL *string `json:"imageUrl"`
}

// PublicImageResponse is one gallery picture as a customer sees it. The provider's
// opaque identifier, the position and the primary flag are deliberately absent.
type PublicImageResponse struct {
	// ID identifies the picture.
	ID uuid.UUID `json:"id"`
	// URL is the displayable link returned by the provider.
	URL string `json:"url"`
	// Width and Height are the stored pixel dimensions.
	Width  int `json:"width"`
	Height int `json:"height"`
}

// PublicVariantResponse is one variant as a customer reads it on the detail: its
// name, effective price, image, sell state and whether it is in stock (FR-003,
// FR-005). The SKU is deliberately absent.
type PublicVariantResponse struct {
	// ID is the variant's stable identifier, the value a customer sends to add
	// the variant to the cart.
	ID uuid.UUID `json:"id"`
	// Name is the variant's display name.
	Name string `json:"name"`
	// Price is the variant's effective price.
	Price PriceResponse `json:"price"`
	// ImageURL is the variant's image link, or null when it has none.
	ImageURL *string `json:"imageUrl"`
	// SellState is where the variant is in its selling life.
	SellState string `json:"sellState"`
	// InStock reports whether the variant currently has stock a customer can take.
	InStock bool `json:"inStock"`
}

// PublicProductDetailResponse is one product's full detail for a customer: its
// shared content, its gallery and its visible variants (FR-003, FR-005).
type PublicProductDetailResponse struct {
	PublicProductResponse
	// Description may be empty; an empty description is valid.
	Description string `json:"description"`
	// Images holds every gallery picture in display order. It is always an array,
	// never null.
	Images []PublicImageResponse `json:"images"`
	// Variants holds the visible variants in display order. It is always an
	// array, never null.
	Variants []PublicVariantResponse `json:"variants"`
}

// PriceRequest is money supplied by an operator on a create or an edit. It is an
// integer amount in the currency's minor unit plus the currency.
type PriceRequest struct {
	// Amount is the price in the currency's minor unit.
	Amount int64 `json:"amount"`
	// Currency is the currency code: exactly three uppercase letters.
	Currency string `json:"currency"`
}

// VariantPriceRequest is a variant's own price as the operator supplies it. The
// currency is always the product's, so none is accepted; the amount is optional,
// and omitting it (or sending null) means the variant uses the product's default.
type VariantPriceRequest struct {
	// Amount is the variant's own price in the currency's minor unit.
	Amount int64 `json:"amount"`
}

// OptionalVariantPrice distinguishes "the key was absent" from "the key was
// present and null". A plain *VariantPriceRequest cannot, and the variant PATCH
// needs the difference: an omitted price keeps the variant's current amount while
// an explicit null clears it back to the product's default (FR-017).
type OptionalVariantPrice struct {
	// Value is the parsed price, or nil when the key was present and null.
	Value *VariantPriceRequest
	// Set reports whether the JSON key was present at all.
	Set bool
}

// UnmarshalJSON records that the key was present and parses it. A JSON null sets
// Set true with a nil Value, which is what makes clearing the amount expressible.
func (o *OptionalVariantPrice) UnmarshalJSON(raw []byte) error {
	o.Set = true
	if string(raw) == "null" {
		o.Value = nil
		return nil
	}
	var price VariantPriceRequest
	if err := json.Unmarshal(raw, &price); err != nil {
		return err
	}
	o.Value = &price
	return nil
}

// CreateVariantRequest is one variant of a product create
// (contracts/openapi.yaml, CreateVariantRequest).
type CreateVariantRequest struct {
	// Name is the variant's display name.
	Name string `json:"name"`
	// SKU is the variant's globally unique stock-keeping unit (FR-011).
	SKU string `json:"sku"`
	// Price is the variant's own price; null or omitted uses the product's
	// default (FR-017).
	Price *VariantPriceRequest `json:"price"`
	// Position is the ordering preference.
	Position int `json:"position"`
}

// CreateProductRequest is the administrator create body (contracts/openapi.yaml,
// CreateProductRequest). It carries the product fields and at least one variant;
// a product is never created without one (FR-012).
type CreateProductRequest struct {
	// Name is the product name.
	Name string `json:"name"`
	// Code is the unique operator-facing product code (FR-011).
	Code string `json:"code"`
	// Slug is the public link segment.
	Slug string `json:"slug"`
	// Description is the text shown to customers; an omitted or empty value is
	// stored empty.
	Description string `json:"description"`
	// DefaultPrice is the product's default price with its currency.
	DefaultPrice PriceRequest `json:"defaultPrice"`
	// CategoryID is the one category the product belongs to.
	CategoryID uuid.UUID `json:"categoryId"`
	// Position is the ordering preference.
	Position int `json:"position"`
	// Variants is the variants the product is created with; it must hold at least
	// one (FR-012).
	Variants []CreateVariantRequest `json:"variants"`
}

// UpdateProductRequest is the administrator partial-edit body
// (contracts/openapi.yaml, UpdateProductRequest). An omitted member keeps its
// current value; the variants are edited through their own routes.
type UpdateProductRequest struct {
	// Name is the new name; nil keeps the current value.
	Name *string `json:"name"`
	// Code is the new product code; nil keeps the current value.
	Code *string `json:"code"`
	// Slug is the new slug; nil keeps the current value.
	Slug *string `json:"slug"`
	// Description is the new description; nil keeps the current value, an empty
	// string clears it.
	Description *string `json:"description"`
	// DefaultPrice is the new default price; nil keeps the current value.
	DefaultPrice *PriceRequest `json:"defaultPrice"`
	// CategoryID is the new category; nil keeps the current value.
	CategoryID *uuid.UUID `json:"categoryId"`
	// Position is the new position; nil keeps the current value.
	Position *int `json:"position"`
}

// UpdateVariantRequest is the administrator variant partial-edit body
// (contracts/openapi.yaml, UpdateVariantRequest). An omitted member keeps its
// current value; an omitted price keeps the variant's own amount while an explicit
// null clears it back to the product's default (FR-017).
type UpdateVariantRequest struct {
	// Name is the new name; nil keeps the current value.
	Name *string `json:"name"`
	// SKU is the new SKU; nil keeps the current value.
	SKU *string `json:"sku"`
	// Price is the new own price; the JSON key being absent keeps the current
	// amount (FR-017).
	Price OptionalVariantPrice `json:"price"`
	// Position is the new position; nil keeps the current value.
	Position *int `json:"position"`
}

// ChangeVariantStateRequest is the administrator variant sell-state transition
// body (contracts/openapi.yaml, the variant state endpoint). It carries only the
// target state.
type ChangeVariantStateRequest struct {
	// To is the requested target state. It must be one of the four sell states;
	// the use case validates it and reports an unknown value against this member,
	// which is why it is a string here rather than the domain enum.
	To string `json:"to"`
}

// AdminVariantPriceResponse is a variant's own price as the administrator reads
// it: just its amount, because the currency is always the product's.
type AdminVariantPriceResponse struct {
	// Amount is the variant's own price in the currency's minor unit.
	Amount int64 `json:"amount"`
}

// AdminVariantResponse is one variant of the administrator detail, including the
// members a customer must never receive: the SKU, the position and the sell state
// (FR-010).
type AdminVariantResponse struct {
	// ID is the variant's stable identifier.
	ID uuid.UUID `json:"id"`
	// Name is the variant's display name.
	Name string `json:"name"`
	// SKU is the operator-written stock-keeping unit (FR-011).
	SKU string `json:"sku"`
	// Price is the variant's own amount, or null when it falls back to the
	// product's default (FR-017).
	Price *AdminVariantPriceResponse `json:"price"`
	// EffectivePrice is the price actually charged.
	EffectivePrice PriceResponse `json:"effectivePrice"`
	// ImageURL is the variant's image link, or null when it has none.
	ImageURL *string `json:"imageUrl"`
	// Position is the display order; the smallest is the default variant.
	Position int `json:"position"`
	// SellState is where the variant is in its selling life.
	SellState string `json:"sellState"`
}

// AdminImageResponse is one gallery picture as an administrator sees it, including
// the provider's opaque identifier, its position and whether it is the main one.
type AdminImageResponse struct {
	// ID identifies the picture.
	ID uuid.UUID `json:"id"`
	// PublicID is the provider's opaque identifier.
	PublicID string `json:"publicId"`
	// URL is the displayable link.
	URL string `json:"url"`
	// Width and Height are the stored pixel dimensions.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Position is the display order.
	Position int `json:"position"`
	// IsPrimary reports whether this is the product's main picture.
	IsPrimary bool `json:"isPrimary"`
}

// AdminProductResponse is one entry of the administrator surface, including the
// members a customer must not receive: the code, the description, the category,
// the position and the variant count (FR-010).
type AdminProductResponse struct {
	// ID is the product's stable identifier.
	ID uuid.UUID `json:"id"`
	// Name is the name the operator wrote.
	Name string `json:"name"`
	// Code is the operator-facing product code (FR-011).
	Code string `json:"code"`
	// Slug is the link segment the operator wrote.
	Slug string `json:"slug"`
	// Description is the text shown to customers; an empty string is valid.
	Description string `json:"description"`
	// DefaultPrice is the product's default price with its currency.
	DefaultPrice PriceResponse `json:"defaultPrice"`
	// CategoryID is the one category the product belongs to.
	CategoryID uuid.UUID `json:"categoryId"`
	// Position is the operator's ordering preference.
	Position int `json:"position"`
	// VariantCount is how many variants the product carries.
	VariantCount int `json:"variantCount"`
	// ImageURL is the main picture's link, or null when the product has none.
	ImageURL *string `json:"imageUrl"`
	// CreatedAt and UpdatedAt are the stored timestamps.
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AdminProductDetailResponse is one product's full detail for an administrator:
// the administrator fields plus its gallery and every variant (FR-010).
type AdminProductDetailResponse struct {
	AdminProductResponse
	// Images holds every picture in display order. It is always an array, never
	// null.
	Images []AdminImageResponse `json:"images"`
	// Variants holds every variant in display order. It is always an array, never
	// null.
	Variants []AdminVariantResponse `json:"variants"`
}
