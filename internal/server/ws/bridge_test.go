package ws

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// logBuffer is a goroutine-safe buffer for capturing bridge logs.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestBridgeReconnectsAfterFailures is the B1-5 regression: an unreachable
// Redis must not kill the bridge. The supervisor retries with backoff, logs
// each failure, and forwards again once the subscription comes back.
func TestBridgeReconnectsAfterFailures(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Close)

	client := hub.newClient()
	channel := LogChannel("srv-bridge", "ctr")
	hub.subscribe(client, channel)

	var logs logBuffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	var attempts atomic.Int32
	messages := make(chan *redis.Message, 4)
	bridge := NewBridge(hub, nil, logger)
	bridge.minBackoff = time.Millisecond
	bridge.maxBackoff = 2 * time.Millisecond
	bridge.subscribe = func(context.Context) (messageStream, error) {
		if attempts.Add(1) < 3 {
			return messageStream{}, errors.New("connection refused")
		}
		return messageStream{messages: messages, close: func() {}}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = bridge.Run(ctx) }()

	waitForCondition(t, 3*time.Second, "bridge reconnect", func() bool {
		return attempts.Load() >= 3
	})

	// Once reconnected, messages reach hub subscribers again.
	messages <- &redis.Message{Channel: channel, Payload: "recovered"}

	select {
	case payload := <-client.send:
		if string(payload) != "recovered" {
			t.Errorf("forwarded payload = %q, want %q", payload, "recovered")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bridge did not forward after reconnecting (B1-5)")
	}

	waitForCondition(t, 2*time.Second, "failure log", func() bool {
		return strings.Contains(logs.String(), "subscribe failed")
	})
}

// TestBridgeBackoffAfterCycle is the U8 regression: the reconnect backoff is
// reset only after a subscription stayed up for stableAfter, so a Redis that
// accepts then immediately drops keeps backing off.
func TestBridgeBackoffAfterCycle(t *testing.T) {
	bridge := NewBridge(NewHub(), nil, slog.Default())
	bridge.minBackoff = 250 * time.Millisecond
	bridge.maxBackoff = 30 * time.Second
	bridge.stableAfter = 5 * time.Second

	if got := bridge.backoffAfterCycle(10*time.Second, 6*time.Second); got != bridge.minBackoff {
		t.Errorf("stable cycle backoff = %s, want %s", got, bridge.minBackoff)
	}
	if got := bridge.backoffAfterCycle(10*time.Second, time.Second); got != 10*time.Second {
		t.Errorf("unstable cycle backoff = %s, want it unchanged", got)
	}
}

// TestBridgeRunStopsOnCancel verifies the supervisor returns promptly once its
// context ends, so Realtime.Close does not hang on the bridge goroutine.
func TestBridgeRunStopsOnCancel(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Close)

	bridge := NewBridge(hub, nil, slog.Default())
	bridge.minBackoff = time.Millisecond
	bridge.maxBackoff = time.Millisecond
	bridge.subscribe = func(context.Context) (messageStream, error) {
		return messageStream{}, errors.New("down")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = bridge.Run(ctx); close(done) }()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge.Run did not return after cancellation")
	}
}
