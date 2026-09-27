package deploy

import (
	"context"
	"strings"

	"github.com/redis/go-redis/v9"
)

// redisPublisher fans deploy log events out through Redis. The realtime
// bridge already PSubscribes to logs:*:*, so publishing on
// logs:{serverID}:{deploymentID} reaches WebSocket clients unchanged.
type redisPublisher struct {
	client *redis.Client
}

// Compile-time guarantee that redisPublisher satisfies the Publisher seam.
var _ Publisher = (*redisPublisher)(nil)

// NewRedisPublisher returns a Publisher over the Redis instance at addr. An
// empty address returns nil, which turns the emitter into a no-op (logs then
// reach only subscribers that poll the deployments API).
func NewRedisPublisher(addr string) Publisher {
	if strings.TrimSpace(addr) == "" {
		return nil
	}
	return &redisPublisher{client: redis.NewClient(&redis.Options{Addr: addr})}
}

// Publish implements Publisher.
func (p *redisPublisher) Publish(ctx context.Context, channel, payload string) error {
	return p.client.Publish(ctx, channel, payload).Err()
}

// Close releases the Redis connection; it is called by Service.Close.
func (p *redisPublisher) Close() error {
	return p.client.Close()
}

// defaultPublisher picks the configured publisher, falling back to a Redis
// one built from the configured address (nil when neither is set).
func defaultPublisher(cfg Config) Publisher {
	if cfg.Publisher != nil {
		return cfg.Publisher
	}
	return NewRedisPublisher(cfg.RedisAddr)
}
