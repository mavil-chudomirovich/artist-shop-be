// Package domainerr defines the auth module's business errors.
package domainerr

import "errors"

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailTaken         = errors.New("email already registered")
	ErrUserNotFound       = errors.New("user not found")
	ErrSessionNotFound    = errors.New("session not found")
	ErrResetNotFound      = errors.New("password reset request not found")

	ErrInvalidTransition = errors.New("invalid account status transition")
	ErrAccountPending    = errors.New("account is pending email confirmation")
	ErrAccountDisabled   = errors.New("account is disabled")
	ErrAccountLocked     = errors.New("too many failed sign-in attempts")

	ErrWeakPassword  = errors.New("password does not meet the policy")
	ErrInvalidToken  = errors.New("invalid token")
	ErrExpiredToken  = errors.New("token expired")
	ErrRefreshReused = errors.New("refresh token already used")
	ErrResetInvalid  = errors.New("invalid password reset token")

	ErrOTPInvalid         = errors.New("invalid OTP")
	ErrOTPExpired         = errors.New("OTP expired")
	ErrOTPTooManyAttempts = errors.New("too many OTP attempts")
	ErrOTPBlocked         = errors.New("OTP is blocked")
	ErrResendCooldown     = errors.New("OTP resend cooldown active")
)
