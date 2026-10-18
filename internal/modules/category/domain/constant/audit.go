package constant

// Audit actions emitted by the category module. The values are the action names
// persisted in audit_logs, so they are part of the audit contract: changing one
// makes historical rows undecodable and MUST be treated as a breaking change.
//
// Outcomes (SUCCESS / FAILURE) are not defined here; the shared audit package
// owns them so every module spells them the same way.
const (
	// AuditCategoryCreated is recorded when an administrator creates a category.
	AuditCategoryCreated = "CATEGORY_CREATED"
	// AuditCategoryUpdated is recorded when an administrator edits a category's
	// name, slug, description or position.
	AuditCategoryUpdated = "CATEGORY_UPDATED"
	// AuditCategoryHidden is recorded when a category is taken off display.
	AuditCategoryHidden = "CATEGORY_HIDDEN"
	// AuditCategoryShown is recorded when a category is put back on display.
	AuditCategoryShown = "CATEGORY_SHOWN"
	// AuditCategoryDeleted is recorded when a category is removed. Removal is a
	// hard delete (research D5); the audit entry is what survives it.
	AuditCategoryDeleted = "CATEGORY_DELETED"
	// AuditCategoryReordered is recorded when an administrator reorders the
	// catalogue-wide category list in one action. A bulk reorder has no single
	// target row, so the affected ids travel in the event metadata and the
	// target names the ordered scope (research D9).
	AuditCategoryReordered = "CATEGORY_REORDERED"
)
