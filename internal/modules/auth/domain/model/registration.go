package model

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"
)

// RegistrationReferenceBytes is the entropy of a registration reference. 32
// random bytes make the reference unpredictable and independent of the email
// (FR-005): a client cannot derive one address's reference from another.
const RegistrationReferenceBytes = 32

// Temporary-registration states. A record is created pending; once its code is
// confirmed and the account is durably created it is marked completed and kept
// (code-less) until its own TTL, so a repeated confirmation can report success
// without creating a second account (FR-010, FR-011).
const (
	RegistrationStatusPending   = "pending"
	RegistrationStatusCompleted = "completed"
)

// PendingRegistration is a sign-up that has not yet been confirmed by email. It
// is held only in the temporary store until the emailed code is verified; no
// durable account is created from it before then (FR-004). The password is
// carried as an Argon2id hash and never in plaintext (FR-019).
type PendingRegistration struct {
	// RegistrationID is the opaque reference the client sends on verify,
	// resend and cancel. It keys the temporary store and is never derived from
	// the email.
	RegistrationID string
	// Email is normalized (trimmed and lowercased) before it is stored
	// (FR-002).
	Email string
	// Username is optional; it is checked for duplicates at start and again
	// when the account is created (FR-023).
	Username *string
	// DisplayName is optional.
	DisplayName *string
	// PasswordHash is the Argon2id encoded password; never plaintext.
	PasswordHash string
	// CreatedAt is when the registration was stored, in UTC.
	CreatedAt time.Time
	// ExpiresAt is when the registration itself expires. It is derived from the
	// configured registration lifetime for reporting.
	ExpiresAt time.Time
	// Status is the registration's lifecycle state: pending until its code is
	// confirmed, completed once the account exists. A completed record is
	// retained until TTL so replaying the confirmation stays idempotent.
	Status string
}

// NewRegistrationID returns an opaque, unpredictable registration reference: 32
// random bytes from crypto/rand encoded with unpadded base64url so it is safe in
// URLs and JSON without escaping.
func NewRegistrationID() (string, error) {
	buf := make([]byte, RegistrationReferenceBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate registration reference: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
