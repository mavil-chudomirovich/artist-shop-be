// Package model holds the product module's entities and value objects.
//
// The domain validates structure and the transitions only. Whether a slug, a
// product code or a SKU is already used is a catalogue-wide invariant that a single
// entity cannot decide, so the storage layer enforces it and the application
// reports it; this package owns the rule a human reads and the shape and bounds of
// the values (Constitution I).
package model

import (
	"strings"
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// Contract member names of a product, used when a rejection has to say which input
// is wrong. They are the exact member names of the product create/update contract,
// so presentation can put one straight into the response detail without a second
// vocabulary that could drift away from the contract.
const (
	// FieldName is the request member carrying a product name.
	FieldName = "name"
	// FieldCode is the request member carrying the operator-facing product code
	// (FR-011).
	FieldCode = "code"
	// FieldSlug is the request member carrying a product slug.
	FieldSlug = "slug"
	// FieldDescription is the request member carrying a product description.
	FieldDescription = "description"
	// FieldPrice is the request member carrying a product price.
	FieldPrice = "price"
	// FieldCurrency is the request member carrying the price currency.
	FieldCurrency = "currency"
	// FieldCategoryID is the request member carrying the category identifier.
	FieldCategoryID = "categoryId"
	// FieldPosition is the request member carrying the ordering preference.
	FieldPosition = "position"
	// FieldVariants is the request member carrying the variants a product is
	// created with (FR-012). A product is created with at least one variant, so an
	// empty list is refused naming this member.
	FieldVariants = "variants"
	// FieldTo is the request member carrying a requested variant sell-state
	// target (FR-015). A target that is not one of the four states is refused
	// naming it.
	FieldTo = "to"
)

// Product is the parent a customer browses and describes: a name, a unique product
// code, a default price, a category, a description, a link segment and its own
// picture gallery. It groups one or more variants and is never sold on its own
// (FR-001, FR-003, research D1, D2).
//
// It has no sell state of its own: the sell state lives on each variant (FR-015).
// The default price is what a variant with no own price falls back to (FR-017).
//
// NormalizedSlug is the folding key the storage layer indexes for catalogue-wide
// uniqueness. It is recomputed by every edit from the value being stored, so it can
// never disagree with Slug. It exists only to be stored, indexed and compared: it
// is never returned to anyone, never audited and never named in the API contract.
type Product struct {
	// ID identifies the product. It is generated once and is stable for the
	// product's lifetime, so a saved link keeps working (FR-009).
	ID uuid.UUID
	// Name is the name the operator typed, trimmed. Not unique.
	Name string
	// Code is the operator-facing product code, unique across products, compared
	// after trimming surrounding whitespace and ignoring letter case (FR-011).
	Code string
	// Slug is the link segment the operator wrote, lowercase and URL-safe by
	// validation (FR-009).
	Slug string
	// NormalizedSlug is the folding key for Slug (FR-011).
	NormalizedSlug string
	// Description is what the operator wrote. An empty string is valid.
	Description string
	// Price is the default integer amount in the currency's minor unit plus its
	// currency; a variant with no own price falls back to it (FR-017, FR-024).
	Price Price
	// CategoryID is the one category the product belongs to (FR-004).
	CategoryID uuid.UUID
	// Position is the operator's place in the order. Any whole number, including
	// zero and negative; it is a preference, not a sequence.
	Position int
	// CreatedAt is when the product was created. Set once.
	CreatedAt time.Time
	// UpdatedAt is when the product was last written.
	UpdatedAt time.Time
}

// ProductDraft carries the caller-supplied members of a new product.
type ProductDraft struct {
	// Name is the product name. It must not be blank and must fit MaxNameRunes
	// characters.
	Name string
	// Code is the unique operator-facing product code. It must not be blank and
	// must fit MaxCodeRunes characters (FR-011).
	Code string
	// Slug is the public link segment. It must match SlugPattern and fit
	// MaxSlugRunes characters.
	Slug string
	// Description is the text shown to customers. It may be empty and must fit
	// MaxDescriptionRunes characters.
	Description string
	// Price is the default price with its currency. It must be positive and carry
	// three uppercase letters.
	Price Price
	// CategoryID is the category the product belongs to.
	CategoryID uuid.UUID
	// Position is the ordering preference.
	Position int
	// Variants carries the variants the product is created with. A product is
	// created with at least one variant, so an empty list is refused (FR-012,
	// research D14).
	Variants []VariantDraft
}

// ProductEdit carries the changes of a partial edit.
//
// A nil member keeps its current value. The variants are managed through their own
// operations, so they are deliberately absent here.
type ProductEdit struct {
	// Name is the new name; nil keeps the current value.
	Name *string
	// Code is the new product code; nil keeps the current value (FR-011).
	Code *string
	// Slug is the new slug; nil keeps the current value.
	Slug *string
	// Description is the new description; nil keeps the current value.
	Description *string
	// Price is the new default price; nil keeps the current value.
	Price *Price
	// CategoryID is the new category; nil keeps the current value.
	CategoryID *uuid.UUID
	// Position is the new position; nil keeps the current value.
	Position *int
}

// NewProduct builds a product.
//
// It trims the free-text members and the code, validates them and the price, derives
// the folding key from the slug it is about to store, and refuses a product with no
// variant (FR-012, research D14). A rejected draft yields no product at all, so a
// caller cannot persist half of one. The variants themselves are built by the caller
// through NewVariant, so each is validated with the variant's own rules.
func NewProduct(draft ProductDraft, now time.Time) (*Product, error) {
	name := strings.TrimSpace(draft.Name)
	code := strings.TrimSpace(draft.Code)
	slug := strings.TrimSpace(draft.Slug)
	description := strings.TrimSpace(draft.Description)

	if err := ValidateName(name); err != nil {
		return nil, err
	}
	if err := ValidateCode(code); err != nil {
		return nil, err
	}
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	if err := ValidateDescription(description); err != nil {
		return nil, err
	}
	if err := draft.Price.Validate(); err != nil {
		return nil, err
	}
	if len(draft.Variants) == 0 {
		return nil, domainerr.InvalidProductField(
			FieldVariants, "at least one variant is required")
	}

	return &Product{
		ID:             uuid.New(),
		Name:           name,
		Code:           code,
		Slug:           slug,
		NormalizedSlug: FoldKey(slug),
		Description:    description,
		Price:          draft.Price,
		CategoryID:     draft.CategoryID,
		Position:       draft.Position,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// Apply writes a partial edit.
//
// It stages the whole change, validates it, and only then writes, so a rejection
// leaves the product exactly as it was (FR-009, FR-010). The folding key is
// recomputed from the slug the entity ends up storing, so it always agrees with
// Slug. The identifier and the creation timestamp are never touched.
func (p *Product) Apply(edit ProductEdit, now time.Time) error {
	name := p.Name
	code := p.Code
	slug := p.Slug
	description := p.Description
	price := p.Price
	categoryID := p.CategoryID
	position := p.Position

	if edit.Name != nil {
		name = strings.TrimSpace(*edit.Name)
	}
	if edit.Code != nil {
		code = strings.TrimSpace(*edit.Code)
	}
	if edit.Slug != nil {
		slug = strings.TrimSpace(*edit.Slug)
	}
	if edit.Description != nil {
		description = strings.TrimSpace(*edit.Description)
	}
	if edit.Price != nil {
		price = *edit.Price
	}
	if edit.CategoryID != nil {
		categoryID = *edit.CategoryID
	}
	if edit.Position != nil {
		position = *edit.Position
	}

	if err := ValidateName(name); err != nil {
		return err
	}
	if err := ValidateCode(code); err != nil {
		return err
	}
	if err := ValidateSlug(slug); err != nil {
		return err
	}
	if err := ValidateDescription(description); err != nil {
		return err
	}
	if err := price.Validate(); err != nil {
		return err
	}

	p.Name = name
	p.Code = code
	p.Slug = slug
	p.NormalizedSlug = FoldKey(slug)
	p.Description = description
	p.Price = price
	p.CategoryID = categoryID
	p.Position = position
	p.UpdatedAt = now
	return nil
}
