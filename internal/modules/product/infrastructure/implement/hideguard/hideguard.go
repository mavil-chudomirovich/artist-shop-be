// Package hideguard adapts the product module's own repository to the
// contracts.ProductCategoryVariants port. The category hide guard depends on
// that port; this adapter is supplied at the composition root, so the category
// never imports this module's internals (Constitution I, specs/016-hide-guard
// research D2).
package hideguard

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/interface"
)

// Adapter answers the category-to-variant resolution over the product module's
// own repository, so the variant set comes from module 04's data through module
// 04's own read. It never opens a transaction: the read joins the transaction
// the hiding use case already carries (Constitution I).
type Adapter struct {
	// Products is the product module's repository port. Only its bulk
	// variant-by-categories read is used here; no product internals cross the
	// boundary.
	Products appinterface.ProductRepository
}

// New creates the hide-guard adapter over the product repository.
func New(products appinterface.ProductRepository) *Adapter {
	return &Adapter{Products: products}
}

// VariantIDsByCategories returns the identifiers of every variant whose product
// belongs to one of the given categories, in a stable order. An empty input
// answers an empty slice, never an error.
func (a *Adapter) VariantIDsByCategories(ctx context.Context, categoryIDs []uuid.UUID) ([]uuid.UUID, error) {
	return a.Products.VariantIDsByCategories(ctx, categoryIDs)
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would
// make that dependency-free package import a module, which is exactly backwards.
var _ contracts.ProductCategoryVariants = (*Adapter)(nil)
