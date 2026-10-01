package e2e

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// p4BaseImage is the base image every fixture builds from: a few megabytes,
// one pull, and busybox httpd serves the application on the container port.
const p4BaseImage = "busybox:1.36"

// p4ContainerPort is the port the fixture application listens on.
const p4ContainerPort = 8080

// p4Dockerfile is the application the happy-path scenarios deploy: a static
// page served by busybox httpd, with no healthcheck (Docker then reports a
// running container as healthy, which is the orchestrator's own semantics).
const p4Dockerfile = `FROM ` + p4BaseImage + `
COPY index.html /www/index.html
EXPOSE 8080
CMD ["httpd", "-f", "-p", "8080", "-h", "/www"]
`

// p4BrokenDockerfile fails on the second instruction. The marker is echoed
// before the failure so the test can prove the build output reached the
// deployment's log.
const p4BrokenDockerfile = `FROM ` + p4BaseImage + `
RUN echo P4-BUILD-BOOM && exit 1
`

// p4Fixture is a local git repository standing in for the public repository an
// application deploys from. The control plane clones it from disk (allowed only
// because GOTHAM_DEV_CLONE_LOCAL=true is set on the harness), so no provider
// API and no network are involved.
type p4Fixture struct {
	dir    string
	repo   string
	marker string
}

// newP4Fixture creates a repository with the working application committed on
// main. name is the owner/name the seeded webhook watches.
func newP4Fixture(t *testing.T, name, marker string) *p4Fixture {
	t.Helper()
	fixture := initFixture(t, name)
	fixture.commit(t, marker, p4Dockerfile)
	return fixture
}

// newBrokenP4Fixture creates a repository whose Dockerfile fails to build.
func newBrokenP4Fixture(t *testing.T, name, marker string) *p4Fixture {
	t.Helper()
	fixture := initFixture(t, name)
	fixture.commit(t, marker, p4BrokenDockerfile)
	return fixture
}

// initFixture creates the empty repository on main with its git identity set.
func initFixture(t *testing.T, name string) *p4Fixture {
	t.Helper()
	requireGit(t)
	fixture := &p4Fixture{dir: t.TempDir(), repo: name}
	git(t, fixture.dir, "init")
	git(t, fixture.dir, "symbolic-ref", "HEAD", "refs/heads/main")
	git(t, fixture.dir, "config", "user.email", "e2e@gotham.test")
	git(t, fixture.dir, "config", "user.name", "Gotham E2E")
	git(t, fixture.dir, "config", "commit.gpgsign", "false")
	return fixture
}

// commit writes the marker page (and the Dockerfile) and commits it on main,
// so the next deployment clones exactly this revision.
func (f *p4Fixture) commit(t *testing.T, marker, dockerfile string) {
	t.Helper()
	f.marker = marker
	files := map[string]string{
		"Dockerfile": dockerfile,
		"index.html": "<!doctype html><html><body>" + marker + "</body></html>\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	git(t, f.dir, "add", "-A")
	git(t, f.dir, "commit", "--no-gpg-sign", "-m", marker)
}

// requireGit fails the opted-in test when no git binary is available: the
// control plane clones with the git binary, so the suite cannot run without
// it. It is only reached behind the requireE2E gate, where a missing tool is
// an infrastructure failure, never a green skip (CI runners ship git).
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("GOTHAM_E2E=1 requires git: %v", err)
	}
}

// git runs one git command inside dir and fails the test when it errors.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}

// waitForHTTPBody polls url until it answers 200 with a body containing want.
// The container reports healthy before its server necessarily accepts
// connections, so the first attempt is allowed to fail.
func waitForHTTPBody(t *testing.T, url, want string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	last := "no response"
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err != nil {
			last = err.Error()
		} else {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil {
				last = readErr.Error()
			} else if response.StatusCode != http.StatusOK {
				last = "status " + response.Status + ": " + string(body)
			} else if strings.Contains(string(body), want) {
				return
			} else {
				last = "body without marker: " + string(body)
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("%s never served %q; last: %s", url, want, last)
}
