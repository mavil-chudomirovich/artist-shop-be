// Package dto defines the product module's use-case input/output types.
//
// They are separate from the HTTP shapes: these carry what a use case computes,
// while presentation owns what a response looks like. The public and
// administrator outputs are deliberately two families of types, so a field that
// only the administrator may see cannot leak into a customer response by accident
// (FR-008). Money travels as model.Price — an integer minor-unit amount plus its
// currency — so no floating point appears anywhere in the path (FR-024).
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
)

// PublicProductOutput is one entry of the customer-facing catalogue: exactly one
// per product, with the product's variants collapsed into a single card price and
// image (FR-001, FR-002). The sell state, the position and the category are absent
// by design (FR-008).
type PublicProductOutput struct {
	// ID is the product's stable public identifier.
	ID uuid.UUID
	// Name is the name the operator wrote.
	Name string
	// Code is the segment a customer-facing link is built from.
	Code string
	// Price is the lowest effective price among the product's on-sale variants,
	// as an integer amount plus its currency (FR-002).
	Price model.Price
	// ImageURL is the card image's link, or nil when none exists. It is the
	// default variant's image, falling back to the product's primary picture
	// (FR-002).
	ImageURL *string
}

// PublicVariantOutput is one variant as a customer reads it on the product detail:
// its name, effective price, image, sell state and whether it is currently in
// stock (FR-003, FR-005). The SKU is deliberately absent: it is stored but never
// shown to a customer (FR-011).
type PublicVariantOutput struct {
	// ID is the variant's stable identifier, the value a customer sends to add
	// the variant to the cart (FR-006).
	ID uuid.UUID
	// Name is the variant's display name.
	Name string
	// Price is the variant's effective price, its own amount or the product's
	// default, in the product's currency (FR-017).
	Price model.Price
	// ImageURL is the variant's image link, or nil when it has none.
	ImageURL *string
	// SellState is where the variant is in its selling life (FR-015).
	SellState constant.SellState
	// InStock reports whether the variant currently has stock a customer can
	// take. It is the inventory read's answer, not a re-derivation (FR-003, D10).
	InStock bool
}

// PublicImageOutput is one picture of the product's gallery as a customer sees it.
// The provider's opaque identifier is deliberately absent: a customer has no use
// for it and it is only needed to release the stored asset.
type PublicImageOutput struct {
	// ID identifies the picture; the public detail lists pictures by it.
	ID uuid.UUID
	// URL is the displayable link returned by the provider.
	URL string
	// Width and Height are the stored pixel dimensions.
	Width  int
	Height int
}

// PublicProductDetailOutput is one product's full detail for a customer: its
// shared content, its gallery and its visible variants (FR-003, FR-005). A
// variant that is not yet on sale or retired is absent.
type PublicProductDetailOutput struct {
	PublicProductOutput
	// Description may be empty; an empty description is valid.
	Description string
	// Images holds every gallery picture in display order. It is nil when the
	// product has no picture.
	Images []PublicImageOutput
	// Variants holds the product's visible variants (on sale or out of stock) in
	// display order. It is nil when the product has none, which the application
	// never lets be stored (FR-012).
	Variants []PublicVariantOutput
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
// customer-facing category link carries. The slug is resolved to an identifier
// by the use case, which then supplies the repository's VisibleListQuery; the
// repository never sees a slug.
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

// PublicProductRefInput addresses one product of the public surface by the code a
// customer-facing link is built from.
type PublicProductRefInput struct {
	// Code is the public link segment.
	Code string
}

// AdminVariantOutput is one variant of the administrator detail: the members a
// customer must never receive — the SKU, the own amount, the position and the
// sell state — plus the effective price the shop charges (FR-010).
type AdminVariantOutput struct {
	// ID is the variant's stable identifier.
	ID uuid.UUID
	// Name is the variant's display name.
	Name string
	// SKU is the operator-written stock-keeping unit (FR-011).
	SKU string
	// PriceAmount is the variant's own price in the currency's minor unit, or nil
	// when it falls back to the product's default (FR-017).
	PriceAmount *int64
	// EffectivePrice is the price actually charged: the variant's own amount or
	// the product's default, with the product's currency.
	EffectivePrice model.Price
	// ImageURL is the variant's image link, or nil when it has none.
	ImageURL *string
	// Position is the display order; the smallest is the default variant.
	Position int
	// SellState is where the variant is in its selling life (FR-015).
	SellState constant.SellState
}

// AdminProductOutput is one entry of the administrator surface, including the
// members a customer must never receive: the description, the category, the
// position and the variant count (FR-010).
type AdminProductOutput struct {
	// ID is the product's stable identifier.
	ID uuid.UUID
	// Name is the name the operator wrote.
	Name string
	// Code is the product code (FR-011).
	Code string
	// Description is the text shown to customers; an empty string is valid.
	Description string
	// Price is the product's default price, an integer amount plus its currency.
	Price model.Price
	// CategoryID is the one category the product belongs to.
	CategoryID uuid.UUID
	// Position is the operator's ordering preference.
	Position int
	// VariantCount is how many variants the product carries.
	VariantCount int
	// ImageURL is the main picture's link, or nil when the product has none.
	ImageURL *string
	// CreatedAt is when the product was created.
	CreatedAt time.Time
	// UpdatedAt is when the product was last written.
	UpdatedAt time.Time
}

// AdminImageOutput is one gallery picture as an administrator sees it, including
// the provider's opaque identifier, its position and whether it is the main one.
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

// AdminProductDetailOutput is one product's full detail for an administrator:
// every administrator field plus its gallery and its variants, including the ones
// withheld from customers (FR-010).
type AdminProductDetailOutput struct {
	AdminProductOutput
	// Images holds every picture in display order. It is nil when the product
	// has no picture.
	Images []AdminImageOutput
	// Variants holds every variant in display order, visible or not.
	Variants []AdminVariantOutput
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

// CreateVariantInput carries one variant of a product create. The variant's
// currency is always the product's, so none is accepted (FR-024).
type CreateVariantInput struct {
	// Name is the variant's display name.
	Name string
	// SKU is the variant's globally unique stock-keeping unit (FR-011).
	SKU string
	// PriceAmount is the variant's own price, or nil to fall back to the
	// product's default (FR-017).
	PriceAmount *int64
	// Position is the ordering preference.
	Position int
}

// CreateProductInput creates a product together with at least one variant. The
// acting administrator is taken from the session, never from this input (FR-015).
type CreateProductInput struct {
	// Name is the product name.
	Name string
	// Code is the product code (FR-011).
	Code string
	// Description is the text shown to customers; an empty string is valid.
	Description string
	// Price is the product's default price with its currency.
	Price model.Price
	// CategoryID is the one category the product belongs to.
	CategoryID uuid.UUID
	// Position is the ordering preference.
	Position int
	// Variants carries the variants the product is created with. A product is
	// created with at least one variant, so an empty list is refused (FR-012,
	// research D14).
	Variants []CreateVariantInput
}

// UpdateProductInput edits a product. Every member keeps its current value when
// the corresponding pointer is nil; a pointer to a zero value changes it. The
// variants are managed through their own operations, so they are absent here.
type UpdateProductInput struct {
	// ID is the product to edit.
	ID uuid.UUID
	// Name is the new name; nil keeps the current value.
	Name *string
	// Code is the new product code; nil keeps the current value (FR-011).
	Code *string
	// Description is the new description; nil keeps the current value.
	Description *string
	// Price is the new default price; nil keeps the current value.
	Price *model.Price
	// CategoryID is the new category; nil keeps the current value.
	CategoryID *uuid.UUID
	// Position is the new position; nil keeps the current value.
	Position *int
}

// AddVariantInput adds one variant to a product.
type AddVariantInput struct {
	// ProductID is the product the variant is added to.
	ProductID uuid.UUID
	// Name is the variant's display name.
	Name string
	// SKU is the variant's globally unique stock-keeping unit (FR-011).
	SKU string
	// PriceAmount is the variant's own price, or nil to fall back to the
	// product's default (FR-017).
	PriceAmount *int64
	// Position is the ordering preference.
	Position int
}

// UpdateVariantInput edits one variant. Every member keeps its current value when
// the corresponding pointer is nil; PriceAmountSet distinguishes "keep the price"
// from "clear it and fall back to the product's default".
type UpdateVariantInput struct {
	// ProductID is the product the variant belongs to, used to return the
	// product detail.
	ProductID uuid.UUID
	// VariantID is the variant to edit.
	VariantID uuid.UUID
	// Name is the new name; nil keeps the current value.
	Name *string
	// SKU is the new SKU; nil keeps the current value.
	SKU *string
	// PriceAmount is the new own price. It is consulted only when PriceAmountSet
	// is true: a non-nil value sets it, a nil value clears it back to the
	// product's default (FR-017).
	PriceAmount *int64
	// PriceAmountSet reports whether the edit is changing the price at all.
	PriceAmountSet bool
	// Position is the new position; nil keeps the current value.
	Position *int
}

// VariantRefInput addresses one variant of one product.
type VariantRefInput struct {
	// ProductID is the product the variant belongs to.
	ProductID uuid.UUID
	// VariantID is the variant to reach.
	VariantID uuid.UUID
}

// AddVariantImageInput carries an uploaded variant image to the use case. The
// bytes are validated by the domain from their content, never from the
// client-declared file name, which is why no name is carried here at all
// (FR-016).
type AddVariantImageInput struct {
	// ProductID is the product the variant belongs to.
	ProductID uuid.UUID
	// VariantID is the variant the image is attached to.
	VariantID uuid.UUID
	// Content is the uploaded payload, already capped at the configured ceiling.
	Content []byte
}

// ChangeVariantStateInput requests one variant sell-state transition. The target
// is routed through the domain's transition table; a target the table has no edge
// for is refused (FR-015).
type ChangeVariantStateInput struct {
	// ProductID is the product the variant belongs to, used to return the detail.
	ProductID uuid.UUID
	// VariantID is the variant to move.
	VariantID uuid.UUID
	// To is the requested target state.
	To constant.SellState
}

// AddPictureInput carries an uploaded gallery picture to the use case. The bytes
// are validated by the domain from their content, never from the client-declared
// file name, which is why no name is carried here at all (FR-019).
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
