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
	containers, err := decodeContainers(raw)
	if err != nil {
		return nil, false, err
	}
	return containers, true, nil
}

// Set caches the list with the default TTL.
func (c *RedisCache) Set(ctx context.Context, serverID uuid.UUID, containers []Container) error {
	raw, err := encodeContainers(containers)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.key(serverID), raw, c.ttl).Err()
}

// cachedContainer is the cache's wire form of a Container. Container's public
// JSON tags deliberately hide internal evidence (labels, mounts, restart
// policy) from API responses, so a plain json.Marshal would strip them and a
// cache hit would look like a container with no labels — silently defeating any
// caller that reads them. The cache therefore round-trips the full record
// through this codec.
type cachedContainer struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	State         string            `json:"state"`
	Status        string            `json:"status"`
	Ports         []string          `json:"ports"`
	Created       *time.Time        `json:"created,omitempty"`
	PortsReported bool              `json:"ports_reported,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Mounts        []ContainerMount  `json:"mounts,omitempty"`
	RestartPolicy string            `json:"restart_policy,omitempty"`
	Health        string            `json:"health,omitempty"`
}

// encodeContainers serialises a list for the cache, preserving internal
// evidence that Container's API tags omit. cachedContainer has the same fields
// as Container but with JSON tags, so the conversion is a reinterpretation.
func encodeContainers(containers []Container) ([]byte, error) {
	cached := make([]cachedContainer, 0, len(containers))
	for _, container := range containers {
		cached = append(cached, cachedContainer(container))
	}
	return json.Marshal(cached)
}

// decodeContainers reverses encodeContainers. The list is never nil so the API
// renders [] rather than null.
func decodeContainers(raw []byte) ([]Container, error) {
	var cached []cachedContainer
	if err := json.Unmarshal(raw, &cached); err != nil {
		return nil, err
	}
	containers := make([]Container, 0, len(cached))
	for _, item := range cached {
		containers = append(containers, Container(item))
	}
	return containers, nil
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
