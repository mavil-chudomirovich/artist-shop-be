package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
)

// An issued code verifies with the right value, clears the attempt counter, and
// is only removed when the caller invalidates it after the token is written, so
// a failed durable write can be retried with the same code (FR-006, research
// D3).
func TestResetOTPIssueAndVerify(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	store := NewPasswordResetOTPStore(c, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute)

	hash, _ := token.Hasher{}.Hash("123456")
	if err := store.Issue(ctx, "user@example.com", hash); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := store.Verify(ctx, "user@example.com", "123456"); err != nil {
		t.Fatalf("expected verification to succeed, got %v", err)
	}
	if exists, _ := c.Exists(ctx, resetOTPKey("user@example.com")); !exists {
		t.Fatal("the code must survive a successful verification until the caller invalidates it")
	}

	if err := store.Invalidate(ctx, "user@example.com"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if exists, _ := c.Exists(ctx, resetOTPKey("user@example.com")); exists {
		t.Fatal("expected the code to be deleted on invalidate")
	}
	if exists, _ := c.Exists(ctx, resetAttemptsKey("user@example.com")); exists {
		t.Fatal("expected the attempt counter to be deleted on invalidate")
	}
}

// Wrong codes count failures and block after the attempt limit; a blocked code
// then refuses even the correct value until a new code is issued (FR-007,
// FR-008).
func TestResetOTPWrongAttemptsBlock(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	store := NewPasswordResetOTPStore(c, token.Hasher{}, 3, 5*time.Minute, time.Minute, time.Minute)

	hash, _ := token.Hasher{}.Hash("123456")
	if err := store.Issue(ctx, "user@example.com", hash); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := store.Verify(ctx, "user@example.com", "000000"); !errors.Is(err, domainerr.ErrOTPInvalid) {
			t.Fatalf("attempt %d: expected ErrOTPInvalid, got %v", i+1, err)
		}
	}
	if err := store.Verify(ctx, "user@example.com", "000000"); !errors.Is(err, domainerr.ErrOTPTooManyAttempts) {
		t.Fatalf("expected ErrOTPTooManyAttempts on the third failure, got %v", err)
	}
	if err := store.Verify(ctx, "user@example.com", "123456"); !errors.Is(err, domainerr.ErrOTPBlocked) {
		t.Fatalf("expected ErrOTPBlocked after the limit, got %v", err)
	}
	if err := store.CanResend(ctx, "user@example.com"); !errors.Is(err, domainerr.ErrOTPBlocked) {
		t.Fatalf("expected CanResend to report ErrOTPBlocked, got %v", err)
	}
}

// Issuing arms the resend cooldown and disarming clears only that marker, so a
// failed delivery does not consume the wait the customer was told about
// (FR-015, FR-018).
func TestResetOTPCooldownAndDisarm(t *testing.T) {
	c, server := newTestCache(t)
	ctx := context.Background()
	store := NewPasswordResetOTPStore(c, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute)

	hash, _ := token.Hasher{}.Hash("333333")
	if err := store.Issue(ctx, "user@example.com", hash); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := store.CanResend(ctx, "user@example.com"); !errors.Is(err, domainerr.ErrResendCooldown) {
		t.Fatalf("expected ErrResendCooldown, got %v", err)
	}

	server.FastForward(2 * time.Minute)
	if err := store.CanResend(ctx, "user@example.com"); err != nil {
		t.Fatalf("expected the cooldown to expire, got %v", err)
	}

	// Arm it again, then disarm: the code must survive the disarm.
	if err := store.Issue(ctx, "user@example.com", hash); err != nil {
		t.Fatalf("Issue again: %v", err)
	}
	if err := store.DisarmCooldown(ctx, "user@example.com"); err != nil {
		t.Fatalf("DisarmCooldown: %v", err)
	}
	if err := store.CanResend(ctx, "user@example.com"); err != nil {
		t.Fatalf("a disarm must clear the cooldown, got %v", err)
	}
	if err := store.Verify(ctx, "user@example.com", "333333"); err != nil {
		t.Fatalf("the code must survive a disarm, got %v", err)
	}
}

// The code expires on its own lifetime; afterwards verification reports it as
// expired so the user must request a new one (FR-004, FR-009).
func TestResetOTPExpiresByTTL(t *testing.T) {
	c, server := newTestCache(t)
	ctx := context.Background()
	store := NewPasswordResetOTPStore(c, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute)

	hash, _ := token.Hasher{}.Hash("444444")
	if err := store.Issue(ctx, "user@example.com", hash); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	server.FastForward(6 * time.Minute)
	if err := store.Verify(ctx, "user@example.com", "444444"); !errors.Is(err, domainerr.ErrOTPExpired) {
		t.Fatalf("expected the expired code to be refused, got %v", err)
	}
}

// A new code replaces the previous one, so only the newest value works; the old
// value is simply a wrong code and never a duplicate valid one (FR-004).
func TestResetOTPNewCodeReplacesPrevious(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	store := NewPasswordResetOTPStore(c, token.Hasher{}, 5, 5*time.Minute, time.Minute, time.Minute)

	first, _ := token.Hasher{}.Hash("111111")
	if err := store.Issue(ctx, "user@example.com", first); err != nil {
		t.Fatalf("Issue first: %v", err)
	}
	second, _ := token.Hasher{}.Hash("222222")
	if err := store.Issue(ctx, "user@example.com", second); err != nil {
		t.Fatalf("Issue second: %v", err)
	}

	if err := store.Verify(ctx, "user@example.com", "111111"); !errors.Is(err, domainerr.ErrOTPInvalid) {
		t.Fatalf("the replaced code must be refused, got %v", err)
	}
	if err := store.Verify(ctx, "user@example.com", "222222"); err != nil {
		t.Fatalf("the newest code must verify, got %v", err)
	}
}
