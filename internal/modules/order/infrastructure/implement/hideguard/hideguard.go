// Package hideguard adapts the order module's own repository to the
// contracts.OrderHideGuard port. A hiding use case depends on that port; this
// adapter is supplied at the composition root, so the consumer never imports
// this module's internals (Constitution I, specs/016-hide-guard research D1).
package hideguard

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/application/interface"
)

// Adapter answers the pending-order lookup over the order module's own
// repository, so the blocking orders come from module 07's data through module
// 07's own read. It never opens a transaction: the read joins the transaction
// the hiding use case already carries, so the check and the hide commit
// together or not at all (Constitution I).
type Adapter struct {
	// Orders is the order module's repository port. Only its bulk pending-order
	// read is used here.
	Orders appinterface.OrderRepository
}

// New creates the hide-guard adapter over the order repository.
func New(orders appinterface.OrderRepository) *Adapter {
	return &Adapter{Orders: orders}
}

// PendingOrdersForVariants returns the identifiers of the PENDING or
// PAYMENT_PENDING orders that hold at least one of the given variants, in a
// stable order. Variants held only by orders in any other state are absent, and
// an empty input answers an empty slice, never an error.
func (a *Adapter) PendingOrdersForVariants(ctx context.Context, variantIDs []uuid.UUID) ([]uuid.UUID, error) {
	return a.Orders.FindPendingOrdersByVariants(ctx, variantIDs)
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would
// make that dependency-free package import a module, which is exactly backwards.
var _ contracts.OrderHideGuard = (*Adapter)(nil)
