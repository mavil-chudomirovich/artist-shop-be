package availability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainrepo "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/repository"
)

// fakeRepository implements only the single bulk read the adapter uses. The
// embedded interface is nil on purpose: any other method called on it panics, so
// a test that drifts from the adapter's actual dependency fails loudly instead of
// returning a zero value.
type fakeRepository struct {
	domainrepo.InventoryRepository
	readings []domainrepo.AvailabilityReading
	err      error
	gotNow   time.Time
}

func (f *fakeRepository) Availability(_ context.Context, _ []uuid.UUID, now time.Time) ([]domainrepo.AvailabilityReading, error) {
	f.gotNow = now
	return f.readings, f.err
}

// fakeClock is the injected instant, so the test observes the clock the adapter
// actually used rather than the wall clock.
type fakeClock struct{ at time.Time }

func (c fakeClock) Now() time.Time { return c.at }

// research D2: the adapter derives availability as physical minus active holds,
// returns one entry per requested identifier in the request's order, and answers
// zero for a product with no stock row.
func TestAvailableQuantityDerivesPhysicalMinusHeldInRequestOrder(t *testing.T) {
	stocked := uuid.New()
	held := uuid.New()
	missing := uuid.New()
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)

	repo := &fakeRepository{readings: []domainrepo.AvailabilityReading{
		{ProductID: stocked, Level: 10, Held: 0},
		{ProductID: held, Level: 5, Held: 2},
	}}
	adapter := New(repo, fakeClock{at: now})

	got, err := adapter.AvailableQuantity(context.Background(), []uuid.UUID{stocked, held, missing})
	if err != nil {
		t.Fatalf("AvailableQuantity: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries (one per requested id), got %d", len(got))
	}

	if got[0].ProductID != stocked || got[0].Available != 10 {
		t.Errorf("first = %+v, want %s available 10", got[0], stocked)
	}
	if got[1].ProductID != held || got[1].Available != 3 {
		t.Errorf("second = %+v, want %s available 3 (5 physical minus 2 held)", got[1], held)
	}
	if got[2].ProductID != missing || got[2].Available != 0 {
		t.Errorf("third = %+v, want %s available 0 (no stock row)", got[2], missing)
	}
	if !repo.gotNow.Equal(now) {
		t.Errorf("adapter read the clock %v, want %v", repo.gotNow, now)
	}
}

// An unavailable quantity is refused by the domain rule rather than silently
// reported as negative, so a caller never sees a nonsensical shelf (research D1).
func TestAvailableQuantityRefusesHeldAbovePhysical(t *testing.T) {
	id := uuid.New()
	repo := &fakeRepository{readings: []domainrepo.AvailabilityReading{
		{ProductID: id, Level: 1, Held: 2},
	}}

	if _, err := New(repo, nil).AvailableQuantity(context.Background(), []uuid.UUID{id}); err == nil {
		t.Fatal("expected an error when held exceeds physical, got nil")
	}
}

// A repository failure is reported as itself, never swallowed or replaced.
func TestAvailableQuantityReportsTheRepositoryError(t *testing.T) {
	want := errors.New("storage unavailable")

	_, err := New(&fakeRepository{err: want}, nil).AvailableQuantity(context.Background(), []uuid.UUID{uuid.New()})
	if !errors.Is(err, want) {
		t.Fatalf("expected the repository error, got %v", err)
	}
}
