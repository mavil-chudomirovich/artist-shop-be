package constant

// Stable, machine-readable error codes of the user module. They mirror
// specs/003-user-profile/contracts/error-codes.md one for one and are mapped to
// HTTP status codes by presentation/http/errors.go. Clients branch on
// error.code, never on error.message.
//
// A code is part of the module's public contract: renaming one breaks every
// client that already handles it.
const (
	// CodeUserNotFound is returned by the administrator lookup when no account
	// carries the requested identifier.
	CodeUserNotFound = "USER_NOT_FOUND"
	// CodeAddressNotFound covers unknown, hidden and not-owned addresses alike,
	// so the response never confirms that another customer's address exists.
	CodeAddressNotFound = "USER_ADDRESS_NOT_FOUND"
	// CodeInvalidPhone reports a phone number that is not a valid Vietnamese
	// mobile number.
	CodeInvalidPhone = "USER_INVALID_PHONE"
	// CodeUnknownProvince reports a province code that is absent from the
	// official dataset.
	CodeUnknownProvince = "USER_UNKNOWN_PROVINCE"
	// CodeUnknownWard reports a ward code that is absent from the official
	// dataset.
	CodeUnknownWard = "USER_UNKNOWN_WARD"
	// CodeWardProvinceMismatch reports a ward that exists but belongs to another
	// province, which means the two client selects are out of sync.
	CodeWardProvinceMismatch = "USER_WARD_PROVINCE_MISMATCH"
	// CodeAvatarTypeUnsupported reports uploaded bytes that are not JPEG, PNG or
	// WebP.
	CodeAvatarTypeUnsupported = "USER_AVATAR_TYPE_UNSUPPORTED"
	// CodeAvatarTooLarge reports an upload above the configured ceiling.
	CodeAvatarTooLarge = "USER_AVATAR_TOO_LARGE"
	// CodeMediaUnavailable reports that the media service refused or could not
	// store the upload. It is the only retryable code of this module: the
	// previous avatar was left untouched.
	CodeMediaUnavailable = "USER_MEDIA_UNAVAILABLE"
)
