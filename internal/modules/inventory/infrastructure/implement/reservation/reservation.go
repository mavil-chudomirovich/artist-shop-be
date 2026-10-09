// Package reservation adapts the inventory module's delivered reservation use
// cases to the contracts.InventoryReservation port. The order module depends on
// that port; this adapter is supplied at the composition root, so the order never
// imports this module's internals (Constitution I, research D4). It is the
// adapter feature 007's deferred D1 required before the reservation contract could
// be published.
package reservation

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
)

// Service is the slice of module 05's use-case surface this adapter needs.
// Declaring it here rather than importing the module's whole service keeps the
// adapter honest about what it depends on and lets a test supply a fake that
// records the calls (research D4).
type Service interface {
	// Reserve holds a quantity of a product for one order while it is being paid.
	Reserve(ctx context.Context, in dto.ReserveInput) error
	// Release returns an order's active hold on a product to availability.
	Release(ctx context.Context, in dto.ReleaseInput) error
	// ApplySale consumes an order's hold on a product, at most once.
	ApplySale(ctx context.Context, in dto.SaleInput) error
}

// Adapter answers the order's hold lifecycle over module 05's reservation use
// cases, so holding, consuming and releasing are module 05's own tested
// capability and never re-implemented in the order module (research D4). It opens
// no transaction: each call joins the transaction the context already carries, so
// a hold and the order that caused it commit together or not at all
// (Constitution I, II).
type Adapter struct {
	// Inventory is the inventory module's reservation use-case surface.
	Inventory Service
}

// New creates the reservation adapter.
func New(inventory Service) *Adapter {
	return &Adapter{Inventory: inventory}
}

// Reserve holds the ordered quantity of a product for one order. A reserve of the
// same order and product is a no-op in module 05, so a retried checkout never
// holds twice (FR-013, FR-017, research D4).
func (a *Adapter) Reserve(ctx context.Context, orderID, productID uuid.UUID, quantity int64) error {
	return a.Inventory.Reserve(ctx, dto.ReserveInput{
		OrderID:   orderID,
		ProductID: productID,
		Quantity:  quantity,
	})
}

// Release returns an order's active hold on a product to availability. A hold a
// concurrent operation already resolved is a no-op, so a release is applied at
// most once (FR-015, research D4).
func (a *Adapter) Release(ctx context.Context, orderID, productID uuid.UUID) error {
	return a.Inventory.Release(ctx, dto.ReleaseInput{
		OrderID:   orderID,
		ProductID: productID,
	})
}

// ApplySale turns an order's hold on a product into a sale, keyed by the payment
// event's per-line source reference so a replay changes stock at most once
// (FR-014, research D5).
func (a *Adapter) ApplySale(ctx context.Context, orderID, productID uuid.UUID, sourceReference string) error {
	return a.Inventory.ApplySale(ctx, dto.SaleInput{
		OrderID:         orderID,
		ProductID:       productID,
		SourceReference: sourceReference,
	})
}

// HoldWindow returns module 05's hold window, so the order derives its own
// expires_at from the one place that decides when a hold is over (FR-016,
// research D4, D6).
func (a *Adapter) HoldWindow() time.Duration {
	return constant.HoldTTL
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would make
// that dependency-free package import a module, which is exactly backwards
// (internal/contracts/doc.go, module 05's availability adapter).
var _ contracts.InventoryReservation = (*Adapter)(nil)
