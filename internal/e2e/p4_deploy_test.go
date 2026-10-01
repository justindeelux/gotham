package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestP4DeployRunning proves the core Phase 4 (M4) scenario end to end: a
// fixture repository is cloned by the control plane, built on the node by the
// Dockerfile engine through BuildImage, pushed to the node registry, started,
// and finally reached over its host port.
//
// Everything runs against real infrastructure — PostgreSQL, Redis, a mTLS
// agent and the local Docker daemon — so it is gated by GOTHAM_E2E=1 and fails
// when any precondition is missing; only the feature-off default run skips.
func TestP4DeployRunning(t *testing.T) {
	h := newP4Harness(t)
	suffix := uuid.New().String()[:8]
	fixture := newP4Fixture(t, "e2e/p4-app-"+suffix, "gotham-p4-v1-"+suffix)
	hostPort := freeHostPort(t)

	app := h.createApplication(t, p4CreateApplication{
		Name:      "p4-running-" + suffix,
		Provider:  "github",
		Repo:      fixture.repo,
		CloneURL:  fixture.dir,
		Branch:    "main",
		BuildPack: "dockerfile",
		Port:      p4ContainerPort,
		HostPort:  hostPort,
		ServerID:  h.serverID.String(),
	})

	queued := h.deploy(t, app.ID)
	if queued.State != "queued" {
		t.Errorf("queued deployment state = %q, want queued", queued.State)
	}
	running := h.waitForTerminal(t, app.ID, queued.ID)
	if running.State != "running" {
		t.Fatalf("deployment state = %q, want running (error: %s)", running.State, running.Error)
	}
	if running.ImageTag == "" {
		t.Error("running deployment has no image tag")
	}
	if running.RegistryImage == "" {
		t.Error("running deployment has no node registry reference")
	}
	if running.ContainerID == "" {
		t.Error("running deployment has no container")
	}

	// The build streamed into the realtime deployment log before the container
	// started, and the application answers on its published host port.
	h.waitForDeployLog(t, queued.ID, "image built: ")
	waitForHTTPBody(t, fmt.Sprintf("http://127.0.0.1:%d/index.html", hostPort), fixture.marker)

	t.Logf("deploy ok: app=%s deployment=%s image=%s port=%d",
		app.ID, running.ID, running.ImageTag, hostPort)
}

// TestP4FailedBuild proves the failure half of the state machine: a Dockerfile
// that cannot build leaves the deployment `failed` with an actionable error on
// the row, and the build output it produced before failing is attached to the
// deployment's log.
func TestP4FailedBuild(t *testing.T) {
	h := newP4Harness(t)
	suffix := uuid.New().String()[:8]
	fixture := newBrokenP4Fixture(t, "e2e/p4-broken-"+suffix, "gotham-p4-broken-"+suffix)
	hostPort := freeHostPort(t)

	// The legacy Docker builder leaves the failed RUN step's scratch container
	// behind without any label, so cleanup must match it by command text.
	t.Cleanup(func() { removeContainersMatchingCommand(t, "P4-BUILD-BOOM") })

	app := h.createApplication(t, p4CreateApplication{
		Name:      "p4-broken-" + suffix,
		Provider:  "github",
		Repo:      fixture.repo,
		CloneURL:  fixture.dir,
		Branch:    "main",
		BuildPack: "dockerfile",
		Port:      p4ContainerPort,
		HostPort:  hostPort,
		ServerID:  h.serverID.String(),
	})

	queued := h.deploy(t, app.ID)
	failed := h.waitForTerminal(t, app.ID, queued.ID)
	if failed.State != "failed" {
		t.Fatalf("deployment state = %q, want failed", failed.State)
	}
	if failed.Error == "" {
		t.Fatal("failed deployment carries no error")
	}
	if !strings.Contains(failed.Error, "build image") {
		t.Errorf("error = %q, want it to name the failing build step", failed.Error)
	}
	if !containsAny(failed.Error, "non-zero code", "exit code", "did not complete successfully", "P4-BUILD-BOOM") {
		t.Errorf("error = %q, want the Docker build failure detail", failed.Error)
	}

	// The failure is logged on the deployment, together with the build output
	// the engine streamed before the daemon reported the error.
	h.waitForDeployLog(t, queued.ID, "step building failed")
	buildLog := h.deployLogs(queued.ID)
	if !containsAny(buildLog, "P4-BUILD-BOOM", "busybox", "Step ") {
		t.Errorf("deployment log has no build output; got:\n%s", buildLog)
	}

	t.Logf("failed build ok: error=%q", failed.Error)
}

// TestP4Rollback proves the release history is usable: v1 runs, the fixture
// moves to v2 and deploys, then POST /applications/{id}/rollback redeploys the
// image v1 built and the original page is served again.
func TestP4Rollback(t *testing.T) {
	h := newP4Harness(t)
	suffix := uuid.New().String()[:8]
	v1Marker := "gotham-p4-v1-" + suffix
	v2Marker := "gotham-p4-v2-" + suffix
	fixture := newP4Fixture(t, "e2e/p4-rollback-"+suffix, v1Marker)
	hostPort := freeHostPort(t)
	appURL := fmt.Sprintf("http://127.0.0.1:%d/index.html", hostPort)

	app := h.createApplication(t, p4CreateApplication{
		Name:      "p4-rollback-" + suffix,
		Provider:  "github",
		Repo:      fixture.repo,
		CloneURL:  fixture.dir,
		Branch:    "main",
		BuildPack: "dockerfile",
		Port:      p4ContainerPort,
		HostPort:  hostPort,
		ServerID:  h.serverID.String(),
	})

	// 1. Release v1.
	first := h.deploy(t, app.ID)
	running1 := h.waitForTerminal(t, app.ID, first.ID)
	if running1.State != "running" {
		t.Fatalf("first deployment state = %q, want running (error: %s)", running1.State, running1.Error)
	}
	waitForHTTPBody(t, appURL, v1Marker)

	// 2. Publish v2 and release it over the same application.
	fixture.commit(t, v2Marker, p4Dockerfile)
	second := h.deploy(t, app.ID)
	running2 := h.waitForTerminal(t, app.ID, second.ID)
	if running2.State != "running" {
		t.Fatalf("second deployment state = %q, want running (error: %s)", running2.State, running2.Error)
	}
	if running2.ID == running1.ID {
		t.Fatal("the second deploy reused the first deployment row")
	}
	waitForHTTPBody(t, appURL, v2Marker)

	// 3. Roll back to the previous release.
	rollback := h.rollback(t, app.ID, nil)
	if rollback.Kind != "rollback" {
		t.Errorf("rollback kind = %q, want rollback", rollback.Kind)
	}
	if rollback.RollbackFrom != running1.ID {
		t.Errorf("rollback_from = %q, want the first release %q", rollback.RollbackFrom, running1.ID)
	}
	rolledBack := h.waitForTerminal(t, app.ID, rollback.ID)
	if rolledBack.State != "running" {
		t.Fatalf("rollback state = %q, want running (error: %s)", rolledBack.State, rolledBack.Error)
	}
	waitForHTTPBody(t, appURL, v1Marker)

	t.Logf("rollback ok: %s -> %s -> %s", first.ID, second.ID, rollback.ID)
}
