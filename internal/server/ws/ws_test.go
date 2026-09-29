package ws

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/auth"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// stubVerifier accepts one fixed token.
type stubVerifier struct {
	token string
}

func (s stubVerifier) VerifyAccessToken(token string) (*auth.Claims, error) {
	if token == s.token {
		return &auth.Claims{}, nil
	}
	return nil, errors.New("unauthorized")
}

// fakeLogStream replays chunks, then fails with fail (io.EOF by default).
type fakeLogStream struct {
	grpc.ServerStreamingClient[agentv1.LogChunk]
	chunks [][]byte
	fail   error
	pos    int
}

func (f *fakeLogStream) Recv() (*agentv1.LogChunk, error) {
	if f.pos >= len(f.chunks) {
		if f.fail != nil {
			return nil, f.fail
		}
		return nil, io.EOF
	}
	chunk := &agentv1.LogChunk{Data: f.chunks[f.pos]}
	f.pos++
	return chunk, nil
}

// fakeStreamer feeds PublishStream without a live agent.
type fakeStreamer struct {
	stream grpc.ServerStreamingClient[agentv1.LogChunk]
	err    error
}

func (f fakeStreamer) StreamLogs(context.Context, *agentv1.StreamLogsRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error) {
	return f.stream, f.err
}

// recordPublisher captures published payloads for assertions.
type recordPublisher struct {
	hub     *Hub
	channel string
	got     [][]byte
}

func (p *recordPublisher) Publish(_ context.Context, channel string, payload []byte) error {
	p.channel = channel
	p.got = append(p.got, payload)
	if p.hub != nil {
		p.hub.Broadcast(channel, payload)
	}
	return nil
}

const testToken = "test-token"

func newTestServer(t *testing.T) (*Hub, *httptest.Server) {
	t.Helper()

	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Close)

	handler := NewHandler(hub, stubVerifier{token: testToken}, nil, nil)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return hub, server
}

func wsURL(server *httptest.Server, query string) string {
	return "ws" + strings.TrimPrefix(server.URL, "http") + query
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()

	conn, err := websocket.Dial(url, "", "http://localhost/")
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func receive(t *testing.T, conn *websocket.Conn) Message {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	var msg Message
	if err := websocket.JSON.Receive(conn, &msg); err != nil {
		t.Fatalf("receive: %v", err)
	}
	return msg
}

// collectLogs reads exactly want log messages, skipping subscribed acks.
func collectLogs(t *testing.T, conn *websocket.Conn, want int) []string {
	t.Helper()

	var got []string
	for len(got) < want {
		msg := receive(t, conn)
		if msg.Type == TypeSubscribed {
			continue
		}
		if msg.Type != TypeLog {
			t.Fatalf("message type = %q, want %q (data %q)", msg.Type, TypeLog, msg.Data)
		}
		got = append(got, msg.Data)
	}
	return got
}

// nextNonAck reads the next message that is not a subscribed ack.
func nextNonAck(t *testing.T, conn *websocket.Conn) Message {
	t.Helper()

	for {
		msg := receive(t, conn)
		if msg.Type != TypeSubscribed {
			return msg
		}
	}
}

func TestLogChannel(t *testing.T) {
	if got := LogChannel("srv1", "ctr9"); got != "logs:srv1:ctr9" {
		t.Errorf("LogChannel = %q, want %q", got, "logs:srv1:ctr9")
	}
}

func TestRealtimeEnabled(t *testing.T) {
	t.Setenv(RealtimeEnv, "")
	if !RealtimeEnabled() {
		t.Error("RealtimeEnabled(unset) = false, want true")
	}
	t.Setenv(RealtimeEnv, "false")
	if RealtimeEnabled() {
		t.Error("RealtimeEnabled(false) = true, want false")
	}
	t.Setenv(RealtimeEnv, "FALSE")
	if RealtimeEnabled() {
		t.Error("RealtimeEnabled(FALSE) = true, want false")
	}
	t.Setenv(RealtimeEnv, "true")
	if !RealtimeEnabled() {
		t.Error("RealtimeEnabled(true) = false, want true")
	}
}

func TestUnauthorized(t *testing.T) {
	_, server := newTestServer(t)

	if _, err := websocket.Dial(wsURL(server, "/?token=wrong"), "", "http://localhost/"); err == nil {
		t.Error("dial with wrong token succeeded, want handshake error")
	}
	if _, err := websocket.Dial(wsURL(server, "/"), "", "http://localhost/"); err == nil {
		t.Error("dial without token succeeded, want handshake error")
	}
}

func TestTwoClientsReceiveIdenticalData(t *testing.T) {
	hub, server := newTestServer(t)
	channel := LogChannel("srv1", "ctr1")

	// First client preselects via ?channel=, second subscribes with a frame.
	first := dial(t, wsURL(server, "/?token="+testToken+"&channel="+channel))
	second := dial(t, wsURL(server, "/?token="+testToken))
	if err := websocket.JSON.Send(second, clientRequest{Subscribe: channel}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for hub.Subscribers(channel) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := hub.Subscribers(channel); got != 2 {
		t.Fatalf("subscribers = %d, want 2", got)
	}

	streamer := fakeStreamer{stream: &fakeLogStream{
		chunks: [][]byte{[]byte("line1\n"), []byte("line2\n")},
		fail:   io.EOF,
	}}
	req := &agentv1.StreamLogsRequest{ContainerId: "ctr1", Follow: true}

	pub := &recordPublisher{hub: hub}
	done := make(chan error, 1)
	go func() { done <- PublishStream(context.Background(), streamer, pub, "srv1", "ctr1", req) }()

	firstGot := collectLogs(t, first, 2)
	secondGot := collectLogs(t, second, 2)

	for i := range firstGot {
		if firstGot[i] != secondGot[i] {
			t.Fatalf("client data diverges at %d: %q vs %q", i, firstGot[i], secondGot[i])
		}
	}
	if firstGot[0] != "line1\n" || firstGot[1] != "line2\n" {
		t.Fatalf("client data = %q, want [line1 line2]", firstGot)
	}
	if pub.channel != channel {
		t.Errorf("publish channel = %q, want %q", pub.channel, channel)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("PublishStream did not return after EOF")
	}

	// EOF also produces a disconnect notice on the stream channel.
	msg := nextNonAck(t, first)
	if msg.Type != TypeDisconnect || msg.Channel != channel {
		t.Errorf("post-EOF message = %+v, want disconnect on %q", msg, channel)
	}
}

func TestKillStreamDisconnectNotice(t *testing.T) {
	hub, server := newTestServer(t)
	channel := LogChannel("srv9", "ctr9")

	first := dial(t, wsURL(server, "/?token="+testToken+"&channel="+channel))
	second := dial(t, wsURL(server, "/?token="+testToken))
	if err := websocket.JSON.Send(second, clientRequest{Subscribe: channel}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for hub.Subscribers(channel) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// Mid-stream kill: one chunk, then the connection drops.
	streamer := fakeStreamer{stream: &fakeLogStream{
		chunks: [][]byte{[]byte("partial\n")},
		fail:   errors.New("connection reset by peer"),
	}}
	pub := &recordPublisher{hub: hub}
	done := make(chan error, 1)
	go func() {
		done <- PublishStream(context.Background(), streamer, pub, "srv9", "ctr9",
			&agentv1.StreamLogsRequest{ContainerId: "ctr9", Follow: true})
	}()

	if got := collectLogs(t, first, 1); got[0] != "partial\n" {
		t.Fatalf("first data = %q, want partial", got)
	}
	if got := collectLogs(t, second, 1); got[0] != "partial\n" {
		t.Fatalf("second data = %q, want partial", got)
	}

	for _, conn := range []*websocket.Conn{first, second} {
		msg := nextNonAck(t, conn)
		if msg.Type != TypeDisconnect {
			t.Errorf("message type = %q, want %q", msg.Type, TypeDisconnect)
		}
		if msg.Channel != channel {
			t.Errorf("message channel = %q, want %q", msg.Channel, channel)
		}
		if msg.Data == "" {
			t.Error("disconnect notice has empty reason")
		}
	}

	select {
	case err := <-done:
		if err == nil {
			t.Error("PublishStream(killed) = nil, want stream error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PublishStream did not return after kill")
	}
}

func TestStreamOpenFailureNotifies(t *testing.T) {
	hub, server := newTestServer(t)
	channel := LogChannel("srv7", "ctr7")

	conn := dial(t, wsURL(server, "/?token="+testToken+"&channel="+channel))

	deadline := time.Now().Add(5 * time.Second)
	for hub.Subscribers(channel) < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	streamer := fakeStreamer{err: errors.New("agent unreachable")}
	pub := &recordPublisher{hub: hub}
	if err := PublishStream(context.Background(), streamer, pub, "srv7", "ctr7",
		&agentv1.StreamLogsRequest{ContainerId: "ctr7"}); err == nil {
		t.Error("PublishStream(open failure) = nil, want error")
	}

	msg := nextNonAck(t, conn)
	if msg.Type != TypeDisconnect || msg.Channel != channel {
		t.Errorf("message = %+v, want disconnect on %q", msg, channel)
	}
}

func TestSubscribeUnsubscribeFrames(t *testing.T) {
	hub, server := newTestServer(t)
	channel := LogChannel("s", "c")

	conn := dial(t, wsURL(server, "/?token="+testToken))
	if err := websocket.JSON.Send(conn, clientRequest{Subscribe: channel}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for hub.Subscribers(channel) < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := hub.Subscribers(channel); got != 1 {
		t.Fatalf("subscribers = %d, want 1", got)
	}

	if err := websocket.JSON.Send(conn, clientRequest{Unsubscribe: channel}); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for hub.Subscribers(channel) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := hub.Subscribers(channel); got != 0 {
		t.Fatalf("subscribers after unsubscribe = %d, want 0", got)
	}
}
