// Package domainerr defines the order module's business sentinel errors. Codes in
// specs/009-order/contracts/error-codes.md map to these errors in
// presentation/http; the domain never knows an HTTP status.
package domainerr

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/order/domain/constant"
)

// Business sentinel errors returned by the order module.
var (
	// ErrNotFound is returned for an unknown order, and for one that belongs to
	// another customer. The two are one answer deliberately: a route that
	// answered differently would confirm another customer's order (FR-020). It
	// maps to ORDER_NOT_FOUND (404).
	ErrNotFound = errors.New("order not found")
	// ErrCartEmpty is returned when checkout is asked for a cart with no line. An
	// order is never created from nothing (FR-006). It maps to ORDER_CART_EMPTY
	// (409).
	ErrCartEmpty = errors.New("cart is empty")
	// ErrItemNotPurchasable is returned when a cart line's product is off sale or
	// removed, so it cannot be ordered. It is the single sentinel for the typed
	// refusals built by ItemNotPurchasable, so the module keeps one error to map
	// (FR-004). It maps to ORDER_ITEM_NOT_PURCHASABLE (409).
	ErrItemNotPurchasable = errors.New("order item is not purchasable")
	// ErrItemPriceChanged is returned when a cart line's product is priced
	// differently from the price the customer was shown. The whole checkout is
	// refused rather than silently re-priced (FR-004). It maps to
	// ORDER_ITEM_PRICE_CHANGED (409).
	ErrItemPriceChanged = errors.New("order item price changed")
	// ErrQuantityExceedsAvailable is returned when a line asks for more than is
	// available, or when the goods could not be held. It is the single sentinel
	// for the typed refusals built by QuantityExceedsAvailable (FR-005, FR-017).
	// It maps to ORDER_QUANTITY_EXCEEDS_AVAILABLE (409).
	ErrQuantityExceedsAvailable = errors.New("order quantity exceeds available")
	// ErrNoAddress is returned when the customer has no delivery address, so an
	// order has nowhere to go (FR-003). It maps to ORDER_NO_ADDRESS (409).
	ErrNoAddress = errors.New("customer has no address")
	// ErrStateTransitionInvalid is returned when a move the order's current state
	// does not allow is attempted. It is the single sentinel for the typed
	// refusals built by StateTransitionInvalid, so the module keeps one error to
	// map (FR-009, FR-010, FR-011). It maps to ORDER_STATE_TRANSITION_INVALID
	// (409).
	ErrStateTransitionInvalid = errors.New("order state transition invalid")
	// ErrNotTransferable is returned when a transfer is attempted on an order
	// that is not paid (FR-024). It maps to ORDER_NOT_TRANSFERABLE (409).
	ErrNotTransferable = errors.New("order is not transferable")
	// ErrTransferTargetNotFound is returned when no account carries the email a
	// transfer names (FR-024). It maps to ORDER_TRANSFER_TARGET_NOT_FOUND (404).
	ErrTransferTargetNotFound = errors.New("order transfer target not found")
	// ErrInvalidValue is returned when a value fails its own rule and the
	// response must name the member a client has to fix — for example an
	// `addressId` that is not one of the customer's addresses (FR-003,
	// error-codes.md). It is the single sentinel for the typed refusals built by
	// InvalidValue. It maps to the shared VALIDATION_ERROR (400).
	ErrInvalidValue = errors.New("invalid order value")
)

// ItemNotPurchasableError carries the product that cannot be bought, so the
// response can name it without the caller re-reading the catalogue
// (error-codes.md, "Where the offending item is named").
type ItemNotPurchasableError struct {
	// ProductID is the product that is not on sale or was removed.
	ProductID uuid.UUID
}

// Error implements the error interface.
func (e *ItemNotPurchasableError) Error() string {
	return ErrItemNotPurchasable.Error() + ": " + e.ProductID.String()
}

// Is makes errors.Is(err, ErrItemNotPurchasable) match this error.
func (e *ItemNotPurchasableError) Is(target error) bool { return target == ErrItemNotPurchasable }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *ItemNotPurchasableError) Unwrap() error { return ErrItemNotPurchasable }

// ItemNotPurchasable builds the typed refusal for one product.
func ItemNotPurchasable(productID uuid.UUID) error {
	return &ItemNotPurchasableError{ProductID: productID}
}

// ItemPriceChangedError carries the product whose price moved, so the response
// can name the line the customer must review (FR-004).
type ItemPriceChangedError struct {
	// ProductID is the product whose price differs from the cart's snapshot.
	ProductID uuid.UUID
}

// Error implements the error interface.
func (e *ItemPriceChangedError) Error() string {
	return ErrItemPriceChanged.Error() + ": " + e.ProductID.String()
}

// Is makes errors.Is(err, ErrItemPriceChanged) match this error.
func (e *ItemPriceChangedError) Is(target error) bool { return target == ErrItemPriceChanged }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *ItemPriceChangedError) Unwrap() error { return ErrItemPriceChanged }

// ItemPriceChanged builds the typed refusal for one product.
func ItemPriceChanged(productID uuid.UUID) error {
	return &ItemPriceChangedError{ProductID: productID}
}

// QuantityExceedsAvailableError carries what the refusal is about, so the response
// can state how far the customer must reduce the line. It is the single typed
// carrier for ErrQuantityExceedsAvailable (FR-005, FR-017).
type QuantityExceedsAvailableError struct {
	// ProductID is the product the line is for.
	ProductID uuid.UUID
	// Available is what could have been taken when the shelf said no.
	Available int64
	// Requested is the quantity the operation asked for.
	Requested int64
}

// Error implements the error interface. It names the two amounts, which is what
// the customer must be told.
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

// StateTransitionError carries the move that was refused and the state that
// refused it, so the response can name the current state (FR-011).
type StateTransitionError struct {
	// From is the state the order was in when the move was attempted.
	From constant.Status
	// To is the state the move asked for.
	To constant.Status
}

// Error implements the error interface. It names the current state, which is what
// FR-011 requires the caller to be told.
func (e *StateTransitionError) Error() string {
	return fmt.Sprintf("%s: cannot move from %s to %s", ErrStateTransitionInvalid, e.From, e.To)
}

// Is makes errors.Is(err, ErrStateTransitionInvalid) match this error.
func (e *StateTransitionError) Is(target error) bool { return target == ErrStateTransitionInvalid }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *StateTransitionError) Unwrap() error { return ErrStateTransitionInvalid }

// StateTransitionInvalid builds the typed refusal for one refused move.
func StateTransitionInvalid(from, to constant.Status) error {
	return &StateTransitionError{From: from, To: to}
}

// InvalidValueError reports which member of a request is not acceptable. It is
// one typed carrier for ErrInvalidValue rather than a family of per-member
// sentinels, so the module keeps a single error to map and a single one to test.
// It mirrors the same-named error modules 05 and 06 use.
//
// Field holds the contract's member name, which is what presentation puts into
// the response detail; Issue is a short, non-sensitive explanation.
type InvalidValueError struct {
	// Field is the contract member name, such as "addressId".
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
