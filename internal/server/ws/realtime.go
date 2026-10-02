package ws

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Log stream reaper tuning. A stream started for a viewer is cancelled once
// the channel has had no hub subscribers for streamIdleGrace, so a client that
// vanishes without unsubscribing cannot leak an agent stream (B2-1/B1-8).
var (
	streamIdleCheck = 5 * time.Second
	streamIdleGrace = 30 * time.Second
)

// LogStreamOpener dials (or otherwise obtains) a log streamer. A non-nil
// closer is closed when the stream ends, so the opener owns the connection
// lifecycle.
type LogStreamOpener func(ctx context.Context) (LogStreamer, io.Closer, error)

// Realtime owns the mounted realtime resources: the hub, the optional Redis
// client, the supervised bridge and the active log streams. Close releases all
// of them, so a control-plane shutdown leaves nothing running (B1-8).
type Realtime struct {
	Hub    *Hub
	logger *slog.Logger

	rdb *redis.Client
	pub Publisher

	ctx    context.Context
	cancel context.CancelFunc

	bridgeWG sync.WaitGroup

	streamsMu sync.Mutex
	streams   map[string]context.CancelFunc
	streamsWG sync.WaitGroup
}

// Mount registers WS /api/v1/ws on the /api router, starts the hub and —
// unless REALTIME_ENABLED=false — a supervised Redis pub/sub bridge on
// logs:{serverID}:{containerID} (config key redis.addr). authorize gates every
// log subscription on the node's team; nil leaves subscriptions open. The
// returned Realtime owns the lifecycle; call Close on shutdown. Mount is
// intended as a one-line call from Server.routes.
func Mount(api chi.Router, verifier TokenVerifier, redisAddr string, logger *slog.Logger, authorize SubscriptionAuthorizer) *Realtime {
	if logger == nil {
		logger = slog.Default()
	}

	hub := NewHub()
	go hub.Run()

	ctx, cancel := context.WithCancel(context.Background())
	rt := &Realtime{
		Hub:     hub,
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
		streams: make(map[string]context.CancelFunc),
	}

	if RealtimeEnabled() && strings.TrimSpace(redisAddr) != "" {
		rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
		rt.rdb = rdb
		rt.pub = RedisPublisher{RDB: rdb}
		bridge := NewBridge(hub, rdb, logger)
		rt.bridgeWG.Add(1)
		go func() {
			defer rt.bridgeWG.Done()
			_ = bridge.Run(ctx)
		}()
	} else {
		rt.pub = HubPublisher{Hub: hub}
		logger.Info("ws: realtime bridge disabled, clients should poll")
	}

	handler := NewHandler(hub, verifier, logger, authorize)
	api.Get("/v1/ws", handler.ServeHTTP)
	return rt
}

// Publisher returns the publisher the realtime path uses (Redis when enabled,
// the hub directly otherwise), so callers start streams without knowing which
// transport is active.
func (rt *Realtime) Publisher() Publisher {
	return rt.pub
}

// ActiveStreams reports how many agent log streams the manager is running.
// Exposed for tests and diagnostics.
func (rt *Realtime) ActiveStreams() int {
	rt.streamsMu.Lock()
	defer rt.streamsMu.Unlock()
	return len(rt.streams)
}

// StartLogStream begins bridging an agent log stream for one container into
// the realtime publisher. It is idempotent per channel: while a stream is
// active the call is a no-op, so every viewer/retry shares one agent stream.
// The stream is cancelled when the control plane shuts down or the channel has
// no subscribers for the idle grace.
func (rt *Realtime) StartLogStream(opener LogStreamOpener, serverID, containerID string, req *agentv1.StreamLogsRequest) error {
	if opener == nil {
		return errors.New("ws: log stream opener is nil")
	}
	channel := LogChannel(serverID, containerID)

	rt.streamsMu.Lock()
	if _, ok := rt.streams[channel]; ok {
		rt.streamsMu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(rt.ctx)
	rt.streams[channel] = cancel
	rt.streamsMu.Unlock()

	rt.streamsWG.Add(2) // the stream forwarder and its idle reaper
	go func() {
		defer rt.streamsWG.Done()
		rt.reapIdleStream(ctx, cancel, channel)
	}()
	go func() {
		defer rt.streamsWG.Done()
		rt.runStream(ctx, cancel, channel, opener, serverID, containerID, req)
	}()
	return nil
}

// runStream opens the agent stream and forwards it until it ends or ctx is
// cancelled, then removes the channel from the active set.
func (rt *Realtime) runStream(ctx context.Context, cancel context.CancelFunc, channel string, opener LogStreamOpener, serverID, containerID string, req *agentv1.StreamLogsRequest) {
	defer cancel()
	defer func() {
		rt.streamsMu.Lock()
		delete(rt.streams, channel)
		rt.streamsMu.Unlock()
	}()

	streamer, closer, err := opener(ctx)
	if err != nil {
		_ = rt.pub.Publish(ctx, channel, disconnectNotice(channel, "log stream unavailable: "+err.Error()))
		rt.logger.Warn("ws: open log stream", "channel", channel, "error", err)
		return
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}
	// PublishStream only returns once the stream has ended (always with a
	// non-nil reason), so log it unless the shutdown cancelled the stream.
	streamErr := PublishStream(ctx, streamer, rt.pub, serverID, containerID, req)
	if ctx.Err() == nil {
		rt.logger.Debug("ws: log stream ended", "channel", channel, "error", streamErr)
	}
}

// reapIdleStream cancels the stream once its channel has had no hub
// subscribers for streamIdleGrace.
func (rt *Realtime) reapIdleStream(ctx context.Context, cancel context.CancelFunc, channel string) {
	ticker := time.NewTicker(streamIdleCheck)
	defer ticker.Stop()

	idle := time.Duration(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if rt.Hub.Subscribers(channel) == 0 {
				idle += streamIdleCheck
				if idle >= streamIdleGrace {
					cancel()
					return
				}
				continue
			}
			idle = 0
		}
	}
}

// Close releases the mounted realtime resources: it cancels every log stream
// and the bridge, waits for them to finish, closes the Redis client and shuts
// the hub down. It is safe to call once.
func (rt *Realtime) Close() {
	rt.cancel()

	rt.streamsMu.Lock()
	for channel, cancel := range rt.streams {
		cancel()
		delete(rt.streams, channel)
	}
	rt.streamsMu.Unlock()
	rt.streamsWG.Wait()

	rt.bridgeWG.Wait()
	if rt.rdb != nil {
		_ = rt.rdb.Close()
	}
	rt.Hub.Close()
}
