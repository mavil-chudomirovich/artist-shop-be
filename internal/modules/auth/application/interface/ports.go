// Package appinterface declares the auth module's use-case interface and the
// external-service ports the application depends on (including UnitOfWork).
package appinterface

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/access"
)

// OTPStore persists email-confirmation challenges.
type OTPStore interface {
	Issue(ctx context.Context, email, otpHash string) error
	CanResend(ctx context.Context, email string) error
	Verify(ctx context.Context, email, otp string) error
	Invalidate(ctx context.Context, email string) error
	// DisarmCooldown clears the resend marker. It deletes that marker and
	// nothing else: the code, its lifetime, the attempt counter and the block
	// marker are the brute-force state and a failed delivery must not touch them
	// (FR-018). It exists because the marker is armed before delivery is
	// attempted, so a send that did not happen must not consume the cooldown the
	// customer is then told they can use (FR-024).
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
	Register(ctx context.Context, in dto.RegisterInput) error
	VerifyEmail(ctx context.Context, in dto.VerifyEmailInput) error
	ResendVerification(ctx context.Context, in dto.EmailInput) error
	Login(ctx context.Context, in dto.LoginInput) (dto.SessionOutput, error)
	Refresh(ctx context.Context, in dto.RefreshInput) (dto.SessionOutput, error)
	Logout(ctx context.Context, in dto.RefreshInput) error
	ForgotPassword(ctx context.Context, in dto.EmailInput) error
	ResetPassword(ctx context.Context, in dto.ResetPasswordInput) (dto.SessionOutput, error)
	ChangePassword(ctx context.Context, in dto.ChangePasswordInput) (dto.SessionOutput, error)
	VerifyAccessToken(ctx context.Context, raw string) (Claims, error)
	Identity(ctx context.Context, id uuid.UUID) (dto.IdentityOutput, error)
	ProvisionAdmin(ctx context.Context, in dto.ProvisionAdminInput) error
}

// UserProvisioner is the admin-seed use case consumed by presentation/cli.
type UserProvisioner interface {
	ProvisionAdmin(ctx context.Context, in dto.ProvisionAdminInput) error
}
