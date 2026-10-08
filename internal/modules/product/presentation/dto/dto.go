// Package httpdto defines the product module's HTTP request and response
// shapes.
//
// The public shapes are deliberately the members a customer reads (FR-004,
// FR-005, FR-008); the administrator shapes, which carry the sell state, the
// position, the category and the set flag the public shape must not, are added
// with the administrator routes.
package httpdto

import (
	"github.com/google/uuid"
)

// PriceResponse is money as an integer amount in the currency's minor unit plus
// the currency (FR-030, research D3). It is never a floating point number, so
// the amount a customer sees is exactly the amount the operator entered. It is
// declared as an object rather than a bare number so the currency always travels
// with the amount.
type PriceResponse struct {
	// Amount is the price in the currency's minor unit.
	Amount int64 `json:"amount"`
	// Currency is the currency code: exactly three uppercase letters.
	Currency string `json:"currency"`
}

// PublicProductResponse is exactly what a customer reads from the list.
//
// It carries the public identifier, the name, the slug, the price with its
// currency, the main picture's link and whether the product is a pre-order, and
// nothing else. The sell state, the position, the category, the set flag and the
// folding keys are how the operator manages the catalogue and are never part of
// a public answer (FR-004, FR-008).
type PublicProductResponse struct {
	// ID is the product's stable public identifier.
	ID uuid.UUID `json:"id"`
	// Name is the name the operator wrote.
	Name string `json:"name"`
	// Slug is the segment a customer-facing link is built from.
	Slug string `json:"slug"`
	// Price is the integer amount with its currency.
	Price PriceResponse `json:"price"`
	// ImageURL is the main picture's link, or null when the product has none.
	ImageURL *string `json:"imageUrl"`
	// IsPreorder reports whether the product is announced but not yet available.
	// Such a product is visible and not buyable (FR-002, FR-040).
	IsPreorder bool `json:"isPreorder"`
}

// PublicImageResponse is one picture as a customer sees it. The provider's
// opaque identifier, the position and the primary flag are deliberately absent:
// a customer has no use for them.
type PublicImageResponse struct {
	// ID identifies the picture.
	ID uuid.UUID `json:"id"`
	// URL is the displayable link returned by the provider.
	URL string `json:"url"`
	// Width and Height are the stored pixel dimensions.
	Width  int `json:"width"`
	Height int `json:"height"`
}

// PublicProductDetailResponse is one product's full detail for a customer: the
// list fields plus the description and every picture in order (FR-005). The
// pictures are in display order, which is what FR-005 requires.
type PublicProductDetailResponse struct {
	PublicProductResponse
	// Description may be empty; an empty description is valid.
	Description string `json:"description"`
	// Images holds every picture in display order. It is always an array, never
	// null, so a product with no picture answers `[]`.
	Images []PublicImageResponse `json:"images"`
}
