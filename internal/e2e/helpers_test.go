package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/websocket"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/server/ws"
	"github.com/justindeelux/gotham/internal/servers"
)

// Environment knobs for the gated suite. Only GOTHAM_E2E is required; the
// other two exist for machines whose daemon or Redis does not listen on the
// defaults.
const (
	e2eEnv      = "GOTHAM_E2E"
	e2eRedisEnv = "GOTHAM_E2E_REDIS"
	e2eSockEnv  = "GOTHAM_E2E_DOCKER_SOCK"

	defaultE2ERedis = "127.0.0.1:6379"
	defaultE2ESock  = "/var/run/docker.sock"
)

// requireE2E gates every test that needs a live Docker daemon or Redis. The
// tests run only when GOTHAM_E2E=1 is set and the suite is not in -short
// mode, so a plain `go test ./...` stays green on machines without Docker.
//
// GOTHAM_E2E=1 is an explicit opt-in: once it is set, a missing harness
// precondition (Docker, Postgres, Redis, git) must fail the test instead of
// skipping it green — the dedicated CI workflow provides all of them, and a
// silent skip would mask a broken job. Only the gate itself, -short mode and
// owner-only opt-ins CI cannot guarantee (live DNS-01 issuance) keep
// skipping.
func requireE2E(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping end-to-end test in -short mode")
	}
	if os.Getenv(e2eEnv) != "1" {
		t.Skipf("set %s=1 to run this test against a local Docker daemon and Redis", e2eEnv)
	}
}

// e2eRedisAddr returns the Redis address used by the gated suite.
func e2eRedisAddr() string {
	if addr := strings.TrimSpace(os.Getenv(e2eRedisEnv)); addr != "" {
		return addr
	}
	return defaultE2ERedis
}

// e2eDockerSock returns the Docker socket the agent talks to.
func e2eDockerSock() string {
	if sock := strings.TrimSpace(os.Getenv(e2eSockEnv)); sock != "" {
		return sock
	}
	return defaultE2ESock
}

// testLogger logs to stderr instead of t.Log because background goroutines
// (the agent server, the realtime bridge) outlive the test body and t.Log is
// unsafe once the test has finished.
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// acceptAnyToken stands in for the production JWT verifier: these tests prove
// the realtime transport, while token validation lives in internal/auth.
type acceptAnyToken struct{}

// VerifyAccessToken accepts every token.
func (acceptAnyToken) VerifyAccessToken(string) (*auth.Claims, error) {
	return &auth.Claims{}, nil
}

// staticRegistry resolves exactly one node: the local e2e agent.
type staticRegistry struct {
	server *servers.Server
}

// Get returns the seeded server when id matches, else ErrNotFound.
func (r *staticRegistry) Get(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	if r.server == nil || r.server.ID != id {
		return nil, servers.ErrNotFound
	}
	return r.server, nil
}

// wsURL builds the WebSocket endpoint URL for an httptest server with query
// already encoded by url.Values.Encode.
func wsURL(base, path, query string) string {
	return "ws" + strings.TrimPrefix(base, "http") + path + "?" + query
}

// dialWS opens a WebSocket connection and closes it when the test ends.
func dialWS(t *testing.T, rawURL string) *websocket.Conn {
	t.Helper()
	conn, err := websocket.Dial(rawURL, "", "http://localhost/")
	if err != nil {
		t.Fatalf("dial %s: %v", rawURL, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// startWSReader pumps every frame off conn into a channel until it closes, so
// tests can select on messages without per-read deadlines interrupting a
// partially read frame.
func startWSReader(conn *websocket.Conn) (<-chan ws.Message, <-chan error) {
	messages := make(chan ws.Message, 64)
	errs := make(chan error, 1)
	go func() {
		defer close(messages)
		for {
			var msg ws.Message
			if err := websocket.JSON.Receive(conn, &msg); err != nil {
				errs <- err
				return
			}
			messages <- msg
		}
	}()
	return messages, errs
}

// nextWSMsg returns the next message of type want, skipping messages of the
// types in skip (subscription acks and interleaved log chunks). It fails the
// test when the socket closes or the timeout expires first.
func nextWSMsg(t *testing.T, messages <-chan ws.Message, errs <-chan error,
	timeout time.Duration, want string, skip ...string) ws.Message {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case err := <-errs:
			t.Fatalf("websocket closed while waiting for a %s message: %v", want, err)
		case msg := <-messages:
			switch {
			case msg.Type == want:
				return msg
			case slices.Contains(skip, msg.Type):
				// Not the message we are waiting for; keep reading.
			default:
				t.Fatalf("got message %+v while waiting for a %s message", msg, want)
			}
		case <-deadline:
			t.Fatalf("timed out after %s waiting for a %s message", timeout, want)
		}
	}
}

// waitBridgeReady publishes probe payloads until the subscribed client echoes
// one back, proving the Redis pattern subscription is live before the real
// log stream starts publishing (Redis pub/sub drops messages published before
// a subscriber attaches).
func waitBridgeReady(t *testing.T, ctx context.Context, rdb *redis.Client,
	messages <-chan ws.Message, errs <-chan error, channel, probe string) {
	t.Helper()
	payload, err := json.Marshal(ws.Message{Channel: channel, Type: ws.TypeLog, Data: probe})
	if err != nil {
		t.Fatalf("marshal bridge probe: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := rdb.Publish(ctx, channel, payload).Err(); err != nil {
			t.Fatalf("publish bridge probe: %v", err)
		}
		select {
		case err := <-errs:
			t.Fatalf("websocket closed during bridge probe: %v", err)
		case msg := <-messages:
			if msg.Type == ws.TypeLog && msg.Data == probe {
				return
			}
		case <-time.After(time.Second):
		}
	}
	t.Fatal("realtime bridge did not relay a probe: the Redis pub/sub bridge is not running")
}

// mustList fails the test when the service cannot list the node's containers.
func mustList(t *testing.T, ctx context.Context, svc containers.ContainerService, serverID uuid.UUID) []containers.Container {
	t.Helper()
	list, err := svc.List(ctx, serverID)
	if err != nil {
		t.Fatalf("svc.List: %v", err)
	}
	return list
}

// findContainer returns the listed container with the given id.
func findContainer(list []containers.Container, id string) (containers.Container, bool) {
	for _, container := range list {
		if container.ID == id || strings.HasPrefix(id, container.ID) || strings.HasPrefix(container.ID, id) {
			return container, true
		}
	}
	return containers.Container{}, false
}

// waitForState polls the container service until the container reaches
// wantState. The service caches list results for 10s and invalidates on every
// mutation, so the deadline exceeds the TTL: reaching the new state proves
// the mutation invalidated the cache rather than serving a stale read.
func waitForState(t *testing.T, ctx context.Context, svc containers.ContainerService,
	serverID uuid.UUID, id, wantState string) containers.Container {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	last := "not listed"
	for {
		if found, ok := findContainer(mustList(t, ctx, svc, serverID), id); ok {
			if found.State == wantState {
				return found
			}
			last = found.State + " (" + found.Status + ")"
		}
		if time.Now().After(deadline) {
			t.Fatalf("container %s never reached state %q; last observed %q", shortID(id), wantState, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// shortID trims a container id for log messages.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// removeContainer schedules `docker rm -f name` so e2e containers do not
// outlive the run. The Docker CLI is used only for cleanup: the suite itself
// drives Docker exclusively through the agent.
func removeContainer(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() {
		docker, err := exec.LookPath("docker")
		if err != nil {
			t.Logf("cleanup: docker CLI not found, remove container %s manually", name)
			return
		}
		output, err := exec.Command(docker, "rm", "-f", name).CombinedOutput()
		if err != nil {
			t.Logf("cleanup: docker rm -f %s: %v: %s", name, err, strings.TrimSpace(string(output)))
		}
	})
}

// imagePresentLocally reports whether the daemon already knows the image, so
// a failed pull is tolerated only when there is nothing left to download.
func imagePresentLocally(image string) bool {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return false
	}
	return exec.Command(docker, "image", "inspect", image).Run() == nil
}

// syncBuffer captures log output from background goroutines safely.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends p to the buffer.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns the captured output.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitForLog polls until the captured log contains want.
func waitForLog(t *testing.T, logs *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logs.String(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("log output does not contain %q; got: %s", want, logs.String())
}
