// Package httpdto defines the category module's HTTP request and response
// shapes. The public shape is deliberately the four members a customer reads
// (FR-004, FR-007); the administrator shapes are added by US2.
package httpdto

import "github.com/google/uuid"

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
