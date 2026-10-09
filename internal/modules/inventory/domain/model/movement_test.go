package model

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
)

// This file exercises the movement entity and its kind rules: a signed delta that
// is never zero, a resulting quantity that is never negative, an optional note and
// the four kinds (FR-004, research D9).

// FR-004, research D9: a zero delta is not a change and is refused.
func TestNewMovementRefusesAZeroDelta(t *testing.T) {
	_, err := NewMovement(uuid.New(), constant.MovementRestock, 0, 5, nil, nil, nil, time.Now().UTC())
	if !errors.Is(err, domainerr.ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}

// FR-009: a recorded result can never describe a negative shelf.
func TestNewMovementRefusesANegativeResultingQuantity(t *testing.T) {
	_, err := NewMovement(uuid.New(), constant.MovementDamage, -1, -1, nil, nil, nil, time.Now().UTC())
	if !errors.Is(err, domainerr.ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}

// FR-004: an unlisted kind cannot be stored.
func TestNewMovementRefusesAnUnknownKind(t *testing.T) {
	_, err := NewMovement(uuid.New(), constant.MovementKind("ARCHIVED"), 1, 1, nil, nil, nil, time.Now().UTC())
	if !errors.Is(err, domainerr.ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}

// FR-004: a manual movement carries its optional note, its signed delta and the
// resulting quantity.
func TestNewMovementCarriesTheOptionalNote(t *testing.T) {
	note := "kiểm kê tháng 10"
	actor := uuid.New()

	movement, err := NewMovement(uuid.New(), constant.MovementAdjustment, -2, 8, &actor, nil, &note, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewMovement: %v", err)
	}
	if movement.Delta != -2 || movement.ResultingQuantity != 8 {
		t.Fatalf("the movement does not carry its amounts: %+v", movement)
	}
	if movement.Note == nil || *movement.Note != note {
		t.Fatalf("the movement did not carry its note: %+v", movement.Note)
	}
	if movement.ActorID == nil || *movement.ActorID != actor {
		t.Fatalf("the movement did not carry its actor: %+v", movement.ActorID)
	}
	if movement.ID == uuid.Nil {
		t.Fatal("a movement must carry an identifier")
	}
}

// research D9: an adjustment is recorded as its signed difference, and correcting
// to the stored value yields a zero difference the use case omits.
func TestAdjustmentDeltaIsTheSignedDifference(t *testing.T) {
	for _, tc := range []struct {
		current, counted int64
		want             int64
	}{
		{current: 10, counted: 12, want: 2},
		{current: 10, counted: 7, want: -3},
		{current: 10, counted: 10, want: 0},
	} {
		delta, err := AdjustmentDelta(tc.current, tc.counted)
		if err != nil {
			t.Fatalf("AdjustmentDelta(%d, %d): %v", tc.current, tc.counted, err)
		}
		if delta != tc.want {
			t.Fatalf("AdjustmentDelta(%d, %d) = %d, want %d", tc.current, tc.counted, delta, tc.want)
		}
	}
}

// FR-012: a counted value cannot be negative.
func TestAdjustmentDeltaRefusesANegativeCount(t *testing.T) {
	if _, err := AdjustmentDelta(3, -1); !errors.Is(err, domainerr.ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}
