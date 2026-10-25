package server

import (
	"context"
	"errors"
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

type acceptToken struct{}

func (acceptToken) VerifyAccessToken(string) (*auth.Claims, error) {
	return &auth.Claims{}, nil
}

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

type fakeDockerStreamer struct {
	agentv1.DockerServiceClient
	stream grpc.ServerStreamingClient[agentv1.LogChunk]
}

func (f fakeDockerStreamer) StreamLogs(context.Context, *agentv1.StreamLogsRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error) {
	return f.stream, nil
}

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

func startLogStreamRequest(serverID string, userID uuid.UUID) *http.Request {
	const containerID = "ctr"
	req := httptest.NewRequest(http.MethodPost,
		"/v1/servers/"+serverID+"/containers/"+containerID+"/logs/stream", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", serverID)
	rctx.URLParams.Add("containerID", containerID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, userIDKey, userID)
	return req.WithContext(ctx)
}

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

	s := &Server{servers: dialer, realtime: rt, logger: logger, auth: ownerAuthService()}
	rec := httptest.NewRecorder()
	s.handleStartLogStream(rec, startLogStreamRequest(serverID.String(), testUserID))
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

func TestHandleStartLogStreamRejectsUnknownNode(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rt := ws.Mount(chi.NewRouter(), nil, "", logger, nil)
	t.Cleanup(rt.Close)

	s := &Server{servers: newFakeServerService(), realtime: rt, logger: logger}
	rec := httptest.NewRecorder()
	s.handleStartLogStream(rec, startLogStreamRequest(uuid.New().String(), uuid.New()))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleStartLogStreamTransientErrorIs500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rt := ws.Mount(chi.NewRouter(), nil, "", logger, nil)
	t.Cleanup(rt.Close)

	s := &Server{
		auth:     ownerAuthService(),
		servers:  &failingGetServerService{fakeServerService: newFakeServerService()},
		realtime: rt,
		logger:   logger,
	}
	rec := httptest.NewRecorder()
	s.handleStartLogStream(rec, startLogStreamRequest(uuid.New().String(), testUserID))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d for a transient lookup error", rec.Code, http.StatusInternalServerError)
	}
}

type failingGetServerService struct {
	*fakeServerService
}

func (f *failingGetServerService) Get(context.Context, uuid.UUID) (*servers.Server, error) {
	return nil, errors.New("database unavailable")
}

func TestHandleStartLogStreamRequiresAgentDialer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	base := newFakeServerService()
	serverID := uuid.New()
	base.items[serverID] = servers.Server{ID: serverID, Name: "node", Status: servers.StatusReady}

	rt := ws.Mount(chi.NewRouter(), nil, "", logger, nil)
	t.Cleanup(rt.Close)

	s := &Server{servers: base, realtime: rt, logger: logger, auth: ownerAuthService()}
	rec := httptest.NewRecorder()
	s.handleStartLogStream(rec, startLogStreamRequest(serverID.String(), testUserID))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}
