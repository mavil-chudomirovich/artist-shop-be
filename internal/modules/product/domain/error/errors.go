// Package domainerr defines the product module's business sentinel errors.
//
// Codes in specs/015-product-sku-model/contracts/error-codes.md map to these
// errors in presentation/http; the domain never knows an HTTP status.
package domainerr

import (
	"errors"
	"fmt"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
)

// Business sentinel errors returned by the product module.
var (
	// ErrProductNotFound is returned for an unknown product, a product withheld
	// from customers (no on-sale variant, retired, or in a hidden category) and a
	// removed one. All share one error so the public response never confirms that
	// a hidden product exists (FR-008). The administrator surface uses it for the
	// unknown and removed cases.
	ErrProductNotFound = errors.New("product not found")
	// ErrProductImageLimitReached is returned when adding a picture would exceed
	// the ten-picture ceiling.
	ErrProductImageLimitReached = errors.New("product image limit reached")
	// ErrProductImageTypeUnsupported is returned when an uploaded payload begins
	// with no supported image signature (FR-019).
	ErrProductImageTypeUnsupported = errors.New("product image type unsupported")
	// ErrProductImageTooLarge is returned when an uploaded payload exceeds the
	// size ceiling (FR-019).
	ErrProductImageTooLarge = errors.New("product image too large")
	// ErrProductMediaUnavailable is returned when the media provider refused or
	// could not be reached, or the adapter has no credentials. It is the module's
	// only retryable error.
	ErrProductMediaUnavailable = errors.New("product media unavailable")
	// ErrProductInvalid is returned when a value fails its own rule: a blank
	// required member, a value over its bound, a code that is not URL-safe, a
	// price that is not positive, or a currency that is not three uppercase
	// letters.
	//
	// It carries no new client-facing code: presentation maps it to the shared
	// VALIDATION_ERROR, the same code request-shape problems already use. What it
	// carries is the offending member, because the response must name the field a
	// client has to fix.
	ErrProductInvalid = errors.New("invalid product")
	// ErrProductCodeTaken is returned when another product already uses the code,
	// compared after trimming surrounding whitespace and ignoring letter case
	// (FR-011). It is a catalogue-wide invariant, so the storage layer enforces
	// it and the adapter reports it here.
	ErrProductCodeTaken = errors.New("product code taken")
	// ErrVariantSKUTaken is returned when another variant already uses the SKU,
	// compared after trimming surrounding whitespace and ignoring letter case
	// (FR-011). It is a catalogue-wide invariant, so the storage layer enforces
	// it and the adapter reports it here.
	ErrVariantSKUTaken = errors.New("variant sku taken")
	// ErrVariantNotFound is returned for an unknown variant identifier, or one
	// that does not belong to the addressed product. A variant that exists but is
	// not buyable is not this error: a catalogue read simply omits it and a cart
	// add uses the cart's not-purchasable refusal.
	ErrVariantNotFound = errors.New("variant not found")
	// ErrVariantLastRemoval is returned when removing a product's last variant is
	// refused; a product must keep at least one variant (FR-012).
	ErrVariantLastRemoval = errors.New("variant last removal")
	// ErrVariantStateTransitionInvalid is the sentinel behind every refused
	// variant sell-state move; the message names the state the move started from
	// (FR-015).
	ErrVariantStateTransitionInvalid = errors.New("variant state transition invalid")
)

// VariantStateTransitionError carries the state a refused variant sell-state change
// started from, so the response can name it without the caller re-reading the
// variant (FR-015). It is the single typed carrier for
// ErrVariantStateTransitionInvalid, so the module keeps one sentinel to map and one
// type to test.
type VariantStateTransitionError struct {
	// Current is the state the variant was in when the move was attempted.
	Current constant.SellState
	// Requested is the target state the caller asked for.
	Requested constant.SellState
}

// Error implements the error interface. The current state is named because
// FR-015 requires the operator to be told which state made the move invalid.
func (e *VariantStateTransitionError) Error() string {
	return fmt.Sprintf("%s: cannot move from %q to %q",
		ErrVariantStateTransitionInvalid, e.Current, e.Requested)
}

// Is makes errors.Is(err, ErrVariantStateTransitionInvalid) match this error.
func (e *VariantStateTransitionError) Is(target error) bool {
	return target == ErrVariantStateTransitionInvalid
}

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *VariantStateTransitionError) Unwrap() error { return ErrVariantStateTransitionInvalid }

// InvalidVariantStateTransition builds the typed refusal for one attempted variant
// move.
func InvalidVariantStateTransition(current, requested constant.SellState) error {
	return &VariantStateTransitionError{Current: current, Requested: requested}
}

// ProductFieldError reports which member of a product or variant is not
// acceptable. It is one typed carrier for ErrProductInvalid rather than a family of
// per-member sentinels, so the module keeps a single error to map and a single one
// to test.
//
// Field holds the contract's member name, which is what presentation puts into the
// response detail; Issue is a short, non-sensitive explanation.
type ProductFieldError struct {
	// Field is the contract member name, such as "code".
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
