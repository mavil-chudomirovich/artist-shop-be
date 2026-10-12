package redis

import (
	"context"
	"errors"
	"time"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/cache"
)

// Redis key prefixes for a password-reset code and its brute-force state. All
// keys carry a TTL and are keyed by the normalized email the code was sent to;
// the code, its attempt counter and its block share the code lifetime, the send
// marker the cooldown.
const (
	resetOTPPrefix      = "auth:reset:otp:"
	resetAttemptsPrefix = "auth:reset:otp:attempts:"
	resetBlockedPrefix  = "auth:reset:otp:blocked:"
	resetSentPrefix     = "auth:reset:otp:sent:"
)

func resetOTPKey(email string) string      { return resetOTPPrefix + email }
func resetAttemptsKey(email string) string { return resetAttemptsPrefix + email }
func resetBlockedKey(email string) string  { return resetBlockedPrefix + email }
func resetSentKey(email string) string     { return resetSentPrefix + email }

// PasswordResetOTPStore implements appinterface.PasswordResetOTPStore on Redis.
// It holds the hashed reset code with its attempt, block and cooldown markers;
// nothing here is durable, so losing the store at worst forces a new code.
type PasswordResetOTPStore struct {
	cache       *cache.Cache
	hasher      appinterface.PasswordHasher
	maxAttempts int
	ttl         time.Duration
	blockTTL    time.Duration
	cooldown    time.Duration
}

var _ appinterface.PasswordResetOTPStore = (*PasswordResetOTPStore)(nil)

// NewPasswordResetOTPStore creates a Redis-backed password-reset code store.
func NewPasswordResetOTPStore(c *cache.Cache, hasher appinterface.PasswordHasher, maxAttempts int, ttl, blockTTL, cooldown time.Duration) *PasswordResetOTPStore {
	return &PasswordResetOTPStore{
		cache:       c,
		hasher:      hasher,
		maxAttempts: maxAttempts,
		ttl:         ttl,
		blockTTL:    blockTTL,
		cooldown:    cooldown,
	}
}

// Issue stores a new code hash, resets the attempt counter, clears any block and
// starts the resend cooldown. Reissuing overwrites the hash, so only the newest
// code works (FR-004).
func (s *PasswordResetOTPStore) Issue(ctx context.Context, email, otpHash string) error {
	if err := s.cache.Set(ctx, resetOTPKey(email), otpHash, s.ttl); err != nil {
		return err
	}
	if err := s.cache.Del(ctx, resetAttemptsKey(email), resetBlockedKey(email)); err != nil {
		return err
	}
	return s.cache.Set(ctx, resetSentKey(email), "1", s.cooldown)
}

// CanResend reports whether a new code may be requested for the address.
func (s *PasswordResetOTPStore) CanResend(ctx context.Context, email string) error {
	blocked, err := s.cache.Exists(ctx, resetBlockedKey(email))
	if err != nil {
		return err
	}
	if blocked {
		return domainerr.ErrOTPBlocked
	}
	sent, err := s.cache.Exists(ctx, resetSentKey(email))
	if err != nil {
		return err
	}
	if sent {
		return domainerr.ErrResendCooldown
	}
	return nil
}

// Verify checks the submitted code. After maxAttempts failures the code is
// removed and the address is blocked until the block expires. A correct code
// clears the attempt counter but is not consumed here: the caller invalidates it
// once the durable reset token is written, so a transient failure between
// verification and that write can be retried with the same code (research D3).
func (s *PasswordResetOTPStore) Verify(ctx context.Context, email, otp string) error {
	blocked, err := s.cache.Exists(ctx, resetBlockedKey(email))
	if err != nil {
		return err
	}
	if blocked {
		return domainerr.ErrOTPBlocked
	}

	storedHash, err := s.cache.Get(ctx, resetOTPKey(email))
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
		return s.cache.Del(ctx, resetAttemptsKey(email))
	}

	attempts, err := s.cache.Incr(ctx, resetAttemptsKey(email), s.ttl)
	if err != nil {
		return err
	}
	if attempts >= int64(s.maxAttempts) {
		if err := s.cache.Set(ctx, resetBlockedKey(email), "1", s.blockTTL); err != nil {
			return err
		}
		if err := s.cache.Del(ctx, resetOTPKey(email)); err != nil {
			return err
		}
		return domainerr.ErrOTPTooManyAttempts
	}
	return domainerr.ErrOTPInvalid
}

// Invalidate removes the active code and its attempt counter.
func (s *PasswordResetOTPStore) Invalidate(ctx context.Context, email string) error {
	return s.cache.Del(ctx, resetOTPKey(email), resetAttemptsKey(email))
}

// DisarmCooldown deletes only the resend marker. The code, its lifetime and the
// brute-force state are deliberately left alone: none of them belongs to
// delivery (FR-018).
func (s *PasswordResetOTPStore) DisarmCooldown(ctx context.Context, email string) error {
	return s.cache.Del(ctx, resetSentKey(email))
}
