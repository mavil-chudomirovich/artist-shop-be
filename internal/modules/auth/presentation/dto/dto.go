// Package httpdto defines the HTTP request/response payloads for the auth API.
package httpdto

// RegisterRequest is the POST /register body.
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// VerifyEmailRequest is the POST /verify-email body.
type VerifyEmailRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

// EmailRequest targets an email address.
type EmailRequest struct {
	Email string `json:"email"`
}

// LoginRequest is the POST /login body.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// TokenRequest carries a refresh token.
type TokenRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// ResetRequest completes a password reset.
type ResetRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

// ChangePasswordRequest changes a signed-in account's password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	RefreshToken    string `json:"refreshToken"`
}

// SessionResponse is the issued token pair.
type SessionResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
}

// IdentityResponse is the current account.
type IdentityResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// MessageResponse is a generic message.
type MessageResponse struct {
	Message string `json:"message"`
}
