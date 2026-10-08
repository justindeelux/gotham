package e2e

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/agent"
)

// TestGS8ComposeDeployRollbackDelete drives a compose application through
// the real stack (API -> deploy service -> mTLS agent ComposeServer ->
// `docker compose` CLI -> Docker): pasted v1 deploys, an edit to v2
// redeploys, a rollback to the v1 deployment re-applies the v1 document,
// and deleting the application tears the project down. A hostile document
// is refused at the API with a 400.
//
// The marker is an environment value read back out of the running web
// container, so each phase proves which document the node actually runs.
func TestGS8ComposeDeployRollbackDelete(t *testing.T) {
	h := newP4HarnessWithAgentOptions(t, agent.WithComposeService(agent.NewComposeServer(
		agent.ComposeServerConfig{
			Root:       gs8ComposeRoot(t),
			DockerHost: e2eDockerSock(),
			Logger:     testLogger(t),
		})))
	suffix := uuid.New().String()[:8]

	v1 := `services:
  web:
    image: busybox:1.36
    command: ["sleep", "3600"]
    environment:
      MARKER: v1
`
	v2 := `services:
  web:
    image: busybox:1.36
    command: ["sleep", "3600"]
    environment:
      MARKER: v2
`
	app := h.createApplication(t, p4CreateApplication{
		EnvironmentID:  h.envID.String(),
		Name:           "gs8-compose-" + suffix,
		SourceType:     "compose",
		ComposeContent: v1,
		ComposeService: "web",
		Port:           3000,
		HostPort:       0,
		ServerID:       h.serverID.String(),
	})

	queued := h.deploy(t, app.ID)
	v1Running := h.waitForTerminal(t, app.ID, queued.ID)
	if v1Running.State != "running" {
		t.Fatalf("v1 state = %q, want running (error: %s)", v1Running.State, v1Running.Error)
	}
	if v1Running.ContainerID == "" {
		t.Fatal("v1 running deployment has no container")
	}
	if got := gs8ContainerMarker(t, v1Running.ContainerID); got != "v1" {
		t.Fatalf("v1 marker = %q, want v1", got)
	}

	// Edit to v2 and redeploy: the node runs the new document.
	h.updateApplication(t, app.ID, map[string]any{"compose_content": v2})
	queuedV2 := h.deploy(t, app.ID)
	v2Running := h.waitForTerminal(t, app.ID, queuedV2.ID)
	if v2Running.State != "running" {
		t.Fatalf("v2 state = %q, want running (error: %s)", v2Running.State, v2Running.Error)
	}
	if got := gs8ContainerMarker(t, v2Running.ContainerID); got != "v2" {
		t.Fatalf("v2 marker = %q, want v2", got)
	}

	// Roll back to the v1 deployment: the v1 document is re-applied, not
	// the live v2 file.
	rolled := h.rollback(t, app.ID, map[string]any{"deployment_id": v1Running.ID})
	back := h.waitForTerminal(t, app.ID, rolled.ID)
	if back.State != "running" {
		t.Fatalf("rollback state = %q, want running (error: %s)", back.State, back.Error)
	}
	if got := gs8ContainerMarker(t, back.ContainerID); got != "v1" {
		t.Fatalf("rollback marker = %q, want v1", got)
	}

	// Deleting the application tears the compose project down: no container
	// of the project may survive.
	h.deleteApplication(t, app.ID)
	if leftovers := gs8ProjectContainers(t, app.ID); len(leftovers) != 0 {
		t.Errorf("project containers survive the delete: %v", leftovers)
	}
}

// TestGS8ComposeHostileRejected proves the confinement gate at the seam
// that matters: hostile documents through the real API are 400s and queue
// nothing, including a YAML-tagged scalar smuggling a live reference (the
// tag skips substitution, so it must fail closed at the gate).
func TestGS8ComposeHostileRejected(t *testing.T) {
	h := newP4HarnessWithAgentOptions(t, agent.WithComposeService(agent.NewComposeServer(
		agent.ComposeServerConfig{
			Root:       gs8ComposeRoot(t),
			DockerHost: e2eDockerSock(),
			Logger:     testLogger(t),
		})))
	suffix := uuid.New().String()[:8]

	hostiles := map[string]string{
		"privileged":  "services:\n  web:\n    image: busybox:1.36\n    privileged: true\n",
		"tagged":      "services:\n  web:\n    image: busybox:1.36\n    environment:\n      LEAK: !x \"${GS8_AGENT_PROBE:-nothing}\"\n",
		"tagged bind": "services:\n  web:\n    image: busybox:1.36\n    volumes:\n      - !x \"/tmp/${GS8_AGENT_PROBE:-nothing}:/mnt\"\n",
	}
	for name, document := range hostiles {
		t.Run(name, func(t *testing.T) {
			status, raw := h.api(t, http.MethodPost, "/v1/applications", p4CreateApplication{
				EnvironmentID:  h.envID.String(),
				Name:           "gs8-hostile-" + suffix + "-" + strings.ReplaceAll(name, " ", ""),
				SourceType:     "compose",
				ComposeContent: document,
				ComposeService: "web",
				Port:           3000,
				ServerID:       h.serverID.String(),
			}, nil)
			if status != http.StatusBadRequest {
				t.Fatalf("hostile create status = %d, want 400 (body %s)", status, raw)
			}
		})
	}
}

// gs8ComposeRoot returns a canonical temp directory for the test agent's
// compose projects (macOS temp paths carry /var -> /private/var symlinks
// the agent's no-follow traversal rejects).
func gs8ComposeRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonical temp dir: %v", err)
	}
	return root
}

// gs8ContainerMarker reads the MARKER value out of a running container.
func gs8ContainerMarker(t *testing.T, containerID string) string {
	t.Helper()
	out, err := runDocker(context.Background(), "exec", containerID, "printenv", "MARKER")
	if err != nil {
		t.Fatalf("exec printenv in %s: %v", containerID, err)
	}
	return strings.TrimSpace(string(out))
}

// gs8ProjectContainers lists the containers still carrying the project's
// compose label.
func gs8ProjectContainers(t *testing.T, appID string) []string {
	t.Helper()
	out, err := runDocker(context.Background(), "ps", "-a", "--filter", "label=com.docker.compose.project=gotham-"+appID,
		"--format", "{{.ID}}")
	if err != nil {
		t.Fatalf("list project containers: %v", err)
	}
	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			ids = append(ids, strings.TrimSpace(line))
		}
	}
	return ids
}

// updateApplication applies a partial update through the API (200).
func (h *p4Harness) updateApplication(t *testing.T, appID string, body map[string]any) {
	t.Helper()
	var out map[string]any
	if status, raw := h.api(t, http.MethodPut, "/v1/applications/"+appID, body, &out); status != http.StatusOK {
		t.Fatalf("update application: status %d: %s", status, raw)
	}
}

// deleteApplication removes the application through the API (204).
func (h *p4Harness) deleteApplication(t *testing.T, appID string) {
	t.Helper()
	if status, raw := h.api(t, http.MethodDelete, "/v1/applications/"+appID, nil, nil); status != http.StatusNoContent {
		t.Fatalf("delete application: status %d: %s", status, raw)
	}
}
