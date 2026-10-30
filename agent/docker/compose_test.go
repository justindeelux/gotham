package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const composeTestProject = "gotham-3f2a4b6c-8d0e-4f1a-9b2c-3d4e5f607182"

const composeTestDocument = `services:
  web:
    image: nginx:1.23
    volumes:
      - data:/data
  worker:
    image: busybox:1.36
volumes:
  data:
`

func fakeDockerCLI(t *testing.T, script string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "calls.log")
	body := `#!/bin/sh
{
  echo "args=$@"
  echo "cwd=$PWD"
  echo "docker_host=${DOCKER_HOST:-}"
} >> "$FAKE_DOCKER_CALLS"
` + script + "\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("FAKE_DOCKER_CALLS", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func composeCalls(t *testing.T, logPath string) []string {
	t.Helper()
	content, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	lines := []string{}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "args=") {
			lines = append(lines, line)
		}
	}
	return lines
}

func newTestComposeServer(t *testing.T, cfg ComposeServerConfig) *ComposeServer {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonical temp dir: %v", err)
	}
	cfg.Root = root
	if cfg.Logger == nil {
		cfg.Logger = discardLogger()
	}
	return NewComposeServer(cfg)
}

func TestComposeValidateWritesAndInspects(t *testing.T) {
	logPath := fakeDockerCLI(t, `
case "$7" in
  --services) printf 'web\nworker\n' ;;
  --volumes) printf 'data\n' ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	response, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	})
	if err != nil {
		t.Fatalf("ComposeValidate: %v", err)
	}
	if strings.Join(response.GetServices(), ",") != "web,worker" {
		t.Errorf("services = %v", response.GetServices())
	}
	if strings.Join(response.GetVolumes(), ",") != "data" {
		t.Errorf("volumes = %v", response.GetVolumes())
	}
	if !response.GetConfinedEnforced() {
		t.Error("confined_enforced = false, want true for the default (confined) request")
	}

	file := filepath.Join(server.root, composeTestProject, "compose.yaml")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("compose file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("compose file mode = %o, want 600", mode)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read compose file: %v", err)
	}
	if string(content) != composeTestDocument {
		t.Errorf("compose file content = %q", content)
	}
	calls := composeCalls(t, logPath)
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want config --services and --volumes", calls)
	}
	for _, call := range calls {
		if !strings.Contains(call, "-p "+composeTestProject) {
			t.Errorf("call missing project name: %q", call)
		}
	}
}

func TestComposeProjectValidation(t *testing.T) {
	for _, name := range []string{
		"",
		"../../etc",
		"/etc",
		"gotham-../etc",
		"gotham-3F2A4B6C-8D0E-4F1A-9B2C-3D4E5F607182",
		"other-3f2a4b6c-8d0e-4f1a-9b2c-3d4e5f607182",
		"gotham-not-a-uuid",
	} {
		t.Run(name, func(t *testing.T) {
			server := newTestComposeServer(t, ComposeServerConfig{})
			_, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
				ProjectName: name,
				ComposeYaml: []byte(composeTestDocument),
			})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument (%v)", status.Code(err), err)
			}
		})
	}
}

func TestComposeInputBounds(t *testing.T) {
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("empty document code = %v, want InvalidArgument", status.Code(err))
	}
	oversized := make([]byte, maxComposeYAML+1)
	for i := range oversized {
		oversized[i] = 'x'
	}
	if _, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: oversized,
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("oversized document code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestComposeUpDownCommands(t *testing.T) {
	logPath := fakeDockerCLI(t, "exit 0")
	server := newTestComposeServer(t, ComposeServerConfig{})

	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("ComposeUp: %v", err)
	}
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
		Restart:     true,
	}); err != nil {
		t.Fatalf("ComposeUp(restart): %v", err)
	}
	if _, err := server.ComposeDown(context.Background(), &agentv1.ComposeDownRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("ComposeDown: %v", err)
	}

	calls := composeCalls(t, logPath)
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "up -d --remove-orphans") {
		t.Errorf("no up -d --remove-orphans call:\n%s", joined)
	}
	if !strings.Contains(joined, "restart -d") && !strings.Contains(joined, "restart") {
		t.Errorf("no restart call:\n%s", joined)
	}
	if !strings.Contains(joined, "down --remove-orphans") {
		t.Errorf("no down --remove-orphans call:\n%s", joined)
	}
	for _, call := range calls {
		if strings.Contains(call, "--volumes") || strings.Contains(call, " -v") {
			t.Errorf("down must never remove volumes: %q", call)
		}
	}
	for _, call := range calls {
		if !strings.Contains(call, filepath.Join(server.root, composeTestProject, "compose.yaml")) &&
			!strings.Contains(call, "-f ") {
			t.Errorf("call missing -f: %q", call)
		}
	}
}

func TestComposeDownRemovesRecordedVolumesOnly(t *testing.T) {
	logPath := fakeDockerCLI(t, `exit 0`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	otherProject := "gotham-00000000-0000-0000-0000-000000000001"
	document := []byte(`services:
  web:
    image: nginx:1.23
    volumes:
      - data:/data
      - shared:/shared
volumes:
  data:
  shared:
    external: true
  foreign:
    name: ` + otherProject + `_data
`)

	response, err := server.ComposeDown(context.Background(), &agentv1.ComposeDownRequest{
		ProjectName:   composeTestProject,
		ComposeYaml:   document,
		Unconfined:    true,
		RemoveVolumes: true,
	})
	if err != nil {
		t.Fatalf("ComposeDown: %v", err)
	}
	warnings := response.GetVolumeWarnings()
	if len(warnings) != 1 || warnings[0] != "kept volume "+otherProject+"_data: it has a custom name" {
		t.Errorf("volume warnings = %v, want the custom-name volume reported", warnings)
	}

	removed := map[string]bool{}
	for _, call := range composeCalls(t, logPath) {
		if fields := strings.Fields(strings.TrimPrefix(call, "args=")); len(fields) == 3 && fields[0] == "volume" {
			removed[fields[2]] = true
		}
	}
	if len(removed) != 1 || !removed[composeTestProject+"_data"] {
		t.Fatalf("removed volumes = %v, want only %s_data", removed, composeTestProject)
	}
	if removed[otherProject+"_data"] || removed[composeTestProject+"_shared"] || removed["shared"] {
		t.Errorf("removed volumes = %v, must not remove a custom-name or external volume", removed)
	}
}

func TestComposeDownReportsVolumesItCannotRemove(t *testing.T) {
	logPath := fakeDockerCLI(t, `
if [ "$1" = "volume" ]; then
  echo "Error response from daemon: remove $3: volume is in use" >&2
  exit 1
fi
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})

	response, err := server.ComposeDown(context.Background(), &agentv1.ComposeDownRequest{
		ProjectName:   composeTestProject,
		ComposeYaml:   []byte(composeTestDocument),
		Unconfined:    true,
		RemoveVolumes: true,
	})
	if err != nil {
		t.Fatalf("ComposeDown = %v, want success with a warning", err)
	}
	warnings := response.GetVolumeWarnings()
	if len(warnings) != 1 || !strings.Contains(warnings[0], composeTestProject+"_data") {
		t.Errorf("volume warnings = %v, want one for %s_data", warnings, composeTestProject)
	}
	if !strings.Contains(strings.Join(composeCalls(t, logPath), "\n"), "volume rm "+composeTestProject+"_data") {
		t.Error("the volume removal was not attempted")
	}
}

func TestComposeDownIgnoresAMissingVolume(t *testing.T) {
	fakeDockerCLI(t, `
if [ "$1" = "volume" ]; then
  echo "Error response from daemon: get $3: no such volume" >&2
  exit 1
fi
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})

	response, err := server.ComposeDown(context.Background(), &agentv1.ComposeDownRequest{
		ProjectName:   composeTestProject,
		ComposeYaml:   []byte(composeTestDocument),
		Unconfined:    true,
		RemoveVolumes: true,
	})
	if err != nil {
		t.Fatalf("ComposeDown: %v", err)
	}
	if len(response.GetVolumeWarnings()) != 0 {
		t.Errorf("volume warnings = %v, want none for an already absent volume", response.GetVolumeWarnings())
	}
}

func managedVolumeDocument(device string) []byte {
	return []byte(fmt.Sprintf(`services:
  web:
    image: nginx:1.23
    volumes:
      - data:/data
volumes:
  data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: %s
`, device))
}

func TestComposeUpCreatesManagedVolumeDirs(t *testing.T) {
	logPath := fakeDockerCLI(t, "exit 0")
	volumeRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonical volume root: %v", err)
	}
	server := newTestComposeServer(t, ComposeServerConfig{
		VolumeRoot: filepath.Join(volumeRoot, "volumes"),
	})
	appID := strings.TrimPrefix(composeTestProject, "gotham-")
	device := filepath.Join(server.volumeRoot, appID, "data")

	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: managedVolumeDocument(device),
	}); err != nil {
		t.Fatalf("ComposeUp: %v", err)
	}
	info, err := os.Stat(device)
	if err != nil {
		t.Fatalf("volume device directory: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("volume device = %s, want a directory", device)
	}
	if !strings.Contains(strings.Join(composeCalls(t, logPath), "\n"), "up -d --remove-orphans") {
		t.Error("compose up did not run after the device directory was created")
	}
}

func TestComposeUpManagedVolumeDirRejectsOutsideRoot(t *testing.T) {
	volumeRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonical volume root: %v", err)
	}
	server := newTestComposeServer(t, ComposeServerConfig{VolumeRoot: volumeRoot})
	appID := strings.TrimPrefix(composeTestProject, "gotham-")
	appDir := filepath.Join(volumeRoot, appID)

	outside := filepath.Join(filepath.Dir(volumeRoot), "outside", "data")
	otherApp := filepath.Join(volumeRoot, "22222222-2222-2222-2222-222222222222", "data")
	for name, device := range map[string]string{
		"outside root":     outside,
		"other app dir":    otherApp,
		"nested under app": filepath.Join(appDir, "nested", "data"),
	} {
		t.Run(name, func(t *testing.T) {
			err := server.ensureManagedVolumeDirs(composeTestProject, managedVolumeDocument(device))
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument (%v)", status.Code(err), err)
			}
			if _, statErr := os.Stat(device); !os.IsNotExist(statErr) {
				t.Errorf("device %q was created despite rejection", device)
			}
		})
	}

	t.Run("traversing volume name", func(t *testing.T) {
		document := []byte(fmt.Sprintf(`services:
  web:
    image: x
volumes:
  "../escape":
    driver_opts:
      type: none
      o: bind
      device: %s
`, filepath.Join(volumeRoot, "escape")))
		if err := server.ensureManagedVolumeDirs(composeTestProject, document); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("code = %v, want InvalidArgument (%v)", status.Code(err), err)
		}
	})

	t.Run("device symlink", func(t *testing.T) {
		if err := os.MkdirAll(appDir, 0o755); err != nil {
			t.Fatalf("mkdir app dir: %v", err)
		}
		target := filepath.Join(t.TempDir(), "target")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		device := filepath.Join(appDir, "data")
		if err := os.Symlink(target, device); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		err := server.ensureManagedVolumeDirs(composeTestProject, managedVolumeDocument(device))
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("code = %v, want InvalidArgument (%v)", status.Code(err), err)
		}
	})

	t.Run("app dir symlink", func(t *testing.T) {
		elsewhereRoot, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatalf("canonical root: %v", err)
		}
		elsewhere := filepath.Join(elsewhereRoot, "elsewhere")
		if err := os.MkdirAll(elsewhere, 0o755); err != nil {
			t.Fatalf("mkdir elsewhere: %v", err)
		}
		linkedRoot := filepath.Join(elsewhereRoot, "volumes")
		if err := os.MkdirAll(linkedRoot, 0o755); err != nil {
			t.Fatalf("mkdir linked root: %v", err)
		}
		if err := os.Symlink(elsewhere, filepath.Join(linkedRoot, appID)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		linked := newTestComposeServer(t, ComposeServerConfig{VolumeRoot: linkedRoot})
		device := filepath.Join(linkedRoot, appID, "data")
		if err := linked.ensureManagedVolumeDirs(composeTestProject, managedVolumeDocument(device)); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("code = %v, want InvalidArgument (%v)", status.Code(err), err)
		}
	})
}

func TestComposeDockerHost(t *testing.T) {
	logPath := fakeDockerCLI(t, "exit 0")
	server := newTestComposeServer(t, ComposeServerConfig{DockerHost: "/var/run/docker.sock"})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("ComposeUp: %v", err)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if !strings.Contains(string(content), "docker_host=unix:///var/run/docker.sock") {
		t.Errorf("DOCKER_HOST not forwarded as a unix URL:\n%s", content)
	}

	logPath = fakeDockerCLI(t, "exit 0")
	server = newTestComposeServer(t, ComposeServerConfig{DockerHost: "tcp://127.0.0.1:2375"})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("ComposeUp(tcp): %v", err)
	}
	content, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if !strings.Contains(string(content), "docker_host=tcp://127.0.0.1:2375") {
		t.Errorf("DOCKER_HOST not forwarded:\n%s", content)
	}
}

func TestComposePsParsesJSONLines(t *testing.T) {
	fakeDockerCLI(t, `
printf '%s\n' '{"ID":"abc123","Name":"proj-web-1","Service":"web","Image":"nginx:1.23","State":"running","Status":"Up 2 minutes","Health":"healthy","Ports":"0.0.0.0:8080->80/tcp, 443/tcp"}'
printf '%s\n' '{"ID":"def456","Names":"proj-worker-1","Service":"worker","Image":"busybox:1.36","State":"exited","Status":"Exited (0) 1 second ago","Ports":""}'
exit 0
`)
	file := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(file, []byte(composeTestDocument), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	response, err := server.ComposePs(context.Background(), &agentv1.ComposePsRequest{ProjectName: composeTestProject})
	if err != nil {
		t.Fatalf("ComposePs: %v", err)
	}
	containers := response.GetContainers()
	if len(containers) != 2 {
		t.Fatalf("containers = %+v", containers)
	}
	web := containers[0]
	if web.GetService() != "web" || web.GetName() != "proj-web-1" || web.GetState() != "running" ||
		web.GetHealth() != "healthy" || len(web.GetPorts()) != 2 {
		t.Errorf("web = %+v", web)
	}
	worker := containers[1]
	if worker.GetName() != "proj-worker-1" || worker.GetState() != "exited" || len(worker.GetPorts()) != 0 {
		t.Errorf("worker = %+v", worker)
	}

	if _, err := server.ComposePs(context.Background(), &agentv1.ComposePsRequest{
		ProjectName: "gotham-00000000-0000-4000-8000-000000000000",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("ps for an unwritten project = %v, want FailedPrecondition", status.Code(err))
	}
}

func TestComposePsIgnoresStderrWarnings(t *testing.T) {
	fakeDockerCLI(t, `
printf 'WARN[0000] a warning that must not corrupt the output\n' >&2
printf '%s\n' '{"ID":"a","Service":"web","State":"running"}'
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	response, err := server.ComposePs(context.Background(), &agentv1.ComposePsRequest{ProjectName: composeTestProject})
	if err != nil {
		t.Fatalf("ComposePs: %v", err)
	}
	if len(response.GetContainers()) != 1 || response.GetContainers()[0].GetService() != "web" {
		t.Fatalf("containers = %+v", response.GetContainers())
	}
}

func TestComposePsAcceptsJSONArray(t *testing.T) {
	containers, err := parseComposePs([]byte(`[{"ID":"a","Service":"web","State":"running","Ports":"80/tcp"}]`))
	if err != nil {
		t.Fatalf("parseComposePs: %v", err)
	}
	if len(containers) != 1 || containers[0].GetService() != "web" {
		t.Fatalf("containers = %+v", containers)
	}
	if empty, err := parseComposePs(nil); err != nil || len(empty) != 0 {
		t.Fatalf("parseComposePs(nil) = %v, %v", empty, err)
	}
}

type fakeComposeStream struct {
	grpc.ServerStream
	ctx     context.Context
	chunks  [][]byte
	frames  []*agentv1.ComposeLogChunk
	sendErr error
}

func (s *fakeComposeStream) Context() context.Context { return s.ctx }

func (s *fakeComposeStream) Send(chunk *agentv1.ComposeLogChunk) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.frames = append(s.frames, chunk)
	s.chunks = append(s.chunks, chunk.GetData())
	return nil
}

func TestComposeLogsRefusesUnknownServiceBeforeCLI(t *testing.T) {
	logPath := fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\n' ;;
  logs) printf 'no such service: missing\n' 1>&2; exit 1 ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	err := server.ComposeLogs(&agentv1.ComposeLogsRequest{
		ProjectName: composeTestProject,
		Service:     "missing",
	}, &fakeComposeStream{ctx: context.Background()})
	if status.Code(err) != codes.InvalidArgument || !strings.Contains(status.Convert(err).Message(), "no such compose service") {
		t.Fatalf("ComposeLogs(unknown) = %v, want InvalidArgument", err)
	}
	for _, call := range composeCalls(t, logPath) {
		if strings.Contains(call, "logs") {
			t.Fatalf("the logs command ran for an unknown selector: %q", call)
		}
	}
}

func TestComposeLogsQuietDescendantCancellation(t *testing.T) {
	fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\n' ;;
  logs)
    printf 'first line\n'
    sleep 8 &
    wait
    ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &slowComposeStream{ctx: ctx}
	done := make(chan error, 1)
	go func() {
		done <- server.ComposeLogs(&agentv1.ComposeLogsRequest{ProjectName: composeTestProject, Follow: true}, stream)
	}()
	waitForBytes(t, stream, 1)
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancelled stream = %v, want nil", err)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("cancellation took %s; a quiet descendant kept the pipes open", elapsed)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("a cancelled stream with a quiet descendant did not return")
	}
}

func TestComposeFailedVerbOutputIsDiagnosticBounded(t *testing.T) {
	fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\n' ;;
  up)
    head -c 4194304 /dev/zero | tr '\0' 'u'
    printf 'up failed\n' 1>&2
    exit 1
    ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	})
	runtime.ReadMemStats(&after)
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 1<<20 {
		t.Fatalf("a failed up retained %d bytes of stdout", grown)
	}
	if message := status.Convert(err).Message(); len(message) > maxComposeError+200 {
		t.Fatalf("diagnostic is unbounded: %d bytes", len(message))
	}
}

type acceptanceFailStream struct {
	grpc.ServerStream
	ctx     context.Context
	pidFile string
	sendErr error
}

func (s *acceptanceFailStream) Context() context.Context { return s.ctx }

func (s *acceptanceFailStream) Send(chunk *agentv1.ComposeLogChunk) error {
	if chunk.GetReady() {
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(s.pidFile); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return errors.New("the CLI never wrote its pid")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	return s.sendErr
}

func TestComposeLogsAcceptanceFailureReapsCLI(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "cli.pid")
	t.Setenv("FAKE_PID_FILE", pidFile)
	fakeDockerCLI(t, `
case "$6" in
  logs)
    echo $$ > "$FAKE_PID_FILE"
    sleep 5 &
    wait
    ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	stream := &acceptanceFailStream{ctx: context.Background(), pidFile: pidFile, sendErr: errors.New("transport gone")}
	if err := server.ComposeLogs(&agentv1.ComposeLogsRequest{
		ProjectName: composeTestProject,
		Follow:      true,
	}, stream); err != nil {
		t.Fatalf("ComposeLogs = %v, want nil (client gone)", err)
	}

	content, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read the CLI pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatalf("parse the CLI pid %q: %v", content, err)
	}
	var status syscall.WaitStatus
	waited, waitErr := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	if waitErr == nil && waited == pid {
		t.Fatalf("the CLI process %d was left unreaped after the RPC returned", pid)
	}
	if !errors.Is(waitErr, syscall.ECHILD) {
		t.Fatalf("Wait4(%d) = %d, %v; want ECHILD (already reaped)", pid, waited, waitErr)
	}
}

func TestComposeLogsStreams(t *testing.T) {
	logPath := fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\nworker\n' ;;
  logs)
    printf 'worker-1  | alive\n'
    printf 'worker-1  | alive again\n'
    ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	stream := &fakeComposeStream{ctx: context.Background()}
	if err := server.ComposeLogs(&agentv1.ComposeLogsRequest{
		ProjectName: composeTestProject,
		Service:     "worker",
		Tail:        50,
	}, stream); err != nil {
		t.Fatalf("ComposeLogs: %v", err)
	}
	joined := string(joinChunks(stream.chunks))
	if !strings.Contains(joined, "alive again") {
		t.Errorf("logs = %q", joined)
	}
	if len(stream.frames) == 0 || !stream.frames[0].GetReady() {
		t.Fatalf("the first frame must be the acceptance frame: %+v", stream.frames)
	}
	for _, frame := range stream.frames {
		if frame.GetReady() && len(frame.GetData()) > 0 {
			t.Errorf("an acceptance frame must carry no data: %+v", frame)
		}
	}
	calls := composeCalls(t, logPath)
	if !strings.Contains(strings.Join(calls, "\n"), "--tail 50") {
		t.Errorf("tail not forwarded:\n%v", calls)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "logs --no-color") {
		t.Errorf("logs call missing:\n%v", calls)
	}
	if !strings.Contains(strings.Join(calls, "\n"), " worker") {
		t.Errorf("service selector not forwarded:\n%v", calls)
	}

	if err := server.ComposeLogs(&agentv1.ComposeLogsRequest{
		ProjectName: composeTestProject,
		Service:     "bad service",
	}, &fakeComposeStream{ctx: context.Background()}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("invalid service code = %v, want InvalidArgument", status.Code(err))
	}

	if err := server.ComposeLogs(&agentv1.ComposeLogsRequest{
		ProjectName: composeTestProject,
		Follow:      true,
	}, &fakeComposeStream{ctx: context.Background()}); err != nil {
		t.Fatalf("ComposeLogs(follow): %v", err)
	}
	if !strings.Contains(strings.Join(composeCalls(t, logPath), "\n"), "--follow") {
		t.Errorf("--follow not forwarded")
	}
}

func TestComposeCLIFailureIsBounded(t *testing.T) {
	fakeDockerCLI(t, `
printf 'error: service "web" refers to undefined volume\n' >&2
i=0
while [ $i -lt 400 ]; do
  printf 'padding line %s\n' "$i" >&2
  i=$((i+1))
done
exit 1
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	_, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
	message := status.Convert(err).Message()
	if len(message) > maxComposeError+200 {
		t.Errorf("error message is unbounded: %d bytes", len(message))
	}
	if !strings.Contains(message, "undefined volume") {
		t.Errorf("error lost the CLI diagnosis: %q", message)
	}
}

func TestComposeOperationsAreSerializedPerProject(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	resume := filepath.Join(dir, "resume")
	t.Setenv("FAKE_STARTED", started)
	t.Setenv("FAKE_RESUME", resume)

	fakeDockerCLI(t, `
doc=$(cat "$3")
case "$doc" in
  *first*)
    touch "$FAKE_STARTED"
    i=0
    while [ ! -f "$FAKE_RESUME" ] && [ $i -lt 400 ]; do sleep 0.05; i=$((i+1)); done
    ;;
esac
case "$doc" in
  *first*) printf 'first\n' ;;
  *second*) printf 'second\n' ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})

	firstDone := make(chan []string, 1)
	firstErr := make(chan error, 1)
	go func() {
		response, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
			ProjectName: composeTestProject,
			ComposeYaml: []byte("services:\n  first:\n    image: nginx:1.23\n"),
		})
		if err != nil {
			firstErr <- err
			return
		}
		firstDone <- response.GetServices()
	}()

	waitForFile(t, started)

	secondDone := make(chan []string, 1)
	secondErr := make(chan error, 1)
	go func() {
		response, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
			ProjectName: composeTestProject,
			ComposeYaml: []byte("services:\n  second:\n    image: nginx:1.23\n"),
		})
		if err != nil {
			secondErr <- err
			return
		}
		secondDone <- response.GetServices()
	}()

	time.Sleep(300 * time.Millisecond)
	select {
	case services := <-secondDone:
		t.Fatalf("a concurrent request completed while another operation was in flight: %v", services)
	default:
	}
	if err := os.WriteFile(resume, []byte("go"), 0o600); err != nil {
		t.Fatalf("resume: %v", err)
	}

	select {
	case err := <-firstErr:
		t.Fatalf("first validate: %v", err)
	case services := <-firstDone:
		if strings.Join(services, ",") != "first" {
			t.Fatalf("the first operation executed another request's document: %v", services)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("first validate did not finish")
	}
	select {
	case err := <-secondErr:
		t.Fatalf("second validate: %v", err)
	case services := <-secondDone:
		if strings.Join(services, ",") != "second" {
			t.Fatalf("the second operation executed another request's document: %v", services)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("second validate did not finish")
	}
}

func TestComposeValidateAndUpAreSerialized(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	resume := filepath.Join(dir, "resume")
	t.Setenv("FAKE_STARTED", started)
	t.Setenv("FAKE_RESUME", resume)

	fakeDockerCLI(t, `
doc=$(cat "$3")
case "$doc" in
  *first*)
    touch "$FAKE_STARTED"
    i=0
    while [ ! -f "$FAKE_RESUME" ] && [ $i -lt 400 ]; do sleep 0.05; i=$((i+1)); done
    ;;
esac
case "$doc" in
  *first*) printf 'first\n' ;;
  *second*) printf 'second\n' ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})

	firstDone := make(chan []string, 1)
	go func() {
		response, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
			ProjectName: composeTestProject,
			ComposeYaml: []byte("services:\n  first:\n    image: nginx:1.23\n"),
		})
		if err != nil {
			firstDone <- []string{"error: " + err.Error()}
			return
		}
		firstDone <- response.GetServices()
	}()
	waitForFile(t, started)

	upDone := make(chan error, 1)
	go func() {
		_, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
			ProjectName: composeTestProject,
			ComposeYaml: []byte("services:\n  second:\n    image: nginx:1.23\n"),
		})
		upDone <- err
	}()

	time.Sleep(300 * time.Millisecond)
	select {
	case err := <-upDone:
		t.Fatalf("a concurrent up completed while a validate was in flight: %v", err)
	default:
	}
	if err := os.WriteFile(resume, []byte("go"), 0o600); err != nil {
		t.Fatalf("resume: %v", err)
	}
	select {
	case services := <-firstDone:
		if strings.Join(services, ",") != "first" {
			t.Fatalf("the validate executed another request's document: %v", services)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the validate did not finish")
	}
	select {
	case err := <-upDone:
		if err != nil {
			t.Fatalf("up: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the up did not finish")
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s never appeared", path)
}

func TestComposeLogsDrainsSlowConsumer(t *testing.T) {
	const totalBytes = 2 << 20
	fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\n' ;;
  logs) head -c `+fmt.Sprint(totalBytes)+` /dev/zero | tr '\0' 'x' ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	stream := &slowComposeStream{ctx: context.Background(), delay: 2 * time.Millisecond}
	if err := server.ComposeLogs(&agentv1.ComposeLogsRequest{ProjectName: composeTestProject}, stream); err != nil {
		t.Fatalf("ComposeLogs: %v", err)
	}
	if received := stream.receivedBytes(); received != totalBytes {
		t.Fatalf("delivered %d bytes of %d: the tail was truncated", received, totalBytes)
	}

	stream = &slowComposeStream{ctx: context.Background(), failAfter: 1, sendErr: errors.New("client gone")}
	done := make(chan error, 1)
	go func() {
		done <- server.ComposeLogs(&agentv1.ComposeLogsRequest{ProjectName: composeTestProject}, stream)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ComposeLogs after a send failure = %v, want nil (client gone)", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a failing consumer did not stop the log stream")
	}
}

func TestComposeLogsFollowCancellation(t *testing.T) {
	fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\n' ;;
  logs)
    i=0
    while [ $i -lt 4000 ]; do
      printf 'tick\n'
      sleep 0.05
      i=$((i+1))
    done
    ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream := &slowComposeStream{ctx: ctx}
	done := make(chan error, 1)
	go func() {
		done <- server.ComposeLogs(&agentv1.ComposeLogsRequest{ProjectName: composeTestProject, Follow: true}, stream)
	}()
	waitForBytes(t, stream, 1)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancelled follow stream = %v, want nil", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a cancelled follow stream did not return")
	}
}

func TestComposeOutputCaptureIsBounded(t *testing.T) {
	fakeDockerCLI(t, `
head -c 4194304 /dev/zero | tr '\0' 'x' 1>&2
printf 'undefined volume\n' 1>&2
exit 1
`)
	server := newTestComposeServer(t, ComposeServerConfig{})

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	})
	runtime.ReadMemStats(&after)
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 1<<20 {
		t.Fatalf("the CLI diagnostic capture allocated %d bytes for a 4 MiB stream", grown)
	}

	fakeDockerCLI(t, `
head -c 2097152 /dev/zero | tr '\0' 'y'
exit 0
`)
	server = newTestComposeServer(t, ComposeServerConfig{})
	_, err = server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	})
	if status.Code(err) != codes.Internal || !strings.Contains(status.Convert(err).Message(), "exceeds") {
		t.Fatalf("oversized parsed output = %v, want an Internal exceeds error", err)
	}
}

type slowComposeStream struct {
	grpc.ServerStream
	ctx       context.Context
	delay     time.Duration
	failAfter int
	sendErr   error

	mu       sync.Mutex
	sends    int
	received int
}

func (s *slowComposeStream) Context() context.Context { return s.ctx }

func (s *slowComposeStream) Send(chunk *agentv1.ComposeLogChunk) error {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sends++
	if s.failAfter > 0 && s.sends > s.failAfter {
		return s.sendErr
	}
	s.received += len(chunk.GetData())
	return nil
}

func (s *slowComposeStream) receivedBytes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.received
}

func waitForBytes(t *testing.T, stream *slowComposeStream, want int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if stream.receivedBytes() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("stream received %d bytes, want at least %d", stream.receivedBytes(), want)
}

func joinChunks(chunks [][]byte) []byte {
	var out []byte
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out
}

func TestComposeConfinedEnforcesAllowlist(t *testing.T) {
	hostiles := map[string]string{
		"privileged":        "services:\n  web:\n    image: x\n    privileged: true\n",
		"driver opts bind":  "services:\n  web:\n    image: x\n    volumes:\n      - data:/mnt\nvolumes:\n  data:\n    driver_opts:\n      type: none\n      o: bind\n      device: /\n",
		"secrets file":      "services:\n  web:\n    image: x\n    secrets:\n      - s\nsecrets:\n  s:\n    file: /etc/hostname\n",
		"parent bind":       "services:\n  web:\n    image: x\n    volumes:\n      - ..:/mnt\n",
		"cap prefix":        "services:\n  web:\n    image: x\n    cap_add:\n      - CAP_SYS_ADMIN\n",
		"network container": "services:\n  web:\n    image: x\n    network_mode: container:other\n",
		"security opt":      "services:\n  web:\n    image: x\n    security_opt:\n      - seccomp=unconfined\n",
		"sweep label":       "services:\n  web:\n    image: x\n    labels:\n      gotham.app_id: 00000000-0000-0000-0000-000000000000\n",
		"multi document":    "services:\n  web:\n    image: x\n---\nservices:\n  evil:\n    image: y\n",
	}
	for name, document := range hostiles {
		t.Run(name, func(t *testing.T) {
			logPath := fakeDockerCLI(t, "exit 0")
			server := newTestComposeServer(t, ComposeServerConfig{})
			_, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
				ProjectName: composeTestProject,
				ComposeYaml: []byte(document),
			})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument (%v)", status.Code(err), err)
			}
			if calls := composeCalls(t, logPath); len(calls) != 0 {
				t.Errorf("docker invocations = %v, want none before validation", calls)
			}
		})
	}
}

func TestComposeConfinedRejectsTags(t *testing.T) {
	tagged := map[string]string{
		"tagged env":  "services:\n  web:\n    image: x\n    environment:\n      LEAK: !x \"${AGENT_PROBE:-nothing}\"\n",
		"tagged bind": "services:\n  web:\n    image: x\n    volumes:\n      - !x \"/tmp/${AGENT_PROBE:-nothing}:/mnt\"\n",
	}
	for name, document := range tagged {
		t.Run(name, func(t *testing.T) {
			logPath := fakeDockerCLI(t, "exit 0")
			server := newTestComposeServer(t, ComposeServerConfig{})
			_, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
				ProjectName: composeTestProject,
				ComposeYaml: []byte(document),
			})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument (%v)", status.Code(err), err)
			}
			if calls := composeCalls(t, logPath); len(calls) != 0 {
				t.Errorf("docker invocations = %v, want none before validation", calls)
			}
		})
	}
}

func TestComposeConfinedAcceptsSafe(t *testing.T) {
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, _, err := server.validateComposeInput(context.Background(), composeTestProject,
		[]byte(composeTestDocument), true); err != nil {
		t.Fatalf("validate confined safe document = %v, want nil", err)
	}
}

func TestComposeSweepLabelsRejected(t *testing.T) {
	server := newTestComposeServer(t, ComposeServerConfig{})
	spoofed := "services:\n  web:\n    image: x\n    labels:\n      gotham.app_id: 00000000-0000-0000-0000-000000000000\n"
	if _, _, err := server.validateComposeInput(context.Background(), composeTestProject,
		[]byte(spoofed), true); status.Code(err) != codes.InvalidArgument {
		t.Errorf("sweep label code = %v, want InvalidArgument", status.Code(err))
	}
	routed := "services:\n  web:\n    image: x\n    labels:\n      gotham.domain: example.com\n"
	if _, _, err := server.validateComposeInput(context.Background(), composeTestProject,
		[]byte(routed), true); err != nil {
		t.Errorf("service routing label = %v, want nil (Services surface)", err)
	}
	if _, _, err := server.validateComposeInput(context.Background(), composeTestProject,
		[]byte(routed), false); status.Code(err) != codes.InvalidArgument {
		t.Errorf("confined routing label code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestComposeUnconfinedKeepsServicesWorking(t *testing.T) {
	server := newTestComposeServer(t, ComposeServerConfig{})
	document := `services:
  web:
    image: nginx:1.23
    privileged: true
    labels:
      gotham.domain: example.com
    volumes:
      - /srv/data:/data
`
	if _, _, err := server.validateComposeInput(context.Background(), composeTestProject,
		[]byte(document), true); err != nil {
		t.Errorf("unconfined services document = %v, want nil", err)
	}
	if _, _, err := server.validateComposeInput(context.Background(), composeTestProject,
		[]byte(document), false); status.Code(err) != codes.InvalidArgument {
		t.Errorf("absent-flag code = %v, want InvalidArgument (confined by default)", status.Code(err))
	}
}

func TestComposeValidateReportsEnforcement(t *testing.T) {
	script := `
case "$7" in
  --services) printf 'web\n' ;;
  --volumes) printf '\n' ;;
esac
exit 0
`
	document := "services:\n  web:\n    image: x\n"
	t.Run("confined reports true", func(t *testing.T) {
		fakeDockerCLI(t, script)
		server := newTestComposeServer(t, ComposeServerConfig{})
		response, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
			ProjectName: composeTestProject,
			ComposeYaml: []byte(document),
		})
		if err != nil {
			t.Fatalf("ComposeValidate: %v", err)
		}
		if !response.GetConfinedEnforced() {
			t.Error("confined_enforced = false, want true for the default request")
		}
	})
	t.Run("unconfined reports false", func(t *testing.T) {
		fakeDockerCLI(t, script)
		server := newTestComposeServer(t, ComposeServerConfig{})
		response, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
			ProjectName: composeTestProject,
			ComposeYaml: []byte(document),
			Unconfined:  true,
		})
		if err != nil {
			t.Fatalf("ComposeValidate: %v", err)
		}
		if response.GetConfinedEnforced() {
			t.Error("confined_enforced = true, want false for the unconfined request")
		}
	})
}

func TestComposeVolumeRootDefaults(t *testing.T) {
	server := newTestComposeServer(t, ComposeServerConfig{})
	if server.volumeRoot != DefaultManagedVolumeRoot {
		t.Errorf("volume root = %q, want the node default %q", server.volumeRoot, DefaultManagedVolumeRoot)
	}
	custom := newTestComposeServer(t, ComposeServerConfig{VolumeRoot: "/data/volumes"})
	if custom.volumeRoot != "/data/volumes" {
		t.Errorf("volume root = %q, want the configured value", custom.volumeRoot)
	}
}
