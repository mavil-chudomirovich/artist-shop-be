// Package httpdto defines the category module's HTTP request and response
// shapes. The public shape is deliberately the four members a customer reads
// (FR-004, FR-007); the administrator shapes carry the display state and the
// position the public shape must not (FR-009).
package httpdto

import (
	"time"

	"github.com/google/uuid"
)

// PublicCategoryResponse is exactly what a customer reads from the catalogue.
//
// It carries the public identifier, the name, the slug and the description, and
// nothing else. The display state, the position and the folding keys are how the
// operator manages the catalogue and never part of a public answer (FR-004,
// FR-007).
type PublicCategoryResponse struct {
	// ID is the category's stable public identifier.
	ID uuid.UUID `json:"id"`
	// Name is the name the operator wrote.
	Name string `json:"name"`
	// Slug is the segment a customer-facing link is built from.
	Slug string `json:"slug"`
	// Description may be empty; an empty description is valid.
	Description string `json:"description"`
}

// AdminCategoryResponse is what an administrator reads from the catalogue,
// including the members a customer must never receive: the display state and the
// position, plus the timestamps the operator uses to see what changed and when
// (FR-009).
type AdminCategoryResponse struct {
	// ID is the category's stable identifier.
	ID uuid.UUID `json:"id"`
	// Name is the name the operator wrote.
	Name string `json:"name"`
	// Slug is the link segment the operator wrote.
	Slug string `json:"slug"`
	// Description is the text shown to customers; it may be empty.
	Description string `json:"description"`
	// Position is the operator's ordering preference.
	Position int `json:"position"`
	// IsVisible reports whether a customer can see the category.
	IsVisible bool `json:"isVisible"`
	// CreatedAt is when the category was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the category was last written.
	UpdatedAt time.Time `json:"updatedAt"`
}

// CreateCategoryRequest is the body of an administrator create.
//
// The schema is additionalProperties: false, and the handler decodes it with
// unknown members refused, so a client cannot smuggle a member the contract does
// not declare — including any attempt to name the acting account (FR-014).
type CreateCategoryRequest struct {
	// Name is required; it is trimmed before it is stored.
	Name string `json:"name"`
	// Slug is required; it must be URL-safe and is never derived from the name.
	Slug string `json:"slug"`
	// Description is optional; an omitted or empty description is stored as
	// empty.
	Description string `json:"description"`
	// Position is optional and defaults to zero. Any whole number, including
	// zero and negative, is valid.
	Position int `json:"position"`
}

// UpdateCategoryRequest is the body of a partial administrator edit.
//
// Every member is optional: a nil pointer keeps the category's current value.
// This is what makes "change only the description" expressible, and it keeps a
// deliberate zero position or a move to hidden distinguishable from an omitted
// member. Sending the values a category already holds succeeds — a category is
// never a duplicate of itself (FR-022).
type UpdateCategoryRequest struct {
	// Name is the new name; nil keeps the current value.
	Name *string `json:"name"`
	// Slug is the new slug; nil keeps the current value.
	Slug *string `json:"slug"`
	// Description is the new description; nil keeps the current value.
	Description *string `json:"description"`
	// Position is the new position; nil keeps the current value.
	Position *int `json:"position"`
	// IsVisible is the requested display state; nil keeps the current value. It
	// is routed to the hide or show transition, never assigned directly.
	IsVisible *bool `json:"isVisible"`
}
