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
