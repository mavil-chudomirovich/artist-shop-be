package constant

// Audit actions and outcomes emitted by the auth module.
const (
	// AuditRegister is recorded when a registration created an account *and* its
	// verification message was delivered.
	AuditRegister = "AUTH_REGISTER"
	// AuditRegisterDeliveryFailed is recorded with OutcomeFailure when a
	// registration created an account but the verification message could not be
	// delivered. The account stays, so the creation is a real state change that has
	// to be reconcilable (FR-011); the action is distinct from AuditRegister
	// because the outcome recorded alongside it is a failure, and the reason is
	// carried in metadata by classification rather than in the action name.
	AuditRegisterDeliveryFailed = "AUTH_REGISTER_DELIVERY_FAILED"
	AuditEmailVerified          = "AUTH_EMAIL_VERIFIED"
	AuditSignInSucceeded        = "AUTH_SIGN_IN_SUCCEEDED"
	AuditSignInFailed           = "AUTH_SIGN_IN_FAILED"
	AuditSignOut                = "AUTH_SIGN_OUT"
	AuditSessionRefreshed       = "AUTH_SESSION_REFRESHED"
	AuditRefreshReused          = "AUTH_REFRESH_REUSED"
	AuditPasswordResetReq       = "AUTH_PASSWORD_RESET_REQUESTED"
	AuditPasswordReset          = "AUTH_PASSWORD_RESET"
	AuditPasswordChanged        = "AUTH_PASSWORD_CHANGED"
	AuditAdminProvisioned       = "AUTH_ADMIN_PROVISIONED"
	AuditPrivilegeDenied        = "AUTH_PRIVILEGE_DENIED"

	OutcomeSuccess = "SUCCESS"
	OutcomeFailure = "FAILURE"
)
