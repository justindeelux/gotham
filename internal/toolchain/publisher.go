package toolchain

import (
	"context"
	"strings"

	"github.com/redis/go-redis/v9"
)

// redisPublisher fans toolchain events out through Redis. The realtime bridge
// already PSubscribes to logs:*:*, and Channel lives in that namespace, so
// publishing reaches WebSocket clients with no bridge change.
type redisPublisher struct {
	client *redis.Client
}

// Compile-time guarantee that redisPublisher satisfies the Publisher seam.
var _ Publisher = (*redisPublisher)(nil)

// NewRedisPublisher returns a Publisher over the Redis instance at addr. An
// empty address returns nil, which turns the emitter into a no-op (job state is
// then only visible by polling the jobs API).
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

// Close releases the Redis connection; Service.Close calls it.
func (p *redisPublisher) Close() error {
	return p.client.Close()
}
