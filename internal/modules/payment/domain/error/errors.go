package domainerr

import (
	"errors"
	"fmt"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/payment/domain/constant"
)

// Business sentinel errors returned by the payment module. Each carries the
// message pieces presentation needs to answer the code in
// specs/011-order-payment/contracts/error-codes.md.
var (
	// ErrNotFound is returned for an unknown payment. It maps to PAYMENT_NOT_FOUND
	// (404).
	ErrNotFound = errors.New("payment not found")
	// ErrOrderNotFound is returned for an unknown order, and for one that belongs
	// to another customer. The two are one answer deliberately: a route that
	// answered differently would confirm another customer's order exists
	// (FR-017). It maps to PAYMENT_ORDER_NOT_FOUND (404).
	ErrOrderNotFound = errors.New("payment order not found")
	// ErrOrderNotPayable is returned when a payment is started for an order that
	// is not awaiting payment — already paid, cancelled, or still awaiting the
	// artist's confirmation (FR-002). It maps to PAYMENT_ORDER_NOT_PAYABLE (409).
	ErrOrderNotPayable = errors.New("order is not awaiting payment")
	// ErrAlreadyLive is returned when a live payment exists for the order but its
	// link can no longer be returned, so a new one cannot be created while the
	// stale row stands (FR-003). It maps to PAYMENT_ALREADY_LIVE (409).
	ErrAlreadyLive = errors.New("a live payment already exists")
	// ErrProviderFailure is returned when the provider refused to create the link
	// or was unreachable. Nothing is created and the order is unchanged (FR-002).
	// It maps to PAYMENT_PROVIDER_ERROR (422).
	ErrProviderFailure = errors.New("payment provider failure")
	// ErrStaleConfirmation is returned when a verified confirmation belongs to
	// content the order no longer carries — its stored order_version differs from
	// the order's current version, or the order is no longer awaiting payment — so
	// it is recorded as UNMATCHED and never applied (FR-012, research D8). It is
	// the single sentinel for the typed refusal built by StaleConfirmation.
	ErrStaleConfirmation = errors.New("stale payment confirmation")
	// ErrStateTransitionInvalid is returned when a move the payment's current
	// state does not allow is attempted. It is the single sentinel for the typed
	// refusals built by StateTransitionInvalid, so the module keeps one error to
	// map (FR-011, Constitution III).
	ErrStateTransitionInvalid = errors.New("payment state transition invalid")
	// ErrUnmatchedReasonRequired is returned when a payment is recorded UNMATCHED
	// without a reason; the storage check `(status = 'UNMATCHED') = (reason IS NOT
	// NULL)` requires one, and the operator's queue needs it (FR-012, FR-013).
	ErrUnmatchedReasonRequired = errors.New("unmatched reason is required")
	// ErrWebhookInvalidSignature is returned when a webhook's signature does not
	// verify, so the event is not from the provider and nothing is applied
	// (FR-019). It maps to PAYMENT_WEBHOOK_INVALID_SIGNATURE (401).
	ErrWebhookInvalidSignature = errors.New("payment webhook signature invalid")
	// ErrMalformedWebhook is returned when a webhook body could not be read as
	// the provider's envelope at all — it does not parse — so the signature check
	// never ran. It maps to the shared MALFORMED_REQUEST (400); the request is
	// retired rather than retried (contracts/error-codes.md).
	ErrMalformedWebhook = errors.New("payment webhook malformed")
	// ErrInvalidValue is returned when a value fails its own rule and the response
	// must name the member a client has to fix. It is the single sentinel for the
	// typed refusals built by InvalidValue. It maps to the shared VALIDATION_ERROR
	// (400).
	ErrInvalidValue = errors.New("invalid payment value")
	// ErrOrderCodeCollision is returned when the generated order code already
	// belongs to another payment. The caller retries with a fresh code, so a
	// collision never surfaces to a client (research D4).
	ErrOrderCodeCollision = errors.New("payment order code collision")
)

// StateTransitionError carries the move that was refused and the state that
// refused it, so the response can name the current state (FR-011).
type StateTransitionError struct {
	// From is the state the payment was in when the move was attempted.
	From constant.Status
	// To is the state the move asked for.
	To constant.Status
}

// Error implements the error interface. It names the current state.
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

// StaleConfirmationError carries the payment's stored order version and the
// order's current version, so the reconciliation reason can name the mismatch. It
// is the single typed carrier for ErrStaleConfirmation (FR-012, research D8).
type StaleConfirmationError struct {
	// OrderVersion is the version the payment was created for.
	OrderVersion int64
	// CurrentVersion is the order's content version now.
	CurrentVersion int64
}

// Error implements the error interface. It names both versions.
func (e *StaleConfirmationError) Error() string {
	return fmt.Sprintf("%s: payment is for order version %d, order is at %d",
		ErrStaleConfirmation, e.OrderVersion, e.CurrentVersion)
}

// Is makes errors.Is(err, ErrStaleConfirmation) match this error.
func (e *StaleConfirmationError) Is(target error) bool { return target == ErrStaleConfirmation }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *StaleConfirmationError) Unwrap() error { return ErrStaleConfirmation }

// StaleConfirmation builds the typed refusal naming the two versions.
func StaleConfirmation(orderVersion, currentVersion int64) error {
	return &StaleConfirmationError{OrderVersion: orderVersion, CurrentVersion: currentVersion}
}

// UnmatchedReasonError reports that a payment was recorded UNMATCHED without a
// reason. It is the single typed carrier for ErrUnmatchedReasonRequired.
type UnmatchedReasonError struct{}

// Error implements the error interface.
func (e *UnmatchedReasonError) Error() string { return ErrUnmatchedReasonRequired.Error() }

// Is makes errors.Is(err, ErrUnmatchedReasonRequired) match this error.
func (e *UnmatchedReasonError) Is(target error) bool { return target == ErrUnmatchedReasonRequired }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *UnmatchedReasonError) Unwrap() error { return ErrUnmatchedReasonRequired }

// UnmatchedReasonRequired builds the typed refusal for a missing reason.
func UnmatchedReasonRequired() error { return &UnmatchedReasonError{} }

// InvalidValueError reports which member of a request is not acceptable. It is one
// typed carrier for ErrInvalidValue rather than a family of per-member sentinels,
// so the module keeps a single error to map and a single one to test.
type InvalidValueError struct {
	// Field is the contract's member name, which presentation puts into the
	// response detail.
	Field string
	// Issue is a short, non-sensitive explanation.
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

// ProviderFailure wraps a provider transport failure in the module's sentinel, so
// presentation can answer PAYMENT_PROVIDER_ERROR without inspecting the provider's
// own error (FR-002).
func ProviderFailure(cause error) error {
	return fmt.Errorf("%w: %v", ErrProviderFailure, cause)
}
