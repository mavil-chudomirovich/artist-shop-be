// Package visibility adapts the category module's repository to the
// contracts.CategoryQuery port. Another module depends on that port; this
// adapter is supplied at the composition root, so the consumer never imports
// this module's internals (Constitution I, research D1).
package visibility

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/category/domain/repository"
)

// Adapter answers the cross-module category questions over the category module's
// own repository, so the answers come from module 03's data through module 03's
// own reads rather than from a second copy or a table join.
type Adapter struct {
	// Categories is the category module's repository port. The adapter never
	// opens a transaction: it performs single reads (Constitution I).
	Categories domainrepo.CategoryRepository
}

// New creates the visibility adapter over the category repository.
func New(categories domainrepo.CategoryRepository) *Adapter {
	return &Adapter{Categories: categories}
}

// VisibleCategoryIDs returns the identifiers of every category on display. It
// is the set question module 04 cannot answer itself (research D1).
func (a *Adapter) VisibleCategoryIDs(ctx context.Context) ([]uuid.UUID, error) {
	return a.Categories.VisibleIDs(ctx)
}

// CategoryIDBySlug resolves the category a customer-facing slug names, reporting
// whether a row carries it. It is the second question module 04 asks: the public
// list's optional `category=<slug>` filter cannot be built from a set of
// identifiers alone (research D1).
//
// A hidden slug still resolves; the consumer's visibility predicate excludes it
// afterwards, which is what makes a hidden slug and an unknown slug answer the
// same empty list (FR-006).
func (a *Adapter) CategoryIDBySlug(ctx context.Context, slug string) (uuid.UUID, bool, error) {
	return a.Categories.IDBySlug(ctx, slug)
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would
// make that dependency-free package import a module, which is exactly backwards
// (internal/contracts/user.go declares the interface and module 02 satisfies
// it, never the other way round).
var _ contracts.CategoryQuery = (*Adapter)(nil)
