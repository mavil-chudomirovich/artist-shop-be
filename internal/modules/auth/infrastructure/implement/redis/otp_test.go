package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/infrastructure/implement/token"
)

// TestDisarmingTheCooldownLeavesTheCodeAndItsCountersAlone is the assertion that
// catches a half-finished fix (FR-024): a send that did not happen must not
// consume the resend cooldown, and clearing the cooldown must change nothing
// else. The code, its lifetime and the brute-force attempt counter are the
// security state, and FR-018 pins all three.
func TestDisarmingTheCooldownLeavesTheCodeAndItsCountersAlone(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	otp := NewOTPStore(c, token.Hasher{}, 3, 15*time.Minute, time.Minute, time.Minute)
	const email = "failed@example.com"
	hash, err := token.Hasher{}.Hash("123456")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := otp.Issue(ctx, email, hash); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := otp.CanResend(ctx, email); !errors.Is(err, domainerr.ErrResendCooldown) {
		t.Fatalf("expected the issued code to arm the cooldown, got %v", err)
	}
	lifetime, err := c.TTL(ctx, otpKey(email))
	if err != nil {
		t.Fatalf("read the code lifetime: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := otp.Verify(ctx, email, "000000"); !errors.Is(err, domainerr.ErrOTPInvalid) {
			t.Fatalf("attempt %d: expected ErrOTPInvalid, got %v", i+1, err)
		}
	}
	attempts, err := c.Get(ctx, attemptsKey(email))
	if err != nil {
		t.Fatalf("read the attempt counter: %v", err)
	}
	if attempts != "2" {
		t.Fatalf("expected two recorded attempts, got %q", attempts)
	}

	if err := otp.DisarmCooldown(ctx, email); err != nil {
		t.Fatalf("DisarmCooldown: %v", err)
	}

	if err := otp.CanResend(ctx, email); err != nil {
		t.Fatalf("a send that did not happen must not consume the cooldown: %v", err)
	}
	if remaining, err := c.TTL(ctx, otpKey(email)); err != nil || remaining != lifetime {
		t.Fatalf("the code lifetime must be untouched: %s before, %s after (err %v)", lifetime, remaining, err)
	}
	if after, err := c.Get(ctx, attemptsKey(email)); err != nil || after != attempts {
		t.Fatalf("the attempt counter must be untouched: %q before, %q after (err %v)", attempts, after, err)
	}
	if err := otp.Verify(ctx, email, "123456"); err != nil {
		t.Fatalf("the code issued for the failed attempt must still work: %v", err)
	}
}

// TestDisarmingTheCooldownNeverClearsTheBlockMarker keeps the disarm narrow: the
// block marker is the brute-force protection, and a customer must not be able to
// shed it by having their message fail to send.
func TestDisarmingTheCooldownNeverClearsTheBlockMarker(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	otp := NewOTPStore(c, token.Hasher{}, 3, 15*time.Minute, time.Minute, time.Minute)
	const email = "blocked@example.com"
	hash, _ := token.Hasher{}.Hash("123456")
	if err := otp.Issue(ctx, email, hash); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	for i := 0; i < 3; i++ {
		_ = otp.Verify(ctx, email, "000000")
	}
	if err := otp.CanResend(ctx, email); !errors.Is(err, domainerr.ErrOTPBlocked) {
		t.Fatalf("expected ErrOTPBlocked, got %v", err)
	}

	if err := otp.DisarmCooldown(ctx, email); err != nil {
		t.Fatalf("DisarmCooldown: %v", err)
	}

	if err := otp.CanResend(ctx, email); !errors.Is(err, domainerr.ErrOTPBlocked) {
		t.Fatalf("disarming the cooldown must not lift the block, got %v", err)
	}
	if err := otp.Verify(ctx, email, "123456"); !errors.Is(err, domainerr.ErrOTPBlocked) {
		t.Fatalf("disarming the cooldown must not unblock verification, got %v", err)
	}
}

// TestOnlyTheMostRecentlyIssuedCodeIsAcceptedAfterAFailedDelivery covers the
// other half of FR-018: the code issued for the failed attempt is replaced by the
// one the customer asks for, and a replaced code can never complete verification.
func TestOnlyTheMostRecentlyIssuedCodeIsAcceptedAfterAFailedDelivery(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	otp := NewOTPStore(c, token.Hasher{}, 3, 15*time.Minute, time.Minute, time.Minute)
	const email = "replaced@example.com"
	first, err := token.Hasher{}.Hash("111111")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	second, err := token.Hasher{}.Hash("222222")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := otp.Issue(ctx, email, first); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := otp.DisarmCooldown(ctx, email); err != nil {
		t.Fatalf("DisarmCooldown: %v", err)
	}
	if err := otp.Issue(ctx, email, second); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := otp.Verify(ctx, email, "111111"); !errors.Is(err, domainerr.ErrOTPInvalid) {
		t.Fatalf("expected the replaced code to be rejected, got %v", err)
	}
	if err := otp.Verify(ctx, email, "222222"); err != nil {
		t.Fatalf("expected the most recent code to be accepted, got %v", err)
	}
}
