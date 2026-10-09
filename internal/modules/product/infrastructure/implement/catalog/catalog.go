// Package catalog adapts the product module's own repository to the
// contracts.ProductCatalog port. The cart depends on that port; this adapter is
// supplied at the composition root, so the cart never imports this module's
// internals (Constitution I, research D1).
package catalog

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// Adapter answers the cart's product-facts read over the product module's own
// repository, so the facts come from module 04's data through module 04's own
// read rather than from a second copy. It never opens a transaction; the read
// joins the transaction the context already carries (Constitution I).
type Adapter struct {
	// Repo is the product module's repository port. Only its bulk read is used
	// here; the adapter loads no pictures and no set membership.
	Repo domainrepo.ProductRepository
}

// New creates the catalog adapter over the product repository.
func New(products domainrepo.ProductRepository) *Adapter {
	return &Adapter{Repo: products}
}

// Products returns the facts of every requested product that exists, one entry
// per existing product. A product no identifier carries is absent from the
// result, exactly as the contract declares, so the cart can report a removed
// product as no longer available rather than as an error (research D1).
//
// OnSale is the domain's own buyable decision rather than a re-reading of the
// sell state here, so the meaning of "may be sold" keeps one owner (research D1).
func (a *Adapter) Products(ctx context.Context, productIDs []uuid.UUID) ([]contracts.ProductSummary, error) {
	products, err := a.Repo.FindByIDs(ctx, productIDs)
	if err != nil {
		return nil, err
	}
	out := make([]contracts.ProductSummary, 0, len(products))
	for _, product := range products {
		out = append(out, contracts.ProductSummary{
			ID:     product.ID,
			Name:   product.Name,
			Slug:   product.Slug,
			OnSale: product.Buyable(),
			Price: contracts.ProductPrice{
				Amount:   product.Price.Amount,
				Currency: product.Price.Currency,
			},
		})
	}
	return out, nil
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would make
// that dependency-free package import a module, which is exactly backwards
// (internal/contracts/doc.go, module 03's visibility adapter).
var _ contracts.ProductCatalog = (*Adapter)(nil)
