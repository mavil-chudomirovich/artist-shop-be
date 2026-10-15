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
	// VariantGroup is the selector present when the product is an option of a
	// variant group, and omitted for a standalone product (FR-007).
	VariantGroup *PublicProductVariantGroupResponse `json:"variantGroup,omitempty"`
}

// PublicCatalogueEntryResponse is one row of the merged customer-facing
// catalogue: a standalone product or a variant group, discriminated by Type
// (FR-001, contracts/openapi.yaml PublicCatalogueEntry).
//
// The client picks the route by Type: a product at /products/{slug}, a group at
// /product-groups/{slug}. A group carries its own main picture and the lowest
// price among its visible options; a standalone product carries its own.
type PublicCatalogueEntryResponse struct {
	// Type is "product" or "variant-group".
	Type string `json:"type"`
	// ID is the entry's stable public identifier.
	ID uuid.UUID `json:"id"`
	// Name is the name the operator wrote.
	Name string `json:"name"`
	// Slug is the segment a customer-facing link is built from.
	Slug string `json:"slug"`
	// Price is the integer amount with its currency.
	Price PriceResponse `json:"price"`
	// ImageURL is the entry's main picture's link, or null when it has none.
	ImageURL *string `json:"imageUrl"`
	// IsPreorder reports whether a standalone product is announced but not yet
	// available; it is always false for a group, which carries no sell state.
	IsPreorder bool `json:"isPreorder"`
}

// PublicVariantOptionResponse is one option as a customer sees it in a group:
// the option's own label, price, picture and preorder flag; nothing is inherited
// from the group or its siblings (FR-016).
type PublicVariantOptionResponse struct {
	// ID is the option product's stable identifier.
	ID uuid.UUID `json:"id"`
	// Slug is the option product's public link segment (FR-007).
	Slug string `json:"slug"`
	// Label is the option's value in the group's single choice dimension.
	Label string `json:"label"`
	// Price is the option's own price and currency (FR-016).
	Price PriceResponse `json:"price"`
	// ImageURL is the option's main picture's link, or null when it has none.
	ImageURL *string `json:"imageUrl"`
	// IsPreorder reports whether the option is an announced pre-order.
	IsPreorder bool `json:"isPreorder"`
}

// PublicVariantGroupDetailResponse is one variant group's full detail for a
// customer: its shared content, its own gallery and its visible options
// (FR-002, FR-005, contracts/openapi.yaml PublicVariantGroupDetail).
type PublicVariantGroupDetailResponse struct {
	// ID is the group's stable identifier.
	ID uuid.UUID `json:"id"`
	// Name is the group's shared name.
	Name string `json:"name"`
	// Slug is the group's public link segment.
	Slug string `json:"slug"`
	// Description may be empty.
	Description string `json:"description"`
	// OptionName is the single choice-dimension name, for example "Card".
	OptionName string `json:"optionName"`
	// Images holds the group's own pictures in display order. It is always an
	// array, never null.
	Images []PublicImageResponse `json:"images"`
	// Options holds the visible options in display order. It is always an array,
	// never null.
	Options []PublicVariantOptionResponse `json:"options"`
}

// PublicProductVariantGroupResponse is the selector a product detail carries
// when the product is an option: the group's shared content, its own gallery and
// its visible siblings, so a shared link to one option resolves and the customer
// can switch (FR-007). It carries no description.
type PublicProductVariantGroupResponse struct {
	// ID is the group's stable identifier.
	ID uuid.UUID `json:"id"`
	// Slug is the group's public link segment.
	Slug string `json:"slug"`
	// Name is the group's shared name.
	Name string `json:"name"`
	// OptionName is the single choice-dimension name.
	OptionName string `json:"optionName"`
	// Images holds the group's own pictures in display order; always an array.
	Images []PublicImageResponse `json:"images"`
	// Options holds the group's visible options in display order; always an
	// array.
	Options []PublicVariantOptionResponse `json:"options"`
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

// OptionalUUID is a request member that distinguishes "the key was absent" from
// "the key was present and null". A plain *uuid.UUID cannot, and the product PATCH
// needs the difference: an omitted variantGroupId keeps the product's current
// attachment while an explicit null detaches it (research D13). It is decoded by
// the standard library through UnmarshalJSON, so a handler only has to read Set
// and Value.
type OptionalUUID struct {
	// Value is the parsed identifier, or nil when the key was present and null.
	Value *uuid.UUID
	// Set reports whether the JSON key was present at all.
	Set bool
}

// UnmarshalJSON records that the key was present and parses it. A JSON null sets
// Set true with a nil Value, which is what makes an explicit detach expressible.
func (o *OptionalUUID) UnmarshalJSON(raw []byte) error {
	o.Set = true
	if string(raw) == "null" {
		o.Value = nil
		return nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil {
		return err
	}
	o.Value = &id
	return nil
}

// OptionalString is the string twin of OptionalUUID, used for the option label.
type OptionalString struct {
	// Value is the string, or nil when the key was present and null.
	Value *string
	// Set reports whether the JSON key was present at all.
	Set bool
}

// UnmarshalJSON records presence and parses the value; a JSON null sets Set true
// with a nil Value.
func (o *OptionalString) UnmarshalJSON(raw []byte) error {
	o.Set = true
	if string(raw) == "null" {
		o.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}

// CreateProductRequest is the administrator create body (contracts/openapi.yaml,
// CreateProductRequest). It carries only the members FR-010 names plus the set
// (FR-039), pre-order (FR-040) and variant-attachment (FR-010) members.
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
	// VariantGroupID attaches the new product to a group as an option; an omitted
	// or null value creates an ordinary product (FR-010). It requires
	// VariantLabel.
	VariantGroupID *uuid.UUID `json:"variantGroupId"`
	// VariantLabel is the option's short choice label; required when
	// VariantGroupID is present (FR-021).
	VariantLabel *string `json:"variantLabel"`
}

// UpdateProductRequest is the administrator partial-edit body
// (contracts/openapi.yaml, UpdateProductRequest). An omitted member keeps its
// current value; a pointer to a zero value changes it. `sellState` is not
// accepted here: a state change is a transition and has its own endpoint
// (research D11).
//
// VariantGroupID and VariantLabel are presence-tracked (research D13): an omitted
// key keeps the current attachment, an explicit null detaches, and a non-null value
// attaches an ordinary product.
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
	// VariantGroupID attaches or detaches the product's option role; the JSON key
	// being absent keeps the current attachment (research D13).
	VariantGroupID OptionalUUID `json:"variantGroupId" swaggertype:"string"`
	// VariantLabel sets or clears the option label; the JSON key being absent
	// keeps the current label (research D13).
	VariantLabel OptionalString `json:"variantLabel" swaggertype:"string"`
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

// --- Variant-group administrator surface (US2) ---

// AdminVariantGroupResponse is one entry of the administrator group list, with the
// members a customer must not receive (FR-013,
// contracts/openapi.yaml AdminVariantGroup).
type AdminVariantGroupResponse struct {
	// ID is the group's stable identifier.
	ID uuid.UUID `json:"id"`
	// Name is the name the operator wrote.
	Name string `json:"name"`
	// Slug is the link segment the operator wrote.
	Slug string `json:"slug"`
	// Description is the text shown to customers; an empty string is valid.
	Description string `json:"description"`
	// CategoryID is the one category the group belongs to.
	CategoryID uuid.UUID `json:"categoryId"`
	// Position is the operator's ordering preference.
	Position int `json:"position"`
	// OptionName is the single choice-dimension name, for example "Card".
	OptionName string `json:"optionName"`
	// ImageCount is how many pictures the group has.
	ImageCount int `json:"imageCount"`
	// ImageURL is the group's main picture's link, or null when it has none.
	ImageURL *string `json:"imageUrl"`
	// CreatedAt and UpdatedAt are the stored timestamps.
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AdminGroupImageResponse is one group picture as an administrator sees it,
// including the provider's opaque identifier, its position and whether it is the
// main one (FR-018).
type AdminGroupImageResponse struct {
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
	// IsPrimary reports whether this is the group's main picture.
	IsPrimary bool `json:"isPrimary"`
}

// AdminGroupOptionResponse is one option as the administrator sees it: the
// option's own label, price, picture, position and sell state, so the operator can
// manage it without a second read (FR-013, FR-017).
type AdminGroupOptionResponse struct {
	// ID is the option product's stable identifier.
	ID uuid.UUID `json:"id"`
	// Name is the option product's name.
	Name string `json:"name"`
	// Slug is the option product's public link segment.
	Slug string `json:"slug"`
	// Label is the option's value in the group's single choice dimension.
	Label string `json:"label"`
	// Price is the option's own price and currency.
	Price PriceResponse `json:"price"`
	// ImageURL is the option's main picture's link, or null when it has none.
	ImageURL *string `json:"imageUrl"`
	// Position is the option product's ordering preference (research D5).
	Position int `json:"position"`
	// SellState is the option product's selling-life state (FR-017).
	SellState string `json:"sellState"`
	// IsPreorder reports whether the option is an announced pre-order.
	IsPreorder bool `json:"isPreorder"`
}

// AdminVariantGroupDetailResponse is one group's full detail for an administrator:
// the administrator fields plus its pictures and, including the withheld options,
// the full option list (FR-013).
type AdminVariantGroupDetailResponse struct {
	AdminVariantGroupResponse
	// Images holds every picture in display order. It is always an array, never
	// null.
	Images []AdminGroupImageResponse `json:"images"`
	// Options holds every option, visible or not. It is always an array, never
	// null.
	Options []AdminGroupOptionResponse `json:"options"`
}

// CreateVariantGroupRequest is the administrator group-create body
// (contracts/openapi.yaml, CreateVariantGroupRequest). The acting administrator is
// never part of the body (FR-015).
type CreateVariantGroupRequest struct {
	// Name is the group name.
	Name string `json:"name"`
	// Slug is the public link segment.
	Slug string `json:"slug"`
	// Description is the text shown to customers; an omitted or empty value is
	// stored empty.
	Description string `json:"description"`
	// CategoryID is the one category the group belongs to (FR-023).
	CategoryID uuid.UUID `json:"categoryId"`
	// Position is the ordering preference.
	Position int `json:"position"`
	// OptionName is the single choice-dimension name (FR-009).
	OptionName string `json:"optionName"`
}

// UpdateVariantGroupRequest is the administrator group partial-edit body
// (contracts/openapi.yaml, UpdateVariantGroupRequest). An omitted member keeps its
// current value. The pictures are edited through their own endpoints.
type UpdateVariantGroupRequest struct {
	// Name is the new name; nil keeps the current value.
	Name *string `json:"name"`
	// Slug is the new slug; nil keeps the current value.
	Slug *string `json:"slug"`
	// Description is the new description; nil keeps the current value, an empty
	// string clears it.
	Description *string `json:"description"`
	// CategoryID is the new category; nil keeps the current value.
	CategoryID *uuid.UUID `json:"categoryId"`
	// Position is the new position; nil keeps the current value.
	Position *int `json:"position"`
	// OptionName is the new choice-dimension name; nil keeps the current value.
	OptionName *string `json:"optionName"`
}
