package constant

// Stable, machine-readable error codes of the product module. They mirror
// specs/015-product-sku-model/contracts/error-codes.md one for one and are mapped
// to HTTP status codes by presentation/http/errors.go. Clients branch on
// error.code, never on error.message.
//
// A code is part of the module's public contract: renaming one breaks every client
// that already handles it.
const (
	// CodeProductNotFound covers an unknown identifier or code, a product hidden
	// because no variant is on sale, a product whose category is hidden, and a
	// removed product. On the public surface the situations are deliberately one
	// answer (FR-008).
	CodeProductNotFound = "PRODUCT_NOT_FOUND"
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
	// CodeProductHasPendingOrders reports that turning the product's visibility
	// flag off was refused because at least one PENDING or PAYMENT_PENDING order
	// holds one of its variants. The refusal names the blocking orders so an
	// operator can act on them (specs/016-hide-guard).
	CodeProductHasPendingOrders = "PRODUCT_HAS_PENDING_ORDERS"
	// CodeVariantHasPendingOrders reports that moving a variant away from its
	// on-sale state was refused for the same reason (specs/016-hide-guard).
	CodeVariantHasPendingOrders = "VARIANT_HAS_PENDING_ORDERS"
	// CodeProductReorderDuplicateID reports that the submitted product reorder
	// names the same product more than once (specs/018-catalog-reorder). It is a
	// request-shape failure, so the answer is 400 with a code distinct from the
	// completeness refusal.
	CodeProductReorderDuplicateID = "PRODUCT_REORDER_DUPLICATE_ID"
	// CodeVariantReorderDuplicateID reports that the submitted variant reorder
	// names the same variant more than once (specs/018-catalog-reorder).
	CodeVariantReorderDuplicateID = "VARIANT_REORDER_DUPLICATE_ID"
	// CodeVariantBelongsToOtherProduct reports that a variant reorder names a
	// variant that does not belong to the product on the path
	// (specs/018-catalog-reorder). It is a request-shape failure, so the answer
	// is 400 and the detail names the offending member.
	CodeVariantBelongsToOtherProduct = "VARIANT_BELONGS_TO_OTHER_PRODUCT"
)
