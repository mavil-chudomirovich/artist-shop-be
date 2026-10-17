// Package statistics adapts the inventory module's own low-stock read to the
// contracts.InventoryStatistics port. The admin module depends on that port; this
// adapter is supplied at the composition root, so the dashboard reads inventory's
// tables only through inventory (Constitution I, research D9).
package statistics

import (
	"context"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
)

// Repository is the slice of the inventory repository this adapter needs.
// Declaring it here keeps the adapter honest about its dependency and lets a test
// supply a fake (research D9).
type Repository interface {
	// LowStock answers the variants whose available quantity is below the
	// threshold at the given instant.
	LowStock(ctx context.Context, threshold int64, now time.Time) ([]model.LowStockVariant, error)
}

// Clock reads the current time. It is the inventory module's single clock seam,
// so a test can decide which holds are still live (research D9, D15).
type Clock interface {
	// Now returns the current instant.
	Now() time.Time
}

// Adapter answers the dashboard's low-stock read over the inventory module's own
// repository, using the module's injected clock rather than the wall clock.
type Adapter struct {
	// Inventory is the inventory module's repository port.
	Inventory Repository
	// Clock is the inventory module's clock; a nil clock falls back to the wall
	// clock, exactly as the use cases do.
	Clock Clock
}

// New creates the inventory statistics adapter.
func New(inventory Repository, clock Clock) *Adapter {
	return &Adapter{Inventory: inventory, Clock: clock}
}

// now reads the injected clock, falling back to the wall clock when the
// composition left it unset.
func (a *Adapter) now() time.Time {
	if a.Clock == nil {
		return time.Now().UTC()
	}
	return a.Clock.Now()
}

// LowStockVariants answers every variant with a stock record whose available
// quantity is below the threshold, using the injected instant to decide which
// holds are still live (FR-017, FR-018).
func (a *Adapter) LowStockVariants(ctx context.Context, threshold int64) ([]contracts.LowStockVariant, error) {
	variants, err := a.Inventory.LowStock(ctx, threshold, a.now())
	if err != nil {
		return nil, err
	}
	out := make([]contracts.LowStockVariant, 0, len(variants))
	for _, variant := range variants {
		out = append(out, contracts.LowStockVariant{
			VariantID: variant.VariantID,
			Available: variant.Available,
		})
	}
	return out, nil
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would make
// that dependency-free package import a module (Constitution I).
var _ contracts.InventoryStatistics = (*Adapter)(nil)
