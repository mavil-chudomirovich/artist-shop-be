package redis

import (
	"context"
	"time"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/cache"
)

// LoginGuard tracks failed sign-in attempts and locks out a source temporarily.
type LoginGuard struct {
	cache       *cache.Cache
	maxFailures int
	window      time.Duration
	blockTTL    time.Duration
}

// NewLoginGuard creates a login guard.
func NewLoginGuard(c *cache.Cache, maxFailures int, window, blockTTL time.Duration) *LoginGuard {
	return &LoginGuard{cache: c, maxFailures: maxFailures, window: window, blockTTL: blockTTL}
}

func loginFailuresKey(source string) string { return "auth:login:failures:" + source }
func loginBlockedKey(source string) string  { return "auth:login:blocked:" + source }

// Blocked reports whether the source is currently locked out.
func (g *LoginGuard) Blocked(ctx context.Context, source string) (bool, error) {
	return g.cache.Exists(ctx, loginBlockedKey(source))
}

// RecordFailure increments the failure counter and locks the source when the
// threshold is reached. It reports whether the source is now blocked.
func (g *LoginGuard) RecordFailure(ctx context.Context, source string) (bool, error) {
	attempts, err := g.cache.Incr(ctx, loginFailuresKey(source), g.window)
	if err != nil {
		return false, err
	}
	if attempts >= int64(g.maxFailures) {
		if err := g.cache.Set(ctx, loginBlockedKey(source), "1", g.blockTTL); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// Reset clears the failure counter and lockout after a successful sign-in.
func (g *LoginGuard) Reset(ctx context.Context, source string) error {
	return g.cache.Del(ctx, loginFailuresKey(source), loginBlockedKey(source))
}
