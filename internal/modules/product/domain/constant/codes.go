package constant

// Stable, machine-readable error codes of the product module. They mirror
// specs/006-product-catalog/contracts/error-codes.md and
// specs/014-product-variants/contracts/error-codes.md one for one and are mapped to
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
	// CodeVariantGroupNotFound covers an unknown variant-group identifier or
	// slug, a group hidden because its category is hidden or none of its options
	// is visible, and a removed group. On the public surface the situations are
	// deliberately one answer (FR-008).
	CodeVariantGroupNotFound = "VARIANT_GROUP_NOT_FOUND"
	// CodeVariantGroupSlugTaken reports that another variant group already uses
	// the slug, ignoring letter case and surrounding whitespace (FR-019).
	CodeVariantGroupSlugTaken = "VARIANT_GROUP_SLUG_TAKEN"
	// CodeVariantGroupHasOptions reports that a group still has options and
	// cannot be removed until they are detached (FR-025).
	CodeVariantGroupHasOptions = "VARIANT_GROUP_HAS_OPTIONS"
	// CodeVariantLabelTaken reports that another option of the same group already
	// uses the label (compared after trimming surrounding whitespace, FR-021).
	CodeVariantLabelTaken = "VARIANT_LABEL_TAKEN"
	// CodeProductAlreadyInVariantGroup reports that the product is already an
	// option of a group, so attaching it to another is refused — a product
	// belongs to at most one group (FR-021).
	CodeProductAlreadyInVariantGroup = "PRODUCT_ALREADY_IN_VARIANT_GROUP"
)
