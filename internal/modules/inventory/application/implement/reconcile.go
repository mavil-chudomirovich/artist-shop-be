package implement

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
)

// This file is US4: availability follows the stock by itself. Every writing use
// case measures a product's availability before and after its change and asks the
// product module to move the product's sell state only when availability crossed
// zero. The rule is stated once here — research D10's four-row table — and it runs
// inside the caller's transaction, so a product is never committed buyable with
// nothing available (FR-024 to FR-028, FR-026, research D3, D10).
//
// The signal is a signal, not a state write: inventory never learns product's
// state names and never reads product's table. The product module maps each signal
// onto its own transition and tolerates the states it must not move (announced,
// retired, already the target), so a correct stock operation is never failed by a
// no-op (research D4).

// reconcileAvailability runs change inside the caller's transaction, measuring
// availability immediately before and after it, and signals the product's sell
// state only when availability crossed zero (FR-024, FR-025, FR-028).
//
// The before value is read under the level row lock the caller already holds on
// the decreasing paths, so the crossing is decided against the same quantities the
// change itself saw. A failure surfaced by change or by the signal rolls the whole
// transaction back: the availability consequence travels with the change and can
// never disagree with it (FR-011, FR-026).
func (s *Service) reconcileAvailability(ctx context.Context, productID uuid.UUID, now time.Time, change func() error) error {
	before, err := s.availabilityAt(ctx, productID, now)
	if err != nil {
		return err
	}
	if err := change(); err != nil {
		return err
	}
	after, err := s.availabilityAt(ctx, productID, now)
	if err != nil {
		return err
	}
	return s.signalCrossing(ctx, productID, before, after)
}

// reconcileExpiry runs a sweep's release of expired holds and signals the product
// when that release crossed zero (FR-025, research D6).
//
// A hold that has passed its expiry is already excluded from the active-hold sum,
// so availability does not move when the sweep merely resolves the row: the
// crossing happened at the expiry instant. The pre-expiry availability is therefore
// reconstructed from what the sweep actually released — before = after − released —
// which is why change reports the quantity it resolved rather than a fixed figure.
// A hold a concurrent consume or release already resolved is not counted, so the
// reconstruction stays honest under a race.
func (s *Service) reconcileExpiry(ctx context.Context, productID uuid.UUID, now time.Time, change func() (int64, error)) error {
	released, err := change()
	if err != nil {
		return err
	}
	after, err := s.availabilityAt(ctx, productID, now)
	if err != nil {
		return err
	}
	before := after - released
	if before < 0 {
		// Availability is never negative, so a pre-expiry figure that the
		// arithmetic places below zero is zero: the shelf was already fully
		// promised (FR-009, FR-018).
		before = 0
	}
	return s.signalCrossing(ctx, productID, before, after)
}

// availabilityAt derives what a customer can take at the given instant: the
// physical count minus the quantity active holds have set aside. It is never
// stored, so it cannot drift from the rows it comes from (FR-013, FR-018, research
// D1). The instant is the injected clock's, so the sum and the sweeper agree on
// what has expired (research D15).
func (s *Service) availabilityAt(ctx context.Context, productID uuid.UUID, now time.Time) (int64, error) {
	physical, err := s.Inventory.Level(ctx, productID)
	if err != nil {
		return 0, err
	}
	held, err := s.Inventory.ActiveHeld(ctx, productID, now)
	if err != nil {
		return 0, err
	}
	return model.Availability(physical, held)
}

// signalCrossing is research D10's four-row table, expressed once. Only a genuine
// zero crossing moves the product: a change that stays positive, or stays at zero,
// leaves the sell state exactly as it was, so an operator's deliberate
// out-of-stock marking survives a non-crossing change (FR-028).
//
// A nil port is tolerated like a nil auditor: a use case constructed without the
// product adapter (a unit test, a read-only build) still runs, it just cannot
// signal. Production always supplies the adapter at the composition root (T046).
func (s *Service) signalCrossing(ctx context.Context, productID uuid.UUID, before, after int64) error {
	if s.Availability == nil {
		return nil
	}
	switch {
	case before > 0 && after == 0:
		return s.Availability.MarkOutOfStock(ctx, productID)
	case before == 0 && after > 0:
		return s.Availability.MarkOnSale(ctx, productID)
	default:
		return nil
	}
}
