// Package availability adapts the inventory module's own repository to the
// contracts.InventoryAvailability port. The cart depends on that port; this
// adapter is supplied at the composition root, so the cart never imports this
// module's internals (Constitution I, research D2).
package availability

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/contracts"
	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/interface"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/repository"
)

// Adapter answers the cart's availability read over the inventory module's own
// repository, so the answer comes from module 05's data through module 05's own
// read. It never opens a transaction; the read joins the transaction the context
// already carries when there is one (Constitution I).
type Adapter struct {
	// Inventory is the inventory module's repository port.
	Inventory domainrepo.InventoryRepository
	// Clock is the module's injected clock. Availability depends on which holds
	// have expired, and this adapter reads the instant from the same clock the
	// sweeper uses, so the two cannot disagree about expiry (research D2, D15).
	Clock appinterface.Clock
}

// New creates the availability adapter.
func New(inventory domainrepo.InventoryRepository, clock appinterface.Clock) *Adapter {
	return &Adapter{Inventory: inventory, Clock: clock}
}

// now reads the injected clock, falling back to the wall clock when the
// composition left it unset. Only "what time is it" is injected; the expiry rule
// stays in the module (research D15).
func (a *Adapter) now() time.Time {
	if a.Clock == nil {
		return time.Now().UTC()
	}
	return a.Clock.Now()
}

// AvailableQuantity returns one entry per requested identifier, in the same
// order, with zero for a product that has no stock row. Availability is physical
// stock minus the quantity active holds have set aside; the subtraction is the
// domain's own rule, so the number the cart is shown is the same rule the
// inventory use cases enforce (research D2).
func (a *Adapter) AvailableQuantity(ctx context.Context, productIDs []uuid.UUID) ([]contracts.Availability, error) {
	readings, err := a.Inventory.Availability(ctx, productIDs, a.now())
	if err != nil {
		return nil, err
	}
	byProduct := make(map[uuid.UUID]domainrepo.AvailabilityReading, len(readings))
	for _, reading := range readings {
		byProduct[reading.ProductID] = reading
	}

	out := make([]contracts.Availability, 0, len(productIDs))
	for _, productID := range productIDs {
		reading := byProduct[productID]
		available, err := model.Availability(reading.Level, reading.Held)
		if err != nil {
			return nil, err
		}
		out = append(out, contracts.Availability{ProductID: productID, Available: available})
	}
	return out, nil
}

// The adapter must satisfy the cross-module contract. The assertion lives in the
// adapter's own package deliberately: placing it in internal/contracts would make
// that dependency-free package import a module, which is exactly backwards
// (internal/contracts/doc.go, the category visibility adapter).
var _ contracts.InventoryAvailability = (*Adapter)(nil)
