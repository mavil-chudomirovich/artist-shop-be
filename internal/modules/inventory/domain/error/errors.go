package domainerr

import (
	"errors"
	"fmt"
)

// Business sentinel errors returned by the inventory module.
var (
	// ErrInsufficientStock is the one refusal the module owns: a change would take
	// a product's physical quantity below zero, or below the quantity currently
	// held for orders being paid (FR-009). It maps to
	// INVENTORY_INSUFFICIENT_STOCK (409).
	ErrInsufficientStock = errors.New("insufficient stock")
	// ErrProductNotFound is returned for an unknown product. The product is
	// module 04's resource, so presentation answers the shared PRODUCT_NOT_FOUND
	// (404) rather than inventing a second code for one condition
	// (error-codes.md). It is also what a foreign-key refusal on a missing product
	// is classified into, as the storage guard (research D2, D12).
	ErrProductNotFound = errors.New("product not found")
	// ErrInvalidValue is returned when a value fails its own rule: a quantity that
	// is not a positive whole number, a counted value below zero, a movement whose
	// delta is zero, or a hold operation against a hold that is no longer active
	// (FR-012). It carries the offending member, because the response must name the
	// field a client has to fix; presentation maps it to the shared
	// VALIDATION_ERROR (400).
	ErrInvalidValue = errors.New("invalid inventory value")
	// ErrHoldAlreadyExists is reported when an order already holds a product: the
	// partial unique index on active (order_id, product_id) refused a second hold.
	// It is the storage guard FR-019 rests on, and a reserve that meets it is a
	// no-op rather than a failure, so a retried checkout never holds twice. It is
	// deliberately separate from ErrInvalidValue because the caller treats it as
	// already-applied rather than as a rejected input.
	ErrHoldAlreadyExists = errors.New("hold already exists")
	// ErrAlreadyApplied is reported when an outside event's source reference is
	// already carried by a movement: the partial unique index on source_reference
	// refused a second movement. It is the storage guard FR-020 to FR-022 rest on,
	// and an application that meets it answers success rather than a failure, so a
	// retrying caller stops retrying and a concurrent duplicate changes stock only
	// once (FR-021). Like ErrHoldAlreadyExists it is deliberately separate from
	// ErrInvalidValue, because it is an outcome the caller treats as success rather
	// than a rejected input.
	ErrAlreadyApplied = errors.New("event already applied")
)

// InsufficientStockError carries what the refusal is about, so the response can
// name the field without the caller re-reading the shelf. It is the single typed
// carrier for ErrInsufficientStock, so the module keeps one sentinel to map and
// one type to test.
type InsufficientStockError struct {
	// Field is the contract member name the refusal is about, normally
	// "quantity".
	Field string
	// Available is what could have been taken when the shelf said no.
	Available int64
	// Requested is the amount the operation asked for.
	Requested int64
}

// Error implements the error interface. It names the field and the two amounts,
// which is what FR-009 requires the operator to be told.
func (e *InsufficientStockError) Error() string {
	return fmt.Sprintf("%s: %s requested %d, available %d", ErrInsufficientStock, e.Field, e.Requested, e.Available)
}

// Is makes errors.Is(err, ErrInsufficientStock) match this error.
func (e *InsufficientStockError) Is(target error) bool { return target == ErrInsufficientStock }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *InsufficientStockError) Unwrap() error { return ErrInsufficientStock }

// InsufficientStock builds the typed refusal for one operation.
func InsufficientStock(field string, available, requested int64) error {
	return &InsufficientStockError{Field: field, Available: available, Requested: requested}
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
