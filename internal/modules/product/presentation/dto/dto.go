// Package httpdto defines the product module's HTTP request and response
// shapes.
//
// The public shapes are deliberately the members a customer reads (FR-004,
// FR-005, FR-008); the administrator shapes, which carry the sell state, the
// position, the category and the set flag the public shape must not, are added
// with the administrator routes.
package httpdto

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// preorderDateLayout is the calendar-date form the contract declares for a
// pre-order's expected-availability date (format: date). It is named so the
// intent does not depend on reading the layout string.
const preorderDateLayout = "2006-01-02"

// PreorderDate is the calendar date a pre-order is expected on, as the contract
// declares it (format: date). It accepts the date-only form the contract
// documents — "2026-12-01" — as well as the RFC 3339 form, so a client is not
// forced to add a time it does not mean (FR-040, research D9). It is used on
// both sides of the contract: a request may send either form, and a response
// writes the date-only form, so an administrator answer honours `format: date`
// instead of marshalling the embedded time.Time as RFC 3339.
type PreorderDate struct {
	time.Time
}

// MarshalJSON writes the calendar date, so a response carries "2026-12-01"
// rather than the RFC 3339 form encoding/json gives an embedded time.Time. It
// is the mirror of UnmarshalJSON and is what makes the response side of the
// contract's `format: date` true.
func (d PreorderDate) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Format(preorderDateLayout))
}

// UnmarshalJSON reads a date-only or RFC 3339 value, refusing anything else as a
// malformed request rather than silently storing a zero date.
func (d *PreorderDate) UnmarshalJSON(raw []byte) error {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	for _, layout := range []string{preorderDateLayout, time.RFC3339} {
		if parsed, err := time.Parse(layout, text); err == nil {
			d.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("preorderExpectedAt: expected a date in YYYY-MM-DD or RFC 3339 form, got %q", text)
}

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

// PriceRequest is money supplied by an operator on a create or an edit. It is an
// integer amount in the currency's minor unit plus the currency, never a floating
// point number, so the stored amount equals the amount supplied exactly (FR-031).
type PriceRequest struct {
	// Amount is the price in the currency's minor unit.
	Amount int64 `json:"amount"`
	// Currency is the currency code: exactly three uppercase letters.
	Currency string `json:"currency"`
}

// CreateProductRequest is the administrator create body (contracts/openapi.yaml,
// CreateProductRequest). It carries only the members FR-010 names plus the set
// (FR-039) and pre-order (FR-040) members.
type CreateProductRequest struct {
	// Name is the product name.
	Name string `json:"name"`
	// Slug is the public link segment.
	Slug string `json:"slug"`
	// Description is the text shown to customers; an omitted or empty value is
	// stored empty.
	Description string `json:"description"`
	// Price is the price with its currency.
	Price PriceRequest `json:"price"`
	// CategoryID is the one category the product belongs to.
	CategoryID uuid.UUID `json:"categoryId"`
	// Position is the ordering preference.
	Position int `json:"position"`
	// IsSet reports whether the product is a combo set (FR-039, research D10).
	IsSet bool `json:"isSet"`
	// MemberProductIDs are the products inside the set, in the order they are
	// listed. They are recorded only when IsSet is true (research D10).
	MemberProductIDs []uuid.UUID `json:"memberProductIds"`
	// IsPreorder reports whether the product is announced as a pre-order
	// (FR-040, research D9). A created product is COMING_SOON, so the label may
	// always be set here; the domain refuses it only once the product is on sale.
	IsPreorder bool `json:"isPreorder"`
	// PreorderExpectedAt is the optional expected-availability date, in the
	// contract's date-only form. It is meaningful only together with IsPreorder.
	PreorderExpectedAt *PreorderDate `json:"preorderExpectedAt" swaggertype:"string"`
}

// UpdateProductRequest is the administrator partial-edit body
// (contracts/openapi.yaml, UpdateProductRequest). An omitted member keeps its
// current value; a pointer to a zero value changes it. `sellState` is not
// accepted here: a state change is a transition and has its own endpoint
// (research D11).
type UpdateProductRequest struct {
	// Name is the new name; nil keeps the current value.
	Name *string `json:"name"`
	// Slug is the new slug; nil keeps the current value.
	Slug *string `json:"slug"`
	// Description is the new description; nil keeps the current value, an empty
	// string clears it.
	Description *string `json:"description"`
	// Price is the new price; nil keeps the current value.
	Price *PriceRequest `json:"price"`
	// CategoryID is the new category; nil keeps the current value.
	CategoryID *uuid.UUID `json:"categoryId"`
	// Position is the new position; nil keeps the current value.
	Position *int `json:"position"`
	// IsSet is the new combo-set flag; nil keeps the current value.
	IsSet *bool `json:"isSet"`
	// MemberProductIDs replaces the whole member list when non-nil. It is
	// recorded only when the product is a set (research D10); sending an empty
	// list clears the set's members.
	MemberProductIDs *[]uuid.UUID `json:"memberProductIds"`
	// IsPreorder is the new pre-order label; nil keeps the current value. Setting
	// it false also clears PreorderExpectedAt (FR-040). The domain refuses
	// setting it true while the product is on sale, naming isPreorder.
	IsPreorder *bool `json:"isPreorder"`
	// PreorderExpectedAt is the new expected-availability date, in the
	// contract's date-only form; nil keeps the current value.
	PreorderExpectedAt *PreorderDate `json:"preorderExpectedAt" swaggertype:"string"`
}

// ChangeStateRequest is the administrator sell-state transition body
// (contracts/openapi.yaml, ChangeStateRequest). It carries only the target state:
// a transition is not a field change, so it has its own endpoint (research D11).
type ChangeStateRequest struct {
	// To is the requested target state. It must be one of the four sell states;
	// the use case validates it and reports an unknown value against this member,
	// which is why it is a string here rather than the domain enum.
	To string `json:"to"`
}

// AdminProductResponse is one entry of the administrator surface: the members a
// customer must not receive — the sell state, the position, the category and the
// set flag — plus the timestamps and the picture summary (FR-011). The public
// shape carries none of them (FR-008).
type AdminProductResponse struct {
	// ID is the product's stable identifier.
	ID uuid.UUID `json:"id"`
	// Name is the name the operator wrote.
	Name string `json:"name"`
	// Slug is the link segment the operator wrote.
	Slug string `json:"slug"`
	// Description is the text shown to customers; an empty string is valid.
	Description string `json:"description"`
	// Price is the integer amount with its currency.
	Price PriceResponse `json:"price"`
	// CategoryID is the one category the product belongs to.
	CategoryID uuid.UUID `json:"categoryId"`
	// Position is the operator's ordering preference.
	Position int `json:"position"`
	// SellState is where the product is in its selling life (FR-022).
	SellState string `json:"sellState"`
	// IsSet reports whether the product is a combo set (FR-039).
	IsSet bool `json:"isSet"`
	// IsPreorder reports whether the product is announced as a pre-order.
	IsPreorder bool `json:"isPreorder"`
	// PreorderExpectedAt is the optional expected availability date, or null. It
	// is written in the contract's date-only form (format: date).
	PreorderExpectedAt *PreorderDate `json:"preorderExpectedAt" swaggertype:"string"`
	// ImageCount is how many pictures the product has.
	ImageCount int `json:"imageCount"`
	// ImageURL is the main picture's link, or null when the product has none.
	ImageURL *string `json:"imageUrl"`
	// CreatedAt and UpdatedAt are the stored timestamps.
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AdminImageResponse is one picture as an administrator sees it, including the
// provider's opaque identifier, its position and whether it is the main one.
// None of these three is part of the public picture shape.
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

// SetMemberResponse is one product inside a combo set, as the administrator
// detail lists it. The public shape does not enumerate a set's contents
// (research D18).
type SetMemberResponse struct {
	// ID is the member product's identifier.
	ID uuid.UUID `json:"id"`
	// Name is the member product's name.
	Name string `json:"name"`
	// Slug is the member product's public link segment.
	Slug string `json:"slug"`
}

// AdminProductDetailResponse is one product's full detail for an administrator:
// the administrator fields plus its pictures and, for a set, its members
// (FR-011, research D18).
type AdminProductDetailResponse struct {
	AdminProductResponse
	// Images holds every picture in display order. It is always an array, never
	// null.
	Images []AdminImageResponse `json:"images"`
	// Members holds the products inside a set, in order. It is omitted for a
	// product that is not a set.
	Members []SetMemberResponse `json:"members,omitempty"`
}
