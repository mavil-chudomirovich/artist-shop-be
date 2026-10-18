package constant

// Audit actions emitted by the product module. The values are the action names
// persisted in audit_logs, so they are part of the audit contract: changing one
// makes historical rows undecodable and MUST be treated as a breaking change.
//
// Outcomes (SUCCESS / FAILURE) are not defined here; the shared audit package owns
// them so every module spells them the same way.
const (
	// AuditProductCreated is recorded when an administrator creates a product.
	AuditProductCreated = "PRODUCT_CREATED"
	// AuditProductUpdated is recorded when an administrator edits a product's
	// editable fields.
	AuditProductUpdated = "PRODUCT_UPDATED"
	// AuditProductImageAdded is recorded when a picture is attached to a product's
	// gallery.
	AuditProductImageAdded = "PRODUCT_IMAGE_ADDED"
	// AuditProductImageRemoved is recorded when a gallery picture is released.
	AuditProductImageRemoved = "PRODUCT_IMAGE_REMOVED"
	// AuditProductImagePrimarySet is recorded when a gallery picture is made the
	// product's main one.
	AuditProductImagePrimarySet = "PRODUCT_IMAGE_PRIMARY_SET"
	// AuditProductDeleted is recorded when a product is removed. Removal is a hard
	// delete; the audit entry is what survives it.
	AuditProductDeleted = "PRODUCT_DELETED"
	// AuditProductVariantAdded is recorded when an administrator adds a variant to
	// a product.
	AuditProductVariantAdded = "PRODUCT_VARIANT_ADDED"
	// AuditProductVariantUpdated is recorded when an administrator edits a
	// variant's editable fields, its image or its sell state.
	AuditProductVariantUpdated = "PRODUCT_VARIANT_UPDATED"
	// AuditProductVariantRemoved is recorded when a variant is removed from its
	// product. Removal is a hard delete; the audit entry is what survives it.
	AuditProductVariantRemoved = "PRODUCT_VARIANT_REMOVED"
	// AuditProductReordered is recorded when an administrator reorders the
	// catalogue-wide product list in one action. A bulk reorder has no single
	// target row, so the affected ids travel in the event metadata and the
	// target names the ordered scope (research D9).
	AuditProductReordered = "PRODUCT_REORDERED"
	// AuditProductVariantsReordered is recorded when an administrator reorders
	// the variants of one product. The event targets the parent product and the
	// affected variant ids travel in the metadata (research D9).
	AuditProductVariantsReordered = "PRODUCT_VARIANTS_REORDERED"
)
