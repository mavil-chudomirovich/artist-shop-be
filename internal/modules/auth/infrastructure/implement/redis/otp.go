package redis

import (
	"context"
	"errors"
	"time"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/cache"
)

// OTPStore persists email-confirmation OTP challenges in Redis.
type OTPStore struct {
	cache       *cache.Cache
	hasher      appinterface.PasswordHasher
	maxAttempts int
	ttl         time.Duration
	blockTTL    time.Duration
	cooldown    time.Duration
}

// NewOTPStore creates an OTP store.
func NewOTPStore(c *cache.Cache, hasher appinterface.PasswordHasher, maxAttempts int, ttl, blockTTL, cooldown time.Duration) *OTPStore {
	return &OTPStore{cache: c, hasher: hasher, maxAttempts: maxAttempts, ttl: ttl, blockTTL: blockTTL, cooldown: cooldown}
}

func otpKey(email string) string      { return "auth:otp:" + email }
func attemptsKey(email string) string { return "auth:otp:attempts:" + email }
func blockedKey(email string) string  { return "auth:otp:blocked:" + email }
func sentKey(email string) string     { return "auth:otp:sent:" + email }

// Issue stores a new OTP hash, resets the attempt counter, and starts the resend
// cooldown.
func (s *OTPStore) Issue(ctx context.Context, email, otpHash string) error {
	if err := s.cache.Set(ctx, otpKey(email), otpHash, s.ttl); err != nil {
		return err
	}
	if err := s.cache.Del(ctx, attemptsKey(email), blockedKey(email)); err != nil {
		return err
	}
	return s.cache.Set(ctx, sentKey(email), "1", s.cooldown)
}

// CanResend reports whether a new OTP may be requested.
func (s *OTPStore) CanResend(ctx context.Context, email string) error {
	blocked, err := s.cache.Exists(ctx, blockedKey(email))
	if err != nil {
		return err
	}
	if blocked {
		return domainerr.ErrOTPBlocked
	}
	sent, err := s.cache.Exists(ctx, sentKey(email))
	if err != nil {
		return err
	}
	if sent {
		return domainerr.ErrResendCooldown
	}
	return nil
}

// Verify checks the submitted OTP. After maxAttempts failures the OTP is
// blacklisted and cannot be retried until the block expires.
func (s *OTPStore) Verify(ctx context.Context, email, otp string) error {
	blocked, err := s.cache.Exists(ctx, blockedKey(email))
	if err != nil {
		return err
	}
	if blocked {
		return domainerr.ErrOTPBlocked
	}

	storedHash, err := s.cache.Get(ctx, otpKey(email))
	if errors.Is(err, cache.ErrNotFound) {
		return domainerr.ErrOTPExpired
	}
	if err != nil {
		return err
	}

	ok, err := s.hasher.Verify(storedHash, otp)
	if err != nil {
		return domainerr.ErrOTPInvalid
	}
	if ok {
		return s.cache.Del(ctx, otpKey(email), attemptsKey(email))
	}

	attempts, err := s.cache.Incr(ctx, attemptsKey(email), s.ttl)
	if err != nil {
		return err
	}
	if attempts >= int64(s.maxAttempts) {
		if err := s.cache.Set(ctx, blockedKey(email), "1", s.blockTTL); err != nil {
			return err
		}
		if err := s.cache.Del(ctx, otpKey(email)); err != nil {
			return err
		}
		return domainerr.ErrOTPTooManyAttempts
	}
	return domainerr.ErrOTPInvalid
}

// Invalidate removes any active OTP for the email.
func (s *OTPStore) Invalidate(ctx context.Context, email string) error {
	return s.cache.Del(ctx, otpKey(email), attemptsKey(email))
}

// DisarmCooldown deletes only the resend marker. The marker records "a message
// was sent recently", so a send that never happened must not consume the
// cooldown budget (FR-024). The code, its lifetime, the attempt counter and the
// block marker are deliberately left alone: none of them belongs to delivery,
// and the brute-force state a failed send must not weaken is exactly this
// (FR-018).
func (s *OTPStore) DisarmCooldown(ctx context.Context, email string) error {
	return s.cache.Del(ctx, sentKey(email))
}
