// Package httpdto defines the HTTP request/response payloads for the auth API.
package httpdto

// StartRegistrationRequest is the POST /register body. It carries the same
// payload as the previous RegisterRequest; the handler now creates no account
// and returns a registration reference.
type StartRegistrationRequest struct {
	Email       string  `json:"email"`
	Username    *string `json:"username,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	Password    string  `json:"password"`
}

// VerifyRegistrationRequest is the POST /register/verify-otp body.
type VerifyRegistrationRequest struct {
	RegistrationID string `json:"registrationId"`
	OTP            string `json:"otp"`
}

// RegistrationIDRequest is the body of the resend and cancel calls.
type RegistrationIDRequest struct {
	RegistrationID string `json:"registrationId"`
}

// RegistrationStartedResponse is the POST /register result: the reference and
// the code/resend timings.
type RegistrationStartedResponse struct {
	RegistrationID    string `json:"registrationId"`
	Status            string `json:"status"`
	OTPExpiresIn      int    `json:"otpExpiresIn"`
	ResendAvailableIn int    `json:"resendAvailableIn"`
}

// EmailRequest targets an email address.
type EmailRequest struct {
	Email string `json:"email"`
}

// LoginRequest is the POST /login body.
type LoginRequest struct {
	Email      string `json:"email"`
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

// TokenRequest carries a refresh token.
type TokenRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// VerifyResetOTPRequest is the POST /password/forgot/verify-otp body: the
// address the code was sent to and the submitted code.
type VerifyResetOTPRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

// ResetTokenResponse is the verify-otp result: a single-use reset token and its
// lifetime in seconds. It is not a session and cannot authenticate a request.
type ResetTokenResponse struct {
	ResetToken string `json:"resetToken"`
	ExpiresIn  int    `json:"expiresIn"`
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
