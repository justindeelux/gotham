package ws

import (
	"context"
	"encoding/json"
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

// Log stream tuning.
//
//   - A stream is cancelled once its channel has had no hub subscribers for
//     streamIdleGrace, so a client that vanishes without unsubscribing cannot
//     leak an agent stream (B2-1/B1-8).
//   - streamReplayLines bounds the per-channel replay ring, so a viewer that
//     opens (or reopens) after the first tail was published still sees the last
//     lines instead of an empty drawer (U2).
//   - publish retries keep the agent stream alive while the Redis transport is
//     down and a subscriber is still listening (U3).
var (
	streamIdleCheck         = 5 * time.Second
	streamIdleGrace         = 30 * time.Second
	streamReplayLines       = 200
	streamPublishMinBackoff = 250 * time.Millisecond
	streamPublishMaxBackoff = 5 * time.Second
)

// LogStreamOpener dials (or otherwise obtains) a log streamer. A non-nil
// closer is closed when the stream ends, so the opener owns the connection
// lifecycle.
type LogStreamOpener func(ctx context.Context) (LogStreamer, io.Closer, error)

// managedStream is one active agent log stream plus its replay ring.
type managedStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	replay *replayBuffer
}

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
	streams   map[string]*managedStream
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
		streams: make(map[string]*managedStream),
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

// ActiveStreams reports how many agent log streams the manager is running.
// Exposed for tests and diagnostics.
func (rt *Realtime) ActiveStreams() int {
	rt.streamsMu.Lock()
	defer rt.streamsMu.Unlock()
	return len(rt.streams)
}

// StartLogStream begins bridging an agent log stream for one container into
// the realtime publisher. It is idempotent per channel while a stream is live:
// the call replays the channel's recent lines to the (already subscribed)
// caller instead of starting a second agent stream. A stale entry (its context
// already cancelled) is replaced, so a POST racing the reaper still gets a
// working stream (U6). The stream is cancelled when the control plane shuts
// down or the channel has no subscribers for the idle grace.
func (rt *Realtime) StartLogStream(opener LogStreamOpener, serverID, containerID string, req *agentv1.StreamLogsRequest) error {
	if opener == nil {
		return errors.New("ws: log stream opener is nil")
	}
	channel := LogChannel(serverID, containerID)

	rt.streamsMu.Lock()
	if entry, ok := rt.streams[channel]; ok && entry.ctx.Err() == nil {
		replay := entry.replay.snapshot()
		rt.streamsMu.Unlock()
		// The caller subscribes before starting (see LogViewer), so the replay
		// reaches the new viewer. Frames are tagged Replay so a viewer that
		// already has content skips them instead of duplicating history
		// (round-2 U1).
		for _, msg := range replay {
			msg.Replay = true
			_ = rt.pub.Publish(rt.ctx, channel, mustJSON(msg))
		}
		return nil
	}
	ctx, cancel := context.WithCancel(rt.ctx)
	entry := &managedStream{ctx: ctx, cancel: cancel, replay: newReplayBuffer(streamReplayLines)}
	rt.streams[channel] = entry
	rt.streamsMu.Unlock()

	rt.streamsWG.Add(2) // the stream forwarder and its idle reaper
	go func() {
		defer rt.streamsWG.Done()
		rt.reapIdleStream(ctx, cancel, channel)
	}()
	go func() {
		defer rt.streamsWG.Done()
		rt.runStream(entry, channel, opener, serverID, containerID, req)
	}()
	return nil
}

// runStream opens the agent stream and forwards it until it ends or the stream
// context is cancelled, then removes the channel from the active set. It only
// removes its own entry, so a replacement started after a reap is untouched.
func (rt *Realtime) runStream(entry *managedStream, channel string, opener LogStreamOpener, serverID, containerID string, req *agentv1.StreamLogsRequest) {
	ctx := entry.ctx
	defer entry.cancel()
	defer func() {
		rt.streamsMu.Lock()
		if rt.streams[channel] == entry {
			delete(rt.streams, channel)
		}
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

	// The resilient publisher keeps the agent stream alive through a Redis
	// outage while a subscriber is still listening (U3); the recorder keeps the
	// replay ring for late viewers (U2).
	pub := recordingPublisher{
		inner: resilientPublisher{
			inner:      rt.pub,
			hub:        rt.Hub,
			logger:     rt.logger,
			minBackoff: streamPublishMinBackoff,
			maxBackoff: streamPublishMaxBackoff,
		},
		ring: entry.replay,
	}

	// PublishStream only returns once the stream has ended (always with a
	// non-nil reason). A live-context end is unexpected, so it is a warning.
	streamErr := PublishStream(ctx, streamer, pub, serverID, containerID, req)
	if ctx.Err() == nil {
		rt.logger.Warn("ws: log stream ended", "channel", channel, "error", streamErr)
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
	for channel, entry := range rt.streams {
		entry.cancel()
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

// recordingPublisher stores successfully published log frames in a replay ring.
// It records before publishing so that a frame a subscriber can already see is
// also replayable; an undelivered frame (the publish ultimately failed) is
// removed again.
type recordingPublisher struct {
	inner Publisher
	ring  *replayBuffer
}

func (p recordingPublisher) Publish(ctx context.Context, channel string, payload []byte) error {
	stored := p.ring.add(payload)
	if err := p.inner.Publish(ctx, channel, payload); err != nil {
		if stored {
			p.ring.removeLast()
		}
		return err
	}
	return nil
}

// resilientPublisher retries a failed publish with bounded backoff while the
// stream context is live and the channel still has subscribers. On the first
// failure it broadcasts a disconnect notice straight to the hub (bypassing the
// failed Redis transport), and on the first success after a failure it
// broadcasts a resumed notice, so the viewer sees the interruption and its
// recovery instead of a silently stalled "Streaming" state (U3).
//
// Gap: a bridge-only subscription drop (Redis accepts the subscription but
// stops delivering) makes PUBLISH succeed with zero receivers, so frames during
// that window are lost without an error here. Detecting it needs a bridge
// health signal the publisher does not have; documented rather than guessed.
type resilientPublisher struct {
	inner      Publisher
	hub        *Hub
	logger     *slog.Logger
	minBackoff time.Duration
	maxBackoff time.Duration
}

func (p resilientPublisher) Publish(ctx context.Context, channel string, payload []byte) error {
	backoff := p.minBackoff
	notified := false
	failed := false
	for {
		err := p.inner.Publish(ctx, channel, payload)
		if err == nil {
			// Only a recovered log frame means logs are flowing again. A
			// terminal disconnect-notice publish must not emit "resumed"
			// immediately before the close frame (round-3 U3).
			if failed && isLogPayload(payload) {
				p.hub.Broadcast(channel, resumedNotice(channel))
				p.logger.Info("ws: log publish recovered", "channel", channel)
			}
			return nil
		}
		failed = true
		if ctx.Err() != nil {
			return err
		}
		if p.hub.Subscribers(channel) == 0 {
			return err
		}
		if !notified {
			p.hub.Broadcast(channel, disconnectNotice(channel, "log stream interrupted: "+err.Error()))
			p.logger.Warn("ws: log publish failed, retrying", "channel", channel, "error", err)
			notified = true
		}
		if !sleepContext(ctx, backoff) {
			return err
		}
		backoff = nextBackoff(backoff, p.maxBackoff)
	}
}

// isLogPayload reports whether a framed payload is a log chunk (as opposed to
// a disconnect or resumed notice).
func isLogPayload(payload []byte) bool {
	var msg Message
	return json.Unmarshal(payload, &msg) == nil && msg.Type == TypeLog
}

// replayBuffer holds the most recent log frames of one channel.
type replayBuffer struct {
	mu   sync.Mutex
	max  int
	data []Message
}

// newReplayBuffer builds a ring holding at most max log frames.
func newReplayBuffer(max int) *replayBuffer {
	if max < 0 {
		max = 0
	}
	return &replayBuffer{max: max}
}

// add stores a log frame and reports whether it was stored. Disconnect/resumed
// notices and any non-log frame are ignored.
func (r *replayBuffer) add(payload []byte) bool {
	if r.max == 0 {
		return false
	}
	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil || msg.Type != TypeLog {
		return false
	}
	msg.Replay = false // history is stored untagged; replay tags on the way out
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, msg)
	if len(r.data) > r.max {
		r.data = r.data[len(r.data)-r.max:]
	}
	return true
}

// removeLast drops the most recently added frame. The recording publisher is
// the only producer for a channel, so this undoes the immediately preceding
// add when that frame was never delivered.
func (r *replayBuffer) removeLast() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.data); n > 0 {
		r.data = r.data[:n-1]
	}
}

// snapshot returns a copy of the buffered frames, oldest first.
func (r *replayBuffer) snapshot() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Message, len(r.data))
	copy(out, r.data)
	return out
}
