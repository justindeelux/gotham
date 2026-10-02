package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Bridge retry bounds. The supervisor starts at the minimum and doubles up to
// the maximum, so an unreachable Redis at control-plane start is retried
// forever instead of killing realtime for the process lifetime (B1-5).
const (
	bridgeMinBackoff = 250 * time.Millisecond
	bridgeMaxBackoff = 30 * time.Second
)

// logPattern matches every container/deploy log channel.
const logPattern = "logs:*:*"

// LogChannel is the Redis channel carrying the logs of one container.
func LogChannel(serverID, containerID string) string {
	return "logs:" + serverID + ":" + containerID
}

// LogStreamer opens a container log stream. Its StreamLogs method matches
// agentv1.DockerServiceClient.StreamLogs so any DockerService client (or fake
// in tests) can feed the publisher without depending on the sibling package
// that owns the CP-to-agent mTLS dial.
type LogStreamer interface {
	StreamLogs(ctx context.Context, in *agentv1.StreamLogsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error)
}

// Publisher receives framed log payloads keyed by channel.
type Publisher interface {
	Publish(ctx context.Context, channel string, payload []byte) error
}

// HubPublisher adapts a Hub to the Publisher interface for the
// REALTIME_ENABLED=false fallback (and for tests).
type HubPublisher struct {
	Hub *Hub
}

// Publish broadcasts payload to hub subscribers of channel.
func (p HubPublisher) Publish(_ context.Context, channel string, payload []byte) error {
	p.Hub.Broadcast(channel, payload)
	return nil
}

// RedisPublisher adapts a go-redis client to the Publisher interface.
type RedisPublisher struct {
	RDB *redis.Client
}

// Publish publishes payload to the Redis channel.
func (p RedisPublisher) Publish(ctx context.Context, channel string, payload []byte) error {
	return p.RDB.Publish(ctx, channel, payload).Err()
}

// disconnectNotice frames a stream-loss notice for WS clients.
func disconnectNotice(channel, reason string) []byte {
	payload, _ := json.Marshal(Message{Channel: channel, Type: TypeDisconnect, Data: reason})
	return payload
}

// resumedNotice frames a transport-recovery notice for WS clients.
func resumedNotice(channel string) []byte {
	payload, _ := json.Marshal(Message{Channel: channel, Type: TypeResumed, Data: "log stream resumed"})
	return payload
}

// logPayload frames one log chunk for WS clients.
func logPayload(channel string, data []byte) []byte {
	payload, _ := json.Marshal(Message{Channel: channel, Type: TypeLog, Data: string(data)})
	return payload
}

// PublishStream tails one container through streamer and forwards every chunk
// to pub under LogChannel(serverID, containerID). When the gRPC stream ends
// or fails it publishes a disconnect notice so WS clients learn the stream is
// gone, then returns. The caller decides whether to resubscribe.
func PublishStream(ctx context.Context, streamer LogStreamer, pub Publisher, serverID, containerID string, req *agentv1.StreamLogsRequest) error {
	channel := LogChannel(serverID, containerID)

	stream, err := streamer.StreamLogs(ctx, req)
	if err != nil {
		_ = pub.Publish(ctx, channel, disconnectNotice(channel, "log stream unavailable: "+err.Error()))
		return fmt.Errorf("ws: open log stream: %w", err)
	}

	for {
		chunk, err := stream.Recv()
		if err != nil {
			reason := "log stream closed"
			if ctx.Err() != nil {
				reason = "log stream cancelled"
			} else if err.Error() != "" {
				reason = "log stream closed: " + err.Error()
			}
			_ = pub.Publish(ctx, channel, disconnectNotice(channel, reason))
			return err
		}
		if chunk == nil || len(chunk.Data) == 0 {
			continue
		}
		if err := pub.Publish(ctx, channel, logPayload(channel, chunk.Data)); err != nil {
			return fmt.Errorf("ws: publish logs: %w", err)
		}
	}
}

// messageStream is the slice of a Redis subscription the bridge consumes: the
// message channel plus a close func. It is a seam so the reconnect supervisor
// can be tested without a live Redis.
type messageStream struct {
	messages <-chan *redis.Message
	close    func()
}

// subscribeFunc opens a pattern subscription and blocks until it is confirmed,
// so an unreachable Redis surfaces as an error here.
type subscribeFunc func(ctx context.Context) (messageStream, error)

// bridgeStableAfter is how long a subscription must stay up before its
// reconnect backoff is reset. A Redis that accepts then immediately drops keeps
// backing off instead of hot-looping at the minimum with a warning each time
// (U8).
const bridgeStableAfter = 5 * time.Second

// Bridge subscribes to the Redis log channels and forwards every message into
// the hub. One bridge serves all channels via a pattern subscription, and its
// supervisor reconnects with bounded backoff whenever the subscription drops.
type Bridge struct {
	hub    *Hub
	rdb    *redis.Client
	logger *slog.Logger

	subscribe   subscribeFunc
	minBackoff  time.Duration
	maxBackoff  time.Duration
	stableAfter time.Duration
}

// NewBridge builds a Redis-to-hub bridge. The caller runs Run. A nil logger
// falls back to slog.Default.
func NewBridge(hub *Hub, rdb *redis.Client, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	b := &Bridge{
		hub:         hub,
		rdb:         rdb,
		logger:      logger,
		minBackoff:  bridgeMinBackoff,
		maxBackoff:  bridgeMaxBackoff,
		stableAfter: bridgeStableAfter,
	}
	b.subscribe = b.redisSubscribe
	return b
}

// redisSubscribe opens the production pattern subscription.
func (b *Bridge) redisSubscribe(ctx context.Context) (messageStream, error) {
	sub := b.rdb.PSubscribe(ctx, logPattern)
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return messageStream{}, fmt.Errorf("ws: redis subscribe: %w", err)
	}
	return messageStream{messages: sub.Channel(), close: func() { _ = sub.Close() }}, nil
}

// Run supervises the subscription until ctx ends, reconnecting with bounded
// exponential backoff after every failure. It only returns on ctx cancellation
// (or when the hub is done), so a Redis outage no longer silently disables
// realtime forever.
func (b *Bridge) Run(ctx context.Context) error {
	backoff := b.minBackoff
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stream, err := b.subscribe(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			backoff = b.backoffAfterCycle(backoff, 0)
			b.logRetry("ws: redis bridge subscribe failed", err, backoff)
			if !sleepContext(ctx, backoff) {
				return ctx.Err()
			}
			backoff = nextBackoff(backoff, b.maxBackoff)
			continue
		}

		started := time.Now()
		err = b.forward(ctx, stream.messages)
		stream.close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Reset the backoff only after a subscription that stayed up, so a
		// Redis that accepts then drops does not retry at the minimum forever.
		backoff = b.backoffAfterCycle(backoff, time.Since(started))
		b.logRetry("ws: redis bridge disconnected", err, backoff)
		if !sleepContext(ctx, backoff) {
			return ctx.Err()
		}
		backoff = nextBackoff(backoff, b.maxBackoff)
	}
}

// backoffAfterCycle returns the delay to use for the next retry: the minimum
// once the subscription stayed up for stableAfter, otherwise the current delay
// unchanged (U8).
func (b *Bridge) backoffAfterCycle(current, stableFor time.Duration) time.Duration {
	if stableFor >= b.stableAfter {
		return b.minBackoff
	}
	return current
}

// forward copies messages into the hub until the subscription closes or ctx
// ends.
func (b *Bridge) forward(ctx context.Context, messages <-chan *redis.Message) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-messages:
			if !ok {
				return fmt.Errorf("ws: redis subscription closed")
			}
			b.hub.Broadcast(msg.Channel, []byte(msg.Payload))
		}
	}
}

// logRetry reports a bridge failure and the delay before the next attempt.
func (b *Bridge) logRetry(message string, err error, retryIn time.Duration) {
	b.logger.Warn(message, "error", err, "retry_in", retryIn)
}

// nextBackoff doubles delay up to max.
func nextBackoff(delay, max time.Duration) time.Duration {
	if delay <= 0 {
		return bridgeMinBackoff
	}
	delay *= 2
	if delay > max {
		return max
	}
	return delay
}

// sleepContext waits for delay or ctx cancellation. It reports false when ctx
// ended first.
func sleepContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
