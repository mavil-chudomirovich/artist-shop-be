package constant

// Audit actions emitted by the inventory module. The values are the action names
// persisted in audit_logs, so they are part of the audit contract: changing one
// makes historical rows undecodable and MUST be treated as a breaking change.
//
// Outcomes (SUCCESS / FAILURE) are not defined here; the shared audit package
// owns them so every module spells them the same way.
const (
	// AuditInventoryRestocked is recorded when an administrator increases stock
	// (FR-001, FR-006).
	AuditInventoryRestocked = "INVENTORY_RESTOCKED"
	// AuditInventoryDamaged is recorded when an administrator decreases stock
	// (FR-002, FR-006).
	AuditInventoryDamaged = "INVENTORY_DAMAGED"
	// AuditInventoryAdjusted is recorded when an administrator corrects the count
	// to a recounted value (FR-003, FR-006).
	AuditInventoryAdjusted = "INVENTORY_ADJUSTED"
	// AuditInventorySaleApplied is recorded when an outside payment event turns a
	// hold into a sale. It names the event's source reference with no human actor,
	// so the event's inventory effect is auditable (FR-020, Constitution VI).
	AuditInventorySaleApplied = "INVENTORY_SALE_APPLIED"
)
