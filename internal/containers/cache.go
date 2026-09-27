package containers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// DefaultCacheTTL bounds how long List results are served from Redis.
const DefaultCacheTTL = 10 * time.Second

// defaultRedisAddr applies when no address is configured.
const defaultRedisAddr = "localhost:6379"

// Cache stores List results per server. Implementations must be safe for
// concurrent use; errors are reported so the service can degrade gracefully
// to a direct agent call.
type Cache interface {
	Get(ctx context.Context, serverID uuid.UUID) ([]Container, bool, error)
	Set(ctx context.Context, serverID uuid.UUID, containers []Container) error
	Invalidate(ctx context.Context, serverID uuid.UUID) error
}

// RedisCache is the production Cache backed by go-redis.
type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisCache builds a Cache over Redis at addr (empty falls back to
// localhost:6379). Construction never dials; an unreachable Redis surfaces as
// operation errors that the service treats as a cache miss.
func NewRedisCache(addr string) *RedisCache {
	if addr == "" {
		addr = defaultRedisAddr
	}
	return &RedisCache{
		client: redis.NewClient(&redis.Options{Addr: addr}),
		ttl:    DefaultCacheTTL,
	}
}

// Close releases the underlying Redis client.
func (c *RedisCache) Close() error {
	return c.client.Close()
}

// key namespaces cached container lists per server.
func (c *RedisCache) key(serverID uuid.UUID) string {
	return "containers:" + serverID.String()
}

// Get returns the cached list, or ok=false on a miss or any Redis error.
func (c *RedisCache) Get(ctx context.Context, serverID uuid.UUID) ([]Container, bool, error) {
	raw, err := c.client.Get(ctx, c.key(serverID)).Bytes()
	if err != nil {
		return nil, false, err
	}
	var containers []Container
	if err := json.Unmarshal(raw, &containers); err != nil {
		return nil, false, err
	}
	if containers == nil {
		containers = []Container{}
	}
	return containers, true, nil
}

// Set caches the list with the default TTL.
func (c *RedisCache) Set(ctx context.Context, serverID uuid.UUID, containers []Container) error {
	raw, err := json.Marshal(containers)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.key(serverID), raw, c.ttl).Err()
}

// Invalidate drops the cached list for a server after a mutation.
func (c *RedisCache) Invalidate(ctx context.Context, serverID uuid.UUID) error {
	return c.client.Del(ctx, c.key(serverID)).Err()
}

// NopCache disables caching: every lookup misses and writes are dropped. It
// is useful in tests and in environments without Redis.
type NopCache struct{}

// Get always reports a miss.
func (NopCache) Get(context.Context, uuid.UUID) ([]Container, bool, error) {
	return nil, false, nil
}

// Set discards the list.
func (NopCache) Set(context.Context, uuid.UUID, []Container) error {
	return nil
}

// Invalidate is a no-op.
func (NopCache) Invalidate(context.Context, uuid.UUID) error {
	return nil
}
