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

var (
	streamIdleCheck         = 5 * time.Second
	streamIdleGrace         = 30 * time.Second
	streamReplayLines       = 200
	streamPublishMinBackoff = 250 * time.Millisecond
	streamPublishMaxBackoff = 5 * time.Second
)

type LogStreamOpener func(ctx context.Context) (LogStreamer, io.Closer, error)

type managedStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	replay *replayBuffer
}

type Realtime struct {
	Hub    *Hub
	logger *slog.Logger

	rdb *redis.Client
	pub Publisher

	handler *Handler

	ctx    context.Context
	cancel context.CancelFunc

	bridgeWG sync.WaitGroup

	streamsMu sync.Mutex
	streams   map[string]*managedStream
	streamsWG sync.WaitGroup
}

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
	rt.handler = handler
	api.Get("/v1/ws", handler.ServeHTTP)
	return rt
}

func (rt *Realtime) SetTaskFeed(authorize TaskAuthorizer, snapshot func(teamID string) []string) {
	if rt == nil || rt.handler == nil {
		return
	}
	rt.handler.SetTaskFeed(authorize, snapshot)
}

func (rt *Realtime) ActiveStreams() int {
	rt.streamsMu.Lock()
	defer rt.streamsMu.Unlock()
	return len(rt.streams)
}

func (rt *Realtime) StartLogStream(opener LogStreamOpener, serverID, containerID string, req *agentv1.StreamLogsRequest) (int, error) {
	if opener == nil {
		return 0, errors.New("ws: log stream opener is nil")
	}
	channel := LogChannel(serverID, containerID)

	rt.streamsMu.Lock()
	if entry, ok := rt.streams[channel]; ok && entry.ctx.Err() == nil {
		replay := entry.replay.snapshot()
		rt.streamsMu.Unlock()
		for _, msg := range replay {
			msg.Replay = true
			_ = rt.pub.Publish(rt.ctx, channel, mustJSON(msg))
		}
		_ = rt.pub.Publish(rt.ctx, channel, replayEndNotice(channel))
		return len(replay), nil
	}
	ctx, cancel := context.WithCancel(rt.ctx)
	entry := &managedStream{ctx: ctx, cancel: cancel, replay: newReplayBuffer(streamReplayLines)}
	rt.streams[channel] = entry
	rt.streamsMu.Unlock()

	rt.streamsWG.Add(2)
	go func() {
		defer rt.streamsWG.Done()
		rt.reapIdleStream(ctx, cancel, channel)
	}()
	go func() {
		defer rt.streamsWG.Done()
		rt.runStream(entry, channel, opener, serverID, containerID, req)
	}()
	return 0, nil
}

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

	streamErr := PublishStream(ctx, streamer, pub, serverID, containerID, req)
	if ctx.Err() == nil {
		rt.logger.Warn("ws: log stream ended", "channel", channel, "error", streamErr)
	}
}

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

func isLogPayload(payload []byte) bool {
	var msg Message
	return json.Unmarshal(payload, &msg) == nil && msg.Type == TypeLog
}

type replayBuffer struct {
	mu   sync.Mutex
	max  int
	data []Message
}

func newReplayBuffer(max int) *replayBuffer {
	if max < 0 {
		max = 0
	}
	return &replayBuffer{max: max}
}

func (r *replayBuffer) add(payload []byte) bool {
	if r.max == 0 {
		return false
	}
	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil || msg.Type != TypeLog {
		return false
	}
	msg.Replay = false
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, msg)
	if len(r.data) > r.max {
		r.data = r.data[len(r.data)-r.max:]
	}
	return true
}

func (r *replayBuffer) removeLast() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.data); n > 0 {
		r.data = r.data[:n-1]
	}
}

func (r *replayBuffer) snapshot() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Message, len(r.data))
	copy(out, r.data)
	return out
}
