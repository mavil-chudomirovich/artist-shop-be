package constant

import "time"

// HoldTTL is the fixed window a hold lasts before it returns its quantity to
// availability. It is a single business constant rather than a per-product or
// per-order setting (spec Assumption): whether it should be configurable is a
// later concern, not a reason to leave the behaviour open. It is the one value
// the whole holding behaviour turns on, so it is named once here.
//
// It is sixty minutes so a hold never lapses before the customer pays: feature 010
// gives a confirmed order a sixty-minute payment window and the order reads this
// same window through InventoryReservation.HoldWindow, so the two stay in step
// (FR-008 of 010, research D13).
const HoldTTL = 60 * time.Minute

// HoldStatus is where a hold is in its short life: active, or resolved one of the
// three ways. It is stored as constrained text in stock_holds.status and is only
// ever changed through the hold entity's transitions.
type HoldStatus string

const (
	// HoldStatusActive is a hold whose quantity is set aside: it is unavailable to
	// anyone else, and it is active only while its expiry is in the future.
	HoldStatusActive HoldStatus = "ACTIVE"
	// HoldStatusConsumed is a hold that was paid: its quantity became a sale and
	// the physical stock fell by it (FR-016).
	HoldStatusConsumed HoldStatus = "CONSUMED"
	// HoldStatusReleased is a hold that was cancelled or expired: its quantity
	// returned to availability and the physical stock did not move (FR-015,
	// FR-017).
	HoldStatusReleased HoldStatus = "RELEASED"
)

// IsValidHoldStatus reports whether value is one of the three hold statuses.
func IsValidHoldStatus(value HoldStatus) bool {
	switch value {
	case HoldStatusActive, HoldStatusConsumed, HoldStatusReleased:
		return true
	default:
		return false
	}
}
