package administrative

import "errors"

// Errors returned by division validation.
//
// This package is shared by the user, order, shipping and commission modules,
// so it owns its sentinel errors instead of importing any module's
// domain/error (Constitution I). Presentation layers map them to their own
// module codes, for example USER_UNKNOWN_PROVINCE in the user module.
var (
	// ErrUnknownProvince is returned when a province code is absent from the
	// bundled dataset.
	ErrUnknownProvince = errors.New("unknown province code")
	// ErrUnknownWard is returned when a ward code is absent from the bundled
	// dataset.
	ErrUnknownWard = errors.New("unknown ward code")
	// ErrWardProvinceMismatch is returned when the ward exists but belongs to a
	// different province, which means the two client selects are out of sync.
	ErrWardProvinceMismatch = errors.New("ward does not belong to the province")
)
