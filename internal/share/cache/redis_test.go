package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(server.Close)

	c, err := New(context.Background(), config.RedisConfig{Addr: server.Addr()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestSetGetDel(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	if err := c.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Get(ctx, "k")
	if err != nil || got != "v" {
		t.Fatalf("Get: got %q err %v", got, err)
	}
	if err := c.Del(ctx, "k"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	if _, err := c.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestIncrSetsTTL(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	for i := int64(1); i <= 3; i++ {
		n, err := c.Incr(ctx, "counter", time.Minute)
		if err != nil {
			t.Fatalf("Incr: %v", err)
		}
		if n != i {
			t.Fatalf("expected %d, got %d", i, n)
		}
	}
	ttl, err := c.TTL(ctx, "counter")
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("expected positive TTL, got %s", ttl)
	}
}

func TestExists(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	ok, err := c.Exists(ctx, "missing")
	if err != nil || ok {
		t.Fatalf("expected false, got %v err %v", ok, err)
	}
	_ = c.Set(ctx, "present", "1", time.Minute)
	ok, err = c.Exists(ctx, "present")
	if err != nil || !ok {
		t.Fatalf("expected true, got %v err %v", ok, err)
	}
}
