// Package dto defines the auth module's use-case input/output types.
package dto

import (
	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// RegisterInput starts registration.
type RegisterInput struct {
	Email    string
	Password string
}

// VerifyEmailInput confirms an email OTP.
type VerifyEmailInput struct {
	Email string
	OTP   string
}

// EmailInput targets an email address.
type EmailInput struct {
	Email string
}

// LoginInput authenticates an account.
type LoginInput struct {
	Email     string
	Password  string
	Source    string
	UserAgent string
	IP        string
}

// RefreshInput carries a session credential.
type RefreshInput struct {
	RefreshToken string
	UserAgent    string
	IP           string
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
