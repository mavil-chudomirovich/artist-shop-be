// Package dto defines the category module's use-case input/output types.
//
// They are separate from the HTTP shapes: these carry what a use case computes,
// while presentation owns what a response looks like. The public and
// administrator outputs are deliberately two types, so a field that only the
// administrator may see cannot leak into a customer response by accident
// (FR-007).
package dto

import (
	"time"

	"github.com/google/uuid"
)

// PublicCategoryOutput is what a customer reads from the catalogue: exactly the
// four members FR-004 names and nothing else. The display state, the position
// and the folding keys are absent by design (FR-007).
type PublicCategoryOutput struct {
	// ID is the category's stable public identifier.
	ID uuid.UUID
	// Name is the name the operator wrote.
	Name string
	// Slug is the segment a customer-facing link is built from.
	Slug string
	// Description may be empty; an empty description is valid.
	Description string
}

// PublicCategoryPage is one page of the public catalogue.
type PublicCategoryPage struct {
	// Categories holds the page's categories in the configured order.
	Categories []PublicCategoryOutput
	// Page and PageSize echo the requested window.
	Page     int
	PageSize int
	// Total is the number of categories on display.
	Total int64
}

// ListPublicInput pages through the public catalogue. It carries no filter
// because a visitor only ever sees what is on display.
type ListPublicInput struct {
	// Page is 1-based.
	Page int
	// PageSize is the number of categories per page.
	PageSize int
}

// PublicCategoryRefInput addresses one category of the public surface by the
// slug a customer-facing link is built from.
type PublicCategoryRefInput struct {
	// Slug is the public link segment.
	Slug string
}

// AdminCategoryOutput is what an administrator reads, including the members a
// customer must never receive: the display state, the position and the
// timestamps (FR-009).
type AdminCategoryOutput struct {
	// ID is the category's stable identifier.
	ID uuid.UUID
	// Name is the name the operator wrote.
	Name string
	// Slug is the link segment the operator wrote.
	Slug string
	// Description is the text shown to customers.
	Description string
	// Position is the operator's ordering preference.
	Position int
	// IsVisible reports whether a customer can see the category.
	IsVisible bool
	// CreatedAt is when the category was created.
	CreatedAt time.Time
	// UpdatedAt is when the category was last written.
	UpdatedAt time.Time
}

// AdminCategoryPage is one page of the administrator list.
type AdminCategoryPage struct {
	// Categories holds the page's categories, including the ones not on
	// display, in the configured order.
	Categories []AdminCategoryOutput
	// Page and PageSize echo the requested window.
	Page     int
	PageSize int
	// Total is the number of categories in the catalogue.
	Total int64
}

// ListAdminInput pages through the administrator list.
type ListAdminInput struct {
	// Page is 1-based.
	Page int
	// PageSize is the number of categories per page.
	PageSize int
}

// AdminCategoryRefInput addresses one category of the administrator surface by
// its identifier, which cannot change under a client that is mid-edit.
type AdminCategoryRefInput struct {
	// ID is the category's identifier.
	ID uuid.UUID
}

// CreateCategoryInput creates a category. The acting administrator is taken from
// the session, never from this input (FR-014).
type CreateCategoryInput struct {
	// Name is the category name.
	Name string
	// Slug is the public link segment.
	Slug string
	// Description is the text shown to customers; empty is valid.
	Description string
	// Position is the ordering preference.
	Position int
}

// UpdateCategoryInput edits a category. Every member keeps its current value
// when the corresponding pointer is nil; the display state moves only when
// IsVisible is present (FR-010, FR-011).
type UpdateCategoryInput struct {
	// ID is the category to edit.
	ID uuid.UUID
	// Name is the new name; nil keeps the current value.
	Name *string
	// Slug is the new slug; nil keeps the current value.
	Slug *string
	// Description is the new description; nil keeps the current value.
	Description *string
	// Position is the new position; nil keeps the current value.
	Position *int
	// IsVisible is the requested display state; nil keeps the current value. It
	// is routed to Hide or Show, never assigned directly.
	IsVisible *bool
}
