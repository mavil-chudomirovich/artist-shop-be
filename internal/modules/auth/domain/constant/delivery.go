package constant

// Delivery classifications recorded when the message provider refuses a message.
// The values are this system's own vocabulary
// (specs/004-fix-pending-defects/contracts/contract-changes.md §4): the
// provider's wording is never stored, so a provider editing its copy cannot
// change what an operator reads (FR-017, FR-022).
const (
	// DeliveryTransient means the provider reported that it is temporarily
	// unable; a retry may succeed.
	DeliveryTransient = "TRANSIENT"
	// DeliveryUnreachable means the provider could not be contacted at all; a
	// retry may succeed.
	DeliveryUnreachable = "UNREACHABLE"
	// DeliveryConfiguration means the provider rejected because this deployment
	// is not set up correctly; a retry cannot change the outcome (FR-016).
	DeliveryConfiguration = "CONFIGURATION"
	// DeliveryRefused means the provider answered that the message itself was
	// unacceptable; a retry cannot change the outcome either.
	DeliveryRefused = "REFUSED"
	// DeliveryUnknown means the failure carried no category this system
	// recognises. It is recorded rather than guessed and is never retried.
	DeliveryUnknown = "UNKNOWN"
)
