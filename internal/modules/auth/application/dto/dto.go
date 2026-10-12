// Package dto defines the auth module's use-case input/output types.
package dto

import (
	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// StartRegistrationInput starts a registration. It validates the payload and
// holds a temporary registration only; no account is created until the emailed
// code is confirmed (FR-001, FR-004).
type StartRegistrationInput struct {
	Email       string
	Username    *string
	DisplayName *string
	Password    string
}

// VerifyRegistrationInput confirms a temporary registration with its code.
type VerifyRegistrationInput struct {
	RegistrationID string
	OTP            string
}

// RegistrationIDInput targets a temporary registration by its opaque reference.
type RegistrationIDInput struct {
	RegistrationID string
}

// RegistrationStartedOutput is the result of starting a registration: the
// reference the client uses for the follow-up calls, plus the code and resend
// timings it displays.
type RegistrationStartedOutput struct {
	RegistrationID    string
	Status            string
	OTPExpiresIn      int
	ResendAvailableIn int
}

// EmailInput targets an email address.
type EmailInput struct {
	Email string
}

// LoginInput authenticates an account.
type LoginInput struct {
	Email      string
	Identifier string
	Password   string
	Source     string
	UserAgent  string
	IP         string
}

// RefreshInput carries a session credential.
type RefreshInput struct {
	RefreshToken string
	UserAgent    string
	IP           string
}

// VerifyResetOTPInput verifies an emailed reset code and exchanges it for a
// single-use reset token.
type VerifyResetOTPInput struct {
	Email string
	OTP   string
}

// ResetTokenOutput is the single-use reset token returned by verify-otp,
// together with its lifetime in seconds.
type ResetTokenOutput struct {
	ResetToken string
	ExpiresIn  int
}

// ResetPasswordInput completes a password reset.
type ResetPasswordInput struct {
	Token       string
	NewPassword string
}

// ChangePasswordInput changes the password of a signed-in account.
type ChangePasswordInput struct {
	AccountID           uuid.UUID
	CurrentPassword     string
	NewPassword         string
	CurrentRefreshToken string
	CurrentAccessJTI    string
	UserAgent           string
	IP                  string
}

// ProvisionAdminInput creates or updates the admin account.
type ProvisionAdminInput struct {
	Email    string
	Password string
}

// SessionOutput is an issued token pair.
type SessionOutput struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

// IdentityOutput describes the current account.
type IdentityOutput struct {
	ID    string
	Email string
	Role  access.Role
}
