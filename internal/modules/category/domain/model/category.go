// Package model holds the category module's entities and value objects.
//
// The domain validates structure and the transitions only. Whether a name or a
// slug is already used is a catalogue-wide invariant that a single entity cannot
// decide, so the storage layer enforces it (FR-021) and the application reports
// it; this package owns the rule a human reads and the shape and bounds of the
// values (Constitution I).
package model

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Contract member names of a category, used when a rejection has to say which
// input is wrong. They are the exact member names of
// specs/005-category-catalog/contracts/openapi.yaml, so presentation can put one
// straight into the response detail without a second vocabulary that could drift
// away from the contract (FR-020).
const (
	// FieldName is the request member carrying a category name.
	FieldName = "name"
	// FieldSlug is the request member carrying a category slug.
	FieldSlug = "slug"
	// FieldDescription is the request member carrying a category description.
	FieldDescription = "description"
)

// Category is a flat catalogue category: a way of grouping what the shop sells.
//
// The display state is a single boolean with no settable public path: Hide and
// Show are the transitions that move it, so a caller cannot reach an
// inconsistent state by assigning a field (research D9, Constitution III). There
// is no third state; removal is a hard delete, not a status (research D5).
//
// NormalizedName and NormalizedSlug are the folding keys the storage layer
// indexes for catalogue-wide uniqueness. They are recomputed by every transition
// from the value being stored, so they can never disagree with Name and Slug.
// They exist only to be stored, indexed and compared: they are never returned to
// anyone, never audited and never named in the API contract.
type Category struct {
	// ID identifies the category. It is generated once and is stable for the
	// category's lifetime: it is the reference a product will use and the
	// administrator surface addresses a category by it (FR-015, FR-023).
	ID uuid.UUID
	// Name is the name the operator typed, trimmed. This is what a customer
	// reads.
	Name string
	// NormalizedName is the folding key for Name (FR-016).
	NormalizedName string
	// Slug is the link segment the operator wrote, lowercase and URL-safe by
	// validation (FR-018).
	Slug string
	// NormalizedSlug is the folding key for Slug (FR-017).
	NormalizedSlug string
	// Description is what the operator wrote. An empty string is valid.
	Description string
	// Position is the operator's place in the order. Any whole number,
	// including zero and negative; it is a preference, not a sequence.
	Position int
	// IsVisible reports whether a customer can see the category. It is the only
	// state beyond existing and moves only through Hide and Show.
	IsVisible bool
	// CreatedAt is when the category was created. Set once.
	CreatedAt time.Time
	// UpdatedAt is when the category was last written.
	UpdatedAt time.Time
}

// CategoryDraft carries the caller-supplied members of a new category.
type CategoryDraft struct {
	// Name is the category name. It must not be blank and must fit
	// MaxNameRunes characters.
	Name string
	// Slug is the public link segment. It must match SlugPattern and fit
	// MaxSlugRunes characters.
	Slug string
	// Description is the text shown to customers. It may be empty and must fit
	// MaxDescriptionRunes characters.
	Description string
	// Position is the ordering preference.
	Position int
}

// CategoryEdit carries the changes of a partial edit.
//
// A nil member keeps its current value. The display state is deliberately absent
// so an edit can never move it; Hide and Show own that transition.
type CategoryEdit struct {
	// Name is the new name; nil keeps the current value.
	Name *string
	// Slug is the new slug; nil keeps the current value.
	Slug *string
	// Description is the new description; nil keeps the current value.
	Description *string
	// Position is the new position; nil keeps the current value.
	Position *int
}

// NewCategory builds a category that is on display.
//
// It trims the three free-text members, validates them, and derives the folding
// keys from the values it is about to store. A rejected draft yields no category
// at all, so a caller cannot persist half of one.
//
// A newly created category is always on display (FR-008). There is no argument
// to create one hidden: the operator shows an incomplete category by creating it
// and hiding it, which is the same transition every later change uses.
func NewCategory(draft CategoryDraft, now time.Time) (*Category, error) {
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

	return &Category{
		ID:             uuid.New(),
		Name:           name,
		NormalizedName: FoldKey(name),
		Slug:           slug,
		NormalizedSlug: FoldKey(slug),
		Description:    description,
		Position:       draft.Position,
		IsVisible:      true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// Apply writes a partial edit.
//
// It stages the whole change, validates it, and only then writes, so a rejection
// leaves the category exactly as it was — including the display state, which is
// never touched here (FR-022, FR-010). The folding keys are recomputed from the
// values the entity ends up storing, so they always agree with Name and Slug.
func (c *Category) Apply(edit CategoryEdit, now time.Time) error {
	name := c.Name
	slug := c.Slug
	description := c.Description
	position := c.Position

	if edit.Name != nil {
		name = strings.TrimSpace(*edit.Name)
	}
	if edit.Slug != nil {
		slug = strings.TrimSpace(*edit.Slug)
	}
	if edit.Description != nil {
		description = strings.TrimSpace(*edit.Description)
	}
	if edit.Position != nil {
		position = *edit.Position
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

	c.Name = name
	c.NormalizedName = FoldKey(name)
	c.Slug = slug
	c.NormalizedSlug = FoldKey(slug)
	c.Description = description
	c.Position = position
	c.UpdatedAt = now
	return nil
}

// Hide takes the category off display. It reports whether anything changed.
//
// Hiding an already hidden category is a no-op, not an error (data-model.md):
// the outcome the caller asked for already holds, and updated_at must not move
// for a write that does not happen. Nothing is destroyed — the row and every
// other member stay exactly as they were (FR-011).
func (c *Category) Hide(now time.Time) bool {
	if !c.IsVisible {
		return false
	}
	c.IsVisible = false
	c.UpdatedAt = now
	return true
}

// Show puts the category back on display. It reports whether anything changed.
//
// Showing an already visible category is a no-op, not an error, for the same
// reason Hide's repeat is (FR-011).
func (c *Category) Show(now time.Time) bool {
	if c.IsVisible {
		return false
	}
	c.IsVisible = true
	c.UpdatedAt = now
	return true
}
