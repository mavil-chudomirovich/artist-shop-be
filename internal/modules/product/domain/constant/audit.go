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
	// AuditProductStateChanged is recorded when a product moves through a
	// sell-state transition.
	AuditProductStateChanged = "PRODUCT_STATE_CHANGED"
	// AuditProductImageAdded is recorded when a picture is attached to a product.
	AuditProductImageAdded = "PRODUCT_IMAGE_ADDED"
	// AuditProductImageRemoved is recorded when a picture is released.
	AuditProductImageRemoved = "PRODUCT_IMAGE_REMOVED"
	// AuditProductImagePrimarySet is recorded when a picture is made the product's
	// main one.
	AuditProductImagePrimarySet = "PRODUCT_IMAGE_PRIMARY_SET"
	// AuditProductDeleted is recorded when a product is removed. Removal is a hard
	// delete (research D13); the audit entry is what survives it.
	AuditProductDeleted = "PRODUCT_DELETED"
	// AuditVariantGroupCreated is recorded when an administrator creates a
	// variant group.
	AuditVariantGroupCreated = "PRODUCT_VARIANT_GROUP_CREATED"
	// AuditVariantGroupUpdated is recorded when an administrator edits a variant
	// group's editable fields.
	AuditVariantGroupUpdated = "PRODUCT_VARIANT_GROUP_UPDATED"
	// AuditVariantGroupDeleted is recorded when a variant group is removed. Its
	// options are never removed with it; they become ordinary products again.
	AuditVariantGroupDeleted = "PRODUCT_VARIANT_GROUP_DELETED"
	// AuditVariantGroupImageAdded is recorded when a picture is attached to a
	// variant group.
	AuditVariantGroupImageAdded = "PRODUCT_VARIANT_GROUP_IMAGE_ADDED"
	// AuditVariantGroupImageRemoved is recorded when a variant-group picture is
	// released.
	AuditVariantGroupImageRemoved = "PRODUCT_VARIANT_GROUP_IMAGE_REMOVED"
	// AuditVariantGroupImagePrimarySet is recorded when a picture is made the
	// variant group's main one.
	AuditVariantGroupImagePrimarySet = "PRODUCT_VARIANT_GROUP_IMAGE_PRIMARY_SET"
	// AuditProductVariantAttached is recorded when a product is attached to a
	// variant group as a labelled option. The product keeps its own price,
	// pictures, sell state and link.
	AuditProductVariantAttached = "PRODUCT_VARIANT_ATTACHED"
	// AuditProductVariantDetached is recorded when a product is detached from its
	// variant group and becomes an ordinary product again.
	AuditProductVariantDetached = "PRODUCT_VARIANT_DETACHED"
	// AuditProductVariantAdded is recorded when an administrator adds a variant
	// to a product.
	AuditProductVariantAdded = "PRODUCT_VARIANT_ADDED"
	// AuditProductVariantUpdated is recorded when an administrator edits a
	// variant's editable fields, including its sell state.
	AuditProductVariantUpdated = "PRODUCT_VARIANT_UPDATED"
	// AuditProductVariantRemoved is recorded when a variant is removed from its
	// product. Removal is a hard delete; the audit entry is what survives it.
	AuditProductVariantRemoved = "PRODUCT_VARIANT_REMOVED"
)
