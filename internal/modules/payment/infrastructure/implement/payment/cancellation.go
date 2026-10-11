// Package payment adapts the payment module's (08) use cases to the cross-module
// contracts.PaymentCancellation interface, so module 07 (order) can tell the
// payment module that an order was cancelled and have the provider payment request
// it created cancelled too, without the order ever touching the provider or the
// payment's tables (Constitution I, research D9).
//
// It is the only code that crosses the order→payment cancellation boundary: the
// order module depends on the interface in internal/contracts, and this adapter —
// supplied at the composition root — is what satisfies it.
package payment

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
)

// canceller is the slice of the payment use-case surface this adapter needs. It is
// declared here, rather than reusing the module's full use-case interface, so the
// adapter depends only on the one method it calls and the module's use cases are
// free to grow without this adapter being forced to name them.
type canceller interface {
	// CancelForOrder cancels the live payment of a cancelled order, best-effort.
	CancelForOrder(ctx context.Context, orderID uuid.UUID)
}

// Cancellation implements contracts.PaymentCancellation over the payment module's
// own use-case surface.
type Cancellation struct {
	service canceller
}

var _ contracts.PaymentCancellation = (*Cancellation)(nil)

// New creates the adapter over the payment use-case surface.
func New(service canceller) *Cancellation {
	return &Cancellation{service: service}
}

// CancelForOrder cancels the live payment of a cancelled order, best-effort. It
// reports nothing: the use case logs a provider failure and leaves the payment
// live for reconciliation, and module 07's already-committed cancellation is
// never affected (FR-015, FR-016, research D9).
func (c *Cancellation) CancelForOrder(ctx context.Context, orderID uuid.UUID) {
	c.service.CancelForOrder(ctx, orderID)
}
