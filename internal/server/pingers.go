package server

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

const pingTimeout = 2 * time.Second

type postgresPinger struct {
	dsn string
}

func newPostgresPinger(dsn string) *postgresPinger {
	return &postgresPinger{dsn: dsn}
}

func (p *postgresPinger) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	conn, err := pgx.Connect(ctx, p.dsn)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()

	return conn.Ping(ctx)
}

type redisPinger struct {
	client *redis.Client
}

func newRedisPinger(addr string) *redisPinger {
	return &redisPinger{client: redis.NewClient(&redis.Options{
		Addr:                  addr,
		ContextTimeoutEnabled: true,
	})}
}

func (p *redisPinger) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	return p.client.Ping(ctx).Err()
}

func (p *redisPinger) Close() error {
	return p.client.Close()
}
