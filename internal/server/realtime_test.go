package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/net/websocket"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/server/ws"
	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// acceptToken accepts any WebSocket query token; the test proves the stream
// wiring, while token validation lives in internal/auth.
type acceptToken struct{}

func (acceptToken) VerifyAccessToken(string) (*auth.Claims, error) {
	return &auth.Claims{}, nil
}

// scriptedLogStream replays chunks, then ends with io.EOF.
type scriptedLogStream struct {
	grpc.ServerStreamingClient[agentv1.LogChunk]
	chunks [][]byte
	pos    int
}

func (s *scriptedLogStream) Recv() (*agentv1.LogChunk, error) {
	if s.pos >= len(s.chunks) {
		return nil, io.EOF
	}
	chunk := &agentv1.LogChunk{Data: s.chunks[s.pos]}
	s.pos++
	return chunk, nil
}

// fakeDockerStreamer satisfies agentv1.DockerServiceClient enough to serve a
// scripted log stream.
type fakeDockerStreamer struct {
	agentv1.DockerServiceClient
	stream grpc.ServerStreamingClient[agentv1.LogChunk]
}

func (f fakeDockerStreamer) StreamLogs(context.Context, *agentv1.StreamLogsRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error) {
	return f.stream, nil
}

// fakeLogDialer is a ServerService that also implements the container dialer.
type fakeLogDialer struct {
	*fakeServerService
	stream grpc.ServerStreamingClient[agentv1.LogChunk]
	err    error
}

func (f *fakeLogDialer) DialDockerClient(context.Context, uuid.UUID, ...servers.DockerDialOption) (*servers.DockerClient, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &servers.DockerClient{DockerServiceClient: fakeDockerStreamer{stream: f.stream}}, nil
}

// startLogStreamRequest builds the POST request with chi URL params and the
// authenticated user in its context.
func startLogStreamRequest(serverID, containerID string, userID uuid.UUID) *http.Request {
	req := httptest.NewRequest(http.MethodPost,
		"/v1/servers/"+serverID+"/containers/"+containerID+"/logs/stream", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", serverID)
	rctx.URLParams.Add("containerID", containerID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, userIDKey, userID)
	return req.WithContext(ctx)
}

// TestHandleStartLogStreamWiresPublisher is the B1-2/B2-1 server-side e2e: the
// start endpoint drives the realtime manager, which runs PublishStream against
// the (fake) agent, and a subscribed WebSocket client receives the log frame.
// No test code publishes log content directly.
func TestHandleStartLogStreamWiresPublisher(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	base := newFakeServerService()
	serverID := uuid.New()
	base.items[serverID] = servers.Server{ID: serverID, Name: "node", Status: servers.StatusReady}

	stream := &scriptedLogStream{chunks: [][]byte{[]byte("hello\n")}}
	dialer := &fakeLogDialer{fakeServerService: base, stream: stream}

	api := chi.NewRouter()
	rt := ws.Mount(api, acceptToken{}, "", logger, nil)
	t.Cleanup(rt.Close)
	httpServer := httptest.NewServer(api)
	t.Cleanup(httpServer.Close)

	channel := ws.LogChannel(serverID.String(), "ctr")
	query := url.Values{"token": {"t"}, "channel": {channel}}
	rawURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/v1/ws?" + query.Encode()
	conn, err := websocket.Dial(rawURL, "", "http://localhost/")
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}

	var ack ws.Message
	if err := websocket.JSON.Receive(conn, &ack); err != nil {
		t.Fatalf("receive subscribe ack: %v", err)
	}
	if ack.Type != ws.TypeSubscribed {
		t.Fatalf("ack = %+v, want subscribed", ack)
	}

	s := &Server{servers: dialer, realtime: rt, logger: logger}
	rec := httptest.NewRecorder()
	s.handleStartLogStream(rec, startLogStreamRequest(serverID.String(), "ctr", uuid.New()))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	var msg ws.Message
	if err := websocket.JSON.Receive(conn, &msg); err != nil {
		t.Fatalf("receive log frame: %v", err)
	}
	if msg.Type != ws.TypeLog || msg.Data != "hello\n" {
		t.Errorf("frame = %+v, want a log frame with %q", msg, "hello\n")
	}
}

// TestHandleStartLogStreamRejectsUnknownNode verifies the endpoint authorizes
// the node before starting an agent stream.
func TestHandleStartLogStreamRejectsUnknownNode(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rt := ws.Mount(chi.NewRouter(), nil, "", logger, nil)
	t.Cleanup(rt.Close)

	s := &Server{servers: newFakeServerService(), realtime: rt, logger: logger}
	rec := httptest.NewRecorder()
	s.handleStartLogStream(rec, startLogStreamRequest(uuid.New().String(), "ctr", uuid.New()))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestHandleStartLogStreamRequiresAgentDialer verifies a registry without the
// mTLS dialer is reported as an unavailable agent rather than a silent success.
func TestHandleStartLogStreamRequiresAgentDialer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	base := newFakeServerService()
	serverID := uuid.New()
	base.items[serverID] = servers.Server{ID: serverID, Name: "node", Status: servers.StatusReady}

	rt := ws.Mount(chi.NewRouter(), nil, "", logger, nil)
	t.Cleanup(rt.Close)

	s := &Server{servers: base, realtime: rt, logger: logger}
	rec := httptest.NewRecorder()
	s.handleStartLogStream(rec, startLogStreamRequest(serverID.String(), "ctr", uuid.New()))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}
