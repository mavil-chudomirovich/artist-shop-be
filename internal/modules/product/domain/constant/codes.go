package constant

// Stable, machine-readable error codes of the product module. They mirror
// specs/015-product-sku-model/contracts/error-codes.md one for one and are mapped
// to HTTP status codes by presentation/http/errors.go. Clients branch on
// error.code, never on error.message.
//
// A code is part of the module's public contract: renaming one breaks every client
// that already handles it.
const (
	// CodeProductNotFound covers an unknown identifier or slug, a product hidden
	// because no variant is on sale, a product whose category is hidden, and a
	// removed product. On the public surface the situations are deliberately one
	// answer (FR-008).
	CodeProductNotFound = "PRODUCT_NOT_FOUND"
	// CodeProductSlugTaken reports that another product already uses the slug,
	// ignoring letter case and surrounding whitespace.
	CodeProductSlugTaken = "PRODUCT_SLUG_TAKEN"
	// CodeProductCodeTaken reports that another product already uses the product
	// code, compared after trimming surrounding whitespace and ignoring letter
	// case (FR-011).
	CodeProductCodeTaken = "PRODUCT_CODE_TAKEN"
	// CodeProductImageLimitReached reports that the product already carries the
	// maximum of ten gallery pictures.
	CodeProductImageLimitReached = "PRODUCT_IMAGE_LIMIT_REACHED"
	// CodeProductImageTypeUnsupported reports upload bytes that are not JPEG, PNG
	// or WebP, decided by the content's own signature (FR-019).
	CodeProductImageTypeUnsupported = "PRODUCT_IMAGE_TYPE_UNSUPPORTED"
	// CodeProductImageTooLarge reports an upload over the 2 MB ceiling (FR-019).
	CodeProductImageTooLarge = "PRODUCT_IMAGE_TOO_LARGE"
	// CodeProductMediaUnavailable reports that the media provider refused or could
	// not be reached, or has no credentials. It is the module's only retryable
	// code.
	CodeProductMediaUnavailable = "PRODUCT_MEDIA_UNAVAILABLE"
	// CodeVariantSKUTaken reports that another variant already uses the SKU,
	// compared after trimming surrounding whitespace and ignoring letter case
	// (FR-011).
	CodeVariantSKUTaken = "VARIANT_SKU_TAKEN"
	// CodeVariantNotFound covers an unknown variant identifier, or one that does
	// not belong to the addressed product. A variant that exists but is not
	// buyable is not this code: on a catalogue read it is simply not returned, and
	// on a cart add it is the cart's not-purchasable refusal.
	CodeVariantNotFound = "VARIANT_NOT_FOUND"
	// CodeVariantLastRemoval reports that removing the variant is refused because
	// it is the product's last one; a product must keep at least one variant
	// (FR-012).
	CodeVariantLastRemoval = "VARIANT_LAST_REMOVAL"
	// CodeVariantStateTransitionInvalid reports a sell-state change the variant's
	// current state does not allow. The message names the current state (FR-015).
	CodeVariantStateTransitionInvalid = "VARIANT_STATE_TRANSITION_INVALID"
)
