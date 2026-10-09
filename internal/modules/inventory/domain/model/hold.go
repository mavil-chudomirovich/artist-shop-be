package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
)

// FieldHold is the internal member named when a hold operation is refused because
// the hold is no longer active. It is not a request member.
const FieldHold = "hold"

// Hold is a quantity of one product set aside for one order while that order is
// being paid. While it is active the quantity is unavailable to anyone else; it
// ends by being paid (which turns it into a sale), cancelled (which returns it) or
// expiring (which returns it) (the clarification, FR-014, FR-015).
type Hold struct {
	// ID identifies the hold.
	ID uuid.UUID
	// ProductID is the product set aside.
	ProductID uuid.UUID
	// OrderID is the order the hold belongs to. It is a loose reference: module 07
	// owns orders and does not exist yet (research D5).
	OrderID uuid.UUID
	// Quantity is how many units are set aside. Always positive.
	Quantity int64
	// Status is where the hold is in its short life.
	Status constant.HoldStatus
	// ExpiresAt is when the hold returns its quantity if it is still unpaid.
	ExpiresAt time.Time
	// CreatedAt is when the hold was created. Set once.
	CreatedAt time.Time
	// ResolvedAt is when the hold stopped being active. Nil exactly while the hold
	// is ACTIVE.
	ResolvedAt *time.Time
}

// NewHold builds an active hold for a positive quantity, expiring one HoldTTL
// after the instant the injected clock produced (FR-014, research D15). The
// window is computed from that instant rather than from the database clock, so the
// same time source creates and judges the hold.
func NewHold(productID, orderID uuid.UUID, quantity int64, now time.Time) (*Hold, error) {
	if quantity <= 0 {
		return nil, domainerr.InvalidValue(FieldQuantity, "must be a positive whole number")
	}
	return &Hold{
		ID:        uuid.New(),
		ProductID: productID,
		OrderID:   orderID,
		Quantity:  quantity,
		Status:    constant.HoldStatusActive,
		ExpiresAt: now.Add(constant.HoldTTL),
		CreatedAt: now,
	}, nil
}

// IsActive reports whether the hold is set aside at the given instant: it must be
// unresolved and its expiry must still be in the future. An expired hold is no
// longer active even before the sweeper reaches it, so it neither blocks
// availability nor can be consumed (FR-015, research D6).
func (h Hold) IsActive(at time.Time) bool {
	return h.Status == constant.HoldStatusActive && h.ExpiresAt.After(at)
}

// Consume turns an active hold into a sale. A hold that is already resolved or
// whose window has passed is refused, so a hold can be consumed at most once and
// never after expiry (FR-016, FR-019, research D6).
func (h *Hold) Consume(at time.Time) error {
	if !h.IsActive(at) {
		return domainerr.InvalidValue(FieldHold, "is not active")
	}
	return h.resolve(constant.HoldStatusConsumed, at)
}

// Release returns an active hold's quantity to availability. A hold that is
// already resolved is refused, so a retried cancellation does not release twice
// (FR-017, FR-019).
func (h *Hold) Release(at time.Time) error {
	if h.Status != constant.HoldStatusActive {
		return domainerr.InvalidValue(FieldHold, "is already resolved")
	}
	return h.resolve(constant.HoldStatusReleased, at)
}

// Expire releases an active hold whose window has passed. A hold still inside its
// window is refused, so the sweeper cannot free goods that are still promised
// (FR-015).
func (h *Hold) Expire(at time.Time) error {
	if h.Status != constant.HoldStatusActive {
		return domainerr.InvalidValue(FieldHold, "is already resolved")
	}
	if h.ExpiresAt.After(at) {
		return domainerr.InvalidValue(FieldHold, "has not expired yet")
	}
	return h.resolve(constant.HoldStatusReleased, at)
}

// resolve sets the end state and the moment it was reached, so a resolved hold
// always carries when it ended and the two facts cannot disagree.
func (h *Hold) resolve(status constant.HoldStatus, at time.Time) error {
	moment := at
	h.Status = status
	h.ResolvedAt = &moment
	return nil
}
