package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"

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
