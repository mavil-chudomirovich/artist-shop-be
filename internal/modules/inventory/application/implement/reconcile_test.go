package implement

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/application/mapper"
)

// This file is US4's use-case contract: availability drives the product's sell
// state. The whole rule is research D10's four-row table, and it is proven by
// counting how many times the availability signal was called: a change that does
// not cross zero must call the signal zero times, so counting is what makes "did
// not call" observable rather than assumed (FR-024 to FR-028, SC-005).
//
// The change is driven through the real Service over the in-memory repository, so
// the before and after values the reconciliation reads are the same two reads the
// writing use cases perform.

// fakeAvailability counts the two signals the inventory module can send. Counting
// is the assertion: a non-crossing change proves it did not call by leaving both
// counters at zero.
type fakeAvailability struct {
	outOfStock int
	onSale     int
}

// MarkOutOfStock records one out-of-stock signal.
func (f *fakeAvailability) MarkOutOfStock(context.Context, uuid.UUID) error {
	f.outOfStock++
	return nil
}

// MarkOnSale records one on-sale signal.
func (f *fakeAvailability) MarkOnSale(context.Context, uuid.UUID) error {
	f.onSale++
	return nil
}

// newReconcileService builds the real Service over the in-memory repository with
// the counting availability port, so the reconciliation's reads and its calls can
// both be observed.
func newReconcileService(repo *fakeRepository, availability *fakeAvailability) *Service {
	return New(Service{
		Inventory:    repo,
		Lookup:       &fakeLookup{exists: true},
		Tx:           passThroughUnitOfWork{},
		Clock:        fixedClock{},
		Availability: availability,
		Mapper:       mapper.New(),
	})
}

// FR-024 to FR-028, research D10: only a zero crossing signals the product. The
// four rows are asserted here, one per row, by call count.
func TestReconciliationSignalsOnlyOnAZeroCrossing(t *testing.T) {
	cases := []struct {
		name           string
		before         int64
		after          int64
		wantOutOfStock int
		wantOnSale     int
	}{
		{"positive to positive signals nothing", 5, 3, 0, 0},
		{"availability to zero signals out of stock", 2, 0, 1, 0},
		{"zero to positive signals on sale", 0, 4, 0, 1},
		{"zero to zero signals nothing", 0, 0, 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{physical: tc.before}
			availability := &fakeAvailability{}
			svc := newReconcileService(repo, availability)

			err := svc.reconcileAvailability(adminContext(), uuid.New(), fixedNow, func() error {
				repo.physical = tc.after
				return nil
			})
			if err != nil {
				t.Fatalf("reconcileAvailability: %v", err)
			}
			if availability.outOfStock != tc.wantOutOfStock || availability.onSale != tc.wantOnSale {
				t.Fatalf("signals: got MarkOutOfStock=%d MarkOnSale=%d, want %d/%d",
					availability.outOfStock, availability.onSale, tc.wantOutOfStock, tc.wantOnSale)
			}
		})
	}
}

// A held quantity lowers availability, so a change measured on the physical shelf
// alone would misfire. This is the crossing a hold creates: the physical count
// stays at 3 while a hold of all 3 moves availability from 3 to 0, which must
// signal out of stock (FR-018, FR-024, research D1).
func TestReconciliationMeasuresAvailabilityNotThePhysicalCount(t *testing.T) {
	repo := &fakeRepository{physical: 3}
	availability := &fakeAvailability{}
	svc := newReconcileService(repo, availability)

	if err := svc.reconcileAvailability(adminContext(), uuid.New(), fixedNow, func() error {
		repo.held = 3
		return nil
	}); err != nil {
		t.Fatalf("reconcileAvailability: %v", err)
	}
	if availability.outOfStock != 1 || availability.onSale != 0 {
		t.Fatalf("the crossing is availability, not the physical count, got MarkOutOfStock=%d MarkOnSale=%d",
			availability.outOfStock, availability.onSale)
	}
}

// A sweep releases holds that have already passed their expiry. ActiveHeld already
// excludes an expired hold, so the pre-expiry availability is reconstructed from
// the quantity the sweep actually releases: physical − active holds − released.
// This is what lets a product whose last held unit expires return to sale even
// though the availability predicate itself never moved at sweep time (FR-025,
// research D6).
func TestExpiryReconciliationSignalsOnSaleWhenTheLastHoldExpires(t *testing.T) {
	repo := &fakeRepository{physical: 1}
	availability := &fakeAvailability{}
	svc := newReconcileService(repo, availability)

	err := svc.reconcileExpiry(context.Background(), uuid.New(), fixedNow, func() (int64, error) {
		return 1, nil
	})
	if err != nil {
		t.Fatalf("reconcileExpiry: %v", err)
	}
	if availability.onSale != 1 || availability.outOfStock != 0 {
		t.Fatalf("the sweep that frees the last unit must signal on sale, got %d/%d",
			availability.outOfStock, availability.onSale)
	}
}

// A sweep that frees only part of a product whose availability was already
// positive is a non-crossing change and signals nothing, so an operator's marking
// is preserved (FR-028).
func TestExpiryReconciliationLeavesANonCrossingSweepAlone(t *testing.T) {
	repo := &fakeRepository{physical: 5, held: 2}
	availability := &fakeAvailability{}
	svc := newReconcileService(repo, availability)

	err := svc.reconcileExpiry(context.Background(), uuid.New(), fixedNow, func() (int64, error) {
		repo.held = 0
		return 2, nil
	})
	if err != nil {
		t.Fatalf("reconcileExpiry: %v", err)
	}
	if availability.outOfStock != 0 || availability.onSale != 0 {
		t.Fatalf("a non-crossing sweep must signal nothing, got %d/%d",
			availability.outOfStock, availability.onSale)
	}
}

// The availability port is optional at construction: a use case wire that has not
// been handed the product adapter (a unit test, a read-only build) must still run.
// A nil port tolerates the crossing instead of panicking, exactly as the audit
// port tolerates a nil auditor.
func TestReconciliationToleratesAnUnwiredAvailabilityPort(t *testing.T) {
	repo := &fakeRepository{physical: 1}
	svc := New(Service{
		Inventory: repo,
		Lookup:    &fakeLookup{exists: true},
		Tx:        passThroughUnitOfWork{},
		Clock:     fixedClock{},
		Mapper:    mapper.New(),
	})

	if err := svc.reconcileAvailability(adminContext(), uuid.New(), fixedNow, func() error {
		repo.physical = 0
		return nil
	}); err != nil {
		t.Fatalf("a crossing with no availability port must be a no-op, got %v", err)
	}
}
