package model

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
)

// This file exercises the stock rule: a physical quantity that is never negative,
// availability as the derived physical minus active holds, and the refusal of any
// operation that would take either below zero (FR-009, FR-012, FR-018, research
// D1). The zero boundary is asserted explicitly, because "only below zero is not"
// is what US2 promises.

func newStock(t *testing.T, quantity, held int64) Stock {
	t.Helper()
	stock, err := NewStock(uuid.New(), quantity, held, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewStock(%d, %d): %v", quantity, held, err)
	}
	return stock
}

// FR-009: a decrease that would go below zero is refused.
func TestDecreaseBelowZeroIsRefused(t *testing.T) {
	stock := newStock(t, 3, 0)

	after, err := stock.Decrease(5)
	if !errors.Is(err, domainerr.ErrInsufficientStock) {
		t.Fatalf("expected ErrInsufficientStock, got %v", err)
	}
	if after.Quantity != 3 {
		t.Fatalf("a refused decrease must leave the quantity unchanged, got %d", after.Quantity)
	}
}

// FR-009, US2 scenario 3: a decrease that reaches exactly zero is allowed.
func TestDecreaseToExactlyZeroIsAllowed(t *testing.T) {
	stock := newStock(t, 3, 0)

	after, err := stock.Decrease(3)
	if err != nil {
		t.Fatalf("a decrease to exactly zero must succeed, got %v", err)
	}
	if after.Quantity != 0 {
		t.Fatalf("expected quantity 0, got %d", after.Quantity)
	}
}

// FR-012: an increase requires a positive whole amount.
func TestIncreaseRequiresAPositiveAmount(t *testing.T) {
	stock := newStock(t, 3, 0)

	for _, amount := range []int64{0, -1} {
		if _, err := stock.Increase(amount); !errors.Is(err, domainerr.ErrInvalidValue) {
			t.Fatalf("Increase(%d): expected ErrInvalidValue, got %v", amount, err)
		}
	}
}

// FR-012: a decrease requires a positive whole amount.
func TestDecreaseRequiresAPositiveAmount(t *testing.T) {
	stock := newStock(t, 3, 0)

	for _, amount := range []int64{0, -1} {
		if _, err := stock.Decrease(amount); !errors.Is(err, domainerr.ErrInvalidValue) {
			t.Fatalf("Decrease(%d): expected ErrInvalidValue, got %v", amount, err)
		}
	}
}

// FR-018, research D1: availability is physical minus the active holds.
func TestAvailabilityIsPhysicalMinusActiveHolds(t *testing.T) {
	available, err := Availability(5, 2)
	if err != nil {
		t.Fatalf("Availability(5, 2): %v", err)
	}
	if available != 3 {
		t.Fatalf("Availability(5, 2) = %d, want 3", available)
	}

	stock := newStock(t, 5, 2)
	fromStock, err := stock.Available()
	if err != nil || fromStock != 3 {
		t.Fatalf("Stock.Available() = %d/%v, want 3", fromStock, err)
	}
}

// FR-009, FR-018: availability is never negative.
func TestAvailabilityIsNeverNegative(t *testing.T) {
	if _, err := Availability(2, 5); !errors.Is(err, domainerr.ErrInvalidValue) {
		t.Fatalf("more held than physical must be refused, got %v", err)
	}
	if _, err := Availability(-1, 0); !errors.Is(err, domainerr.ErrInvalidValue) {
		t.Fatalf("a negative physical quantity must be refused, got %v", err)
	}
	if _, err := Availability(0, 0); err != nil {
		t.Fatalf("zero and zero is the empty shelf, not an error: %v", err)
	}
}

// FR-009: NewStock refuses a stock whose held quantity exceeds its physical one.
func TestNewStockRefusesHeldAbovePhysical(t *testing.T) {
	if _, err := NewStock(uuid.New(), 1, 2, time.Now().UTC()); !errors.Is(err, domainerr.ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}
