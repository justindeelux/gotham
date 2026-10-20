package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TestComposeTerminalReal is the JUS-256 live regression: a real Compose
// project reports a short container id through ComposePs, the terminal path
// must resolve it to the full 64-character id the agent validates, and an exec
// shell must open on that id.
//
// It starts one throwaway alpine container in a unique gotham-<uuid> project,
// bounded by pids_limit/mem_limit/cpus, and removes the project with
// t.Cleanup even when the test fails. It never touches the shared
// gotham-dev-postgres/gotham-dev-redis containers or any other node container.
func TestComposeTerminalReal(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run the live Compose terminal test")
	}
	client, err := NewDockerClient("")
	if err != nil {
		t.Skipf("docker client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if _, err := client.Version(ctx); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	composeCheck, composeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer composeCancel()
	if err := exec.CommandContext(composeCheck, "docker", "compose", "version").Run(); err != nil {
		t.Skipf("docker compose plugin unavailable: %v", err)
	}
	if _, err := client.PullImage(ctx, "alpine:3", "", ""); err != nil {
		t.Logf("pull alpine:3: %v (using a local image if present)", err)
	}

	// The compose root is opened component by component with O_NOFOLLOW, so
	// resolve the temporary directory's symlinks first (on darwin t.TempDir
	// lives under the /var -> /private/var symlink).
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve compose root: %v", err)
	}
	server := NewComposeServer(ComposeServerConfig{Root: root})
	project := "gotham-" + uuid.NewString()
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
	// cpus) which the application confinement allowlist does not carry; the
	// project name and document checks still apply.
	_, err = server.ComposeUp(upCtx, &agentv1.ComposeUpRequest{
		ProjectName: project,
		ComposeYaml: []byte(document),
		Unconfined:  true,
	})
	upCancel()
	if err != nil {
		// The project may have been partially created; cleanup still runs.
		t.Cleanup(func() { composeDown(t, server, project, document) })
		t.Fatalf("ComposeUp: %v", err)
	}
	t.Cleanup(func() { composeDown(t, server, project, document) })

	psCtx, psCancel := context.WithTimeout(ctx, time.Minute)
	response, err := server.ComposePs(psCtx, &agentv1.ComposePsRequest{ProjectName: project})
	psCancel()
	if err != nil {
		t.Fatalf("ComposePs: %v", err)
	}
	var web *agentv1.ComposeContainer
	for _, container := range response.GetContainers() {
		if container.GetService() == "web" && strings.EqualFold(container.GetState(), "running") {
			web = container
			break
		}
	}
	if web == nil {
		t.Fatalf("no running web container in %+v", response.GetContainers())
	}
	cliID := strings.TrimSpace(web.GetContainerId())
	if cliID == "" {
		t.Fatalf("web container has no id: %+v", web)
	}

	// This is the control plane's normalization step, against the real node:
	// resolve the CLI id through Docker's container list.
	listCtx, listCancel := context.WithTimeout(ctx, time.Minute)
	listed, err := client.ListContainers(listCtx, true)
	listCancel()
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	full := ""
	for _, container := range listed {
		if container.GetName() == web.GetName() {
			full = container.GetId()
			break
		}
	}
	if full == "" {
		for _, container := range listed {
			if strings.HasPrefix(container.GetId(), cliID) {
				if full != "" && full != container.GetId() {
					t.Fatalf("ambiguous id prefix %q on the node", cliID)
				}
				full = container.GetId()
			}
		}
	}
	if full == "" {
		t.Fatalf("could not resolve %q to a full container id", cliID)
	}
	if !validContainerID(full) {
		t.Fatalf("resolved id %q is not the full 64-character lowercase hex id", full)
	}
	t.Logf("compose ps id %q (%d chars) resolved to full id %q", cliID, len(cliID), full)

	session, err := client.Exec(ctx, full, ExecOptions{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("exec on %s: %v", full, err)
	}
	if _, err := session.Write([]byte("echo compose-terminal-ok; exit 0\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	output := readExecUntil(session, "compose-terminal-ok", 20*time.Second)
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
	if _, err := client.inspectContainer(ctx, full); err != nil {
		t.Fatalf("inspect after exec close: %v", err)
	}
}

// composeDown removes the throwaway project with its own bounded context, even
// when the test body already failed.
func composeDown(t *testing.T, server *ComposeServer, project, document string) {
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
