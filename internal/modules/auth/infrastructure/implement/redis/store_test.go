package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/cache"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

func newTestCache(t *testing.T) (*cache.Cache, *miniredis.Miniredis) {
	t.Helper()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(server.Close)

	c, err := cache.New(context.Background(), config.RedisConfig{Addr: server.Addr()})
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, server
}

func TestOTPVerifyAndBlacklist(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	otp := NewOTPStore(c, token.Hasher{}, 3, 15*time.Minute, time.Minute, time.Minute)

	hash, err := token.Hasher{}.Hash("123456")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := otp.Issue(ctx, "user@example.com", hash); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := otp.Verify(ctx, "user@example.com", "000000"); !errors.Is(err, domainerr.ErrOTPInvalid) {
			t.Fatalf("attempt %d: expected ErrOTPInvalid, got %v", i+1, err)
		}
	}
	if err := otp.Verify(ctx, "user@example.com", "000000"); !errors.Is(err, domainerr.ErrOTPTooManyAttempts) {
		t.Fatalf("expected ErrOTPTooManyAttempts on 3rd failure, got %v", err)
	}
	if err := otp.Verify(ctx, "user@example.com", "123456"); !errors.Is(err, domainerr.ErrOTPBlocked) {
		t.Fatalf("expected ErrOTPBlocked after lockout, got %v", err)
	}
	if err := otp.CanResend(ctx, "user@example.com"); !errors.Is(err, domainerr.ErrOTPBlocked) {
		t.Fatalf("expected ErrOTPBlocked on resend, got %v", err)
	}
}

func TestOTPVerifySuccessAndCooldown(t *testing.T) {
	c, server := newTestCache(t)
	ctx := context.Background()
	otp := NewOTPStore(c, token.Hasher{}, 3, 15*time.Minute, time.Minute, time.Minute)

	hash, _ := token.Hasher{}.Hash("654321")
	if err := otp.Issue(ctx, "a@b.com", hash); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := otp.CanResend(ctx, "a@b.com"); !errors.Is(err, domainerr.ErrResendCooldown) {
		t.Fatalf("expected ErrResendCooldown, got %v", err)
	}

	server.FastForward(2 * time.Minute)
	if err := otp.CanResend(ctx, "a@b.com"); err != nil {
		t.Fatalf("expected cooldown expired, got %v", err)
	}
	if err := otp.Verify(ctx, "a@b.com", "654321"); err != nil {
		t.Fatalf("expected successful verification, got %v", err)
	}
}

func TestBlacklist(t *testing.T) {
	c, server := newTestCache(t)
	ctx := context.Background()
	blacklist := NewBlacklistStore(c)

	if err := blacklist.RevokeJTI(ctx, "jti-1", time.Minute); err != nil {
		t.Fatalf("RevokeJTI: %v", err)
	}
	revoked, err := blacklist.IsJTIRevoked(ctx, "jti-1")
	if err != nil || !revoked {
		t.Fatalf("expected revoked, got %v err %v", revoked, err)
	}

	userID := uuid.New()
	cutoff := time.Now().UTC()
	if err := blacklist.RevokeUserBefore(ctx, userID, cutoff, time.Minute); err != nil {
		t.Fatalf("RevokeUserBefore: %v", err)
	}
	before, err := blacklist.IsIssuedBefore(ctx, userID, cutoff.Add(-time.Minute))
	if err != nil || !before {
		t.Fatalf("expected issued-before true, got %v err %v", before, err)
	}
	after, err := blacklist.IsIssuedBefore(ctx, userID, cutoff.Add(10*time.Second))
	if err != nil || after {
		t.Fatalf("expected issued-before false, got %v err %v", after, err)
	}

	server.FastForward(2 * time.Minute)
	revoked, _ = blacklist.IsJTIRevoked(ctx, "jti-1")
	if revoked {
		t.Fatal("expected jti blacklist to expire")
	}
}

func TestLoginGuardLockoutAndReset(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	guard := NewLoginGuard(c, 10, 15*time.Minute, 15*time.Minute)

	for i := 1; i <= 9; i++ {
		blocked, err := guard.RecordFailure(ctx, "1.2.3.4")
		if err != nil {
			t.Fatalf("RecordFailure: %v", err)
		}
		if blocked {
			t.Fatalf("blocked too early at attempt %d", i)
		}
	}
	blocked, err := guard.RecordFailure(ctx, "1.2.3.4")
	if err != nil || !blocked {
		t.Fatalf("expected lockout at 10th failure, got %v err %v", blocked, err)
	}
	got, _ := guard.Blocked(ctx, "1.2.3.4")
	if !got {
		t.Fatal("expected source blocked")
	}

	if err := guard.Reset(ctx, "1.2.3.4"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	blocked, _ = guard.Blocked(ctx, "1.2.3.4")
	if blocked {
		t.Fatal("expected block cleared after reset")
	}
}
