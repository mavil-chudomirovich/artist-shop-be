// Package domainerr defines the product module's business sentinel errors.
//
// Codes in specs/006-product-catalog/contracts/error-codes.md map to these errors
// in presentation/http; the domain never knows an HTTP status.
package domainerr

import (
	"errors"
	"fmt"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
)

// Business sentinel errors returned by the product module.
var (
	// ErrProductNotFound is returned for an unknown product, a product withheld
	// from customers (not on sale and not a pre-order, retired, or in a hidden
	// category) and a removed one. All share one error so the public response
	// never confirms that a hidden product exists (FR-003). The administrator
	// surface uses it for the unknown and removed cases.
	ErrProductNotFound = errors.New("product not found")
	// ErrProductSlugTaken is returned when another product already uses the slug,
	// compared after trimming surrounding whitespace and ignoring letter case
	// (FR-028). It is a catalogue-wide invariant, so the storage layer enforces it
	// and the adapter reports it here.
	ErrProductSlugTaken = errors.New("product slug taken")
	// ErrProductStateTransitionInvalid is the sentinel behind every refused
	// sell-state move. It is paired with StateTransitionError, which carries the
	// state the move started from (FR-024).
	ErrProductStateTransitionInvalid = errors.New("product state transition invalid")
	// ErrProductImageLimitReached is returned when adding a picture would exceed
	// the ten-picture ceiling (FR-020).
	ErrProductImageLimitReached = errors.New("product image limit reached")
	// ErrProductImageTypeUnsupported is returned when an uploaded payload begins
	// with no supported image signature (FR-019).
	ErrProductImageTypeUnsupported = errors.New("product image type unsupported")
	// ErrProductImageTooLarge is returned when an uploaded payload exceeds the
	// size ceiling (FR-019).
	ErrProductImageTooLarge = errors.New("product image too large")
	// ErrProductMediaUnavailable is returned when the media provider refused or
	// could not be reached, or the adapter has no credentials. It is the module's
	// only retryable error (FR-018).
	ErrProductMediaUnavailable = errors.New("product media unavailable")
	// ErrProductInvalid is returned when a value fails its own rule: a blank
	// required member, a value over its bound, a slug that is not URL-safe, a
	// price that is not positive, or a currency that is not three uppercase
	// letters.
	//
	// It carries no new client-facing code: presentation maps it to the shared
	// VALIDATION_ERROR, the same code request-shape problems already use. What it
	// carries is the offending member, because FR-024, FR-028, FR-030 and FR-032
	// require the response to name the field a client has to fix.
	ErrProductInvalid = errors.New("invalid product")
)

// StateTransitionError carries the state a refused sell-state change started from,
// so the response can name it without the caller re-reading the product (FR-024).
// It is the single typed carrier for ErrProductStateTransitionInvalid, so the
// module keeps one sentinel to map and one type to test.
type StateTransitionError struct {
	// Current is the state the product was in when the move was attempted.
	Current constant.SellState
	// Requested is the target state the caller asked for.
	Requested constant.SellState
}

// Error implements the error interface. The current state is named because
// FR-024 requires the operator to be told which state made the move invalid.
func (e *StateTransitionError) Error() string {
	return fmt.Sprintf("%s: cannot move from %q to %q",
		ErrProductStateTransitionInvalid, e.Current, e.Requested)
}

// Is makes errors.Is(err, ErrProductStateTransitionInvalid) match this error.
func (e *StateTransitionError) Is(target error) bool {
	return target == ErrProductStateTransitionInvalid
}

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *StateTransitionError) Unwrap() error { return ErrProductStateTransitionInvalid }

// InvalidStateTransition builds the typed refusal for one attempted move.
func InvalidStateTransition(current, requested constant.SellState) error {
	return &StateTransitionError{Current: current, Requested: requested}
}

// ProductFieldError reports which member of a product is not acceptable. It is one
// typed carrier for ErrProductInvalid rather than a family of per-member
// sentinels, so the module keeps a single error to map and a single one to test.
//
// Field holds the contract's member name, which is what presentation puts into the
// response detail; Issue is a short, non-sensitive explanation.
type ProductFieldError struct {
	// Field is the contract member name, such as "slug".
	Field string
	// Issue explains what is wrong with it.
	Issue string
}

// Error implements the error interface.
func (e *ProductFieldError) Error() string {
	return ErrProductInvalid.Error() + ": " + e.Field + " " + e.Issue
}

// Is makes errors.Is(err, ErrProductInvalid) match this error, so a caller can
// branch on the single sentinel without knowing the carrier type.
func (e *ProductFieldError) Is(target error) bool { return target == ErrProductInvalid }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *ProductFieldError) Unwrap() error { return ErrProductInvalid }

// InvalidProductField builds the typed rejection for one member.
func InvalidProductField(field, issue string) error {
	return &ProductFieldError{Field: field, Issue: issue}
}
