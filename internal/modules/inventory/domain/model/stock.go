package model

import (
	"time"

	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/inventory/domain/error"
)

// Contract member names used when a rejection has to say which input is wrong.
// They are the exact member names of
// specs/007-inventory-tracking/contracts/openapi.yaml, so presentation can put one
// straight into the response detail without a second vocabulary that could drift
// away from the contract.
const (
	// FieldQuantity is the request member carrying a stock quantity.
	FieldQuantity = "quantity"
	// FieldHeld is the derived held quantity, named when a relationship between
	// the shelf and its holds is impossible rather than a request value.
	FieldHeld = "held"
)

// Stock is the physical count of one product together with, at the moment it is
// asked, the quantity its active holds have set aside. Availability is derived
// from the two and is never stored, so it cannot drift from the rows it comes
// from (FR-013, research D1).
type Stock struct {
	// ProductID identifies the product the count belongs to.
	ProductID uuid.UUID
	// Quantity is the physical units on the shelf. A whole number, never
	// negative.
	Quantity int64
	// Held is the quantity the product's active holds have set aside. It never
	// exceeds Quantity.
	Held int64
	// UpdatedAt is when the level was last written. It is zero for a product that
	// has never been stocked.
	UpdatedAt time.Time
}

// NewStock builds a stock from a physical count and a held quantity, refusing any
// pair that would make availability negative (FR-009, FR-018).
func NewStock(productID uuid.UUID, quantity, held int64, updatedAt time.Time) (Stock, error) {
	stock := Stock{ProductID: productID, Quantity: quantity, Held: held, UpdatedAt: updatedAt}
	if _, err := stock.Available(); err != nil {
		return Stock{}, err
	}
	return stock, nil
}

// Available returns the quantity a customer can take: the physical count minus the
// quantity the active holds have set aside. It is never negative, and a pair that
// would make it negative is refused (FR-018).
func (s Stock) Available() (int64, error) {
	return Availability(s.Quantity, s.Held)
}

// Availability computes physical minus held, refusing a negative physical count,
// a negative held quantity and a held quantity above the physical one, so the
// result is never negative (FR-009, FR-018, research D1).
func Availability(physical, held int64) (int64, error) {
	if physical < 0 {
		return 0, domainerr.InvalidValue(FieldQuantity, "must not be negative")
	}
	if held < 0 {
		return 0, domainerr.InvalidValue(FieldHeld, "must not be negative")
	}
	if held > physical {
		return 0, domainerr.InvalidValue(FieldHeld, "cannot exceed the physical quantity")
	}
	return physical - held, nil
}

// Increase returns a stock whose physical quantity rose by a positive whole
// amount. A zero or negative amount is refused naming `quantity`, because a stock
// is a whole number of units (FR-012).
func (s Stock) Increase(amount int64) (Stock, error) {
	if amount <= 0 {
		return s, domainerr.InvalidValue(FieldQuantity, "must be a positive whole number")
	}
	s.Quantity += amount
	return s, nil
}

// Decrease returns a stock whose physical quantity fell by a positive whole
// amount, refusing any decrease that would take it below zero and leaving the
// receiver untouched when it does (FR-002, FR-009, FR-012).
func (s Stock) Decrease(amount int64) (Stock, error) {
	if amount <= 0 {
		return s, domainerr.InvalidValue(FieldQuantity, "must be a positive whole number")
	}
	if amount > s.Quantity {
		available, _ := s.Available()
		return s, domainerr.InsufficientStock(FieldQuantity, available, amount)
	}
	s.Quantity -= amount
	return s, nil
}
