// Package variantstate adapts the product module's own repository to the
// contracts.ProductVariantStates port. The admin module depends on that port; this
// adapter is supplied at the composition root, so the dashboard can drop
// DISCONTINUED variants without reading module 04's table (Constitution I,
// research D9).
package variantstate

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
)

// Repository is the slice of the product module's variant repository this adapter
// needs. Declaring it here keeps the adapter honest about its dependency and lets
// a test supply a fake (research D9).
type Repository interface {
	// DiscontinuedIDs returns the subset of ids whose variant is DISCONTINUED.
	DiscontinuedIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
}

// Adapter answers the dashboard's discontinued-variant read over the product
// module's own repository. It never opens a transaction; the read joins the
// transaction the context already carries (Constitution I).
type Adapter struct {
	// Variants is the product module's variant repository port.
	Variants Repository
}

// New creates the variant-state adapter.
func New(variants Repository) *Adapter {
	return &Adapter{Variants: variants}
}

// DiscontinuedVariantIDs returns the subset of ids whose variant is DISCONTINUED.
// An empty input answers an empty slice without a query, exactly as the contract
// declares (FR-018).
func (a *Adapter) DiscontinuedVariantIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	return a.Variants.DiscontinuedIDs(ctx, ids)
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would make
// that dependency-free package import a module (Constitution I).
var _ contracts.ProductVariantStates = (*Adapter)(nil)
