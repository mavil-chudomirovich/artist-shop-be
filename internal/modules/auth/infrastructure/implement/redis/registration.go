package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	appinterface "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/application/interface"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/auth/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/cache"
)

// Redis key prefixes for a temporary registration and its code state. All keys
// carry a TTL; the registration and its pointer share the registration lifetime,
// the code state shares the code lifetime.
const (
	regPrefix         = "auth:reg:"
	regEmailPrefix    = "auth:reg:email:"
	regOTPPrefix      = "auth:reg:otp:"
	regAttemptsPrefix = "auth:reg:otp:attempts:"
	regBlockedPrefix  = "auth:reg:otp:blocked:"
	regSentPrefix     = "auth:reg:otp:sent:"
)

func regKey(id string) string         { return regPrefix + id }
func regEmailKey(email string) string { return regEmailPrefix + email }
func regOTPKey(id string) string      { return regOTPPrefix + id }
func regAttemptsKey(id string) string { return regAttemptsPrefix + id }
func regBlockedKey(id string) string  { return regBlockedPrefix + id }
func regSentKey(id string) string     { return regSentPrefix + id }

// saveScript atomically replaces any previous live registration for the same
// email and writes the new record and pointer, so a double-submit can never
// leave two live registrations or orphaned keys (FR-026). The previous
// registration's record, code, attempt counter, block and cooldown markers are
// all deleted so its reference and code stop working.
//
// KEYS[1] new record key, KEYS[2] email pointer key.
// ARGV[1] registration TTL in ms, ARGV[2] new id, ARGV[3] email,
// ARGV[4] username, ARGV[5] displayName, ARGV[6] passwordHash, ARGV[7] createdAt.
var saveScript = fmt.Sprintf(`
local prev = redis.call('GET', KEYS[2])
if prev and prev ~= ARGV[2] then
  redis.call('DEL', '%s' .. prev)
  redis.call('DEL', '%s' .. prev)
  redis.call('DEL', '%s' .. prev)
  redis.call('DEL', '%s' .. prev)
  redis.call('DEL', '%s' .. prev)
end
redis.call('HSET', KEYS[1], 'email', ARGV[3])
redis.call('HSET', KEYS[1], 'username', ARGV[4])
redis.call('HSET', KEYS[1], 'displayName', ARGV[5])
redis.call('HSET', KEYS[1], 'passwordHash', ARGV[6])
redis.call('HSET', KEYS[1], 'createdAt', ARGV[7])
redis.call('HSET', KEYS[1], 'status', 'pending')
redis.call('PEXPIRE', KEYS[1], ARGV[1])
redis.call('SET', KEYS[2], ARGV[2])
redis.call('PEXPIRE', KEYS[2], ARGV[1])
return 1
`, regPrefix, regOTPPrefix, regAttemptsPrefix, regBlockedPrefix, regSentPrefix)

// completeScript marks a registration completed after its account was durably
// created. It removes the code state and the email pointer (only when the
// pointer still names this registration) and sets the record's status, leaving
// the record and its remaining TTL in place so a repeated confirmation reports
// success without creating a second account (FR-010, FR-011).
//
// KEYS[1] record key. ARGV[1] id.
var completeScript = fmt.Sprintf(`
local email = redis.call('HGET', KEYS[1], 'email')
redis.call('HSET', KEYS[1], 'status', 'completed')
redis.call('DEL', '%s' .. ARGV[1])
redis.call('DEL', '%s' .. ARGV[1])
redis.call('DEL', '%s' .. ARGV[1])
redis.call('DEL', '%s' .. ARGV[1])
if email then
  local pkey = '%s' .. email
  if redis.call('GET', pkey) == ARGV[1] then
    redis.call('DEL', pkey)
  end
end
return 1
`, regOTPPrefix, regAttemptsPrefix, regBlockedPrefix, regSentPrefix, regEmailPrefix)

// deleteScript removes a registration's record, code state and email pointer.
// The pointer is only removed when it still points at this registration, so a
// replacement that already re-pointed the email is not undone.
//
// KEYS[1] record key. ARGV[1] id.
var deleteScript = fmt.Sprintf(`
local email = redis.call('HGET', KEYS[1], 'email')
redis.call('DEL', KEYS[1])
redis.call('DEL', '%s' .. ARGV[1])
redis.call('DEL', '%s' .. ARGV[1])
redis.call('DEL', '%s' .. ARGV[1])
redis.call('DEL', '%s' .. ARGV[1])
if email then
  local pkey = '%s' .. email
  if redis.call('GET', pkey) == ARGV[1] then
    redis.call('DEL', pkey)
  end
end
return 1
`, regOTPPrefix, regAttemptsPrefix, regBlockedPrefix, regSentPrefix, regEmailPrefix)

// RegistrationStore implements appinterface.RegistrationStore on Redis. It
// holds the temporary registration and the hashed code with their independent
// lifetimes; nothing here is durable.
type RegistrationStore struct {
	cache       *cache.Cache
	hasher      appinterface.PasswordHasher
	maxAttempts int
	ttl         time.Duration
	blockTTL    time.Duration
	cooldown    time.Duration
	pendingTTL  time.Duration
}

var _ appinterface.RegistrationStore = (*RegistrationStore)(nil)

// NewRegistrationStore creates a Redis-backed temporary registration store.
func NewRegistrationStore(c *cache.Cache, hasher appinterface.PasswordHasher, maxAttempts int, ttl, blockTTL, cooldown, pendingTTL time.Duration) *RegistrationStore {
	return &RegistrationStore{
		cache:       c,
		hasher:      hasher,
		maxAttempts: maxAttempts,
		ttl:         ttl,
		blockTTL:    blockTTL,
		cooldown:    cooldown,
		pendingTTL:  pendingTTL,
	}
}

// Save stores the registration and its email pointer, replacing any live
// registration for the same email atomically.
func (s *RegistrationStore) Save(ctx context.Context, reg model.PendingRegistration) error {
	username, displayName := "", ""
	if reg.Username != nil {
		username = *reg.Username
	}
	if reg.DisplayName != nil {
		displayName = *reg.DisplayName
	}
	keys := []string{regKey(reg.RegistrationID), regEmailKey(reg.Email)}
	args := []any{
		s.pendingTTL.Milliseconds(),
		reg.RegistrationID,
		reg.Email,
		username,
		displayName,
		reg.PasswordHash,
		reg.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	return s.cache.Client().Eval(ctx, saveScript, keys, args...).Err()
}

// ByID loads a registration by its reference.
func (s *RegistrationStore) ByID(ctx context.Context, registrationID string) (model.PendingRegistration, error) {
	fields, err := s.cache.Client().HGetAll(ctx, regKey(registrationID)).Result()
	if err != nil {
		return model.PendingRegistration{}, err
	}
	if len(fields) == 0 {
		return model.PendingRegistration{}, domainerr.ErrRegistrationNotFound
	}
	createdAt, err := time.Parse(time.RFC3339Nano, fields["createdAt"])
	if err != nil {
		return model.PendingRegistration{}, fmt.Errorf("parse registration createdAt: %w", err)
	}
	status := fields["status"]
	if status == "" {
		status = model.RegistrationStatusPending
	}
	return model.PendingRegistration{
		RegistrationID: registrationID,
		Email:          fields["email"],
		Username:       optionalString(fields["username"]),
		DisplayName:    optionalString(fields["displayName"]),
		PasswordHash:   fields["passwordHash"],
		CreatedAt:      createdAt,
		ExpiresAt:      createdAt.Add(s.pendingTTL),
		Status:         status,
	}, nil
}

// Delete removes the registration, its code state and its email pointer. It is
// idempotent, so cancelling an unknown or expired reference is a no-op.
func (s *RegistrationStore) Delete(ctx context.Context, registrationID string) error {
	return s.cache.Client().Eval(ctx, deleteScript, []string{regKey(registrationID)}, registrationID).Err()
}

// Complete marks the registration completed: the code state and the email
// pointer are removed and the record is retained (code-less) until its TTL, so
// a repeated confirmation reports success without creating a second account
// (FR-010, FR-011). It is idempotent.
func (s *RegistrationStore) Complete(ctx context.Context, registrationID string) error {
	return s.cache.Client().Eval(ctx, completeScript, []string{regKey(registrationID)}, registrationID).Err()
}

// IssueOTP stores a new code hash, resets the attempt counter, clears any block
// and starts the resend cooldown.
func (s *RegistrationStore) IssueOTP(ctx context.Context, registrationID, otpHash string) error {
	if err := s.cache.Set(ctx, regOTPKey(registrationID), otpHash, s.ttl); err != nil {
		return err
	}
	if err := s.cache.Del(ctx, regAttemptsKey(registrationID), regBlockedKey(registrationID)); err != nil {
		return err
	}
	return s.cache.Set(ctx, regSentKey(registrationID), "1", s.cooldown)
}

// CanResend reports whether a new code may be requested.
func (s *RegistrationStore) CanResend(ctx context.Context, registrationID string) error {
	blocked, err := s.cache.Exists(ctx, regBlockedKey(registrationID))
	if err != nil {
		return err
	}
	if blocked {
		return domainerr.ErrOTPBlocked
	}
	sent, err := s.cache.Exists(ctx, regSentKey(registrationID))
	if err != nil {
		return err
	}
	if sent {
		return domainerr.ErrResendCooldown
	}
	return nil
}

// VerifyOTP checks the submitted code. After maxAttempts failures the code is
// removed and the reference is blocked until the block expires.
func (s *RegistrationStore) VerifyOTP(ctx context.Context, registrationID, otp string) error {
	blocked, err := s.cache.Exists(ctx, regBlockedKey(registrationID))
	if err != nil {
		return err
	}
	if blocked {
		return domainerr.ErrOTPBlocked
	}

	storedHash, err := s.cache.Get(ctx, regOTPKey(registrationID))
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
		// A correct code is not consumed here: the account insert that follows
		// may fail, and the visitor must be able to retry with the same code.
		// Only the attempt counter is cleared; the code is removed when the
		// registration is completed (FR-010, research D5).
		return s.cache.Del(ctx, regAttemptsKey(registrationID))
	}

	attempts, err := s.cache.Incr(ctx, regAttemptsKey(registrationID), s.ttl)
	if err != nil {
		return err
	}
	if attempts >= int64(s.maxAttempts) {
		if err := s.cache.Set(ctx, regBlockedKey(registrationID), "1", s.blockTTL); err != nil {
			return err
		}
		if err := s.cache.Del(ctx, regOTPKey(registrationID)); err != nil {
			return err
		}
		return domainerr.ErrOTPTooManyAttempts
	}
	return domainerr.ErrOTPInvalid
}

// InvalidateOTP removes the active code and its attempt counter.
func (s *RegistrationStore) InvalidateOTP(ctx context.Context, registrationID string) error {
	return s.cache.Del(ctx, regOTPKey(registrationID), regAttemptsKey(registrationID))
}

// DisarmCooldown deletes only the resend marker. The code, its lifetime, the
// attempt counter and the block marker are deliberately left alone: none of them
// belongs to delivery (FR-018, FR-024).
func (s *RegistrationStore) DisarmCooldown(ctx context.Context, registrationID string) error {
	return s.cache.Del(ctx, regSentKey(registrationID))
}

// optionalString maps an empty stored value back to a nil pointer, so an absent
// optional profile field round-trips as nil.
func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
