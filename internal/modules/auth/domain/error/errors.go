// Package domainerr defines the auth module's business errors.
package domainerr

import "errors"

// Business errors returned by the auth module.
var (
	// ErrInvalidCredentials is returned for both unknown email and wrong password
	// so the API never reveals whether an account exists.
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailTaken         = errors.New("email already registered")
	ErrUsernameTaken      = errors.New("username already registered")
	ErrUserNotFound       = errors.New("user not found")
	ErrSessionNotFound    = errors.New("session not found")
	ErrResetNotFound      = errors.New("password reset request not found")

	// ErrRegistrationNotFound is returned for an unknown, expired or cancelled
	// temporary registration. Verify, resend and cancel all report it the same
	// generic way, so it never reveals whether a reference existed (FR-016).
	ErrRegistrationNotFound = errors.New("registration not found")

	ErrInvalidTransition = errors.New("invalid account status transition")
	ErrAccountPending    = errors.New("account is pending email confirmation")
	ErrAccountDisabled   = errors.New("account is disabled")
	ErrAccountLocked     = errors.New("too many failed sign-in attempts")

	ErrWeakPassword  = errors.New("password does not meet the policy")
	ErrInvalidToken  = errors.New("invalid token")
	ErrExpiredToken  = errors.New("token expired")
	ErrRefreshReused = errors.New("refresh token already used")
	ErrResetInvalid  = errors.New("invalid password reset token")

	// ErrInvalidEmail is returned when a submitted address is not a plain,
	// well-formed email. Registration validates the format before any temporary
	// or durable state is written (FR-001).
	ErrInvalidEmail = errors.New("email address is not valid")

	ErrOTPInvalid         = errors.New("invalid OTP")
	ErrOTPExpired         = errors.New("OTP expired")
	ErrOTPTooManyAttempts = errors.New("too many OTP attempts")
	ErrOTPBlocked         = errors.New("OTP is blocked")
	ErrResendCooldown     = errors.New("OTP resend cooldown active")

	// Delivery classifications. An adapter maps the status category its provider
	// reported onto one of these, and the use case decides whether to retry from
	// the classification alone - never from the provider's wording, which changes
	// between releases (FR-015, FR-017).
	ErrDeliveryTransient     = errors.New("message delivery is temporarily unavailable")
	ErrDeliveryUnreachable   = errors.New("message provider is unreachable")
	ErrDeliveryConfiguration = errors.New("message provider rejected because this deployment is not configured correctly")
	ErrDeliveryRefused       = errors.New("message provider refused the message itself")

	// ErrVerificationDeliveryFailed is returned when an account was created but
	// its verification message could not be delivered. The account is kept, and
	// presentation answers 503 telling the customer that nothing was sent and
	// that a new code can be requested (FR-006, FR-007).
	ErrVerificationDeliveryFailed = errors.New("verification message could not be delivered")
)
