package constant

// Audit actions and outcomes emitted by the auth module.
const (
	// AuditRegister is recorded when a registration created an account *and* its
	// verification message was delivered.
	AuditRegister = "AUTH_REGISTER"
	// AuditRegisterDeliveryFailed is recorded with OutcomeFailure when a
	// registration could not deliver its verification message. It is recorded
	// whether the request created the account or the account was already there
	// and still pending, because both branches answer the same failure and an
	// outage must never be invisible (FR-008a, FR-010). The action names the
	// registration event that did not complete, which is true either way; it does
	// not claim a creation, which only one of the two did. When the account was
	// created by this request it stays, so the creation is a real state change
	// that has to be reconcilable (FR-011). The action is distinct from
	// AuditRegister because the outcome recorded alongside it is a failure, and
	// the reason is carried in metadata by classification rather than in the
	// action name.
	AuditRegisterDeliveryFailed = "AUTH_REGISTER_DELIVERY_FAILED"
	AuditEmailVerified          = "AUTH_EMAIL_VERIFIED"
	// AuditRegistrationStarted is recorded when a registration validates its
	// input and holds the temporary registration successfully (FR-020).
	AuditRegistrationStarted = "AUTH_REGISTRATION_STARTED"
	// AuditRegistrationCancelled is recorded when a temporary registration is
	// explicitly cancelled (FR-020).
	AuditRegistrationCancelled = "AUTH_REGISTRATION_CANCELLED"
	// AuditRegistrationFailed is recorded when a submitted code is rejected, so
	// a confirmation attempt that did not succeed is never invisible (FR-020).
	AuditRegistrationFailed = "AUTH_REGISTRATION_FAILED"
	// AuditRegistrationResent is recorded when a resend replaces the active code
	// (FR-020).
	AuditRegistrationResent = "AUTH_REGISTRATION_RESENT"
	AuditSignInSucceeded    = "AUTH_SIGN_IN_SUCCEEDED"
	AuditSignInFailed       = "AUTH_SIGN_IN_FAILED"
	AuditSignOut            = "AUTH_SIGN_OUT"
	AuditSessionRefreshed   = "AUTH_SESSION_REFRESHED"
	AuditRefreshReused      = "AUTH_REFRESH_REUSED"
	AuditPasswordResetReq   = "AUTH_PASSWORD_RESET_REQUESTED"
	AuditPasswordReset      = "AUTH_PASSWORD_RESET"
	// AuditPasswordResetCodeVerified is recorded when a submitted reset code is
	// accepted and exchanged for a single-use reset token (FR-018).
	AuditPasswordResetCodeVerified = "AUTH_PASSWORD_RESET_CODE_VERIFIED"
	// AuditPasswordResetCodeFailed is recorded when a submitted reset code is
	// rejected: wrong, expired, blocked or over the attempt limit. It names the
	// code, never the address, so it does not reveal whether one is registered
	// (FR-009, FR-018).
	AuditPasswordResetCodeFailed = "AUTH_PASSWORD_RESET_CODE_FAILED"
	// AuditPasswordResetDeliveryFailed is recorded when a reset code could not be
	// delivered. No code reached the customer, so the send cooldown is disarmed
	// and the request can be retried immediately (FR-018).
	AuditPasswordResetDeliveryFailed = "AUTH_PASSWORD_RESET_DELIVERY_FAILED"
	AuditPasswordChanged             = "AUTH_PASSWORD_CHANGED"
	AuditAdminProvisioned            = "AUTH_ADMIN_PROVISIONED"
	AuditPrivilegeDenied             = "AUTH_PRIVILEGE_DENIED"

	OutcomeSuccess = "SUCCESS"
	OutcomeFailure = "FAILURE"
)
