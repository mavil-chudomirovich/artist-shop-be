// Package domainerr defines the user module's business sentinel errors.
//
// Codes in specs/003-user-profile/contracts/error-codes.md map to these errors
// in presentation/http; the domain never knows an HTTP status.
//
// The three division errors (unknown province, unknown ward, ward/province
// mismatch) are deliberately absent: they belong to
// internal/share/administrative, which is shared with the order, shipping and
// commission modules and therefore must not depend on a module's domain errors
// (Constitution I).
package domainerr

import "errors"

// Business errors returned by the user module.
var (
	// ErrUserNotFound is returned when no account carries the requested
	// identifier. Only the administrator lookup can produce it: a profile is a
	// column group on the users row, which cannot go missing.
	ErrUserNotFound = errors.New("user not found")
	// ErrAddressNotFound is returned for an unknown, hidden, or not-owned
	// address. All three cases share one error so the response never confirms
	// that another customer's address exists (FR-006, FR-013). It is also what the
	// address entity reports when a transition is refused on a hidden address: a
	// hidden row has left the account's visible set, so it is genuinely not an
	// address of that account any more.
	ErrAddressNotFound = errors.New("address not found")
	// ErrAddressInvalid is returned when a member of an address is structurally
	// unacceptable — a blank recipient name, province code, ward code or street
	// address, which the row declares NOT NULL and the specification declares
	// non-empty, or a recipient name or street address longer than the maxLength
	// the contract declares for it.
	//
	// It carries no new client-facing code: presentation maps it to the shared
	// VALIDATION_ERROR, the same code request-shape problems already use
	// (contracts/error-codes.md, "Codes deliberately not added"). What it does
	// carry is the offending member, because FR-020 requires the response to name
	// the field a client has to fix.
	ErrAddressInvalid = errors.New("invalid address")

	// ErrInvalidPhone is returned when a phone number is not a Vietnamese
	// mobile number after normalisation (FR-003).
	ErrInvalidPhone = errors.New("invalid phone number")

	// ErrAvatarTypeUnsupported is returned when the uploaded bytes are not
	// JPEG, PNG or WebP. The decision is made from the content, never from the
	// file name or the client-declared media type (FR-014).
	ErrAvatarTypeUnsupported = errors.New("avatar type unsupported")
	// ErrAvatarTooLarge is returned when the upload exceeds the configured
	// ceiling, checked before any byte reaches the media service (FR-015).
	ErrAvatarTooLarge = errors.New("avatar too large")
	// ErrMediaUnavailable is returned when the media service rejects or cannot
	// store the upload. The profile is left unchanged, so the client may retry
	// (FR-017).
	ErrMediaUnavailable = errors.New("media service unavailable")

	// ErrIncompleteAvatar is returned when an avatar reference is missing one of
	// the four values that are stored together. It is an internal consistency
	// failure rather than a client mistake, so presentation keeps it on the
	// shared INTERNAL_ERROR instead of adding a module code no client could act
	// on (contracts/error-codes.md, "Codes deliberately not added").
	ErrIncompleteAvatar = errors.New("incomplete avatar reference")
)

// AddressFieldError reports which member of an address is not acceptable. It is
// one typed carrier for ErrAddressInvalid rather than a family of per-member
// sentinels, so the module keeps a single error to map and a single one to test.
//
// Field holds the contract's member name, which is what FR-020 puts into the
// response detail; Issue is a short, non-sensitive explanation.
type AddressFieldError struct {
	// Field is the contract member name, such as "streetAddress".
	Field string
	// Issue explains what is wrong with it.
	Issue string
}

// Error implements the error interface.
func (e *AddressFieldError) Error() string {
	return ErrAddressInvalid.Error() + ": " + e.Field + " " + e.Issue
}

// Is makes errors.Is(err, ErrAddressInvalid) match this error, so a caller can
// branch on the single sentinel without knowing the carrier type.
func (e *AddressFieldError) Is(target error) bool { return target == ErrAddressInvalid }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *AddressFieldError) Unwrap() error { return ErrAddressInvalid }

// InvalidAddressField builds the typed rejection for one member.
func InvalidAddressField(field, issue string) error {
	return &AddressFieldError{Field: field, Issue: issue}
}
