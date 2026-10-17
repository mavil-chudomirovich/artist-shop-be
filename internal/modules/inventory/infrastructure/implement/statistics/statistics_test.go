package statistics

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/model"
)

// fakeRepository answers the low-stock read and records the instant it was given,
// so a test can prove the adapter uses the injected clock.
type fakeRepository struct {
	variants []model.LowStockVariant
	err      error

	threshold int64
	now       time.Time
}

func (f *fakeRepository) LowStock(_ context.Context, threshold int64, now time.Time) ([]model.LowStockVariant, error) {
	f.threshold, f.now = threshold, now
	return f.variants, f.err
}

// fakeClock is the injected clock.
type fakeClock struct{ at time.Time }

func (c fakeClock) Now() time.Time { return c.at }

// research D9, FR-017, FR-018: the adapter passes the threshold and the injected
// instant to the repository and maps the candidates unchanged.
func TestLowStockVariantsUsesTheInjectedClock(t *testing.T) {
	at := time.Date(2026, time.October, 17, 4, 0, 0, 0, time.UTC)
	first := uuid.New()
	second := uuid.New()
	repo := &fakeRepository{variants: []model.LowStockVariant{
		{VariantID: first, Available: 3},
		{VariantID: second, Available: 0},
	}}

	got, err := New(repo, fakeClock{at: at}).LowStockVariants(context.Background(), 5)
	if err != nil {
		t.Fatalf("LowStockVariants: %v", err)
	}
	if repo.threshold != 5 {
		t.Fatalf("the threshold must be passed through, got %d", repo.threshold)
	}
	if !repo.now.Equal(at) {
		t.Fatalf("the read must use the injected instant, got %v want %v", repo.now, at)
	}
	if len(got) != 2 || got[0].VariantID != first || got[0].Available != 3 ||
		got[1].VariantID != second || got[1].Available != 0 {
		t.Fatalf("candidates did not map: %+v", got)
	}
}

// A nil clock falls back to the wall clock rather than panicking, exactly as the
// inventory use cases do.
func TestLowStockVariantsToleratesANilClock(t *testing.T) {
	repo := &fakeRepository{}

	if _, err := New(repo, nil).LowStockVariants(context.Background(), 5); err != nil {
		t.Fatalf("LowStockVariants: %v", err)
	}
	if repo.now.IsZero() {
		t.Fatal("a nil clock must fall back to the wall clock")
	}
}
