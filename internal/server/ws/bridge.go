package ws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

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

// Bridge subscribes to the Redis log channels and forwards every message into
// the hub. One bridge serves all channels via a pattern subscription.
type Bridge struct {
	hub *Hub
	rdb *redis.Client
}

// NewBridge builds a Redis-to-hub bridge. The caller runs Run.
func NewBridge(hub *Hub, rdb *redis.Client) *Bridge {
	return &Bridge{hub: hub, rdb: rdb}
}

// Run forwards Redis messages on logs:*:* into the hub until ctx ends.
func (b *Bridge) Run(ctx context.Context) error {
	sub := b.rdb.PSubscribe(ctx, "logs:*:*")
	defer func() { _ = sub.Close() }()

	if _, err := sub.Receive(ctx); err != nil {
		return fmt.Errorf("ws: redis subscribe: %w", err)
	}

	messages := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-messages:
			if !ok {
				return nil
			}
			b.hub.Broadcast(msg.Channel, []byte(msg.Payload))
		}
	}
}
