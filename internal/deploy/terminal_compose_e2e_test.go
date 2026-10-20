package deploy

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// composeRealNode backs the production terminal resolution with the real
// Compose and Docker clients, so a regression in resolveComposeTargetIDs fails
// this test instead of a parallel copy of the logic.
type composeRealNode struct {
	*mockNode
	compose *agent.ComposeServer
	docker  *agent.DockerClient
}

func (n *composeRealNode) ComposePs(ctx context.Context, projectName string) ([]ComposeContainer, error) {
	response, err := n.compose.ComposePs(ctx, &agentv1.ComposePsRequest{ProjectName: projectName})
	if err != nil {
		return nil, err
	}
	containers := make([]ComposeContainer, 0, len(response.GetContainers()))
	for _, container := range response.GetContainers() {
		containers = append(containers, ComposeContainer{
			ID:      container.GetContainerId(),
			Service: container.GetService(),
			Name:    container.GetName(),
			Image:   container.GetImage(),
			State:   container.GetState(),
			Status:  container.GetStatus(),
		})
	}
	return containers, nil
}

func (n *composeRealNode) ContainersByLabel(ctx context.Context, labels map[string]string) ([]*agentv1.ContainerInfo, error) {
	containers, err := n.docker.ListContainers(ctx, false, labels)
	if err != nil {
		return nil, mapRPCError(err)
	}
	return containers, nil
}

func (n *composeRealNode) Close() error { return nil }

// TestTerminalComposeReal is the JUS-256 live regression: it starts one bounded
// throwaway Compose project, persists the CLI's id as the deployment would,
// then drives the production terminal targets and create/attach resolution
// against the real node. It fails when the scoped full-id resolution regresses.
//
// Safety: unique gotham-<uuid> project, pids_limit 64, mem_limit 64m, one CPU,
// hard timeouts, and t.Cleanup removes the project even on failure. It never
// touches the shared development containers or any other gotham-* container.
func TestTerminalComposeReal(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run the live Compose terminal test")
	}
	docker, err := agent.NewDockerClient("")
	if err != nil {
		t.Skipf("docker client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if _, err := docker.Version(ctx); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	composeCheck, composeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer composeCancel()
	if err := exec.CommandContext(composeCheck, "docker", "compose", "version").Run(); err != nil {
		t.Skipf("docker compose plugin unavailable: %v", err)
	}
	if _, err := docker.PullImage(ctx, "alpine:3", "", ""); err != nil {
		t.Logf("pull alpine:3: %v (using a local image if present)", err)
	}

	// The compose root is opened component by component with O_NOFOLLOW, so
	// resolve the temporary directory's symlinks first (on darwin t.TempDir
	// lives under the /var -> /private/var symlink).
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve compose root: %v", err)
	}
	compose := agent.NewComposeServer(agent.ComposeServerConfig{Root: root})
	userID := uuid.New()
	app := Application{
		ID: uuid.New(), UserID: userID, TeamID: teams.PersonalTeamID(userID),
		ServerID: uuid.New(), Name: "terminal-e2e", SourceType: SourceCompose,
		ComposeService: "web",
	}
	project := services.ProjectName(app.ID)
	document := `services:
  web:
    image: alpine:3
    command: ["sleep", "300"]
    restart: "no"
    pids_limit: 64
    mem_limit: 64m
    cpus: "1"
`
	upCtx, upCancel := context.WithTimeout(ctx, 4*time.Minute)
	// Unconfined keeps the test's own resource bounds (pids_limit/mem_limit/
	// cpus), which the application confinement allowlist does not carry; the
	// project name, sweep-label and document checks still apply.
	_, err = compose.ComposeUp(upCtx, &agentv1.ComposeUpRequest{
		ProjectName: project,
		ComposeYaml: []byte(document),
		Unconfined:  true,
	})
	upCancel()
	t.Cleanup(func() { composeDown(t, compose, project, document) })
	if err != nil {
		t.Fatalf("ComposeUp: %v", err)
	}

	psCtx, psCancel := context.WithTimeout(ctx, time.Minute)
	ps, err := compose.ComposePs(psCtx, &agentv1.ComposePsRequest{ProjectName: project})
	psCancel()
	if err != nil {
		t.Fatalf("ComposePs: %v", err)
	}
	cliID := ""
	for _, container := range ps.GetContainers() {
		if container.GetService() == "web" && strings.EqualFold(container.GetState(), "running") {
			cliID = strings.TrimSpace(container.GetContainerId())
			break
		}
	}
	if cliID == "" {
		t.Fatalf("no running web container in %+v", ps.GetContainers())
	}

	// A compose deployment persists exactly the CLI's id (short on the Compose
	// versions this regression covers).
	repo := &fakeRepository{app: app}
	node := &composeRealNode{mockNode: newMockNode(), compose: compose, docker: docker}
	svc := newNodeService(t, repo, node)
	seedDeployment(t, repo, app, Deployment{State: StateRunning, ContainerID: cliID})

	set, err := svc.ListTerminalContainers(ctx, userID, app.ID)
	if err != nil {
		t.Fatalf("ListTerminalContainers: %v", err)
	}
	var web TerminalContainer
	found := false
	for _, container := range set.Containers {
		if container.Service == "web" {
			web = container
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("targets = %+v, want the web service", set.Containers)
	}
	if !isFullContainerID(web.ContainerID) {
		t.Fatalf("target id = %q, want the full 64-character id", web.ContainerID)
	}
	if !strings.HasPrefix(web.ContainerID, cliID) {
		t.Fatalf("target id %q does not start with the CLI id %q", web.ContainerID, cliID)
	}
	t.Logf("compose ps id %q (%d chars) resolved by production code to full id %q", cliID, len(cliID), web.ContainerID)

	// Create and attach both re-resolve through ResolveTerminalContainer.
	for attempt := 1; attempt <= 2; attempt++ {
		_, resolved, resolveErr := svc.ResolveTerminalContainer(ctx, userID, app.ID, "web", web.ContainerID)
		if resolveErr != nil || resolved.ContainerID != web.ContainerID {
			t.Fatalf("resolve attempt %d = (%+v, %v)", attempt, resolved, resolveErr)
		}
	}
	// The short id must still be refused before any exec, exactly as a client
	// sending the old targets value would be.
	short := web.ContainerID[:12]
	if _, _, err := svc.ResolveTerminalContainer(ctx, userID, app.ID, "web", short); !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("short id %q = %v, want ErrContainerNotFound", short, err)
	}

	session, err := docker.Exec(ctx, web.ContainerID, agent.ExecOptions{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("exec on %s: %v", web.ContainerID, err)
	}
	if _, err := session.Write([]byte("echo compose-terminal-ok; exit 0\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	output := readTerminalUntil(session, "compose-terminal-ok", 20*time.Second)
	if !strings.Contains(output, "compose-terminal-ok") {
		t.Fatalf("output = %q, want the command result", output)
	}
	// Let the shell process `exit 0` on its own first, so Close cleans up an
	// already-finished exec instead of racing a live shell.
	code, _, err := session.ExitCode(ctx)
	if err != nil {
		t.Fatalf("exit code: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close exec: %v", err)
	}
	// Closing the terminal must not stop the application container.
	inspectCtx, inspectCancel := context.WithTimeout(ctx, 30*time.Second)
	defer inspectCancel()
	containers, err := docker.ListContainers(inspectCtx, false, map[string]string{composeProjectLabel: project})
	if err != nil {
		t.Fatalf("list after exec close: %v", err)
	}
	if len(containers) != 1 || containers[0].GetId() != web.ContainerID {
		t.Fatalf("containers after close = %+v, want the still-running %s", containers, web.ContainerID)
	}
}

// readTerminalUntil reads the exec stream until marker appears or timeout.
func readTerminalUntil(reader io.Reader, marker string, timeout time.Duration) string {
	done := make(chan string, 1)
	go func() {
		var builder strings.Builder
		buffer := make([]byte, 4096)
		for {
			n, err := reader.Read(buffer)
			if n > 0 {
				builder.Write(buffer[:n])
				if strings.Contains(builder.String(), marker) {
					done <- builder.String()
					return
				}
			}
			if err != nil {
				done <- builder.String()
				return
			}
		}
	}()
	select {
	case output := <-done:
		return output
	case <-time.After(timeout):
		return ""
	}
}

// composeDown removes the throwaway project with its own bounded context, even
// when the test body already failed.
func composeDown(t *testing.T, server *agent.ComposeServer, project, document string) {
	t.Helper()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := server.ComposeDown(cleanupCtx, &agentv1.ComposeDownRequest{
		ProjectName: project,
		ComposeYaml: []byte(document),
		Unconfined:  true,
	}); err != nil {
		t.Logf("compose down %s: %v", project, err)
	}
}
