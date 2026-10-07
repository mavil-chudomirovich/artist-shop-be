// Package domainerr defines the category module's business sentinel errors.
//
// Codes in specs/005-category-catalog/contracts/error-codes.md map to these
// errors in presentation/http; the domain never knows an HTTP status.
package domainerr

import "errors"

// Business errors returned by the category module.
var (
	// ErrCategoryNotFound is returned for an unknown category, a category that
	// is withheld from customers, or one that has been removed. All three share
	// one error so the public response never confirms that a withheld category
	// exists (FR-005, SC-004). The administrator surface uses it for the unknown
	// and removed cases.
	ErrCategoryNotFound = errors.New("category not found")
	// ErrCategoryNameTaken is returned when another category already uses the
	// name, compared after trimming surrounding whitespace and ignoring letter
	// case (FR-016, FR-021). It is a catalogue-wide invariant, so the storage
	// layer is what ultimately enforces it and the adapter reports it here.
	ErrCategoryNameTaken = errors.New("category name taken")
	// ErrCategorySlugTaken is returned when another category already uses the
	// slug, compared the same way (FR-017, FR-021).
	ErrCategorySlugTaken = errors.New("category slug taken")
	// ErrCategoryInvalid is returned when a value fails its own rule: a blank
	// required member, a value longer than its bound, or a slug that is not
	// URL-safe.
	//
	// It carries no new client-facing code: presentation maps it to the shared
	// VALIDATION_ERROR, the same code request-shape problems already use
	// (contracts/error-codes.md, "Codes deliberately reused"). What it does
	// carry is the offending member, because FR-018, FR-019 and FR-020 require
	// the response to name the field a client has to fix.
	ErrCategoryInvalid = errors.New("invalid category")
)

// CategoryFieldError reports which member of a category is not acceptable. It
// is one typed carrier for ErrCategoryInvalid rather than a family of
// per-member sentinels, so the module keeps a single error to map and a single
// one to test.
//
// Field holds the contract's member name, which is what FR-020 puts into the
// response detail; Issue is a short, non-sensitive explanation.
type CategoryFieldError struct {
	// Field is the contract member name, such as "slug".
	Field string
	// Issue explains what is wrong with it.
	Issue string
}

// Error implements the error interface.
func (e *CategoryFieldError) Error() string {
	return ErrCategoryInvalid.Error() + ": " + e.Field + " " + e.Issue
}

// Is makes errors.Is(err, ErrCategoryInvalid) match this error, so a caller can
// branch on the single sentinel without knowing the carrier type.
func (e *CategoryFieldError) Is(target error) bool { return target == ErrCategoryInvalid }

// Unwrap exposes the sentinel to errors.Is and errors.As.
func (e *CategoryFieldError) Unwrap() error { return ErrCategoryInvalid }

// InvalidCategoryField builds the typed rejection for one member.
func InvalidCategoryField(field, issue string) error {
	return &CategoryFieldError{Field: field, Issue: issue}
}
