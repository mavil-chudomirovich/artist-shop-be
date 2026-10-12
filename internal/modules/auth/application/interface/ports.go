// Package appinterface declares the auth module's use-case interface and the
// external-service ports the application depends on (including UnitOfWork).
package appinterface

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// RegistrationStore persists a temporary registration and its email code until
// the code is confirmed, the visitor cancels it, or it expires. Every method is
// keyed by the opaque registration reference, not by the email, so a client
// cannot drive another address's registration (FR-005).
type RegistrationStore interface {
	// Save stores the registration and points its email at it. A new
	// registration for an email that already has a live one replaces it,
	// invalidating the previous reference and code (FR-026). The replacement is
	// atomic so a double-submit cannot leave two live registrations.
	Save(ctx context.Context, reg model.PendingRegistration) error
	// ByID loads a registration by its reference. It returns
	// domain/error.ErrRegistrationNotFound for an unknown or expired reference.
	ByID(ctx context.Context, registrationID string) (model.PendingRegistration, error)
	// Delete removes the registration, its code and its email pointer. It is
	// idempotent: an unknown reference is a no-op.
	Delete(ctx context.Context, registrationID string) error
	// Complete marks the registration as completed after its account has been
	// durably created: it removes the code state and the email pointer and sets
	// the record's status to completed, retaining the record until its TTL so a
	// repeated confirmation can report success without creating a second account
	// (FR-010, FR-011). It is idempotent.
	Complete(ctx context.Context, registrationID string) error
	// IssueOTP stores a new code hash, resets the attempt counter, clears any
	// block and arms the resend cooldown (FR-006, FR-015).
	IssueOTP(ctx context.Context, registrationID, otpHash string) error
	// CanResend reports whether a new code may be requested for the reference
	// (FR-014).
	CanResend(ctx context.Context, registrationID string) error
	// VerifyOTP checks the submitted code, counting failures and blocking the
	// code once the attempt limit is reached (FR-008, FR-012, FR-013).
	VerifyOTP(ctx context.Context, registrationID, otp string) error
	// InvalidateOTP removes the active code and its attempt counter.
	InvalidateOTP(ctx context.Context, registrationID string) error
	// DisarmCooldown clears only the resend marker, leaving the code and the
	// brute-force state untouched (FR-018, FR-024).
	DisarmCooldown(ctx context.Context, registrationID string) error
}

// PasswordResetOTPStore holds a password-reset code challenge and its
// brute-force state, keyed by the normalized email address the code was sent
// to. Every key the adapter writes carries a TTL, so losing the store at worst
// forces a new code and never creates a partial reset (FR-010, FR-015).
type PasswordResetOTPStore interface {
	// Issue stores a new code hash, resets the attempt counter, clears any
	// block and arms the resend cooldown. A new code replaces the previous one,
	// so only the latest code is valid (FR-004, FR-015).
	Issue(ctx context.Context, email, otpHash string) error
	// CanResend reports whether a new code may be requested for the address
	// (FR-015).
	CanResend(ctx context.Context, email string) error
	// Verify checks the submitted code, counting failures and blocking the code
	// once the attempt limit is reached (FR-007, FR-008).
	Verify(ctx context.Context, email, otp string) error
	// Invalidate removes the active code and its attempt counter (FR-006).
	Invalidate(ctx context.Context, email string) error
	// DisarmCooldown clears only the resend marker, leaving the code and the
	// brute-force state untouched, so a failed delivery does not consume the
	// wait the customer was told about (FR-018).
	DisarmCooldown(ctx context.Context, email string) error
}

// Blacklist stores revoked access tokens.
type Blacklist interface {
	RevokeJTI(ctx context.Context, jti string, ttl time.Duration) error
	IsJTIRevoked(ctx context.Context, jti string) (bool, error)
	RevokeUserBefore(ctx context.Context, userID uuid.UUID, at time.Time, ttl time.Duration) error
	IsIssuedBefore(ctx context.Context, userID uuid.UUID, iat time.Time) (bool, error)
}

// LoginGuard tracks failed sign-in attempts per source.
type LoginGuard interface {
	Blocked(ctx context.Context, source string) (bool, error)
	RecordFailure(ctx context.Context, source string) (bool, error)
	Reset(ctx context.Context, source string) error
}

// PasswordHasher hashes and verifies passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(encoded, password string) (bool, error)
}

// Claims are the verified contents of an access token.
type Claims struct {
	Subject  uuid.UUID
	Role     access.Role
	ID       string
	IssuedAt time.Time
}

// AccessTokens issues and verifies access tokens.
type AccessTokens interface {
	Issue(userID uuid.UUID, role access.Role) (raw string, expiresAt time.Time, err error)
	Parse(raw string) (Claims, error)
	TTL() time.Duration
}

// RefreshTokens generates and hashes opaque refresh tokens.
type RefreshTokens interface {
	Generate() (raw, hash string, err error)
	Hash(raw string) string
}

// EmailSender delivers outbound messages.
type EmailSender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Auditor records security-relevant events.
type Auditor interface {
	Record(ctx context.Context, action, outcome string, actorID *uuid.UUID, actorRole, targetType, targetID string, metadata map[string]any)
}

// UnitOfWork runs a function inside a database transaction.
type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// AuthService is the module's use-case surface.
type AuthService interface {
	StartRegistration(ctx context.Context, in dto.StartRegistrationInput) (dto.RegistrationStartedOutput, error)
	VerifyRegistrationOTP(ctx context.Context, in dto.VerifyRegistrationInput) error
	ResendRegistrationOTP(ctx context.Context, in dto.RegistrationIDInput) error
	CancelRegistration(ctx context.Context, in dto.RegistrationIDInput) error
	Login(ctx context.Context, in dto.LoginInput) (dto.SessionOutput, error)
	Refresh(ctx context.Context, in dto.RefreshInput) (dto.SessionOutput, error)
	Logout(ctx context.Context, in dto.RefreshInput) error
	ForgotPassword(ctx context.Context, in dto.EmailInput) error
	VerifyResetOTP(ctx context.Context, in dto.VerifyResetOTPInput) (dto.ResetTokenOutput, error)
	ResetPassword(ctx context.Context, in dto.ResetPasswordInput) error
	ChangePassword(ctx context.Context, in dto.ChangePasswordInput) (dto.SessionOutput, error)
	VerifyAccessToken(ctx context.Context, raw string) (Claims, error)
	Identity(ctx context.Context, id uuid.UUID) (dto.IdentityOutput, error)
	ProvisionAdmin(ctx context.Context, in dto.ProvisionAdminInput) error
}

// UserProvisioner is the admin-seed use case consumed by presentation/cli.
type UserProvisioner interface {
	ProvisionAdmin(ctx context.Context, in dto.ProvisionAdminInput) error
}
