package ws

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
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
