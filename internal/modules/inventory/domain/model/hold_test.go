package model

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
)

// This file exercises the hold entity and its expiry rule: an active hold is
// active only while its expiry is in the future; it ends by being consumed,
// released or expired, and a resolved one carries the moment it ended (the
// clarification, research D6).

func newHold(t *testing.T, at time.Time) *Hold {
	t.Helper()
	hold, err := NewHold(uuid.New(), uuid.New(), 2, at)
	if err != nil {
		t.Fatalf("NewHold: %v", err)
	}
	return hold
}

// spec Assumption: a hold lasts a fixed sixty minutes.
func TestNewHoldSetsTheSixtyMinuteWindow(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	hold := newHold(t, now)

	if hold.ExpiresAt.Sub(now) != constant.HoldTTL {
		t.Fatalf("expected a %s window, got %s", constant.HoldTTL, hold.ExpiresAt.Sub(now))
	}
	if hold.Status != constant.HoldStatusActive {
		t.Fatalf("a new hold must be active, got %q", hold.Status)
	}
	if hold.ResolvedAt != nil {
		t.Fatal("an active hold must not carry a resolution time")
	}
}

// FR-015: a hold is active only until its expiry.
func TestAHoldIsActiveOnlyUntilItsExpiry(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	hold := newHold(t, now)

	if !hold.IsActive(now.Add(constant.HoldTTL - time.Second)) {
		t.Fatal("a hold must be active inside its window")
	}
	if hold.IsActive(now.Add(constant.HoldTTL + time.Second)) {
		t.Fatal("a hold must not be active after its expiry")
	}
}

// FR-019: a resolved hold cannot be consumed twice.
func TestAResolvedHoldCannotBeConsumedTwice(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	hold := newHold(t, now)

	if err := hold.Consume(now.Add(time.Minute)); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if hold.Status != constant.HoldStatusConsumed || hold.ResolvedAt == nil {
		t.Fatalf("a consumed hold must be resolved, got %+v", hold)
	}
	if err := hold.Consume(now.Add(2 * time.Minute)); !errors.Is(err, domainerr.ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue on a second consume, got %v", err)
	}
}

// FR-015: an expired hold cannot be consumed, because its quantity is already
// available to others even before the sweeper reaches it (research D6).
func TestConsumeOfAnExpiredHoldIsRefused(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	hold := newHold(t, now)

	if err := hold.Consume(now.Add(constant.HoldTTL + time.Second)); !errors.Is(err, domainerr.ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}

// FR-015: expiring is only allowed once the window has passed.
func TestExpireRequiresTheWindowToHavePassed(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	hold := newHold(t, now)

	if err := hold.Expire(now.Add(time.Minute)); !errors.Is(err, domainerr.ErrInvalidValue) {
		t.Fatalf("a hold inside its window must not expire, got %v", err)
	}
	if err := hold.Expire(now.Add(constant.HoldTTL)); err != nil {
		t.Fatalf("Expire at the boundary: %v", err)
	}
	if hold.Status != constant.HoldStatusReleased || hold.ResolvedAt == nil {
		t.Fatalf("an expired hold is released, got %+v", hold)
	}
}

// FR-017: release returns an active hold to availability without a physical move.
func TestReleaseResolvesAnActiveHold(t *testing.T) {
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	hold := newHold(t, now)

	if err := hold.Release(now.Add(time.Minute)); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if hold.Status != constant.HoldStatusReleased {
		t.Fatalf("expected the hold to be released, got %q", hold.Status)
	}
	if err := hold.Release(now.Add(2 * time.Minute)); !errors.Is(err, domainerr.ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue on a second release, got %v", err)
	}
}

// FR-018: a hold must set aside a positive quantity.
func TestNewHoldRequiresAPositiveQuantity(t *testing.T) {
	now := time.Now().UTC()
	for _, quantity := range []int64{0, -1} {
		if _, err := NewHold(uuid.New(), uuid.New(), quantity, now); !errors.Is(err, domainerr.ErrInvalidValue) {
			t.Fatalf("NewHold(%d): expected ErrInvalidValue, got %v", quantity, err)
		}
	}
}
