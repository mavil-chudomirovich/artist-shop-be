// Package redis implements the auth module's Redis-backed ports: OTP codes,
// token blacklists and the failed-login guard.
package redis

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/cache"
)

// BlacklistStore records revoked access tokens in Redis.
type BlacklistStore struct {
	cache *cache.Cache
}

// NewBlacklistStore creates a blacklist store.
func NewBlacklistStore(c *cache.Cache) *BlacklistStore { return &BlacklistStore{cache: c} }

func jtiKey(jti string) string              { return "auth:blacklist:jti:" + jti }
func userMinIATKey(userID uuid.UUID) string { return "auth:blacklist:user:" + userID.String() }

// RevokeJTI blacklists a single access token until ttl elapses.
func (s *BlacklistStore) RevokeJTI(ctx context.Context, jti string, ttl time.Duration) error {
	return s.cache.Set(ctx, jtiKey(jti), "1", ttl)
}

// IsJTIRevoked reports whether a token identifier has been blacklisted.
func (s *BlacklistStore) IsJTIRevoked(ctx context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}
	return s.cache.Exists(ctx, jtiKey(jti))
}

// RevokeUserBefore records that access tokens issued before at are invalid for
// the user (bulk revocation on password change/reset).
func (s *BlacklistStore) RevokeUserBefore(ctx context.Context, userID uuid.UUID, at time.Time, ttl time.Duration) error {
	return s.cache.Set(ctx, userMinIATKey(userID), strconv.FormatInt(at.UTC().Unix(), 10), ttl)
}

// IsIssuedBefore reports whether an access token issued at iat is invalidated by
// a prior bulk revocation for the user.
func (s *BlacklistStore) IsIssuedBefore(ctx context.Context, userID uuid.UUID, iat time.Time) (bool, error) {
	raw, err := s.cache.Get(ctx, userMinIATKey(userID))
	if err != nil {
		if err == cache.ErrNotFound {
			return false, nil
		}
		return false, err
	}
	minIAT, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return false, err
	}
	return iat.UTC().Unix() < minIAT, nil
}
