package model

import (
	"strings"
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
)

// FieldOptionName is the request member carrying a variant group's single
// choice-dimension name (FR-009). It is the member name of
// CreateVariantGroupRequest in specs/014-product-variants/contracts/openapi.yaml,
// so a rejection can name the exact input the client sent.
const FieldOptionName = "optionName"

// VariantGroup is a catalogue entry that offers one product in several
// interchangeable choices — for example a card set whose options are "Flins" and
// "Dottore" (FR-009, research D1).
//
// It is a display container: it carries no price, no stock and no sell state, and
// it is never bought. The customer buys an option, which is an ordinary
// Product carrying this group's identifier. A group is visible to customers only
// while at least one of its options is visible; that fact is decided in SQL, not
// here (FR-006, research D7).
//
// It deliberately has no folding-slug column: the slug-format check forces the
// stored value to be lowercase and trimmed, so a folding key would always equal
// the slug (data-model.md).
type VariantGroup struct {
	// ID identifies the group. It is generated once and is stable for the group's
	// lifetime (FR-009).
	ID uuid.UUID
	// Name is the name the operator typed, trimmed. Not unique.
	Name string
	// Slug is the link segment the operator wrote, lowercase and URL-safe by
	// validation (FR-019).
	Slug string
	// Description is what the operator wrote. An empty string is valid.
	Description string
	// CategoryID is the one category the group belongs to (FR-023).
	CategoryID uuid.UUID
	// Position is the operator's place in the catalogue order. Any whole number,
	// including zero and negative; ties are broken by created_at then id (FR-009).
	Position int
	// OptionName is the single choice-dimension name, for example "Card". It is
	// what labels the choice the customer picks (FR-009).
	OptionName string
	// CreatedAt is when the group was created. Set once.
	CreatedAt time.Time
	// UpdatedAt is when the group was last written.
	UpdatedAt time.Time
}

// VariantGroupDraft carries the caller-supplied members of a new variant group.
type VariantGroupDraft struct {
	// Name is the group name. It must not be blank and must fit MaxNameRunes.
	Name string
	// Slug is the public link segment. It must match SlugPattern and fit
	// MaxSlugRunes characters.
	Slug string
	// Description is the text shown to customers. It may be empty and must fit
	// MaxDescriptionRunes characters.
	Description string
	// CategoryID is the category the group belongs to.
	CategoryID uuid.UUID
	// Position is the ordering preference.
	Position int
	// OptionName is the single choice-dimension name. It must not be blank.
	OptionName string
}

// VariantGroupEdit carries the changes of a partial edit.
//
// A nil member keeps its current value. The pictures are edited through their own
// operations, so they are deliberately absent here.
type VariantGroupEdit struct {
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

// VariantGroupImage is one picture of a variant group's own gallery (FR-018). It
// mirrors Picture: the service keeps the media provider's reference, never the
// bytes (research D2).
type VariantGroupImage struct {
	// ID identifies the picture; the administrator surface addresses it by this.
	ID uuid.UUID
	// GroupID is the group it belongs to.
	GroupID uuid.UUID
	// PublicID is the provider's opaque identifier, used to release the asset.
	PublicID string
	// URL is the displayable link returned to clients. It must be HTTPS.
	URL string
	// Width and Height are the stored pixel dimensions the provider reported.
	Width  int
	Height int
	// Position is the display order. Insertion order; the operator does not edit
	// it.
	Position int
	// IsPrimary reports whether this is the group's main picture.
	IsPrimary bool
	// CreatedAt is when the picture was added. Set once.
	CreatedAt time.Time
}

// NewVariantGroup builds a variant group.
//
// It trims the free-text members, validates the name, the slug and the description
// with the same rules an ordinary product uses (120/140/5000 characters, the
// URL-safe slug shape) and requires a non-blank choice-dimension name. A rejected
// draft yields no group at all, so a caller cannot persist half of one (FR-009,
// FR-019, FR-020).
func NewVariantGroup(draft VariantGroupDraft, now time.Time) (*VariantGroup, error) {
	name := strings.TrimSpace(draft.Name)
	slug := strings.TrimSpace(draft.Slug)
	description := strings.TrimSpace(draft.Description)
	optionName := strings.TrimSpace(draft.OptionName)

	if err := ValidateName(name); err != nil {
		return nil, err
	}
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	if err := ValidateDescription(description); err != nil {
		return nil, err
	}
	if err := validateOptionName(optionName); err != nil {
		return nil, err
	}

	return &VariantGroup{
		ID:          uuid.New(),
		Name:        name,
		Slug:        slug,
		Description: description,
		CategoryID:  draft.CategoryID,
		Position:    draft.Position,
		OptionName:  optionName,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// Apply writes a partial edit.
//
// It stages the whole change, validates it, and only then writes, so a rejection
// leaves the group exactly as it was (FR-011, FR-019, FR-020). The identifier and
// the creation timestamp are never touched.
func (g *VariantGroup) Apply(edit VariantGroupEdit, now time.Time) error {
	name := g.Name
	slug := g.Slug
	description := g.Description
	categoryID := g.CategoryID
	position := g.Position
	optionName := g.OptionName

	if edit.Name != nil {
		name = strings.TrimSpace(*edit.Name)
	}
	if edit.Slug != nil {
		slug = strings.TrimSpace(*edit.Slug)
	}
	if edit.Description != nil {
		description = strings.TrimSpace(*edit.Description)
	}
	if edit.CategoryID != nil {
		categoryID = *edit.CategoryID
	}
	if edit.Position != nil {
		position = *edit.Position
	}
	if edit.OptionName != nil {
		optionName = strings.TrimSpace(*edit.OptionName)
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
	if err := validateOptionName(optionName); err != nil {
		return err
	}

	g.Name = name
	g.Slug = slug
	g.Description = description
	g.CategoryID = categoryID
	g.Position = position
	g.OptionName = optionName
	g.UpdatedAt = now
	return nil
}

// validateOptionName refuses a blank choice-dimension name, naming the member so
// the client can fix the exact input (FR-009).
func validateOptionName(name string) error {
	if name == "" {
		return domainerr.InvalidProductField(FieldOptionName, requiredIssue)
	}
	return nil
}
