// Package catalog adapts the product module's own repository to the
// contracts.ProductVariants port. The cart and the order depend on that port;
// this adapter is supplied at the composition root, so they never import this
// module's internals (Constitution I, research D6).
package catalog

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/repository"
)

// Adapter answers the cart's and the order's variant-facts read over the product
// module's own repository, so the facts come from module 04's data through module
// 04's own read rather than from a second copy. It never opens a transaction; the
// read joins the transaction the context already carries (Constitution I).
type Adapter struct {
	// Repo is the product module's variant repository port. Only its bulk facts
	// read is used here; the adapter loads no image and no position.
	Repo domainrepo.VariantRepository
}

// New creates the catalog adapter over the variant repository.
func New(variants domainrepo.VariantRepository) *Adapter {
	return &Adapter{Repo: variants}
}

// Variants returns the facts of every requested variant that exists, one entry
// per existing variant. A variant no identifier carries is absent from the result,
// exactly as the contract declares, so the cart can report a removed variant as no
// longer available rather than as an error (research D6).
//
// OnSale is the repository projection's judgement that the variant is ACTIVE,
// rather than a re-reading of the sell state here, so the meaning of "may be sold"
// keeps one owner (research D6). Price is the variant's effective price, its own
// amount or the product's default, in the product's currency (FR-017).
func (a *Adapter) Variants(ctx context.Context, variantIDs []uuid.UUID) ([]contracts.VariantSummary, error) {
	facts, err := a.Repo.FindFactsByIDs(ctx, variantIDs)
	if err != nil {
		return nil, err
	}
	out := make([]contracts.VariantSummary, 0, len(facts))
	for _, fact := range facts {
		out = append(out, contracts.VariantSummary{
			VariantID:   fact.VariantID,
			ProductID:   fact.ProductID,
			ProductName: fact.ProductName,
			ProductCode: fact.ProductCode,
			VariantName: fact.VariantName,
			SKU:         fact.SKU,
			OnSale:      fact.OnSale,
			Price: contracts.ProductPrice{
				Amount:   fact.Price.Amount,
				Currency: fact.Price.Currency,
			},
		})
	}
	return out, nil
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would make
// that dependency-free package import a module, which is exactly backwards
// (internal/contracts/doc.go, module 03's visibility adapter).
var _ contracts.ProductVariants = (*Adapter)(nil)
