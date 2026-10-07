package constant

// Stable, machine-readable error codes of the product module. They mirror
// specs/006-product-catalog/contracts/error-codes.md one for one and are mapped to
// HTTP status codes by presentation/http/errors.go. Clients branch on error.code,
// never on error.message.
//
// A code is part of the module's public contract: renaming one breaks every client
// that already handles it.
const (
	// CodeProductNotFound covers an unknown identifier or slug, a product hidden
	// because it is not on sale and not a pre-order, a retired product, a product
	// whose category is hidden, and a removed product. On the public surface the
	// situations are deliberately one answer (FR-003).
	CodeProductNotFound = "PRODUCT_NOT_FOUND"
	// CodeProductSlugTaken reports that another product already uses the slug,
	// ignoring letter case and surrounding whitespace (FR-028).
	CodeProductSlugTaken = "PRODUCT_SLUG_TAKEN"
	// CodeProductStateTransitionInvalid reports a sell-state change the current
	// state does not allow. The message names the current state (FR-024).
	CodeProductStateTransitionInvalid = "PRODUCT_STATE_TRANSITION_INVALID"
	// CodeProductImageLimitReached reports that the product already carries the
	// maximum of ten pictures (FR-020).
	CodeProductImageLimitReached = "PRODUCT_IMAGE_LIMIT_REACHED"
	// CodeProductImageTypeUnsupported reports upload bytes that are not JPEG, PNG
	// or WebP, decided by the content's own signature (FR-019).
	CodeProductImageTypeUnsupported = "PRODUCT_IMAGE_TYPE_UNSUPPORTED"
	// CodeProductImageTooLarge reports an upload over the 2 MB ceiling (FR-019).
	CodeProductImageTooLarge = "PRODUCT_IMAGE_TOO_LARGE"
	// CodeProductMediaUnavailable reports that the media provider refused or could
	// not be reached, or has no credentials. It is the module's only retryable
	// code (FR-018).
	CodeProductMediaUnavailable = "PRODUCT_MEDIA_UNAVAILABLE"
)
