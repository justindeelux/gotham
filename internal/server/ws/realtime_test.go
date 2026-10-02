package ws

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// ctxStreamer returns a log stream whose Recv blocks until the stream context
// is cancelled, modelling a live agent tail.
type ctxStreamer struct{}

func (ctxStreamer) StreamLogs(ctx context.Context, _ *agentv1.StreamLogsRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error) {
	return ctxStream{ctx: ctx}, nil
}

type ctxStream struct {
	grpc.ServerStreamingClient[agentv1.LogChunk]
	ctx context.Context
}

func (s ctxStream) Recv() (*agentv1.LogChunk, error) {
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

// mountFallback mounts a Realtime with Redis disabled (hub publisher only).
func mountFallback(t *testing.T) *Realtime {
	t.Helper()
	t.Setenv(RealtimeEnv, "false")
	rt := Mount(chi.NewRouter(), nil, "", slog.Default(), nil)
	t.Cleanup(rt.Close)
	return rt
}

// TestStartLogStreamIdempotentAndCleansUp is the B1-2/B2-1 manager regression:
// the production start path runs PublishStream to the hub, a repeat start for
// the same channel is a no-op, and the stream is removed once it ends.
func TestStartLogStreamIdempotentAndCleansUp(t *testing.T) {
	rt := mountFallback(t)

	channel := LogChannel("srv-rt", "ctr")
	client := rt.Hub.newClient()
	rt.Hub.subscribe(client, channel)

	streamer := fakeStreamer{stream: &fakeLogStream{
		chunks: [][]byte{[]byte("hello\n")},
		fail:   io.EOF,
	}}
	opener := func(context.Context) (LogStreamer, io.Closer, error) { return streamer, nil, nil }
	req := &agentv1.StreamLogsRequest{ContainerId: "ctr", Follow: true}

	if err := rt.StartLogStream(opener, "srv-rt", "ctr", req); err != nil {
		t.Fatalf("StartLogStream: %v", err)
	}
	if err := rt.StartLogStream(opener, "srv-rt", "ctr", req); err != nil {
		t.Fatalf("second StartLogStream: %v", err)
	}
	if got := rt.ActiveStreams(); got != 1 {
		t.Errorf("ActiveStreams = %d, want 1 (idempotent)", got)
	}

	select {
	case payload := <-client.send:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("unmarshal frame: %v", err)
		}
		if msg.Type != TypeLog || msg.Data != "hello\n" {
			t.Errorf("frame = %+v, want a log frame with %q", msg, "hello\n")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no log frame reached the hub subscriber")
	}

	waitForCondition(t, 3*time.Second, "stream cleanup", func() bool {
		return rt.ActiveStreams() == 0
	})
}

// TestStreamReapedWhenNoSubscribers verifies a stream left without a hub
// subscriber is cancelled rather than running forever.
func TestStreamReapedWhenNoSubscribers(t *testing.T) {
	oldCheck, oldGrace := streamIdleCheck, streamIdleGrace
	streamIdleCheck, streamIdleGrace = 5*time.Millisecond, 10*time.Millisecond
	defer func() {
		streamIdleCheck, streamIdleGrace = oldCheck, oldGrace
	}()

	rt := mountFallback(t)
	opener := func(context.Context) (LogStreamer, io.Closer, error) { return ctxStreamer{}, nil, nil }
	if err := rt.StartLogStream(opener, "srv-rt", "idle", &agentv1.StreamLogsRequest{ContainerId: "idle"}); err != nil {
		t.Fatalf("StartLogStream: %v", err)
	}

	waitForCondition(t, 3*time.Second, "idle stream reaped", func() bool {
		return rt.ActiveStreams() == 0
	})
}

// TestRealtimeCloseCancelsActiveStream verifies shutdown does not hang on a
// live agent stream (B1-8).
func TestRealtimeCloseCancelsActiveStream(t *testing.T) {
	rt := mountFallback(t)
	// No t.Cleanup(rt.Close): Close is called by the test body.

	started := make(chan struct{})
	opener := func(context.Context) (LogStreamer, io.Closer, error) {
		close(started)
		return ctxStreamer{}, nil, nil
	}
	if err := rt.StartLogStream(opener, "srv-rt", "live", &agentv1.StreamLogsRequest{ContainerId: "live"}); err != nil {
		t.Fatalf("StartLogStream: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream never opened")
	}

	done := make(chan struct{})
	go func() {
		rt.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Realtime.Close hung on an active stream")
	}
	if got := rt.ActiveStreams(); got != 0 {
		t.Errorf("ActiveStreams after Close = %d, want 0", got)
	}
}

// tailStreamer emits one chunk then blocks until the stream context ends, so a
// started stream stays live for a second viewer to join.
type tailStreamer struct{}

func (tailStreamer) StreamLogs(ctx context.Context, _ *agentv1.StreamLogsRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error) {
	return &tailStream{ctx: ctx}, nil
}

type tailStream struct {
	grpc.ServerStreamingClient[agentv1.LogChunk]
	ctx  context.Context
	sent bool
}

func (s *tailStream) Recv() (*agentv1.LogChunk, error) {
	if !s.sent {
		s.sent = true
		return &agentv1.LogChunk{Data: []byte("tail\n")}, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

// nextClientFrame reads and decodes one frame queued for a hub client.
func nextClientFrame(t *testing.T, client *Client, timeout time.Duration) Message {
	t.Helper()
	select {
	case payload := <-client.send:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("unmarshal client frame: %v", err)
		}
		return msg
	case <-time.After(timeout):
		t.Fatal("timed out waiting for a client frame")
		return Message{}
	}
}

// newManualRealtime builds a Realtime with an injected publisher for tests.
func newManualRealtime(hub *Hub, pub Publisher) *Realtime {
	ctx, cancel := context.WithCancel(context.Background())
	return &Realtime{
		Hub:     hub,
		logger:  slog.Default(),
		pub:     pub,
		ctx:     ctx,
		cancel:  cancel,
		streams: make(map[string]*managedStream),
	}
}

// TestStartLogStreamReplaysToLateViewer is the U2 regression: a viewer that
// subscribes after the agent tail was already published still receives the
// buffered lines, because a repeat start replays the channel's ring.
func TestStartLogStreamReplaysToLateViewer(t *testing.T) {
	rt := mountFallback(t)

	channel := LogChannel("srv-rt", "replay")
	opener := func(context.Context) (LogStreamer, io.Closer, error) { return tailStreamer{}, nil, nil }
	req := &agentv1.StreamLogsRequest{ContainerId: "replay"}

	first := rt.Hub.newClient()
	rt.Hub.subscribe(first, channel)
	if err := rt.StartLogStream(opener, "srv-rt", "replay", req); err != nil {
		t.Fatalf("StartLogStream: %v", err)
	}
	if msg := nextClientFrame(t, first, 3*time.Second); msg.Type != TypeLog || msg.Data != "tail\n" {
		t.Fatalf("first viewer frame = %+v, want the tail", msg)
	}

	second := rt.Hub.newClient()
	rt.Hub.subscribe(second, channel)
	if err := rt.StartLogStream(opener, "srv-rt", "replay", req); err != nil {
		t.Fatalf("repeat StartLogStream: %v", err)
	}
	if msg := nextClientFrame(t, second, 3*time.Second); msg.Type != TypeLog || msg.Data != "tail\n" {
		t.Fatalf("late viewer frame = %+v, want the replayed tail", msg)
	}
	if got := rt.ActiveStreams(); got != 1 {
		t.Errorf("ActiveStreams = %d, want 1 (shared stream)", got)
	}
}

// flakyPublisher fails the first N publishes, then delivers to the hub.
type flakyPublisher struct {
	mu       sync.Mutex
	failures int
	calls    int
	hub      *Hub
}

func (p *flakyPublisher) Publish(_ context.Context, channel string, payload []byte) error {
	p.mu.Lock()
	p.calls++
	if p.failures > 0 {
		p.failures--
		p.mu.Unlock()
		return errors.New("redis down")
	}
	p.mu.Unlock()
	p.hub.Broadcast(channel, payload)
	return nil
}

func (p *flakyPublisher) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// TestStartLogStreamRetriesPublishAndNotifies is the U3 regression: a publish
// failure mid-stream does not silently kill the stream. The frame is retried
// until it lands, and the viewer gets an interruption notice broadcast straight
// to the hub while the transport is down.
func TestStartLogStreamRetriesPublishAndNotifies(t *testing.T) {
	oldMin, oldMax := streamPublishMinBackoff, streamPublishMaxBackoff
	streamPublishMinBackoff, streamPublishMaxBackoff = time.Millisecond, 2*time.Millisecond
	defer func() {
		streamPublishMinBackoff, streamPublishMaxBackoff = oldMin, oldMax
	}()

	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Close)

	flaky := &flakyPublisher{failures: 2, hub: hub}
	rt := newManualRealtime(hub, flaky)
	t.Cleanup(rt.Close)

	channel := LogChannel("srv-rt", "flaky")
	client := hub.newClient()
	hub.subscribe(client, channel)
	waitForCondition(t, 2*time.Second, "subscription", func() bool {
		return hub.Subscribers(channel) == 1
	})

	streamer := fakeStreamer{stream: &fakeLogStream{chunks: [][]byte{[]byte("hi\n")}, fail: io.EOF}}
	opener := func(context.Context) (LogStreamer, io.Closer, error) { return streamer, nil, nil }
	if err := rt.StartLogStream(opener, "srv-rt", "flaky", &agentv1.StreamLogsRequest{ContainerId: "flaky"}); err != nil {
		t.Fatalf("StartLogStream: %v", err)
	}

	gotNotice, gotLog := false, false
	deadline := time.After(3 * time.Second)
	for !gotNotice || !gotLog {
		select {
		case payload := <-client.send:
			var msg Message
			if err := json.Unmarshal(payload, &msg); err != nil {
				t.Fatalf("unmarshal frame: %v", err)
			}
			switch {
			case msg.Type == TypeDisconnect && strings.Contains(msg.Data, "interrupted"):
				gotNotice = true
			case msg.Type == TypeLog && msg.Data == "hi\n":
				gotLog = true
			}
		case <-deadline:
			t.Fatalf("did not recover from publish failures: notice=%v log=%v", gotNotice, gotLog)
		}
	}
	if flaky.callCount() < 3 {
		t.Errorf("publish calls = %d, want the failed frame retried", flaky.callCount())
	}
}

// TestStartLogStreamReplacesStaleEntry is the U6 regression: a start racing the
// reaper replaces a stream whose context is already cancelled instead of
// returning a no-op that leaves the viewer on a dead channel.
func TestStartLogStreamReplacesStaleEntry(t *testing.T) {
	rt := mountFallback(t)
	channel := LogChannel("srv-rt", "stale")

	deadCtx, deadCancel := context.WithCancel(rt.ctx)
	deadCancel()
	rt.streamsMu.Lock()
	rt.streams[channel] = &managedStream{ctx: deadCtx, cancel: deadCancel, replay: newReplayBuffer(10)}
	rt.streamsMu.Unlock()

	opener := func(context.Context) (LogStreamer, io.Closer, error) { return ctxStreamer{}, nil, nil }
	if err := rt.StartLogStream(opener, "srv-rt", "stale", &agentv1.StreamLogsRequest{ContainerId: "stale"}); err != nil {
		t.Fatalf("StartLogStream: %v", err)
	}

	rt.streamsMu.Lock()
	entry := rt.streams[channel]
	rt.streamsMu.Unlock()
	if entry == nil || entry.ctx.Err() != nil {
		t.Fatal("stale stream entry was not replaced (U6)")
	}
}
