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
)
