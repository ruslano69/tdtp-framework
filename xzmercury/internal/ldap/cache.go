package ldap

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// CachingClient wraps any Client and caches IsMember results in Pipeline Redis.
// Negative results are also cached to avoid hammering the DC on every request.
type CachingClient struct {
	inner Client
	rdb   *redis.Client
	ttl   time.Duration
}

// NewCachingClient wraps inner with a Redis-backed cache.
// ttl is the duration for which membership results are cached (recommended: 120s).
func NewCachingClient(inner Client, rdb *redis.Client, ttl time.Duration) *CachingClient {
	return &CachingClient{inner: inner, rdb: rdb, ttl: ttl}
}

func (c *CachingClient) IsMember(ctx context.Context, user, group string) (bool, error) {
	cacheKey := fmt.Sprintf("ldap:member:%s:%s", user, group)

	cached, err := c.rdb.Get(ctx, cacheKey).Result()
	if err == nil {
		return cached == "1", nil
	}
	if err != redis.Nil {
		// Redis error — fall through to real LDAP (degraded, not fatal)
		_ = err
	}

	result, err := c.inner.IsMember(ctx, user, group)
	if err != nil {
		return false, err
	}

	val := "0"
	if result {
		val = "1"
	}
	// best-effort cache write; ignore error
	_ = c.rdb.Set(ctx, cacheKey, val, c.ttl).Err()
	return result, nil
}

func (c *CachingClient) Close() error {
	return c.inner.Close()
}
