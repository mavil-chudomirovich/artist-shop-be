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
	// ErrNotEditable is returned when a customer tries to change an order that is
	// neither awaiting the artist nor awaiting payment — it is paid or beyond
	// (FR-012). It maps to ORDER_NOT_EDITABLE (409).
	ErrNotEditable = errors.New("order is not editable")
	// ErrEmptyOrder is returned when an edit would leave the order with no line.
	// An order always carries at least one line; the customer cancels instead
	// (FR-015). It maps to ORDER_EMPTY (409).
	ErrEmptyOrder = errors.New("order is empty")
	// ErrStalePayment is returned when a confirmed payment asks for the paid
	// transition of content the order no longer carries: the order's content
	// version differs from the version the payment was created for, because the
	// order was edited and re-confirmed after the payment link was made. It is a
	// distinct refusal from a state refusal (ErrStateTransitionInvalid) so module
	// 08 can tell a superseded confirmation apart and never apply it (FR-009,
	// FR-012, research D5). It maps to ORDER_STATE_TRANSITION_INVALID (409) at the
	// order level; the payment module records it as UNMATCHED.
	ErrStalePayment = errors.New("order payment is stale")
	// ErrImageTypeUnsupported is returned when an uploaded delivery-evidence
	// photo is not JPEG, PNG or WebP, decided by sniffing its leading bytes and
	// never by a file name or a client-declared media type (FR-009). It maps to
	// ORDER_IMAGE_TYPE_UNSUPPORTED (400).
	ErrImageTypeUnsupported = errors.New("order image type unsupported")
	// ErrImageTooLarge is returned when an uploaded delivery-evidence photo
	// exceeds the size ceiling (FR-009). It maps to ORDER_IMAGE_TOO_LARGE (413).
	ErrImageTooLarge = errors.New("order image too large")
	// ErrImageLimitReached is returned when more than five photos are attached
	// to one delivery step (FR-006, FR-008). It maps to
	// ORDER_IMAGE_LIMIT_REACHED (409).
	ErrImageLimitReached = errors.New("order image limit reached")
	// ErrMediaUnavailable is returned when the media provider refuses a delivery
	// photo or cannot be reached while one is being stored, including the case
	// where the composition wired no store at all. It is a retryable outage, not
	// a client mistake: the order is left unchanged (FR-009). It mirrors module
	// 02's ErrMediaUnavailable and module 04's ErrProductMediaUnavailable, and it
	// maps to ORDER_MEDIA_UNAVAILABLE (503).
	ErrMediaUnavailable = errors.New("media service unavailable")
	// ErrFeedbackNotAllowed is returned when feedback is submitted while the
	// order's state does not allow it — it is not DELIVERED or COMPLETED
	// (FR-015). It is the single sentinel for the typed refusals built by
	// FeedbackNotAllowed, which carry the current state, so the module keeps one
	// error to map. It maps to ORDER_FEEDBACK_NOT_ALLOWED (409).
	ErrFeedbackNotAllowed = errors.New("order feedback not allowed")
	// ErrFeedbackAlreadySent is returned when a second feedback is submitted for
	// an order that already has one (FR-016). It maps to
	// ORDER_FEEDBACK_ALREADY_SENT (409).
	ErrFeedbackAlreadySent = errors.New("order feedback already sent")
)

// ItemNotPurchasableError carries the variant that cannot be bought, so the
// response can name it without the caller re-reading the catalogue
// (error-codes.md, "Where the offending item is named").
type ItemNotPurchasableError struct {
	// VariantID is the variant that is not on sale or was removed.
	VariantID uuid.UUID
}

// Error implements the error interface.
func (e *ItemNotPurchasableError) Error() string {
	return ErrItemNotPurchasable.Error() + ": " + e.VariantID.String()
}

// Is makes errors.Is(err, ErrItemNotPurchasable) match this error.
func (e *ItemNotPurchasableError) Is(target error) bool { return target == ErrItemNotPurchasable }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *ItemNotPurchasableError) Unwrap() error { return ErrItemNotPurchasable }

// ItemNotPurchasable builds the typed refusal for one variant.
func ItemNotPurchasable(variantID uuid.UUID) error {
	return &ItemNotPurchasableError{VariantID: variantID}
}

// ItemPriceChangedError carries the variant whose price moved, so the response
// can name the line the customer must review (FR-004).
type ItemPriceChangedError struct {
	// VariantID is the variant whose price differs from the cart's snapshot.
	VariantID uuid.UUID
}

// Error implements the error interface.
func (e *ItemPriceChangedError) Error() string {
	return ErrItemPriceChanged.Error() + ": " + e.VariantID.String()
}

// Is makes errors.Is(err, ErrItemPriceChanged) match this error.
func (e *ItemPriceChangedError) Is(target error) bool { return target == ErrItemPriceChanged }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *ItemPriceChangedError) Unwrap() error { return ErrItemPriceChanged }

// ItemPriceChanged builds the typed refusal for one variant.
func ItemPriceChanged(variantID uuid.UUID) error {
	return &ItemPriceChangedError{VariantID: variantID}
}

// QuantityExceedsAvailableError carries what the refusal is about, so the response
// can state how far the customer must reduce the line. It is the single typed
// carrier for ErrQuantityExceedsAvailable (FR-005, FR-017).
type QuantityExceedsAvailableError struct {
	// VariantID is the variant the line is for.
	VariantID uuid.UUID
	// Available is what could have been taken when the shelf said no.
	Available int64
	// Requested is the quantity the operation asked for.
	Requested int64
}

// Error implements the error interface. It names the two amounts, which is what
// the customer must be told.
func (e *QuantityExceedsAvailableError) Error() string {
	return fmt.Sprintf("%s: %s requested %d, available %d",
		ErrQuantityExceedsAvailable, e.VariantID, e.Requested, e.Available)
}

// Is makes errors.Is(err, ErrQuantityExceedsAvailable) match this error.
func (e *QuantityExceedsAvailableError) Is(target error) bool {
	return target == ErrQuantityExceedsAvailable
}

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *QuantityExceedsAvailableError) Unwrap() error { return ErrQuantityExceedsAvailable }

// QuantityExceedsAvailable builds the typed refusal for one operation.
func QuantityExceedsAvailable(variantID uuid.UUID, available, requested int64) error {
	return &QuantityExceedsAvailableError{VariantID: variantID, Available: available, Requested: requested}
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

// FeedbackNotAllowedError carries the state that refused a feedback submission,
// so the response can name the current state (FR-015). It is the single typed
// carrier for ErrFeedbackNotAllowed.
type FeedbackNotAllowedError struct {
	// From is the state the order was in when feedback was attempted.
	From constant.Status
}

// Error implements the error interface. It names the current state, which is what
// FR-015 requires the caller to be told.
func (e *FeedbackNotAllowedError) Error() string {
	return fmt.Sprintf("%s: the order is %s, not delivered", ErrFeedbackNotAllowed, e.From)
}

// Is makes errors.Is(err, ErrFeedbackNotAllowed) match this error.
func (e *FeedbackNotAllowedError) Is(target error) bool { return target == ErrFeedbackNotAllowed }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *FeedbackNotAllowedError) Unwrap() error { return ErrFeedbackNotAllowed }

// FeedbackNotAllowed builds the typed refusal for one order state.
func FeedbackNotAllowed(from constant.Status) error {
	return &FeedbackNotAllowedError{From: from}
}

// DeliveryPhotoLimitError carries the delivery step that reached its photo
// ceiling, so the refusal can name the step the operator must trim. It is the
// typed carrier for ErrImageLimitReached (FR-006, FR-008). The stage is carried as
// its stored value rather than as the model's Stage type, because model imports
// this package and importing model back would be a cycle.
type DeliveryPhotoLimitError struct {
	// Stage is the delivery step that is full (SHIPPING or DELIVERED).
	Stage string
}

// Error implements the error interface. It names the full step, which is what the
// operator must act on.
func (e *DeliveryPhotoLimitError) Error() string {
	return fmt.Sprintf("%s: the %s step already holds its maximum photos", ErrImageLimitReached, e.Stage)
}

// Is makes errors.Is(err, ErrImageLimitReached) match this error.
func (e *DeliveryPhotoLimitError) Is(target error) bool { return target == ErrImageLimitReached }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *DeliveryPhotoLimitError) Unwrap() error { return ErrImageLimitReached }

// DeliveryPhotoLimit builds the typed refusal for one delivery step.
func DeliveryPhotoLimit(stage string) error {
	return &DeliveryPhotoLimitError{Stage: stage}
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

// StalePaymentError carries the order's current content version and the version
// the confirmation expected, so the refusal can name the mismatch. It is the
// single typed carrier for ErrStalePayment (FR-009, FR-012, research D5).
type StalePaymentError struct {
	// CurrentVersion is the order's content version now.
	CurrentVersion int64
	// ExpectedVersion is the version the confirmed payment was created for.
	ExpectedVersion int64
}

// Error implements the error interface. It names both versions, which is what the
// reader needs to see that the confirmation belongs to superseded content.
func (e *StalePaymentError) Error() string {
	return fmt.Sprintf("%s: order is at version %d, confirmation expects %d",
		ErrStalePayment, e.CurrentVersion, e.ExpectedVersion)
}

// Is makes errors.Is(err, ErrStalePayment) match this error.
func (e *StalePaymentError) Is(target error) bool { return target == ErrStalePayment }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *StalePaymentError) Unwrap() error { return ErrStalePayment }

// StalePayment builds the typed refusal naming the two versions.
func StalePayment(current, expected int64) error {
	return &StalePaymentError{CurrentVersion: current, ExpectedVersion: expected}
}
