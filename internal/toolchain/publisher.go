package toolchain

import (
	"context"
	"strings"

	"github.com/redis/go-redis/v9"
)

type redisPublisher struct {
	client *redis.Client
}

var _ Publisher = (*redisPublisher)(nil)

func NewRedisPublisher(addr string) Publisher {
	if strings.TrimSpace(addr) == "" {
		return nil
	}
	return &redisPublisher{client: redis.NewClient(&redis.Options{Addr: addr})}
}

func (p *redisPublisher) Publish(ctx context.Context, channel, payload string) error {
	return p.client.Publish(ctx, channel, payload).Err()
}

func (p *redisPublisher) Close() error {
	return p.client.Close()
}
