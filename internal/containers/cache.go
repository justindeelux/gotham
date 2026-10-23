package containers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const DefaultCacheTTL = 10 * time.Second

const defaultRedisAddr = "localhost:6379"

type Cache interface {
	Get(ctx context.Context, serverID uuid.UUID) ([]Container, bool, error)
	Set(ctx context.Context, serverID uuid.UUID, containers []Container) error
	Invalidate(ctx context.Context, serverID uuid.UUID) error
}

type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisCache(addr string) *RedisCache {
	if addr == "" {
		addr = defaultRedisAddr
	}
	return &RedisCache{
		client: redis.NewClient(&redis.Options{Addr: addr}),
		ttl:    DefaultCacheTTL,
	}
}

func (c *RedisCache) Close() error {
	return c.client.Close()
}

func (c *RedisCache) key(serverID uuid.UUID) string {
	return "containers:" + serverID.String()
}

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

func (c *RedisCache) Set(ctx context.Context, serverID uuid.UUID, containers []Container) error {
	raw, err := encodeContainers(containers)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.key(serverID), raw, c.ttl).Err()
}

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

func encodeContainers(containers []Container) ([]byte, error) {
	cached := make([]cachedContainer, 0, len(containers))
	for _, container := range containers {
		cached = append(cached, cachedContainer(container))
	}
	return json.Marshal(cached)
}

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

func (c *RedisCache) Invalidate(ctx context.Context, serverID uuid.UUID) error {
	return c.client.Del(ctx, c.key(serverID)).Err()
}

type NopCache struct{}

func (NopCache) Get(context.Context, uuid.UUID) ([]Container, bool, error) {
	return nil, false, nil
}

func (NopCache) Set(context.Context, uuid.UUID, []Container) error {
	return nil
}

func (NopCache) Invalidate(context.Context, uuid.UUID) error {
	return nil
}
