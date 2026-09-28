package services

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// installQuietComposeCLI puts a fake `docker` on PATH whose logs command stays
// quiet until it is killed (a background child keeps it alive).
func installQuietComposeCLI(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$6" in
  config) printf 'web\n' ;;
  logs) sleep 5 & wait ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestLogsHTTPQuietFollowOpensBeforeOutput is the transport-level regression
// for the quiet follow stream: a real HTTP client must receive the accepted
// response promptly, before the node's command produces any output, and the
// stream must still end cleanly when the client cancels. It drives the real
// exec seam (agent.ComposeServer with a fake CLI), a real gRPC transport, the
// production adapter/service and the real HTTP routes.
func TestLogsHTTPQuietFollowOpensBeforeOutput(t *testing.T) {
	installQuietComposeCLI(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonical temp dir: %v", err)
	}
	composeServer := agent.NewComposeServer(agent.ComposeServerConfig{Root: root, Logger: discardLogger()})

	serviceID := uuid.New()
	userID := uuid.New()
	project := ProjectName(serviceID)
	if _, err := composeServer.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: project,
		ComposeYaml: []byte(testDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	// A real gRPC transport over bufconn, dialed through the production client.
	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	agentv1.RegisterComposeServiceServer(grpcServer, composeServer)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}

	repo := newFakeRepository()
	if _, err := repo.CreateService(context.Background(), Service{
		ID: serviceID, UserID: userID, ServerID: uuid.New(), Name: "shop", Status: StatusRunning,
		ComposeYAML: testDocument, Env: testEnv,
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	svc := NewService(Config{
		Repository: repo,
		Logger:     discardLogger(),
		Dial: func(dialCtx context.Context, _ uuid.UUID) (ComposeAgent, error) {
			client, err := servers.DialComposeClient(dialCtx, "passthrough:///bufnet", nil,
				servers.WithDockerDialOptions(grpc.WithContextDialer(dialer)))
			if err != nil {
				return nil, err
			}
			return NewGRPCComposeAgent(client), nil
		},
	})

	router := chi.NewRouter()
	Mount(router,
		func(next http.Handler) http.Handler { return next },
		func(context.Context) (uuid.UUID, bool) { return userID, true },
		svc)
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		httpServer.URL+"/v1/services/"+serviceID.String()+"/logs?service=web&follow=true", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	start := time.Now()
	response, err := http.DefaultClient.Do(request)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("a quiet follow did not open over real HTTP within %s: %v", elapsed, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	// The command stays quiet for ~5 seconds; acceptance must reach the client
	// long before that.
	if elapsed > 2*time.Second {
		t.Fatalf("a quiet follow took %s to open; the response is buffered until the first output", elapsed)
	}

	// No output may arrive before cancellation.
	body := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(response.Body)
		body <- data
	}()
	select {
	case data := <-body:
		t.Fatalf("a quiet follow delivered output before cancellation: %q", data)
	case <-time.After(400 * time.Millisecond):
	}

	cancel()
	select {
	case data := <-body:
		if len(data) != 0 {
			t.Fatalf("a quiet follow delivered %q", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the quiet follow did not end after the client canceled")
	}
}
