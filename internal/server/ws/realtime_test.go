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

// scriptedTailStreamer emits a fixed list of chunks, then blocks until the
// stream context ends, so its whole history lands in the replay ring.
type scriptedTailStreamer struct {
	chunks [][]byte
}

func (s scriptedTailStreamer) StreamLogs(ctx context.Context, _ *agentv1.StreamLogsRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error) {
	return &scriptedTail{ctx: ctx, chunks: s.chunks}, nil
}

type scriptedTail struct {
	grpc.ServerStreamingClient[agentv1.LogChunk]
	ctx    context.Context
	chunks [][]byte
	pos    int
}

func (s *scriptedTail) Recv() (*agentv1.LogChunk, error) {
	if s.pos < len(s.chunks) {
		chunk := &agentv1.LogChunk{Data: s.chunks[s.pos]}
		s.pos++
		return chunk, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

// nextClientFrame reads and decodes one frame queued for a hub client.
func nextClientFrame(t *testing.T, client *Client) Message {
	t.Helper()
	select {
	case payload := <-client.send:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("unmarshal client frame: %v", err)
		}
		return msg
	case <-time.After(3 * time.Second):
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

// TestStartLogStreamReplaysToLateViewer covers the U2 replay behavior and the
// round-2 U1 fix: a late viewer receives the buffered tail tagged Replay, and
// the first viewer's only untagged copy is the live frame — the second copy it
// also receives is tagged, so the client skips it instead of duplicating lines.
func TestStartLogStreamReplaysToLateViewer(t *testing.T) {
	rt := mountFallback(t)

	channel := LogChannel("srv-rt", "replay")
	opener := func(context.Context) (LogStreamer, io.Closer, error) { return tailStreamer{}, nil, nil }
	req := &agentv1.StreamLogsRequest{ContainerId: "replay"}

	first := rt.Hub.newClient()
	rt.Hub.subscribe(first, channel)
	waitForCondition(t, 2*time.Second, "first subscription", func() bool {
		return rt.Hub.Subscribers(channel) == 1
	})
	if err := rt.StartLogStream(opener, "srv-rt", "replay", req); err != nil {
		t.Fatalf("StartLogStream: %v", err)
	}
	live := nextClientFrame(t, first)
	if live.Type != TypeLog || live.Data != "tail\n" || live.Replay {
		t.Fatalf("first viewer frame = %+v, want an untagged live tail", live)
	}

	second := rt.Hub.newClient()
	rt.Hub.subscribe(second, channel)
	waitForCondition(t, 2*time.Second, "second subscription", func() bool {
		return rt.Hub.Subscribers(channel) == 2
	})
	if err := rt.StartLogStream(opener, "srv-rt", "replay", req); err != nil {
		t.Fatalf("repeat StartLogStream: %v", err)
	}
	replayed := nextClientFrame(t, second)
	if replayed.Type != TypeLog || replayed.Data != "tail\n" || !replayed.Replay {
		t.Fatalf("late viewer frame = %+v, want the replay-tagged tail", replayed)
	}
	// The first viewer receives the same replayed frame, but tagged, so the
	// client will not render a duplicate.
	duplicate := nextClientFrame(t, first)
	if duplicate.Type != TypeLog || duplicate.Data != "tail\n" || !duplicate.Replay {
		t.Fatalf("first viewer replay frame = %+v, want a replay-tagged tail", duplicate)
	}
	if got := rt.ActiveStreams(); got != 1 {
		t.Errorf("ActiveStreams = %d, want 1 (shared stream)", got)
	}
}

// TestReplayEmitsAllBufferedFrames is the round-3 U1 server-side guard: a
// replay must deliver the whole ring, not just the first frame (the client
// renders them as one batch decided at start).
func TestReplayEmitsAllBufferedFrames(t *testing.T) {
	rt := mountFallback(t)

	channel := LogChannel("srv-rt", "multi")
	chunks := [][]byte{[]byte("one\n"), []byte("two\n"), []byte("three\n")}
	opener := func(context.Context) (LogStreamer, io.Closer, error) {
		return scriptedTailStreamer{chunks: chunks}, nil, nil
	}
	req := &agentv1.StreamLogsRequest{ContainerId: "multi"}

	first := rt.Hub.newClient()
	rt.Hub.subscribe(first, channel)
	waitForCondition(t, 2*time.Second, "first subscription", func() bool {
		return rt.Hub.Subscribers(channel) == 1
	})
	if err := rt.StartLogStream(opener, "srv-rt", "multi", req); err != nil {
		t.Fatalf("StartLogStream: %v", err)
	}
	for i, want := range []string{"one\n", "two\n", "three\n"} {
		msg := nextClientFrame(t, first)
		if msg.Type != TypeLog || msg.Data != want || msg.Replay {
			t.Fatalf("live frame %d = %+v, want untagged %q", i, msg, want)
		}
	}

	late := rt.Hub.newClient()
	rt.Hub.subscribe(late, channel)
	waitForCondition(t, 2*time.Second, "late subscription", func() bool {
		return rt.Hub.Subscribers(channel) == 2
	})
	if err := rt.StartLogStream(opener, "srv-rt", "multi", req); err != nil {
		t.Fatalf("repeat StartLogStream: %v", err)
	}
	for i, want := range []string{"one\n", "two\n", "three\n"} {
		msg := nextClientFrame(t, late)
		if msg.Type != TypeLog || msg.Data != want || !msg.Replay {
			t.Fatalf("replay frame %d = %+v, want tagged %q", i, msg, want)
		}
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

	gotNotice, gotResumed, gotLog := false, false, false
	deadline := time.After(3 * time.Second)
	for !gotNotice || !gotResumed || !gotLog {
		select {
		case payload := <-client.send:
			var msg Message
			if err := json.Unmarshal(payload, &msg); err != nil {
				t.Fatalf("unmarshal frame: %v", err)
			}
			switch {
			case msg.Type == TypeDisconnect && strings.Contains(msg.Data, "interrupted"):
				gotNotice = true
			case msg.Type == TypeResumed:
				gotResumed = true
			case msg.Type == TypeLog && msg.Data == "hi\n":
				gotLog = true
			}
		case <-deadline:
			t.Fatalf("did not recover from publish failures: notice=%v resumed=%v log=%v",
				gotNotice, gotResumed, gotLog)
		}
	}
	if flaky.callCount() < 3 {
		t.Errorf("publish calls = %d, want the failed frame retried", flaky.callCount())
	}
}

// failOnCallPublisher fails exactly one call (1-based), then delivers.
type failOnCallPublisher struct {
	mu     sync.Mutex
	failOn int
	calls  int
	hub    *Hub
}

func (p *failOnCallPublisher) Publish(_ context.Context, channel string, payload []byte) error {
	p.mu.Lock()
	p.calls++
	fail := p.calls == p.failOn
	p.mu.Unlock()
	if fail {
		return errors.New("redis down")
	}
	p.hub.Broadcast(channel, payload)
	return nil
}

// TestResumedOnlyForLogFrames is the round-3 U3 regression: recovering the
// terminal disconnect notice must not emit a "resumed" notice immediately
// before the close frame.
func TestResumedOnlyForLogFrames(t *testing.T) {
	oldMin, oldMax := streamPublishMinBackoff, streamPublishMaxBackoff
	streamPublishMinBackoff, streamPublishMaxBackoff = time.Millisecond, 2*time.Millisecond
	defer func() {
		streamPublishMinBackoff, streamPublishMaxBackoff = oldMin, oldMax
	}()

	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Close)

	// Call 1 is the log frame (succeeds); call 2 is the terminal disconnect
	// notice (fails once, then succeeds on retry).
	pub := &failOnCallPublisher{failOn: 2, hub: hub}
	rt := newManualRealtime(hub, pub)
	t.Cleanup(rt.Close)

	channel := LogChannel("srv-rt", "terminal")
	client := hub.newClient()
	hub.subscribe(client, channel)
	waitForCondition(t, 2*time.Second, "subscription", func() bool {
		return hub.Subscribers(channel) == 1
	})

	streamer := fakeStreamer{stream: &fakeLogStream{chunks: [][]byte{[]byte("hi\n")}, fail: io.EOF}}
	opener := func(context.Context) (LogStreamer, io.Closer, error) { return streamer, nil, nil }
	if err := rt.StartLogStream(opener, "srv-rt", "terminal", &agentv1.StreamLogsRequest{ContainerId: "terminal"}); err != nil {
		t.Fatalf("StartLogStream: %v", err)
	}
	waitForCondition(t, 3*time.Second, "stream end", func() bool {
		return rt.ActiveStreams() == 0
	})

	gotLog, gotNotice, gotResumed := false, false, false
drain:
	for {
		select {
		case payload := <-client.send:
			var msg Message
			if err := json.Unmarshal(payload, &msg); err != nil {
				t.Fatalf("unmarshal frame: %v", err)
			}
			switch {
			case msg.Type == TypeLog && msg.Data == "hi\n":
				gotLog = true
			case msg.Type == TypeDisconnect && strings.Contains(msg.Data, "interrupted"):
				gotNotice = true
			case msg.Type == TypeResumed:
				gotResumed = true
			}
		case <-time.After(300 * time.Millisecond):
			break drain
		}
	}
	if !gotLog || !gotNotice {
		t.Fatalf("missing frames: log=%v notice=%v", gotLog, gotNotice)
	}
	if gotResumed {
		t.Fatal("resumed emitted for a non-log frame (round-3 U3)")
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
