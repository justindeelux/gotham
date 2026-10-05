package e2e

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/services"
)

// Phase 7 (BE-7.1) production acceptance. The test drives the real control
// plane service surface (store -> services.Service -> mTLS agent
// ComposeServer -> `docker compose` CLI -> Docker) against the local Docker
// daemon and the dev Postgres database, and asserts the observable compose
// behavior:
//
//   - a two-service project with a named volume deploys and ps reports both
//     containers running,
//   - per-service logs stream and carry the substituted environment value,
//   - the domain label maps to the right compose service,
//   - down keeps the named volume (and its data) across stop/deploy,
//   - deleting the service keeps the volume.
//
// Gated by GOTHAM_E2E=1 like the Phase 6 acceptance: with the gate set, a
// missing prerequisite fails the test instead of skipping it green.
const (
	p7BusyboxImage = "busybox:1.36"
	p7NginxImage   = "nginx:1.23"
	p7PollTimeout  = 3 * time.Minute
)

// TestP7ComposeServiceProduction is gated by GOTHAM_E2E=1. Docker and the
// compose plugin must be reachable (fatal, never skipped as green); Postgres
// follows the suite convention and skips when unreachable.
func TestP7ComposeServiceProduction(t *testing.T) {
	requireE2E(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newP7Harness(t, ctx)

	// The document exercises the label convention, env substitution and a
	// named volume. `$${MESSAGE}` escapes this control plane's interpolation
	// so the container's shell expands the substituted value at runtime.
	domain := "p7-" + h.suffix + ".example.test"
	message := "p7-alive-" + h.suffix
	document := fmt.Sprintf(`services:
  web:
    image: %s
    labels:
      gotham.domain: ${DOMAIN}
      gotham.domain.port: "80"
    ports:
      - "80"
    volumes:
      - data:/data
  worker:
    image: %s
    environment:
      MESSAGE: ${MESSAGE}
    command: ["sh", "-c", "while true; do echo $${MESSAGE}; sleep 1; done"]
volumes:
  data:
`, p7NginxImage, p7BusyboxImage)

	// The domain map maps the labeled compose service to its validated host.
	rendered, err := services.Render(document, map[string]string{"DOMAIN": domain, "MESSAGE": message})
	if err != nil {
		t.Fatalf("render sample compose: %v", err)
	}
	if len(rendered.Spec.Domains) != 1 {
		t.Fatalf("domain map = %+v, want exactly the web route", rendered.Spec.Domains)
	}
	if route := rendered.Spec.Domains[0]; route.Service != "web" || route.Domain != domain || route.Port != 80 {
		t.Fatalf("domain map = %+v, want web -> %s:80", route, domain)
	}

	pullCtx, pullCancel := context.WithTimeout(ctx, p7PollTimeout)
	for _, image := range []string{p7NginxImage, p7BusyboxImage} {
		if err := h.engine.PullImage(pullCtx, image); err != nil {
			pullCancel()
			t.Fatalf("pull %s: %v", image, err)
		}
	}
	pullCancel()

	created, err := h.compose.Create(ctx, h.userID, services.CreateRequest{
		Name:          "p7-" + h.suffix,
		EnvironmentID: h.envID,
		ServerID:      h.serverID,
		ComposeYAML:   document,
		Env:           map[string]string{"DOMAIN": domain, "MESSAGE": message},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	project := services.ProjectName(created.ID)
	volume := project + "_data"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		// The project's containers are down by the end of the test; remove
		// the fixture's own volume so nothing leaks.
		if output, err := runDocker(cleanupCtx, "volume", "rm", volume); err != nil {
			t.Logf("cleanup volume %s: %v: %s", volume, err, output)
		}
	})

	deployed, deploy, err := h.compose.Deploy(ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if deployed.Status != services.StatusRunning || deploy.State != services.DeployRunning {
		t.Fatalf("deployed = %+v, deploy = %+v", deployed, deploy)
	}

	// ps reports both compose services running.
	containerList := p7WaitForRunning(t, ctx, h.compose, h.userID, created.ID)
	t.Logf("containers: %+v", containerList)

	// The domain label becomes a real Traefik route on this node: the
	// generated router points at the project's published backend port and a
	// Host-header request serves the nginx page.
	p7Sync(t, ctx, h.proxy, h.serverID)
	if traefikID := p6FindContainer(t, ctx, h.engine); traefikID != "" {
		h.trackContainer(traefikID)
	}
	if body := p6ExpectHTTP(t, domain, 200); !strings.Contains(body, "Welcome to nginx") {
		t.Fatalf("the service domain served %q, want nginx", body)
	}

	// Per-service logs carry the substituted environment value.
	logs := p7ReadLogs(t, ctx, h.compose, h.userID, created.ID, "worker")
	if !strings.Contains(logs, message) {
		t.Fatalf("worker logs do not contain %q:\n%s", message, logs)
	}

	// Data written into the named volume must survive stop and redeploy.
	p7WriteMarker(t, ctx, volume, "marker-"+h.suffix)
	stopped, err := h.compose.Stop(ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.Status != services.StatusStopped {
		t.Fatalf("stopped status = %q", stopped.Status)
	}
	if !p7VolumeExists(t, ctx, volume) {
		t.Fatalf("down removed the named volume %s", volume)
	}
	if containers := p7ListContainers(t, ctx, h.compose, h.userID, created.ID); len(containers) != 0 {
		t.Fatalf("down left containers behind: %+v", containers)
	}
	// Stop removes the route: the same host now answers 404 through Traefik.
	p7Sync(t, ctx, h.proxy, h.serverID)
	p6ExpectHTTP(t, domain, 404)

	if _, _, err := h.compose.Deploy(ctx, h.userID, created.ID); err != nil {
		t.Fatalf("redeploy: %v", err)
	}
	p7WaitForRunning(t, ctx, h.compose, h.userID, created.ID)
	if marker := p7ReadMarker(t, ctx, volume); !strings.Contains(marker, "marker-"+h.suffix) {
		t.Fatalf("volume data did not survive the redeploy: %q", marker)
	}
	p7Sync(t, ctx, h.proxy, h.serverID)
	p6ExpectHTTP(t, domain, 200)

	// The deploy history snapshots every attempt.
	deploys, err := h.compose.Deploys(ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("Deploys: %v", err)
	}
	if len(deploys) != 2 || deploys[0].State != services.DeployRunning {
		t.Fatalf("deploys = %+v", deploys)
	}

	// Restart restarts the running project in place and keeps the volume
	// data.
	restarted, err := h.compose.Restart(ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if restarted.Status != services.StatusRunning {
		t.Fatalf("restarted status = %q", restarted.Status)
	}
	p7WaitForRunning(t, ctx, h.compose, h.userID, created.ID)
	if marker := p7ReadMarker(t, ctx, volume); !strings.Contains(marker, "marker-"+h.suffix) {
		t.Fatalf("volume data did not survive the restart: %q", marker)
	}

	// Delete stops the project and keeps the data.
	if err := h.compose.Delete(ctx, h.userID, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !p7VolumeExists(t, ctx, volume) {
		t.Fatalf("delete removed the named volume %s", volume)
	}
	if _, err := h.compose.Get(ctx, h.userID, created.ID); err == nil {
		t.Fatal("deleted service is still readable")
	}
	// The deleted service's host is no longer claimed.
	p7Sync(t, ctx, h.proxy, h.serverID)
	p6ExpectHTTP(t, domain, 404)
}

// p7Sync runs one proxy sync, tolerating the diagnostics a stopped or deleted
// project produces (the sync still pushes the healthy configuration).
func p7Sync(t *testing.T, ctx context.Context, svc proxy.ProxyService, serverID uuid.UUID) {
	t.Helper()
	syncCtx, cancel := context.WithTimeout(ctx, p7PollTimeout)
	defer cancel()
	if err := svc.SyncServer(syncCtx, serverID); err != nil && !errors.Is(err, proxy.ErrPartialSync) {
		t.Fatalf("proxy sync: %v", err)
	}
}

// p7ListContainers reads the project's container list through the service.
func p7ListContainers(t *testing.T, ctx context.Context, svc services.ServiceService, userID, serviceID uuid.UUID) []services.ComposeContainer {
	t.Helper()
	listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	containers, err := svc.Containers(listCtx, userID, serviceID)
	if err != nil {
		t.Fatalf("Containers: %v", err)
	}
	return containers
}

// p7WaitForRunning polls ps until every compose service of the project is
// running. The expected count comes from the stored document, so a project
// that declares more services cannot pass the wait with one still down.
func p7WaitForRunning(t *testing.T, ctx context.Context, svc services.ServiceService, userID, serviceID uuid.UUID) []services.ComposeContainer {
	t.Helper()
	service, err := svc.Get(ctx, userID, serviceID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	spec, err := services.Render(service.ComposeYAML, service.Env)
	if err != nil {
		t.Fatalf("render the stored document: %v", err)
	}
	want := len(spec.Spec.Services)
	deadline := time.Now().Add(p7PollTimeout)
	var last []services.ComposeContainer
	for {
		last = p7ListContainers(t, ctx, svc, userID, serviceID)
		running := 0
		for _, container := range last {
			if container.State == "running" {
				running++
			}
		}
		if running == want {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("project never reported %d running containers; last: %+v", want, last)
		}
		time.Sleep(time.Second)
	}
}

// p7ReadLogs reads one compose service's logs (tail, no follow) through the
// service and returns the collected text.
func p7ReadLogs(t *testing.T, ctx context.Context, svc services.ServiceService, userID, serviceID uuid.UUID, composeService string) string {
	t.Helper()
	logCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	stream, err := svc.Logs(logCtx, userID, serviceID, composeService, 100, false)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer func() { _ = stream.Close() }()
	var builder strings.Builder
	for chunk := range stream.Chunks() {
		builder.Write(chunk)
	}
	return builder.String()
}

// p7WriteMarker writes a marker file into the project's named volume through
// the Docker CLI (the suite drives Docker through the agent; the CLI is used
// only for fixture setup and cleanup, like the Phase 6 failure dumps).
func p7WriteMarker(t *testing.T, ctx context.Context, volume, content string) {
	t.Helper()
	output, err := runDocker(ctx, "run", "--rm", "-v", volume+":/data", p7BusyboxImage,
		"sh", "-c", "printf %s "+content+" > /data/marker")
	if err != nil {
		t.Fatalf("write marker: %v: %s", err, output)
	}
}

// p7ReadMarker reads the marker file back from the volume.
func p7ReadMarker(t *testing.T, ctx context.Context, volume string) string {
	t.Helper()
	output, err := runDocker(ctx, "run", "--rm", "-v", volume+":/data", p7BusyboxImage,
		"cat", "/data/marker")
	if err != nil {
		t.Fatalf("read marker: %v: %s", err, output)
	}
	return output
}

// p7VolumeExists reports whether the named volume exists on the node.
func p7VolumeExists(t *testing.T, ctx context.Context, volume string) bool {
	t.Helper()
	output, err := runDocker(ctx, "volume", "ls", "--format", "{{.Name}}")
	if err != nil {
		t.Fatalf("list volumes: %v: %s", err, output)
	}
	for _, name := range strings.Split(output, "\n") {
		if strings.TrimSpace(name) == volume {
			return true
		}
	}
	return false
}
