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
		ServerID:       h.serverID.String(),
	})
	h.addDomain(t, app.ID, "gs8-"+suffix+".example.test", 3000)

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

	h.updateApplication(t, app.ID, map[string]any{"compose_content": v2})
	queuedV2 := h.deploy(t, app.ID)
	v2Running := h.waitForTerminal(t, app.ID, queuedV2.ID)
	if v2Running.State != "running" {
		t.Fatalf("v2 state = %q, want running (error: %s)", v2Running.State, v2Running.Error)
	}
	if got := gs8ContainerMarker(t, v2Running.ContainerID); got != "v2" {
		t.Fatalf("v2 marker = %q, want v2", got)
	}

	rolled := h.rollback(t, app.ID, map[string]any{"deployment_id": v1Running.ID})
	back := h.waitForTerminal(t, app.ID, rolled.ID)
	if back.State != "running" {
		t.Fatalf("rollback state = %q, want running (error: %s)", back.State, back.Error)
	}
	if got := gs8ContainerMarker(t, back.ContainerID); got != "v1" {
		t.Fatalf("rollback marker = %q, want v1", got)
	}

	h.deleteApplication(t, app.ID)
	if leftovers := gs8ProjectContainers(t, app.ID); len(leftovers) != 0 {
		t.Errorf("project containers survive the delete: %v", leftovers)
	}
}

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
				ServerID:       h.serverID.String(),
			}, nil)
			if status != http.StatusBadRequest {
				t.Fatalf("hostile create status = %d, want 400 (body %s)", status, raw)
			}
		})
	}
}

func gs8ComposeRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonical temp dir: %v", err)
	}
	return root
}

func gs8ContainerMarker(t *testing.T, containerID string) string {
	t.Helper()
	out, err := runDocker(context.Background(), "exec", containerID, "printenv", "MARKER")
	if err != nil {
		t.Fatalf("exec printenv in %s: %v", containerID, err)
	}
	return strings.TrimSpace(string(out))
}

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

func (h *p4Harness) updateApplication(t *testing.T, appID string, body map[string]any) {
	t.Helper()
	var out map[string]any
	if status, raw := h.api(t, http.MethodPut, "/v1/applications/"+appID, body, &out); status != http.StatusOK {
		t.Fatalf("update application: status %d: %s", status, raw)
	}
}

func (h *p4Harness) deleteApplication(t *testing.T, appID string) {
	t.Helper()
	if status, raw := h.api(t, http.MethodDelete, "/v1/applications/"+appID, nil, nil); status != http.StatusNoContent {
		t.Fatalf("delete application: status %d: %s", status, raw)
	}
}
