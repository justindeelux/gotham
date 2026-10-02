// Package e2e contains cross-package end-to-end tests that prove the Phase 3
// (M3) exit criteria against real infrastructure.
//
// The suite lives in its own package because the criteria span three
// production packages that must cooperate: the control-plane container
// service (internal/containers), the control-plane-to-agent mTLS dial
// (internal/servers) and the realtime WebSocket bridge (internal/server/ws).
// Hosting the test inside any of them would drag sibling packages into that
// package's test build and risk import cycles later; here they meet as
// external consumers, wired exactly the way internal/server wires them in
// production. Gated tests skip unless GOTHAM_E2E=1, so the default
// `go test ./...` stays green without Docker or Redis.
package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/server/ws"
	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// e2eImage is the image the suite pulls and runs: nginx matches the M3
// walkthrough, and its /bin/sh doubles as the log generator.
const e2eImage = "nginx:1.23"

// TestM3EndToEnd proves the Phase 3 exit criteria against a real local Docker
// daemon and Redis:
//
//  1. list containers on a node through the container service,
//  2. pull an image and run a raw nginx container,
//  3. stop / start / restart it and observe every state change via the
//     service (which requires cache invalidation to work),
//  4. stream its logs through the control-plane publisher and the Redis
//     bridge to a subscribed WebSocket client, and
//  5. deliver a disconnect notice when the container dies.
//
// The control plane never talks to Docker directly: the test drives a real
// agent gRPC server over mTLS, exactly as production does.
func TestM3EndToEnd(t *testing.T) {
	requireE2E(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := testLogger(t)
	nodeID := "m3-e2e-" + uuid.New().String()[:8]
	serverID := uuid.New()
	redisAddr := e2eRedisAddr()
	dockerSock := e2eDockerSock()

	// 1. Preconditions: the local daemon and Redis must be reachable.
	engine, err := agent.NewDockerClient(dockerSock, agent.WithRegistryStateDir(t.TempDir()))
	if err != nil {
		t.Fatalf("docker client for %s: %v", dockerSock, err)
	}
	versionCtx, versionCancel := context.WithTimeout(ctx, 10*time.Second)
	version, err := engine.Version(versionCtx)
	versionCancel()
	if err != nil {
		t.Fatalf("docker daemon unreachable at %s: %v (start Docker and run: docker compose -f deploy/compose.dev.yml up -d)", dockerSock, err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	pingErr := rdb.Ping(pingCtx).Err()
	pingCancel()
	if pingErr != nil {
		t.Fatalf("redis unreachable at %s: %v (run: docker compose -f deploy/compose.dev.yml up -d)", redisAddr, pingErr)
	}
	t.Logf("preconditions ok: docker %s at %s, redis at %s", version, dockerSock, redisAddr)

	// 2. Boot the agent (DockerService + BuildService) over mTLS on the real
	// daemon; the helper issues the certificate and stops the server when the
	// test ends.
	agentAddr, authority := startLocalAgent(t, ctx, engine, nodeID)

	// 3. Wire the container service to the node through the production dialer.
	node := &servers.Server{
		ID:     serverID,
		Name:   "m3-e2e-node",
		IP:     agentAddr,
		Port:   22,
		Status: servers.StatusReady,
		NodeID: &nodeID,
	}
	svc := containers.NewService(containers.Config{
		Registry:  &staticRegistry{server: node},
		RedisAddr: redisAddr,
		Logger:    logger,
		Dial: func(dialCtx context.Context, srv *servers.Server) (containers.DockerClient, error) {
			client, err := servers.DialDockerClient(dialCtx, srv.IP, authority, servers.WithDockerServerName(nodeID))
			if err != nil {
				return nil, err
			}
			return client, nil
		},
	})
	t.Cleanup(func() { _ = svc.Close() })

	// 4. M3: list the node's containers through the service.
	baseline := mustList(t, ctx, svc, serverID)
	t.Logf("listed %d pre-existing container(s) on %s", len(baseline), nodeID)

	// 5. M3: pull the image, tolerating a registry outage only when the
	// daemon already has the image locally.
	if err := svc.Pull(ctx, serverID, e2eImage); err != nil {
		if imagePresentLocally(e2eImage) {
			t.Logf("svc.Pull(%s) failed but the image is present locally: %v", e2eImage, err)
		} else {
			t.Fatalf("svc.Pull(%s): %v", e2eImage, err)
		}
	}

	// 6. M3: run an nginx container and watch it show up as running.
	suffix := uuid.New().String()[:8]
	nginxName := "gotham-e2e-nginx-" + suffix
	removeContainer(t, nginxName)
	nginxID, err := svc.Run(ctx, serverID, containers.RunOptions{
		Image:  e2eImage,
		Name:   nginxName,
		Labels: map[string]string{"gotham.e2e": "m3"},
	})
	if err != nil {
		t.Fatalf("svc.Run(nginx): %v", err)
	}
	running := waitForState(t, ctx, svc, serverID, nginxID, "running")
	if running.Name != nginxName {
		t.Errorf("container name = %q, want %q", running.Name, nginxName)
	}
	if running.Image != e2eImage {
		t.Errorf("container image = %q, want %q", running.Image, e2eImage)
	}

	// 7. M3: stop it; the service must reflect the state change. A stale
	// cache would still report "running" for up to 10s, so this also proves
	// the stop invalidated the cached list.
	if err := svc.Stop(ctx, serverID, nginxID); err != nil {
		t.Fatalf("svc.Stop(nginx): %v", err)
	}
	stopped := waitForState(t, ctx, svc, serverID, nginxID, "exited")
	if strings.TrimSpace(stopped.Status) == "" {
		t.Error("exited container has an empty status")
	}

	// 8. M3: start it again.
	if err := svc.Start(ctx, serverID, nginxID); err != nil {
		t.Fatalf("svc.Start(nginx): %v", err)
	}
	waitForState(t, ctx, svc, serverID, nginxID, "running")

	// 9. M3: restart keeps it running with a fresh uptime status.
	if err := svc.Restart(ctx, serverID, nginxID); err != nil {
		t.Fatalf("svc.Restart(nginx): %v", err)
	}
	restarted := waitForState(t, ctx, svc, serverID, nginxID, "running")
	if !strings.HasPrefix(restarted.Status, "Up") {
		t.Errorf("status after restart = %q, want an Up status", restarted.Status)
	}
	t.Logf("lifecycle ok: %s %s -> running/exited/running/running", shortID(nginxID), nginxName)

	// 10. M3: realtime log streaming. A second container prints a unique
	// marker twice a second so the suite can prove chunks arrive live.
	logsName := "gotham-e2e-logs-" + suffix
	marker := "gotham-e2e-" + suffix
	removeContainer(t, logsName)
	logsID, err := svc.Run(ctx, serverID, containers.RunOptions{
		Image:      e2eImage,
		Name:       logsName,
		Entrypoint: []string{"sh", "-c"},
		Command:    []string{fmt.Sprintf("trap 'exit 0' TERM; while true; do echo %s; sleep 0.2; done", marker)},
		Labels:     map[string]string{"gotham.e2e": "m3"},
	})
	if err != nil {
		t.Fatalf("svc.Run(log generator): %v", err)
	}
	waitForState(t, ctx, svc, serverID, logsID, "running")

	// Mount the production WS endpoint, hub, supervised Redis bridge and the
	// log-stream manager.
	api := chi.NewRouter()
	rt := ws.Mount(api, acceptAnyToken{}, redisAddr, logger, nil)
	t.Cleanup(rt.Close)
	httpServer := httptest.NewServer(api)
	t.Cleanup(httpServer.Close)

	channel := ws.LogChannel(serverID.String(), logsID)
	query := url.Values{"token": {"e2e-token"}, "channel": {channel}}
	conn := dialWS(t, wsURL(httpServer.URL, "/v1/ws", query.Encode()))
	messages, errs := startWSReader(conn)
	ack := nextWSMsg(t, messages, errs, 10*time.Second, ws.TypeSubscribed)
	if ack.Channel != channel {
		t.Fatalf("subscribe ack channel = %q, want %q", ack.Channel, channel)
	}
	waitBridgeReady(t, ctx, rdb, messages, errs, channel, "bridge-ready-"+suffix)

	// Tail the container through the production start path: the manager dials
	// the agent, PublishStream reads real chunks, Redis carries them to the
	// bridge, and the hub fans them out to the WebSocket client. No test code
	// broadcasts log frames directly.
	opener := func(dialCtx context.Context) (ws.LogStreamer, io.Closer, error) {
		client, dialErr := servers.DialDockerClient(dialCtx, agentAddr, authority, servers.WithDockerServerName(nodeID))
		if dialErr != nil {
			return nil, nil, dialErr
		}
		return client, client, nil
	}
	if err := rt.StartLogStream(opener, serverID.String(), logsID, &agentv1.StreamLogsRequest{
		ContainerId: logsID,
		Follow:      true,
		Tail:        20,
	}); err != nil {
		t.Fatalf("rt.StartLogStream: %v", err)
	}

	chunks := 0
	for chunks < 2 {
		msg := nextWSMsg(t, messages, errs, 60*time.Second, ws.TypeLog, ws.TypeSubscribed)
		if !strings.Contains(msg.Data, marker) {
			continue
		}
		if msg.Channel != channel {
			t.Fatalf("log message channel = %q, want %q", msg.Channel, channel)
		}
		chunks++
	}
	t.Logf("received %d live log chunk(s) on %s", chunks, channel)

	// 11. Kill the container: the service reports it dead and the WS client
	// receives the disconnect notice for the ended stream.
	if err := svc.Stop(ctx, serverID, logsID); err != nil {
		t.Fatalf("svc.Stop(log generator): %v", err)
	}
	waitForState(t, ctx, svc, serverID, logsID, "exited")

	disconnect := nextWSMsg(t, messages, errs, 30*time.Second, ws.TypeDisconnect, ws.TypeSubscribed, ws.TypeLog)
	if disconnect.Channel != channel {
		t.Errorf("disconnect channel = %q, want %q", disconnect.Channel, channel)
	}
	if strings.TrimSpace(disconnect.Data) == "" {
		t.Error("disconnect notice has an empty reason")
	}
	// The manager removes the finished stream, so it does not linger.
	deadline := time.Now().Add(15 * time.Second)
	for rt.ActiveStreams() != 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := rt.ActiveStreams(); got != 0 {
		t.Errorf("ActiveStreams after the container stopped = %d, want 0", got)
	}

	t.Logf("M3 e2e ok: node=%s nginx=%s logs=%s marker=%s", nodeID, shortID(nginxID), channel, marker)
}
