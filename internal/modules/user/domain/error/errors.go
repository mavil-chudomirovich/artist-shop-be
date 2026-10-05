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
	// that another customer's address exists (FR-006, FR-013).
	ErrAddressNotFound = errors.New("address not found")

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
)
