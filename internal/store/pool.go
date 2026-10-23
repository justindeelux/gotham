package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	connectAttempts  = 5
	connectBaseDelay = 250 * time.Millisecond
	connectMaxDelay  = 2 * time.Second
	openTimeout      = 15 * time.Second
	pingTimeout      = 5 * time.Second
	probeTimeout     = 3 * time.Second
)

var ErrNilPool = errors.New("store: nil pool")

func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()

	var lastErr error
	delay := connectBaseDelay
	for attempt := 1; attempt <= connectAttempts; attempt++ {
		pool, err := pgxpool.NewWithConfig(context.Background(), config)
		if err == nil {
			if err = Ping(ctx, pool); err == nil {
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err

		if attempt == connectAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("connect to postgres: %w", ctx.Err())
		case <-time.After(delay):
		}
		delay = min(delay*2, connectMaxDelay)
	}
	return nil, fmt.Errorf("connect to postgres after %d attempts: %w", connectAttempts, lastErr)
}

func ProbeOnce(ctx context.Context, dsn string) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()

	return conn.Ping(ctx)
}

func Ping(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return ErrNilPool
	}
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	return pool.Ping(ctx)
}
