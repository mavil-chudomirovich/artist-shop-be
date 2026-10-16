// Package hideguard adapts the cart module's own repository to the
// contracts.CartHideCleanup port. A hiding use case depends on that port; this
// adapter is supplied at the composition root, so the consumer never imports
// this module's internals (Constitution I, specs/016-hide-guard research D4).
package hideguard

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/application/interface"
)

// Adapter removes hidden variants from every cart over the cart module's own
// repository, so the cleanup touches module 06's data through module 06's own
// write. It never opens a transaction: the delete joins the transaction the
// hiding use case already carries, so the hide and the cleanup commit together
// or not at all (Constitution I, II).
type Adapter struct {
	// Carts is the cart module's repository port. Only its bulk line-removal is
	// used here.
	Carts appinterface.CartRepository
}

// New creates the hide-guard adapter over the cart repository.
func New(carts appinterface.CartRepository) *Adapter {
	return &Adapter{Carts: carts}
}

// RemoveVariantsFromAllCarts deletes every cart line that holds one of the given
// variants and returns how many lines were removed, so the hide can record the
// count in its event metadata. An empty input removes nothing and answers zero,
// never an error.
func (a *Adapter) RemoveVariantsFromAllCarts(ctx context.Context, variantIDs []uuid.UUID) (int64, error) {
	return a.Carts.DeleteLinesByVariants(ctx, variantIDs)
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would
// make that dependency-free package import a module, which is exactly backwards.
var _ contracts.CartHideCleanup = (*Adapter)(nil)
