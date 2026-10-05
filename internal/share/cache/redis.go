// Package cache wraps the Redis client used for short-lived state (OTP
// challenges, login lockout counters, access-token blacklist).
package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/config"
)

// ErrNotFound is returned by Get when a key does not exist.
var ErrNotFound = errors.New("cache: key not found")

// Cache is a thin, testable wrapper around a Redis client.
type Cache struct {
	rdb *redis.Client
}

// New connects to Redis and verifies connectivity.
func New(ctx context.Context, cfg config.RedisConfig) (*Cache, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &Cache{rdb: rdb}, nil
}

// Client exposes the underlying client for advanced operations.
func (c *Cache) Client() *redis.Client { return c.rdb }

// Ping checks connectivity.
func (c *Cache) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }

// Close releases the connection.
func (c *Cache) Close() error { return c.rdb.Close() }

// Set stores a value with a TTL.
func (c *Cache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return c.rdb.Set(ctx, key, value, ttl).Err()
}

// Get returns a value or ErrNotFound.
func (c *Cache) Get(ctx context.Context, key string) (string, error) {
	value, err := c.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	return value, err
}

// Del removes keys.
func (c *Cache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.rdb.Del(ctx, keys...).Err()
}

// Exists reports whether a key exists.
func (c *Cache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Incr increments a counter and applies ttl when the key is created.
func (c *Cache) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	pipe := c.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

// TTL returns the remaining time to live of a key.
func (c *Cache) TTL(ctx context.Context, key string) (time.Duration, error) {
	return c.rdb.TTL(ctx, key).Result()
}
