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
	// VariantGroupID attaches the new product to a group as an option; nil
	// creates an ordinary product (FR-010). It is meaningless without
	// VariantLabel, and the domain refuses the pair being half-sent.
	VariantGroupID *uuid.UUID
	// VariantLabel is the option's short choice label; required when
	// VariantGroupID is set (FR-021).
	VariantLabel *string
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
	// VariantGroupID is the target group. It is consulted only when
	// VariantGroupSet is true: a non-nil value attaches or moves the product, a
	// nil value detaches it (FR-010, FR-011).
	VariantGroupID *uuid.UUID
	// VariantGroupSet reports whether the edit is changing the attachment. A
	// plain pointer cannot tell "omitted" from "explicitly null", so this flag is
	// what makes detach a real operation (research D13).
	VariantGroupSet bool
	// VariantLabel is the new option label. It is consulted only when
	// VariantLabelSet is true: a non-blank value sets it, a nil or blank value
	// clears it (FR-021).
	VariantLabel *string
	// VariantLabelSet reports whether the edit is changing the label (research
	// D13).
	VariantLabelSet bool
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

// The catalogue entry discriminator values. They are the `type` field of
// PublicCatalogueEntry in specs/014-product-variants/contracts/openapi.yaml and
// the serialised values domainrepository.CatalogueEntryType carries
// (research D12).
const (
	// CatalogueEntryTypeProduct marks a standalone product: a product with
	// variant_group_id null.
	CatalogueEntryTypeProduct = "product"
	// CatalogueEntryTypeVariantGroup marks a variant group.
	CatalogueEntryTypeVariantGroup = "variant-group"
)

// PublicCatalogueEntry is one row of the merged public catalogue: a standalone
// product or a variant group, discriminated by Type (FR-001, research D4).
//
// The customer-facing members are exactly what the list shows; a group's Price
// is the lowest price among its visible options and its ImageURL is its own main
// picture, while a standalone product's Price and ImageURL are its own. A group
// always carries IsPreorder false, because a group has no sell state (FR-005).
type PublicCatalogueEntry struct {
	// Type is "product" or "variant-group"; the client picks the route by it.
	Type string
	// ID is the entry's stable public identifier (the product's or the group's).
	ID uuid.UUID
	// Name is the name the operator wrote.
	Name string
	// Slug is the segment a customer-facing link is built from. A group is
	// addressed at /product-groups/{slug}, a product at /products/{slug}
	// (research D6).
	Slug string
	// Price is the integer amount in the currency's minor unit plus the currency
	// (FR-004, FR-005, FR-031).
	Price model.Price
	// ImageURL is the entry's main picture's link, or nil when it has none.
	ImageURL *string
	// IsPreorder reports whether a standalone product is announced but not yet
	// available. It is always false for a group.
	IsPreorder bool
}

// PublicCataloguePage is one page of the merged customer-facing catalogue.
type PublicCataloguePage struct {
	// Entries holds the page's products and groups in the configured order.
	Entries []PublicCatalogueEntry
	// Page and PageSize echo the requested window.
	Page     int
	PageSize int
	// Total is the number of entries a customer may see across both kinds.
	Total int64
}

// PublicVariantOptionOutput is one option as a customer sees it in a group: the
// option's own label, price, picture and preorder flag; nothing is inherited
// from the group or its siblings (FR-016, FR-018).
type PublicVariantOptionOutput struct {
	// ID is the option product's stable identifier.
	ID uuid.UUID
	// Slug is the option product's public link segment (FR-007).
	Slug string
	// Label is the option's value in the group's choice dimension (FR-021).
	Label string
	// Price is the option's own price and currency (FR-016).
	Price model.Price
	// ImageURL is the option's main picture's link, or nil when it has none.
	ImageURL *string
	// IsPreorder reports whether the option is an announced pre-order.
	IsPreorder bool
}

// PublicVariantGroupDetailOutput is a group's full detail for a customer: its
// shared content, its own gallery and its visible options (FR-002, FR-005).
type PublicVariantGroupDetailOutput struct {
	// ID is the group's stable identifier.
	ID uuid.UUID
	// Name is the shareable name of the group.
	Name string
	// Slug is the group's public link segment.
	Slug string
	// Description may be empty.
	Description string
	// OptionName is the single choice-dimension name, for example "Card".
	OptionName string
	// Images holds the group's own pictures in display order; nil when none.
	Images []PublicImageOutput
	// Options holds the visible options in display order; nil when none.
	Options []PublicVariantOptionOutput
}

// PublicVariantGroupRefInput addresses a public group by the slug a
// customer-facing link is built from.
type PublicVariantGroupRefInput struct {
	// Slug is the group's public link segment.
	Slug string
}

// AdminVariantGroupOutput is one entry of the administrator group list, with the
// members a customer must never receive (FR-013).
type AdminVariantGroupOutput struct {
	// ID is the group's stable identifier.
	ID uuid.UUID
	// Name is the name the operator wrote.
	Name string
	// Slug is the link segment the operator wrote.
	Slug string
	// Description is the text shown to customers; an empty string is valid.
	Description string
	// CategoryID is the one category the group belongs to.
	CategoryID uuid.UUID
	// Position is the operator's ordering preference.
	Position int
	// OptionName is the single choice-dimension name.
	OptionName string
	// ImageCount is how many pictures the group has.
	ImageCount int
	// ImageURL is the group's main picture's link, or nil when it has none.
	ImageURL *string
	// CreatedAt is when the group was created.
	CreatedAt time.Time
	// UpdatedAt is when the group was last written.
	UpdatedAt time.Time
}

// AdminGroupImageOutput is one group picture as an administrator sees it,
// including the provider's opaque identifier, its position and whether it is the
// main one.
type AdminGroupImageOutput struct {
	// ID identifies the picture; the administrator addresses it by this.
	ID uuid.UUID
	// PublicID is the media provider's opaque identifier (FR-018).
	PublicID string
	// URL is the displayable link.
	URL string
	// Width and Height are the stored pixel dimensions.
	Width  int
	Height int
	// Position is the display order.
	Position int
	// IsPrimary reports whether this is the group's main picture.
	IsPrimary bool
}

// AdminGroupOptionOutput is one option as the administrator sees it: the
// option's own label, price, picture, position and sell state, so the operator
// can manage it without a second read (FR-013, FR-017).
type AdminGroupOptionOutput struct {
	// ID is the option product's stable identifier.
	ID uuid.UUID
	// Name is the option product's name.
	Name string
	// Slug is the option product's public link segment.
	Slug string
	// Label is the option's value in the group's choice dimension.
	Label string
	// Price is the option's own price and currency.
	Price model.Price
	// ImageURL is the option's main picture's link, or nil when it has none.
	ImageURL *string
	// Position is the option product's ordering preference (research D5).
	Position int
	// SellState is the option product's selling-life state (FR-017).
	SellState constant.SellState
	// IsPreorder reports whether the option is an announced pre-order.
	IsPreorder bool
}

// AdminVariantGroupDetailOutput is a group's full detail for an administrator:
// every administrator field plus its pictures and, including the withheld
// options, the full option list (FR-013).
type AdminVariantGroupDetailOutput struct {
	AdminVariantGroupOutput
	// Images holds every picture in display order; nil when none.
	Images []AdminGroupImageOutput
	// Options holds every option, visible or not; nil when the group is empty.
	Options []AdminGroupOptionOutput
}

// AdminVariantGroupPage is one page of the administrator group list.
type AdminVariantGroupPage struct {
	// Groups holds the page's groups in the configured order.
	Groups []AdminVariantGroupOutput
	// Page and PageSize echo the requested window.
	Page     int
	PageSize int
	// Total is the number of groups in the catalogue.
	Total int64
}

// ListAdminGroupsInput pages through the administrator group list.
type ListAdminGroupsInput struct {
	// Page is 1-based.
	Page int
	// PageSize is the number of groups per page.
	PageSize int
}

// AdminVariantGroupRefInput addresses one group of the administrator surface by
// its identifier, which cannot change under a client that is mid-edit.
type AdminVariantGroupRefInput struct {
	// ID is the group's identifier.
	ID uuid.UUID
}

// CreateVariantGroupInput creates a variant group. The acting administrator is
// taken from the session, never from this input (FR-015).
type CreateVariantGroupInput struct {
	// Name is the group name.
	Name string
	// Slug is the public link segment.
	Slug string
	// Description is the text shown to customers; an empty string is valid.
	Description string
	// CategoryID is the one category the group belongs to (FR-023).
	CategoryID uuid.UUID
	// Position is the ordering preference.
	Position int
	// OptionName is the single choice-dimension name (FR-009).
	OptionName string
}

// UpdateVariantGroupInput edits a group. Every member keeps its current value
// when the corresponding pointer is nil; a pointer to a zero value changes it.
// The pictures are edited through their own operations, so they are deliberately
// absent here.
type UpdateVariantGroupInput struct {
	// ID is the group to edit.
	ID uuid.UUID
	// Name is the new name; nil keeps the current value.
	Name *string
	// Slug is the new slug; nil keeps the current value.
	Slug *string
	// Description is the new description; nil keeps the current value.
	Description *string
	// CategoryID is the new category; nil keeps the current value.
	CategoryID *uuid.UUID
	// Position is the new position; nil keeps the current value.
	Position *int
	// OptionName is the new choice-dimension name; nil keeps the current value.
	OptionName *string
}

// AddGroupImageInput carries an uploaded group picture to the use case. The
// bytes are validated by the domain from their content, never from the
// client-declared file name, which is why no name is carried here at all
// (FR-018).
type AddGroupImageInput struct {
	// GroupID is the group the picture is attached to.
	GroupID uuid.UUID
	// Content is the uploaded payload, already capped at the configured ceiling.
	Content []byte
}

// RemoveGroupImageInput detaches one picture from a group.
type RemoveGroupImageInput struct {
	// GroupID is the group the picture belongs to.
	GroupID uuid.UUID
	// ImageID is the picture to remove. It is always scoped to GroupID, so a
	// picture of another group is simply not found (FR-018).
	ImageID uuid.UUID
}

// SetPrimaryGroupImageInput makes one picture the group's main one.
type SetPrimaryGroupImageInput struct {
	// GroupID is the group the picture belongs to.
	GroupID uuid.UUID
	// ImageID is the picture to promote. It is always scoped to GroupID.
	ImageID uuid.UUID
}
