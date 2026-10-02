package server

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

// pingTimeout bounds each individual health-check ping.
const pingTimeout = 2 * time.Second

// postgresPinger checks PostgreSQL by opening a short-lived connection.
type postgresPinger struct {
	dsn string
}

func newPostgresPinger(dsn string) *postgresPinger {
	return &postgresPinger{dsn: dsn}
}

// Ping dials PostgreSQL and verifies the connection, honouring ctx plus the
// internal ping timeout.
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

// redisPinger checks Redis using a reusable client.
type redisPinger struct {
	client *redis.Client
}

func newRedisPinger(addr string) *redisPinger {
	return &redisPinger{client: redis.NewClient(&redis.Options{
		Addr: addr,
		// Respect the health-check deadline. Without this go-redis swaps in
		// context.Background(), so a hung Redis blocks until the 5s socket
		// timeout — past the advertised ~2s health budget.
		ContextTimeoutEnabled: true,
	})}
}

// Ping issues PING, honouring ctx plus the internal ping timeout.
func (p *redisPinger) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	return p.client.Ping(ctx).Err()
}

// Close releases the underlying Redis client.
func (p *redisPinger) Close() error {
	return p.client.Close()
}
