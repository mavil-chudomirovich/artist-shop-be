// Package checkout adapts the cart module's own repository to the
// contracts.CartCheckout port. The order module depends on that port; this
// adapter is supplied at the composition root, so the order never imports this
// module's internals (Constitution I, research D1).
package checkout

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/cart/domain/repository"
)

// Adapter answers the order's cart read and clear over the cart module's own
// repository, so the lines come from module 06's data through module 06's own
// read. It never opens a transaction: every call joins the transaction the
// context already carries, so checkout and the emptied cart commit together or
// not at all (Constitution I, II, research D1).
type Adapter struct {
	// Carts is the cart module's repository port.
	Carts domainrepo.CartRepository
}

// New creates the checkout adapter over the cart repository.
func New(carts domainrepo.CartRepository) *Adapter {
	return &Adapter{Carts: carts}
}

// CartLines returns the caller's cart lines as the order module needs them: one
// entry per product, in the cart's stable order. It locks the cart row before
// reading its lines, so two concurrent checkouts of one cart serialise: the
// second blocks until the first commits and then finds the lines already gone. A
// customer with no cart answers an empty slice rather than an error, because a
// checkout of nothing is the order module's to refuse, not the cart's to hide
// (FR-006, research D1).
func (a *Adapter) CartLines(ctx context.Context, userID uuid.UUID) ([]contracts.CartLine, error) {
	cart, found, err := a.Carts.FindByOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !found {
		return []contracts.CartLine{}, nil
	}
	if err := a.Carts.Lock(ctx, cart.ID); err != nil {
		return nil, err
	}
	lines, err := a.Carts.Lines(ctx, cart.ID)
	if err != nil {
		return nil, err
	}
	out := make([]contracts.CartLine, 0, len(lines))
	for _, line := range lines {
		out = append(out, contracts.CartLine{
			ProductID:       line.ProductID,
			Quantity:        line.Quantity,
			UnitPriceAmount: line.UnitPrice.Amount,
			Currency:        line.UnitPrice.Currency,
		})
	}
	return out, nil
}

// ClearCart empties the caller's cart inside the caller's transaction. A customer
// with no cart is a no-op: there is nothing to empty and a retried clear changes
// nothing, so the order's checkout is idempotent against it (FR-007, research D1).
func (a *Adapter) ClearCart(ctx context.Context, userID uuid.UUID) error {
	cart, found, err := a.Carts.FindByOwner(ctx, userID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return a.Carts.ClearLines(ctx, cart.ID)
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would make
// that dependency-free package import a module, which is exactly backwards
// (internal/contracts/doc.go, module 05's availability adapter).
var _ contracts.CartCheckout = (*Adapter)(nil)
