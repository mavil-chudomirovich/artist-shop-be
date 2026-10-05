package constant

// Audit actions and outcomes emitted by the auth module.
const (
	AuditRegister         = "AUTH_REGISTER"
	AuditEmailVerified    = "AUTH_EMAIL_VERIFIED"
	AuditSignInSucceeded  = "AUTH_SIGN_IN_SUCCEEDED"
	AuditSignInFailed     = "AUTH_SIGN_IN_FAILED"
	AuditSignOut          = "AUTH_SIGN_OUT"
	AuditSessionRefreshed = "AUTH_SESSION_REFRESHED"
	AuditRefreshReused    = "AUTH_REFRESH_REUSED"
	AuditPasswordResetReq = "AUTH_PASSWORD_RESET_REQUESTED"
	AuditPasswordReset    = "AUTH_PASSWORD_RESET"
	AuditPasswordChanged  = "AUTH_PASSWORD_CHANGED"
	AuditAdminProvisioned = "AUTH_ADMIN_PROVISIONED"
	AuditPrivilegeDenied  = "AUTH_PRIVILEGE_DENIED"

	OutcomeSuccess = "SUCCESS"
	OutcomeFailure = "FAILURE"
)
