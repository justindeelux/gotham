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

const (
	bridgeMinBackoff = 250 * time.Millisecond
	bridgeMaxBackoff = 30 * time.Second
)

const logPattern = "logs:*:*"

const taskPattern = "tasks:*"

const notificationPattern = "notifications:*"

func LogChannel(serverID, containerID string) string {
	return "logs:" + serverID + ":" + containerID
}

type LogStreamer interface {
	StreamLogs(ctx context.Context, in *agentv1.StreamLogsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error)
}

type Publisher interface {
	Publish(ctx context.Context, channel string, payload []byte) error
}

type HubPublisher struct {
	Hub *Hub
}

func (p HubPublisher) Publish(_ context.Context, channel string, payload []byte) error {
	p.Hub.Broadcast(channel, payload)
	return nil
}

type RedisPublisher struct {
	RDB *redis.Client
}

func (p RedisPublisher) Publish(ctx context.Context, channel string, payload []byte) error {
	return p.RDB.Publish(ctx, channel, payload).Err()
}

func disconnectNotice(channel, reason string) []byte {
	payload, _ := json.Marshal(Message{Channel: channel, Type: TypeDisconnect, Data: reason})
	return payload
}

func resumedNotice(channel string) []byte {
	payload, _ := json.Marshal(Message{Channel: channel, Type: TypeResumed, Data: "log stream resumed"})
	return payload
}

func replayEndNotice(channel string) []byte {
	payload, _ := json.Marshal(Message{Channel: channel, Type: TypeReplayEnd})
	return payload
}

func logPayload(channel string, data []byte) []byte {
	payload, _ := json.Marshal(Message{Channel: channel, Type: TypeLog, Data: string(data)})
	return payload
}

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

type messageStream struct {
	messages <-chan *redis.Message
	close    func()
}

type subscribeFunc func(ctx context.Context) (messageStream, error)

const bridgeStableAfter = 5 * time.Second

type Bridge struct {
	hub    *Hub
	rdb    *redis.Client
	logger *slog.Logger

	subscribe   subscribeFunc
	minBackoff  time.Duration
	maxBackoff  time.Duration
	stableAfter time.Duration
}

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

func (b *Bridge) redisSubscribe(ctx context.Context) (messageStream, error) {
	sub := b.rdb.PSubscribe(ctx, logPattern, taskPattern, notificationPattern)
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return messageStream{}, fmt.Errorf("ws: redis subscribe: %w", err)
	}
	return messageStream{messages: sub.Channel(), close: func() { _ = sub.Close() }}, nil
}

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
		backoff = b.backoffAfterCycle(backoff, time.Since(started))
		b.logRetry("ws: redis bridge disconnected", err, backoff)
		if !sleepContext(ctx, backoff) {
			return ctx.Err()
		}
		backoff = nextBackoff(backoff, b.maxBackoff)
	}
}

func (b *Bridge) backoffAfterCycle(current, stableFor time.Duration) time.Duration {
	if stableFor >= b.stableAfter {
		return b.minBackoff
	}
	return current
}

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

func (b *Bridge) logRetry(message string, err error, retryIn time.Duration) {
	b.logger.Warn(message, "error", err, "retry_in", retryIn)
}

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
