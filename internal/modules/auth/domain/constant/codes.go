package constant

// Stable, machine-readable auth error codes mapped to HTTP by presentation.
const (
	CodeWeakPassword       = "AUTH_WEAK_PASSWORD"
	CodeInvalidCredentials = "AUTH_INVALID_CREDENTIALS"
	CodeUsernameTaken      = "AUTH_USERNAME_TAKEN"
	CodeEmailTaken         = "AUTH_EMAIL_TAKEN"
	CodeAccountPending     = "AUTH_ACCOUNT_PENDING"
	CodeAccountDisabled    = "AUTH_ACCOUNT_DISABLED"
	CodeOTPInvalid         = "AUTH_OTP_INVALID"
	CodeOTPExpired         = "AUTH_OTP_EXPIRED"
	CodeOTPTooManyAttempts = "AUTH_OTP_TOO_MANY_ATTEMPTS"
	CodeResendCooldown     = "AUTH_RESEND_COOLDOWN"
	CodeLoginLocked        = "AUTH_LOGIN_LOCKED"
	CodeTokenInvalid       = "AUTH_TOKEN_INVALID"
	CodeTokenExpired       = "AUTH_TOKEN_EXPIRED"
	CodeRefreshReused      = "AUTH_REFRESH_REUSED"
	CodeResetInvalid       = "AUTH_RESET_INVALID"
	// CodeRegistrationNotFound is returned when a registration reference is
	// unknown, expired or cancelled. It is deliberately generic so verify,
	// resend and cancel do not reveal whether a reference existed (FR-016).
	CodeRegistrationNotFound = "AUTH_REGISTRATION_NOT_FOUND"
)

// Stable, machine-readable auth success codes answered in MessageResponse.
// They identify which acknowledgement the endpoint returned, so clients
// branch on code rather than on the message text.
const (
	// CodeRegistrationConfirmed answers a verified registration OTP.
	CodeRegistrationConfirmed = "AUTH_REGISTRATION_CONFIRMED"
	// CodeCodeSent answers both the registration resend and the password-reset
	// request: the two share one code and one message on purpose, so a client
	// handles "a code was sent" the same way on either flow.
	CodeCodeSent = "AUTH_CODE_SENT"
	// CodeRegistrationCancelled answers a cancelled registration.
	CodeRegistrationCancelled = "AUTH_REGISTRATION_CANCELLED"
	// CodePasswordReset answers a completed password reset.
	CodePasswordReset = "AUTH_PASSWORD_RESET"
	// CodeAdminProbe answers the admin RBAC probe.
	CodeAdminProbe = "AUTH_ADMIN_PROBE"
)
