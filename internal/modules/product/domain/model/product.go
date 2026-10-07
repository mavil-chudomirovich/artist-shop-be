// Package model holds the product module's entities and value objects.
//
// The domain validates structure and the transitions only. Whether a slug is
// already used is a catalogue-wide invariant that a single entity cannot decide, so
// the storage layer enforces it (FR-033) and the application reports it; this
// package owns the rule a human reads and the shape and bounds of the values
// (Constitution I).
package model

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// Contract member names of a product, used when a rejection has to say which input
// is wrong. They are the exact member names of
// specs/006-product-catalog/contracts/openapi.yaml, so presentation can put one
// straight into the response detail without a second vocabulary that could drift
// away from the contract.
const (
	// FieldName is the request member carrying a product name.
	FieldName = "name"
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
	// FieldIsSet is the request member carrying the combo-set flag.
	FieldIsSet = "isSet"
	// FieldIsPreorder is the request member carrying the pre-order label.
	FieldIsPreorder = "isPreorder"
	// FieldPreorderExpectedAt is the request member carrying the expected date.
	FieldPreorderExpectedAt = "preorderExpectedAt"
	// FieldMemberProductIDs is the request member carrying a set's members.
	FieldMemberProductIDs = "memberProductIds"
)

// Product is one thing the shop sells: a physical item, a combo set or a
// pre-order.
//
// The sell state is method-driven, never a settable one (research D8, Constitution
// III): a caller asks the entity to transition and the entity refuses a move the
// transition table does not have an edge for. SellState is exported only so the
// persistence adapter can scan it; nothing else may assign it.
//
// NormalizedSlug is the folding key the storage layer indexes for catalogue-wide
// uniqueness. It is recomputed by every transition from the value being stored, so
// it can never disagree with Slug. It exists only to be stored, indexed and
// compared: it is never returned to anyone, never audited and never named in the
// API contract.
type Product struct {
	// ID identifies the product. It is generated once and is stable for the
	// product's lifetime, so a saved link keeps working (FR-016).
	ID uuid.UUID
	// Name is the name the operator typed, trimmed. Not unique (research D5).
	Name string
	// Slug is the link segment the operator wrote, lowercase and URL-safe by
	// validation (FR-029).
	Slug string
	// NormalizedSlug is the folding key for Slug (FR-028).
	NormalizedSlug string
	// Description is what the operator wrote. An empty string is valid.
	Description string
	// Price is the integer amount in the currency's minor unit plus its currency
	// (FR-031).
	Price Price
	// CategoryID is the one category the product belongs to (FR-033).
	CategoryID uuid.UUID
	// Position is the operator's place in the order. Any whole number, including
	// zero and negative; it is a preference, not a sequence.
	Position int
	// SellState is where the product is in its selling life. It is changed only
	// through the transition methods below, never assigned.
	SellState constant.SellState
	// IsSet reports whether the product is a combo set (FR-039).
	IsSet bool
	// IsPreorder reports whether the product is announced as a pre-order while it
	// is not on sale (FR-040).
	IsPreorder bool
	// PreorderExpectedAt is the optional expected availability date of a
	// pre-order. Only meaningful while IsPreorder is true (FR-040).
	PreorderExpectedAt *time.Time
	// CreatedAt is when the product was created. Set once.
	CreatedAt time.Time
	// UpdatedAt is when the product was last written.
	UpdatedAt time.Time
}

// ProductDraft carries the caller-supplied members of a new product.
type ProductDraft struct {
	// Name is the product name. It must not be blank and must fit
	// MaxNameRunes characters.
	Name string
	// Slug is the public link segment. It must match SlugPattern and fit
	// MaxSlugRunes characters.
	Slug string
	// Description is the text shown to customers. It may be empty and must fit
	// MaxDescriptionRunes characters.
	Description string
	// Price is the price with its currency. It must be positive and carry three
	// uppercase letters.
	Price Price
	// CategoryID is the category the product belongs to.
	CategoryID uuid.UUID
	// Position is the ordering preference.
	Position int
	// IsSet reports whether the product is a combo set.
	IsSet bool
	// IsPreorder reports whether the product is announced as a pre-order.
	IsPreorder bool
	// PreorderExpectedAt is the optional expected availability date.
	PreorderExpectedAt *time.Time
}

// ProductEdit carries the changes of a partial edit.
//
// A nil member keeps its current value. The sell state is deliberately absent so
// an edit can never move it; the transitions own that (research D8, D11).
type ProductEdit struct {
	// Name is the new name; nil keeps the current value.
	Name *string
	// Slug is the new slug; nil keeps the current value.
	Slug *string
	// Description is the new description; nil keeps the current value.
	Description *string
	// Price is the new price; nil keeps the current value.
	Price *Price
	// CategoryID is the new category; nil keeps the current value.
	CategoryID *uuid.UUID
	// Position is the new position; nil keeps the current value.
	Position *int
	// IsSet is the new combo-set flag; nil keeps the current value.
	IsSet *bool
	// IsPreorder is the new pre-order label; nil keeps the current value. Setting
	// it false also clears PreorderExpectedAt (FR-040).
	IsPreorder *bool
	// PreorderExpectedAt is the new expected date; nil keeps the current value.
	PreorderExpectedAt *time.Time
}

// NewProduct builds a product in COMING_SOON, the first state of the selling life
// (FR-021, research D8).
//
// It trims the three free-text members, validates them and the price, and derives
// the folding key from the slug it is about to store. A rejected draft yields no
// product at all, so a caller cannot persist half of one.
func NewProduct(draft ProductDraft, now time.Time) (*Product, error) {
	name := strings.TrimSpace(draft.Name)
	slug := strings.TrimSpace(draft.Slug)
	description := strings.TrimSpace(draft.Description)

	if err := ValidateName(name); err != nil {
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
	if err := validatePreorder(constant.SellStateComingSoon, draft.IsPreorder, draft.PreorderExpectedAt); err != nil {
		return nil, err
	}

	return &Product{
		ID:                 uuid.New(),
		Name:               name,
		Slug:               slug,
		NormalizedSlug:     FoldKey(slug),
		Description:        description,
		Price:              draft.Price,
		CategoryID:         draft.CategoryID,
		Position:           draft.Position,
		SellState:          constant.SellStateComingSoon,
		IsSet:              draft.IsSet,
		IsPreorder:         draft.IsPreorder,
		PreorderExpectedAt: draft.PreorderExpectedAt,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

// Apply writes a partial edit.
//
// It stages the whole change, validates it, and only then writes, so a rejection
// leaves the product exactly as it was — including its sell state, which is never
// touched here (FR-012, FR-024). The folding key is recomputed from the slug the
// entity ends up storing, so it always agrees with Slug.
func (p *Product) Apply(edit ProductEdit, now time.Time) error {
	name := p.Name
	slug := p.Slug
	description := p.Description
	price := p.Price
	categoryID := p.CategoryID
	position := p.Position
	isSet := p.IsSet
	isPreorder := p.IsPreorder
	expectedAt := p.PreorderExpectedAt

	if edit.Name != nil {
		name = strings.TrimSpace(*edit.Name)
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
	if edit.IsSet != nil {
		isSet = *edit.IsSet
	}
	if edit.IsPreorder != nil {
		isPreorder = *edit.IsPreorder
		if !isPreorder {
			expectedAt = nil
		}
	}
	if edit.PreorderExpectedAt != nil {
		expectedAt = edit.PreorderExpectedAt
	}

	if err := ValidateName(name); err != nil {
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
	if err := validatePreorder(p.SellState, isPreorder, expectedAt); err != nil {
		return err
	}

	p.Name = name
	p.Slug = slug
	p.NormalizedSlug = FoldKey(slug)
	p.Description = description
	p.Price = price
	p.CategoryID = categoryID
	p.Position = position
	p.IsSet = isSet
	p.IsPreorder = isPreorder
	p.PreorderExpectedAt = expectedAt
	p.UpdatedAt = now
	return nil
}

// Launch moves a COMING_SOON product to ACTIVE and clears the pre-order label
// (FR-021, FR-039). A product in any other state is refused naming the state it is
// in.
func (p *Product) Launch(now time.Time) error {
	if p.SellState != constant.SellStateComingSoon {
		return domainerr.InvalidStateTransition(p.SellState, constant.SellStateActive)
	}
	return p.applyState(constant.SellStateActive, now)
}

// SellOut moves an ACTIVE product to OUT_OF_STOCK (FR-021).
func (p *Product) SellOut(now time.Time) error {
	if p.SellState != constant.SellStateActive {
		return domainerr.InvalidStateTransition(p.SellState, constant.SellStateOutOfStock)
	}
	return p.applyState(constant.SellStateOutOfStock, now)
}

// Restock returns an OUT_OF_STOCK product to ACTIVE. It is the edge research D8
// adds to the module document's one-way chain, so a sold-out product can be sold
// again without duplicating it.
func (p *Product) Restock(now time.Time) error {
	if p.SellState != constant.SellStateOutOfStock {
		return domainerr.InvalidStateTransition(p.SellState, constant.SellStateActive)
	}
	return p.applyState(constant.SellStateActive, now)
}

// Retire moves any non-terminal product to DISCONTINUED. The state is terminal:
// a retired product never returns to sale (FR-026).
func (p *Product) Retire(now time.Time) error {
	if !constant.IsAllowedTransition(p.SellState, constant.SellStateDiscontinued) {
		return domainerr.InvalidStateTransition(p.SellState, constant.SellStateDiscontinued)
	}
	return p.applyState(constant.SellStateDiscontinued, now)
}

// ChangeSellState routes a requested target through the transition table, so the
// lifecycle use case never writes a state directly (FR-023, research D8). A target
// the table has no edge for is refused with the current state named (FR-024).
func (p *Product) ChangeSellState(to constant.SellState, now time.Time) error {
	if !constant.IsAllowedTransition(p.SellState, to) {
		return domainerr.InvalidStateTransition(p.SellState, to)
	}
	return p.applyState(to, now)
}

// applyState is the single write of SellState. Going on sale clears the pre-order
// label, because a product that is on sale is not "coming soon" and the two facts
// must not disagree (FR-039).
func (p *Product) applyState(to constant.SellState, now time.Time) error {
	p.SellState = to
	if to == constant.SellStateActive {
		p.IsPreorder = false
		p.PreorderExpectedAt = nil
	}
	p.UpdatedAt = now
	return nil
}

// validatePreorder enforces the two pre-order consistency rules FR-040 states.
// The same rules are check constraints on the table, so the database refuses a
// value that bypassed this function; here the operator is told which field to fix.
func validatePreorder(state constant.SellState, isPreorder bool, expectedAt *time.Time) error {
	if expectedAt != nil && !isPreorder {
		return domainerr.InvalidProductField(
			FieldPreorderExpectedAt, "belongs to a pre-order and to nothing else")
	}
	if isPreorder && state == constant.SellStateActive {
		return domainerr.InvalidProductField(
			FieldIsPreorder, "cannot be set while the product is on sale")
	}
	return nil
}
