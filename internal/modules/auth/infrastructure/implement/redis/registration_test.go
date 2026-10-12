package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
)

func registration(id, email string) model.PendingRegistration {
	username := "handle-" + id
	displayName := "Display " + id
	created := time.Now().UTC()
	return model.PendingRegistration{
		RegistrationID: id,
		Email:          email,
		Username:       &username,
		DisplayName:    &displayName,
		PasswordHash:   "argon2id-hash",
		CreatedAt:      created,
		ExpiresAt:      created.Add(30 * time.Minute),
	}
}

// Save must round-trip every field, including the optional profile fields, so a
// confirmation can rebuild the account without asking the client again (FR-004).
func TestRegistrationSaveAndLoad(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	store := NewRegistrationStore(c, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute, 30*time.Minute)

	reg := registration("ref-1", "user@example.com")
	if err := store.Save(ctx, reg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.ByID(ctx, "ref-1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Email != reg.Email || got.PasswordHash != "argon2id-hash" {
		t.Fatalf("round trip lost a field: %+v", got)
	}
	if got.Username == nil || *got.Username != *reg.Username {
		t.Fatalf("username not round-tripped: %+v", got.Username)
	}
	if got.DisplayName == nil || *got.DisplayName != *reg.DisplayName {
		t.Fatalf("display name not round-tripped: %+v", got.DisplayName)
	}
	if got.ExpiresAt.Sub(got.CreatedAt) != 30*time.Minute {
		t.Fatalf("expected a 30m registration lifetime, got %s", got.ExpiresAt.Sub(got.CreatedAt))
	}
}

// An absent optional profile field must load back as nil, not as an empty
// string, so the created account keeps NULLs rather than "".
func TestRegistrationSaveWithoutOptionalFields(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	store := NewRegistrationStore(c, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute, 30*time.Minute)

	reg := model.PendingRegistration{
		RegistrationID: "ref-bare",
		Email:          "bare@example.com",
		PasswordHash:   "hash",
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.Save(ctx, reg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.ByID(ctx, "ref-bare")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Username != nil || got.DisplayName != nil {
		t.Fatalf("expected nil optionals, got %+v", got)
	}
}

// A new registration for the same email must replace the previous one: the old
// reference stops resolving and the old code stops working (FR-026). This is the
// assertion that catches a non-atomic replace that leaves two live records.
func TestRegistrationSaveReplacesThePreviousForTheSameEmail(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	store := NewRegistrationStore(c, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute, 30*time.Minute)

	first := registration("ref-old", "same@example.com")
	if err := store.Save(ctx, first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	hash, _ := token.Hasher{}.Hash("111111")
	if err := store.IssueOTP(ctx, "ref-old", hash); err != nil {
		t.Fatalf("IssueOTP first: %v", err)
	}

	second := registration("ref-new", "same@example.com")
	if err := store.Save(ctx, second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	if _, err := store.ByID(ctx, "ref-old"); !errors.Is(err, domainerr.ErrRegistrationNotFound) {
		t.Fatalf("expected the replaced reference to be gone, got %v", err)
	}
	if _, err := store.ByID(ctx, "ref-new"); err != nil {
		t.Fatalf("expected the new reference to resolve, got %v", err)
	}
	if exists, _ := c.Exists(ctx, regOTPKey("ref-old")); exists {
		t.Fatal("the replaced registration's code must be deleted")
	}
}

// Delete must remove the record, the code and the pointer, and be idempotent so
// cancelling an already-gone reference is a no-op (FR-017).
func TestRegistrationDeleteRemovesTheRecordAndCode(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	store := NewRegistrationStore(c, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute, 30*time.Minute)

	if err := store.Save(ctx, registration("ref-del", "del@example.com")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	hash, _ := token.Hasher{}.Hash("222222")
	if err := store.IssueOTP(ctx, "ref-del", hash); err != nil {
		t.Fatalf("IssueOTP: %v", err)
	}

	if err := store.Delete(ctx, "ref-del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.ByID(ctx, "ref-del"); !errors.Is(err, domainerr.ErrRegistrationNotFound) {
		t.Fatalf("expected the record to be gone, got %v", err)
	}
	if exists, _ := c.Exists(ctx, regOTPKey("ref-del")); exists {
		t.Fatal("expected the code to be deleted")
	}
	if exists, _ := c.Exists(ctx, regEmailKey("del@example.com")); exists {
		t.Fatal("expected the email pointer to be deleted")
	}
	// Idempotent: deleting again must not fail.
	if err := store.Delete(ctx, "ref-del"); err != nil {
		t.Fatalf("Delete must be idempotent, got %v", err)
	}
}

// The code must count failures and block after the attempt limit, and a blocked
// code must refuse even the correct value until a new code is issued
// (FR-012, FR-013).
func TestRegistrationOTPVerifyAndBlock(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	store := NewRegistrationStore(c, token.Hasher{}, 3, 5*time.Minute, time.Minute, time.Minute, 30*time.Minute)

	hash, _ := token.Hasher{}.Hash("123456")
	if err := store.IssueOTP(ctx, "ref-otp", hash); err != nil {
		t.Fatalf("IssueOTP: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := store.VerifyOTP(ctx, "ref-otp", "000000"); !errors.Is(err, domainerr.ErrOTPInvalid) {
			t.Fatalf("attempt %d: expected ErrOTPInvalid, got %v", i+1, err)
		}
	}
	if err := store.VerifyOTP(ctx, "ref-otp", "000000"); !errors.Is(err, domainerr.ErrOTPTooManyAttempts) {
		t.Fatalf("expected ErrOTPTooManyAttempts on the third failure, got %v", err)
	}
	if err := store.VerifyOTP(ctx, "ref-otp", "123456"); !errors.Is(err, domainerr.ErrOTPBlocked) {
		t.Fatalf("expected ErrOTPBlocked after the limit, got %v", err)
	}
}

// A correct code clears the attempt counter but is deliberately kept until the
// registration is completed, so a failed account insert can be retried with the
// same code (FR-010). Completion then removes the code and the email pointer and
// retains the code-less record until its TTL (FR-011).
func TestRegistrationOTPVerifySucceedsAndKeepsTheCodeUntilCompleted(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	store := NewRegistrationStore(c, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute, 30*time.Minute)

	if err := store.Save(ctx, registration("ref-ok", "ok@example.com")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	hash, _ := token.Hasher{}.Hash("654321")
	if err := store.IssueOTP(ctx, "ref-ok", hash); err != nil {
		t.Fatalf("IssueOTP: %v", err)
	}
	if err := store.VerifyOTP(ctx, "ref-ok", "654321"); err != nil {
		t.Fatalf("expected verification to succeed, got %v", err)
	}
	if exists, _ := c.Exists(ctx, regOTPKey("ref-ok")); !exists {
		t.Fatal("the code must survive a successful verification so the account insert can be retried")
	}

	if err := store.Complete(ctx, "ref-ok"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if exists, _ := c.Exists(ctx, regOTPKey("ref-ok")); exists {
		t.Fatal("expected the code to be deleted on completion")
	}
	if exists, _ := c.Exists(ctx, regEmailKey("ok@example.com")); exists {
		t.Fatal("expected the email pointer to be deleted on completion")
	}
	got, err := store.ByID(ctx, "ref-ok")
	if err != nil {
		t.Fatalf("the completed record must be retained, got %v", err)
	}
	if got.Status != model.RegistrationStatusCompleted {
		t.Fatalf("expected status completed, got %q", got.Status)
	}
	// Completion is idempotent.
	if err := store.Complete(ctx, "ref-ok"); err != nil {
		t.Fatalf("Complete must be idempotent, got %v", err)
	}
}

// Issuing arms the resend cooldown and disarming clears only that marker, so a
// failed delivery does not consume the wait the customer was told about
// (FR-014, FR-024).
func TestRegistrationResendCooldown(t *testing.T) {
	c, server := newTestCache(t)
	ctx := context.Background()
	store := NewRegistrationStore(c, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute, 30*time.Minute)

	hash, _ := token.Hasher{}.Hash("333333")
	if err := store.IssueOTP(ctx, "ref-cooldown", hash); err != nil {
		t.Fatalf("IssueOTP: %v", err)
	}
	if err := store.CanResend(ctx, "ref-cooldown"); !errors.Is(err, domainerr.ErrResendCooldown) {
		t.Fatalf("expected ErrResendCooldown, got %v", err)
	}

	server.FastForward(2 * time.Minute)
	if err := store.CanResend(ctx, "ref-cooldown"); err != nil {
		t.Fatalf("expected the cooldown to expire, got %v", err)
	}

	// Arm it again, then disarm: the code must survive the disarm.
	if err := store.IssueOTP(ctx, "ref-cooldown", hash); err != nil {
		t.Fatalf("IssueOTP: %v", err)
	}
	if err := store.DisarmCooldown(ctx, "ref-cooldown"); err != nil {
		t.Fatalf("DisarmCooldown: %v", err)
	}
	if err := store.CanResend(ctx, "ref-cooldown"); err != nil {
		t.Fatalf("a disarm must clear the cooldown, got %v", err)
	}
	if err := store.VerifyOTP(ctx, "ref-cooldown", "333333"); err != nil {
		t.Fatalf("the code must survive a disarm, got %v", err)
	}
}

// The registration must expire on its own lifetime, after which its reference is
// unknown and no account is created (FR-007).
func TestRegistrationExpiresByTTL(t *testing.T) {
	c, server := newTestCache(t)
	ctx := context.Background()
	store := NewRegistrationStore(c, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute, 30*time.Minute)

	if err := store.Save(ctx, registration("ref-exp", "exp@example.com")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	server.FastForward(31 * time.Minute)
	if _, err := store.ByID(ctx, "ref-exp"); !errors.Is(err, domainerr.ErrRegistrationNotFound) {
		t.Fatalf("expected the expired reference to be gone, got %v", err)
	}
}
