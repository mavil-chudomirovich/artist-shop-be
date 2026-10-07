// Package dto defines the product module's use-case input/output types.
//
// They are separate from the HTTP shapes: these carry what a use case computes,
// while presentation owns what a response looks like. The public and
// administrator outputs are deliberately two families of types, so a field that
// only the administrator may see cannot leak into a customer response by accident
// (FR-008). Money travels as model.Price — an integer minor-unit amount plus its
// currency — so no floating point appears anywhere in the path (FR-031).
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// PublicProductOutput is one entry of the customer-facing list: exactly the
// members FR-004 names. The sell state, the position, the category, the set flag
// and the folding key are absent by design (FR-008).
type PublicProductOutput struct {
	// ID is the product's stable public identifier.
	ID uuid.UUID
	// Name is the name the operator wrote.
	Name string
	// Slug is the segment a customer-facing link is built from.
	Slug string
	// Price is the integer amount in the currency's minor unit plus the
	// currency (FR-004, FR-031).
	Price model.Price
	// ImageURL is the main picture's link, or nil when the product has none. A
	// nil value is the only signal that the product has no picture.
	ImageURL *string
	// IsPreorder reports whether the product is announced but not yet available.
	// Such a product is visible and not buyable (FR-002, FR-040).
	IsPreorder bool
}

// PublicImageOutput is one picture as a customer sees it. The provider's opaque
// identifier is deliberately absent: a customer has no use for it and it is only
// needed to release the stored asset.
type PublicImageOutput struct {
	// ID identifies the picture; the public detail lists pictures by it.
	ID uuid.UUID
	// URL is the displayable link returned by the provider.
	URL string
	// Width and Height are the stored pixel dimensions.
	Width  int
	Height int
}

// PublicProductDetailOutput is one product's full detail for a customer: the list
// fields plus the description and every picture (FR-005).
type PublicProductDetailOutput struct {
	PublicProductOutput
	// Description may be empty; an empty description is valid.
	Description string
	// Images holds every picture in display order. It is nil when the product
	// has no picture.
	Images []PublicImageOutput
}

// PublicProductPage is one page of the customer-facing catalogue.
type PublicProductPage struct {
	// Products holds the page's products in the configured order.
	Products []PublicProductOutput
	// Page and PageSize echo the requested window.
	Page     int
	PageSize int
	// Total is the number of products a customer may see.
	Total int64
}

// ListPublicInput pages through the public catalogue.
//
// It carries the optional `category` filter as a slug, because that is what a
// customer-facing category link carries (research D11). The slug is resolved to
// an identifier by the use case, which then supplies the repository's
// VisibleListQuery; the repository never sees a slug.
type ListPublicInput struct {
	// Page is 1-based.
	Page int
	// PageSize is the number of products per page.
	PageSize int
	// CategorySlug narrows the page to one category when non-empty. A hidden or
	// unknown slug answers an empty page, like a category with no visible
	// product (FR-006).
	CategorySlug string
}

// PublicProductRefInput addresses one product of the public surface by the slug a
// customer-facing link is built from.
type PublicProductRefInput struct {
	// Slug is the public link segment.
	Slug string
}

// AdminProductOutput is one entry of the administrator list, including the
// members a customer must never receive: the description, the category, the
// position, the sell state, the set flag, the pre-order label and the timestamps
// (FR-011).
type AdminProductOutput struct {
	// ID is the product's stable identifier.
	ID uuid.UUID
	// Name is the name the operator wrote.
	Name string
	// Slug is the link segment the operator wrote.
	Slug string
	// Description is the text shown to customers; an empty string is valid.
	Description string
	// Price is the integer amount in the currency's minor unit plus the
	// currency.
	Price model.Price
	// CategoryID is the one category the product belongs to.
	CategoryID uuid.UUID
	// Position is the operator's ordering preference.
	Position int
	// SellState is where the product is in its selling life (FR-022).
	SellState constant.SellState
	// IsSet reports whether the product is a combo set (FR-039).
	IsSet bool
	// IsPreorder reports whether the product is announced as a pre-order
	// (FR-040).
	IsPreorder bool
	// PreorderExpectedAt is the optional expected availability date, or nil.
	PreorderExpectedAt *time.Time
	// ImageCount is how many pictures the product has.
	ImageCount int
	// ImageURL is the main picture's link, or nil when the product has none.
	ImageURL *string
	// CreatedAt is when the product was created.
	CreatedAt time.Time
	// UpdatedAt is when the product was last written.
	UpdatedAt time.Time
}

// AdminImageOutput is one picture as an administrator sees it, including the
// provider's opaque identifier, its position and whether it is the main one.
type AdminImageOutput struct {
	// ID identifies the picture; the administrator addresses it by this.
	ID uuid.UUID
	// PublicID is the media provider's opaque identifier, used to release the
	// asset.
	PublicID string
	// URL is the displayable link.
	URL string
	// Width and Height are the stored pixel dimensions.
	Width  int
	Height int
	// Position is the display order.
	Position int
	// IsPrimary reports whether this is the product's main picture.
	IsPrimary bool
}

// SetMemberOutput is one product inside a combo set, as the administrator detail
// lists it. The public shape does not enumerate a set's contents (research D18).
type SetMemberOutput struct {
	// ID is the member product's identifier.
	ID uuid.UUID
	// Name is the member product's name.
	Name string
	// Slug is the member product's public link segment.
	Slug string
}

// AdminProductDetailOutput is one product's full detail for an administrator:
// every administrator field plus its pictures and, for a set, its members
// (FR-011, research D18).
type AdminProductDetailOutput struct {
	AdminProductOutput
	// Images holds every picture in display order. It is nil when the product
	// has no picture.
	Images []AdminImageOutput
	// Members holds the products inside a set, in order. It is nil for a product
	// that is not a set and for a set with no members.
	Members []SetMemberOutput
}

// AdminProductPage is one page of the administrator list.
type AdminProductPage struct {
	// Products holds the page's products, including the ones not visible to
	// customers, in the configured order.
	Products []AdminProductOutput
	// Page and PageSize echo the requested window.
	Page     int
	PageSize int
	// Total is the number of products in the catalogue.
	Total int64
}

// ListAdminInput pages through the administrator list.
type ListAdminInput struct {
	// Page is 1-based.
	Page int
	// PageSize is the number of products per page.
	PageSize int
}

// AdminProductRefInput addresses one product of the administrator surface by its
// identifier, which cannot change under a client that is mid-edit.
type AdminProductRefInput struct {
	// ID is the product's identifier.
	ID uuid.UUID
}

// CreateProductInput creates a product. The acting administrator is taken from
// the session, never from this input (FR-015).
type CreateProductInput struct {
	// Name is the product name.
	Name string
	// Slug is the public link segment.
	Slug string
	// Description is the text shown to customers; an empty string is valid.
	Description string
	// Price is the price with its currency.
	Price model.Price
	// CategoryID is the one category the product belongs to.
	CategoryID uuid.UUID
	// Position is the ordering preference.
	Position int
	// IsSet reports whether the product is a combo set.
	IsSet bool
	// MemberProductIDs are the products inside the set, in order. They are
	// recorded only when IsSet is true.
	MemberProductIDs []uuid.UUID
	// IsPreorder reports whether the product is announced as a pre-order.
	IsPreorder bool
	// PreorderExpectedAt is the optional expected availability date.
	PreorderExpectedAt *time.Time
}

// UpdateProductInput edits a product. Every member keeps its current value when
// the corresponding pointer is nil; a pointer to a zero value changes it. The
// sell state is deliberately absent: a transition has its own endpoint
// (research D11).
//
// MemberProductIDs is a pointer to a slice because "not sent" and "sent empty"
// mean different things: the first leaves the members alone, the second clears
// the set. PreorderExpectedAt carries no separate presence flag — sending the
// label false clears the date, and the handler decides whether a date was sent.
type UpdateProductInput struct {
	// ID is the product to edit.
	ID uuid.UUID
	// Name is the new name; nil keeps the current value.
	Name *string
	// Slug is the new slug; nil keeps the current value.
	Slug *string
	// Description is the new description; nil keeps the current value.
	Description *string
	// Price is the new price; nil keeps the current value.
	Price *model.Price
	// CategoryID is the new category; nil keeps the current value.
	CategoryID *uuid.UUID
	// Position is the new position; nil keeps the current value.
	Position *int
	// IsSet is the new combo-set flag; nil keeps the current value.
	IsSet *bool
	// MemberProductIDs replaces the whole member list when non-nil. It is
	// recorded only when the product is a set.
	MemberProductIDs *[]uuid.UUID
	// IsPreorder is the new pre-order label; nil keeps the current value. Setting
	// it false clears PreorderExpectedAt (FR-040).
	IsPreorder *bool
	// PreorderExpectedAt is the new expected date; nil keeps the current value.
	PreorderExpectedAt *time.Time
}

// ChangeSellStateInput requests one sell-state transition. The target is routed
// through the domain's transition table; a target the table has no edge for is
// refused (FR-022, FR-023).
type ChangeSellStateInput struct {
	// ID is the product to move.
	ID uuid.UUID
	// To is the requested target state.
	To constant.SellState
}

// AddPictureInput carries an uploaded picture to the use case. The bytes are
// validated by the domain from their content, never from the client-declared file
// name, which is why no name is carried here at all (FR-019).
type AddPictureInput struct {
	// ProductID is the product the picture is attached to.
	ProductID uuid.UUID
	// Content is the uploaded payload, already capped at the configured ceiling.
	Content []byte
}

// RemovePictureInput detaches one picture from a product.
type RemovePictureInput struct {
	// ProductID is the product the picture belongs to.
	ProductID uuid.UUID
	// PictureID is the picture to remove. It is always scoped to ProductID, so a
	// picture of another product is simply not found (FR-016).
	PictureID uuid.UUID
}

// SetPrimaryPictureInput makes one picture the product's main one.
type SetPrimaryPictureInput struct {
	// ProductID is the product the picture belongs to.
	ProductID uuid.UUID
	// PictureID is the picture to promote. It is always scoped to ProductID.
	PictureID uuid.UUID
}
