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

const p4BaseImage = "busybox:1.36"

const p4ContainerPort = 8080

const p4Dockerfile = `FROM ` + p4BaseImage + `
COPY index.html /www/index.html
EXPOSE 8080
CMD ["httpd", "-f", "-p", "8080", "-h", "/www"]
`

const p4BrokenDockerfile = `FROM ` + p4BaseImage + `
RUN echo P4-BUILD-BOOM && exit 1
`

type p4Fixture struct {
	dir    string
	repo   string
	marker string
}

func newP4Fixture(t *testing.T, name, marker string) *p4Fixture {
	t.Helper()
	fixture := initFixture(t, name)
	fixture.commit(t, marker, p4Dockerfile)
	return fixture
}

func newBrokenP4Fixture(t *testing.T, name, marker string) *p4Fixture {
	t.Helper()
	fixture := initFixture(t, name)
	fixture.commit(t, marker, p4BrokenDockerfile)
	return fixture
}

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

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("GOTHAM_E2E=1 requires git: %v", err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}

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
