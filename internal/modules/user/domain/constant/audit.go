// Package constant holds the user module's business constants.
package constant

// Audit actions emitted by the user module. The values are the action names
// persisted in audit_logs, so they are part of the audit contract: changing one
// makes historical rows undecodable and MUST be treated as a breaking change.
//
// Outcomes (SUCCESS / FAILURE) are not defined here; the shared audit package
// owns them so every module spells them the same way.
const (
	// AuditProfileUpdated is recorded when the display name or the phone changes,
	// including when either field is cleared (FR-019).
	AuditProfileUpdated = "USER_PROFILE_UPDATED"
	// AuditAvatarSet is recorded when an avatar is attached or replaced.
	AuditAvatarSet = "USER_AVATAR_SET"
	// AuditAvatarRemoved is recorded when an avatar is detached.
	AuditAvatarRemoved = "USER_AVATAR_REMOVED"
	// AuditAddressCreated is recorded when a shipping address is added.
	AuditAddressCreated = "USER_ADDRESS_CREATED"
	// AuditAddressUpdated is recorded when a shipping address is edited.
	AuditAddressUpdated = "USER_ADDRESS_UPDATED"
	// AuditAddressDeleted is recorded when a shipping address is hidden.
	AuditAddressDeleted = "USER_ADDRESS_DELETED"
	// AuditAddressDefaultSet is recorded when the default address changes,
	// including when it changes as a side effect of another operation.
	AuditAddressDefaultSet = "USER_ADDRESS_DEFAULT_SET"
	// AuditProfileViewedByAdmin is recorded when an administrator reads a
	// customer's contact details. Customer contact data leaves the customer's
	// control only with this trace (FR-022a).
	AuditProfileViewedByAdmin = "USER_PROFILE_VIEWED_BY_ADMIN"
)
