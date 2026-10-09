// Package domainerr defines the cart module's business sentinel errors.
//
// Codes in specs/008-cart/contracts/error-codes.md map to these errors in
// presentation/http; the domain never knows an HTTP status.
package domainerr

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Business sentinel errors returned by the cart module.
var (
	// ErrProductNotFound is returned for a product that does not exist or was
	// removed, and for a line this cart does not hold. The product is module 04's
	// resource, so presentation answers the shared PRODUCT_NOT_FOUND (404) rather
	// than inventing a second code for one condition (error-codes.md). The two
	// cases share one error deliberately: a route that answered differently would
	// confirm what is in another customer's cart (FR-010).
	ErrProductNotFound = errors.New("product not found")
	// ErrProductNotPurchasable is returned when a product exists but is not on
	// sale, so it cannot be added or kept as a line. Presentation maps it to
	// CART_PRODUCT_NOT_PURCHASABLE (409) and names the product (FR-005).
	ErrProductNotPurchasable = errors.New("product not purchasable")
	// ErrQuantityExceedsAvailable is returned when a quantity, or the quantity an
	// add would produce, is greater than what is currently available. Presentation
	// maps it to CART_QUANTITY_EXCEEDS_AVAILABLE (409) and names the offending
	// field and the currently available amount (FR-007).
	ErrQuantityExceedsAvailable = errors.New("quantity exceeds available")
	// ErrInvalidValue is returned when a value fails its own rule: a quantity
	// that is not a positive whole number, or a captured price that is not a
	// positive amount in a three-letter currency (FR-006). It carries the
	// offending member, because the response must name the field a client has to
	// fix; presentation maps it to the shared VALIDATION_ERROR (400).
	ErrInvalidValue = errors.New("invalid cart value")
	// ErrCartAlreadyExists is reported when the unique index on carts.user_id
	// refused the insert of a new cart: another concurrent first add created the
	// account's cart in the same instant (research D8). It is the storage guard
	// FR-001 rests on. The caller retries by finding the cart the other writer
	// created, so this is an outcome the use case treats as success rather than a
	// failure.
	ErrCartAlreadyExists = errors.New("cart already exists")
)

// NotPurchasableError carries the product that cannot be bought, so the response
// can name it without the caller re-reading the catalogue. It is the single typed
// carrier for ErrProductNotPurchasable, so the module keeps one sentinel to map
// and one type to test.
type NotPurchasableError struct {
	// ProductID is the product that is not on sale.
	ProductID uuid.UUID
}

// Error implements the error interface.
func (e *NotPurchasableError) Error() string {
	return ErrProductNotPurchasable.Error() + ": " + e.ProductID.String()
}

// Is makes errors.Is(err, ErrProductNotPurchasable) match this error.
func (e *NotPurchasableError) Is(target error) bool {
	return target == ErrProductNotPurchasable
}

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *NotPurchasableError) Unwrap() error { return ErrProductNotPurchasable }

// ProductNotPurchasable builds the typed refusal for one product.
func ProductNotPurchasable(productID uuid.UUID) error {
	return &NotPurchasableError{ProductID: productID}
}

// QuantityExceedsAvailableError carries what the refusal is about, so the
// response can state how far the customer must reduce the line. It is the single
// typed carrier for ErrQuantityExceedsAvailable, so the module keeps one sentinel
// to map and one type to test.
type QuantityExceedsAvailableError struct {
	// ProductID is the product the line is for.
	ProductID uuid.UUID
	// Available is what could have been taken when the shelf said no.
	Available int64
	// Requested is the quantity the operation asked for.
	Requested int64
}

// Error implements the error interface. It names the two amounts, which is what
// FR-007 requires the customer to be told.
func (e *QuantityExceedsAvailableError) Error() string {
	return fmt.Sprintf("%s: %s requested %d, available %d",
		ErrQuantityExceedsAvailable, e.ProductID, e.Requested, e.Available)
}

// Is makes errors.Is(err, ErrQuantityExceedsAvailable) match this error.
func (e *QuantityExceedsAvailableError) Is(target error) bool {
	return target == ErrQuantityExceedsAvailable
}

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *QuantityExceedsAvailableError) Unwrap() error { return ErrQuantityExceedsAvailable }

// QuantityExceedsAvailable builds the typed refusal for one operation.
func QuantityExceedsAvailable(productID uuid.UUID, available, requested int64) error {
	return &QuantityExceedsAvailableError{ProductID: productID, Available: available, Requested: requested}
}

// InvalidValueError reports which member of a request is not acceptable. It is one
// typed carrier for ErrInvalidValue rather than a family of per-member sentinels,
// so the module keeps a single error to map and a single one to test.
//
// Field holds the contract's member name, which is what presentation puts into
// the response detail; Issue is a short, non-sensitive explanation.
type InvalidValueError struct {
	// Field is the contract member name, such as "quantity".
	Field string
	// Issue explains what is wrong with it.
	Issue string
}

// Error implements the error interface.
func (e *InvalidValueError) Error() string {
	return ErrInvalidValue.Error() + ": " + e.Field + " " + e.Issue
}

// Is makes errors.Is(err, ErrInvalidValue) match this error.
func (e *InvalidValueError) Is(target error) bool { return target == ErrInvalidValue }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *InvalidValueError) Unwrap() error { return ErrInvalidValue }

// InvalidValue builds the typed rejection for one member.
func InvalidValue(field, issue string) error {
	return &InvalidValueError{Field: field, Issue: issue}
}
