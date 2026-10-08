package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// composeAppID is the fixed application id the scope tests confine binds to:
// <managed root>/<appID> under the default root.
var composeAppID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

const composeSafe = `services:
  web:
    image: example.com/app:1.0
    environment:
      FOO: bar
  worker:
    image: example.com/worker:1.0
`

// TestValidateComposeContent checks the GS-8 creation gate: pasted text is
// required, capped at 256 KiB of valid YAML, and must pass the confinement
// allowlist. The full bypass table (30+ hostile payloads with offending
// paths) lives with the validator in composeguard; this pins the thin
// gate: text rules, size, and representative allowlist verdicts.
func TestValidateComposeContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr error
	}{
		{"minimal", "services:\n  web:\n    image: example.com/app:1.0\n", nil},
		{"multi-service", composeSafe, nil},
		{"interpolation refs", "services:\n  web:\n    image: example.com/${APP}:${TAG:-latest}\n", nil},
		{"list environment", "services:\n  web:\n    image: example.com/app:1.0\n    environment:\n      - FOO=bar\n", nil},
		{"empty", "", ErrValidation},
		{"blank", "  \n # only a comment\n", ErrValidation},
		{"nul byte", "services:\n  web:\n    image: a\x00b\n", ErrValidation},
		{"invalid utf-8", "services:\n  web:\n    image: \xff\xfe\n", ErrValidation},
		{"oversize", "services:\n  web:\n    image: x\n" + strings.Repeat("#", MaxComposeBytes), ErrValidation},
		{"not yaml", "services: [unclosed\n", ErrValidation},
		{"no services", "version: \"3\"\nvolumes:\n  data: {}\n", ErrValidation},
		{"empty services", "services: {}\n", ErrValidation},
		{"build context", "services:\n  web:\n    build: .\n    image: example.com/app:1.0\n", ErrValidation},
		{"no image", "services:\n  web:\n    command: sleep infinity\n", ErrValidation},
		{"privileged", "services:\n  web:\n    image: x\n    privileged: true\n", ErrValidation},
		{"cap add rejected", "services:\n  web:\n    image: x\n    cap_add:\n      - NET_BIND_SERVICE\n", ErrValidation},
		{"gotham label rejected", "services:\n  web:\n    image: x\n    labels:\n      gotham.domain: example.com\n", ErrValidation},
		{"secrets file", "services:\n  web:\n    image: x\n    secrets:\n      - s\nsecrets:\n  s:\n    file: /etc/hostname\n", ErrValidation},
		{"multi document", "services:\n  web:\n    image: x\n---\nservices:\n  evil:\n    image: y\n", ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateComposeContent(tc.content, composeAppID)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateComposeContent = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateComposeContent = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestValidateComposeFilePath checks the in-repo reference: short, relative
// and confined to the checkout.
func TestValidateComposeFilePath(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		wantErr error
	}{
		{"plain", "docker-compose.yml", nil},
		{"nested", "deploy/docker-compose.yaml", nil},
		{"padded", "  docker-compose.yml  ", nil},
		{"empty", "", ErrValidation},
		{"absolute", "/etc/docker-compose.yml", ErrValidation},
		{"parent", "../docker-compose.yml", ErrValidation},
		{"nested parent", "a/../../docker-compose.yml", ErrValidation},
		{"dot", ".", ErrValidation},
		{"nul", "docker-compose\x00.yml", ErrValidation},
		{"oversize", strings.Repeat("a", MaxComposeFilePathBytes+1), ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateComposeFilePath(tc.path)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateComposeFilePath = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateComposeFilePath = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestReadComposeFileScope checks the checkout read: the file must resolve
// inside the directory, fit the size limit and not escape through symlinks.
func TestReadComposeFileScope(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "deploy"), 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deploy", "compose.yml"), []byte(composeSafe), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	content, err := readComposeFile(dir, "deploy/compose.yml")
	if err != nil {
		t.Fatalf("read = %v, want nil", err)
	}
	if content != composeSafe {
		t.Errorf("content = %q, want the stored file", content)
	}
	if _, err := readComposeFile(dir, "../escape.yml"); !errors.Is(err, ErrValidation) {
		t.Errorf("traversal = %v, want ErrValidation", err)
	}
	if _, err := readComposeFile(dir, "missing.yml"); !errors.Is(err, ErrValidation) {
		t.Errorf("missing = %v, want ErrValidation", err)
	}
	t.Run("symlink escapes", func(t *testing.T) {
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "evil.yml"), []byte(composeSafe), 0o644); err != nil {
			t.Fatalf("seed outside: %v", err)
		}
		if err := os.Symlink(filepath.Join(outside, "evil.yml"), filepath.Join(dir, "linked.yml")); err != nil {
			t.Fatalf("seed symlink: %v", err)
		}
		if _, err := readComposeFile(dir, "linked.yml"); !errors.Is(err, ErrValidation) {
			t.Errorf("symlink = %v, want ErrValidation", err)
		}
	})
}

// TestValidateComposeService checks the web service gate: structurally a
// service name, and a member of the parsed list that the error names.
func TestValidateComposeService(t *testing.T) {
	if err := ValidateComposeService(composeSafe, "web"); err != nil {
		t.Fatalf("member = %v, want nil", err)
	}
	if err := ValidateComposeService(composeSafe, "worker"); err != nil {
		t.Fatalf("member = %v, want nil", err)
	}
	if err := ValidateComposeService(composeSafe, "missing"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown = %v, want ErrValidation", err)
	} else if !strings.Contains(err.Error(), "web") || !strings.Contains(err.Error(), "worker") {
		t.Errorf("unknown error = %q, want the available services named", err)
	}
	if err := ValidateComposeService(composeSafe, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty = %v, want ErrValidation", err)
	}
	if err := ValidateComposeService(composeSafe, "-bad"); !errors.Is(err, ErrValidation) {
		t.Fatalf("shape = %v, want ErrValidation", err)
	}
	// Repo-backed applications pass no document: only the shape is checked,
	// membership waits for the deploy-time read.
	if err := ValidateComposeService("", "web"); err != nil {
		t.Fatalf("repo mode = %v, want nil", err)
	}
	if err := ValidateComposeService("", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("repo mode empty = %v, want ErrValidation", err)
	}
}

// TestComposeImages checks the pull list: sorted, unique, trimmed.
func TestComposeImages(t *testing.T) {
	images, err := ComposeImages(`services:
  b:
    image: example.com/b:2
  a:
    image: example.com/a:1
  again:
    image: example.com/a:1
`)
	if err != nil {
		t.Fatalf("ComposeImages = %v, want nil", err)
	}
	if len(images) != 2 || images[0] != "example.com/a:1" || images[1] != "example.com/b:2" {
		t.Errorf("images = %q, want the sorted unique references", images)
	}
}

// TestInjectComposeWebPorts checks the routing mapping: appended when
// absent, kept when the container port is already published, and refused
// without a container port or a web service.
func TestInjectComposeWebPorts(t *testing.T) {
	base := "services:\n  web:\n    image: example.com/app:1.0\n"

	t.Run("appends host mapping", func(t *testing.T) {
		out, err := InjectComposeWebPorts(base, "web", 3000, 8080)
		if err != nil {
			t.Fatalf("inject = %v, want nil", err)
		}
		if !strings.Contains(out, "8080:3000") {
			t.Errorf("document = %q, want the 8080:3000 mapping", out)
		}
	})

	t.Run("ephemeral host port", func(t *testing.T) {
		out, err := InjectComposeWebPorts(base, "web", 3000, 0)
		if err != nil {
			t.Fatalf("inject = %v, want nil", err)
		}
		if !strings.Contains(out, "3000") || strings.Contains(out, ":3000") {
			t.Errorf("document = %q, want a bare container port", out)
		}
	})

	t.Run("keeps existing mapping", func(t *testing.T) {
		withPorts := base + "    ports:\n      - \"9090:3000\"\n"
		out, err := InjectComposeWebPorts(withPorts, "web", 3000, 8080)
		if err != nil {
			t.Fatalf("inject = %v, want nil", err)
		}
		if strings.Contains(out, "8080:3000") {
			t.Errorf("document = %q, want the user mapping kept", out)
		}
	})

	t.Run("range covering the port counts", func(t *testing.T) {
		withRange := base + "    ports:\n      - \"8000-8010:8000-8010\"\n"
		out, err := InjectComposeWebPorts(withRange, "web", 8005, 8080)
		if err != nil {
			t.Fatalf("inject = %v, want nil", err)
		}
		if strings.Contains(out, "8080") {
			t.Errorf("document = %q, want no extra mapping inside the range", out)
		}
	})

	t.Run("range missing the port appends", func(t *testing.T) {
		withRange := base + "    ports:\n      - \"8000-8010:8000-8010\"\n"
		out, err := InjectComposeWebPorts(withRange, "web", 9000, 8080)
		if err != nil {
			t.Fatalf("inject = %v, want nil", err)
		}
		if !strings.Contains(out, "8080:9000") {
			t.Errorf("document = %q, want the injected mapping", out)
		}
	})

	t.Run("loopback-only mapping does not suppress", func(t *testing.T) {
		loopback := base + "    ports:\n      - \"127.0.0.1:9090:3000\"\n"
		out, err := InjectComposeWebPorts(loopback, "web", 3000, 8080)
		if err != nil {
			t.Fatalf("inject = %v, want nil", err)
		}
		if !strings.Contains(out, "8080:3000") {
			t.Errorf("document = %q, want a routable mapping next to the loopback one", out)
		}
	})

	t.Run("non-loopback host mapping counts", func(t *testing.T) {
		bound := base + "    ports:\n      - \"0.0.0.0:9090:3000\"\n"
		out, err := InjectComposeWebPorts(bound, "web", 3000, 8080)
		if err != nil {
			t.Fatalf("inject = %v, want nil", err)
		}
		if strings.Contains(out, "8080:3000") {
			t.Errorf("document = %q, want the user mapping kept", out)
		}
	})

	t.Run("ipv6 loopback does not suppress", func(t *testing.T) {
		loopback := base + "    ports:\n      - \"[::1]:9090:3000\"\n"
		out, err := InjectComposeWebPorts(loopback, "web", 3000, 8080)
		if err != nil {
			t.Fatalf("inject = %v, want nil", err)
		}
		if !strings.Contains(out, "8080:3000") {
			t.Errorf("document = %q, want a routable mapping next to the loopback one", out)
		}
	})

	t.Run("long loopback does not suppress", func(t *testing.T) {
		withPorts := base + "    ports:\n      - target: 3000\n        published: 9090\n        host_ip: 127.0.0.1\n"
		out, err := InjectComposeWebPorts(withPorts, "web", 3000, 8080)
		if err != nil {
			t.Fatalf("inject = %v, want nil", err)
		}
		if !strings.Contains(out, "8080") {
			t.Errorf("document = %q, want a routable mapping next to the loopback one", out)
		}
	})

	t.Run("long form counts", func(t *testing.T) {
		withPorts := base + "    ports:\n      - target: 3000\n        published: 9090\n"
		out, err := InjectComposeWebPorts(withPorts, "web", 3000, 8080)
		if err != nil {
			t.Fatalf("inject = %v, want nil", err)
		}
		if strings.Contains(out, "8080") {
			t.Errorf("document = %q, want no extra mapping", out)
		}
	})

	t.Run("other service untouched", func(t *testing.T) {
		doc := base + "  worker:\n    image: example.com/worker:1.0\n"
		out, err := InjectComposeWebPorts(doc, "web", 3000, 8080)
		if err != nil {
			t.Fatalf("inject = %v, want nil", err)
		}
		if strings.Contains(out, "worker:\n      ports") || strings.Count(out, "8080:3000") != 1 {
			t.Errorf("document = %q, want the mapping on web only", out)
		}
	})

	t.Run("unknown service", func(t *testing.T) {
		if _, err := InjectComposeWebPorts(base, "missing", 3000, 8080); !errors.Is(err, ErrValidation) {
			t.Fatalf("inject = %v, want ErrValidation", err)
		}
	})

	t.Run("ports mapping rejected", func(t *testing.T) {
		withMap := base + "    ports:\n      3000: 8080\n"
		if _, err := InjectComposeWebPorts(withMap, "web", 3000, 8080); !errors.Is(err, ErrValidation) {
			t.Fatalf("inject = %v, want ErrValidation", err)
		}
	})

	t.Run("no container port", func(t *testing.T) {
		if _, err := InjectComposeWebPorts(base, "web", 0, 0); !errors.Is(err, ErrValidation) {
			t.Fatalf("inject = %v, want ErrValidation", err)
		}
	})
}

// TestValidateComposeSourceTable checks the source-type gate: exactly one of
// pasted content or repository holds, the web service is required, and the
// repository follows its git flow's rules.
func TestValidateComposeSourceTable(t *testing.T) {
	pasted := func(mutate func(*Application)) Application {
		app := Application{
			ID:             composeAppID,
			SourceType:     SourceCompose,
			ComposeContent: composeSafe,
			ComposeService: "web",
			Port:           3000,
		}
		if mutate != nil {
			mutate(&app)
		}
		return app
	}
	cases := []struct {
		name    string
		app     Application
		wantErr error
	}{
		{"pasted", pasted(nil), nil},
		{"pasted no service", pasted(func(a *Application) { a.ComposeService = "" }), ErrValidation},
		{"pasted unknown service", pasted(func(a *Application) { a.ComposeService = "missing" }), ErrValidation},
		{"pasted dangerous", pasted(func(a *Application) {
			a.ComposeContent = "services:\n  web:\n    image: x\n    privileged: true\n"
			a.ComposeService = "web"
		}), ErrValidation},
		{"pasted with provider", pasted(func(a *Application) { a.Provider = "github" }), ErrValidation},
		{"pasted no port", pasted(func(a *Application) { a.Port = 0 }), ErrValidation},
		{"pasted privileged host port", pasted(func(a *Application) { a.HostPort = 80 }), ErrValidation},
		{"pasted host port 443", pasted(func(a *Application) { a.HostPort = 443 }), ErrValidation},
		{"pasted high host port", pasted(func(a *Application) { a.HostPort = 8080 }), nil},
		{"both modes", pasted(func(a *Application) { a.CloneURL = "https://example.com/r.git"; a.ComposeFile = "c.yml" }), ErrValidation},
		{"neither mode", pasted(func(a *Application) { a.ComposeContent = "" }), ErrValidation},
		{"repo public", Application{
			ID: composeAppID, SourceType: SourceCompose,
			Repo: "o/r", CloneURL: "https://example.com/o/r.git",
			ComposeFile: "docker-compose.yml", ComposeService: "web", Port: 3000,
		}, nil},
		{"repo github", Application{
			ID: composeAppID, SourceType: SourceCompose, Provider: "github",
			Repo: "o/r", CloneURL: "git@github.com:o/r.git",
			ComposeFile: "c.yml", ComposeService: "web", Port: 3000,
		}, nil},
		{"repo no file", Application{
			ID: composeAppID, SourceType: SourceCompose,
			Repo: "o/r", CloneURL: "https://example.com/o/r.git",
			ComposeService: "web", Port: 3000,
		}, ErrValidation},
		{"repo bad url", Application{
			ID: composeAppID, SourceType: SourceCompose,
			Repo: "o/r", CloneURL: "ftp://example.com/r.git",
			ComposeFile: "c.yml", ComposeService: "web", Port: 3000,
		}, ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateComposeSource(tc.app)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("validateComposeSource = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("validateComposeSource = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestSourceTypeImplementedCompose pins the GS-8 enrollment: compose deploys
// as the last enabled source type, alongside every other known type.
func TestSourceTypeImplementedCompose(t *testing.T) {
	for _, source := range []string{"", SourceGitPublic, SourceGitPrivate, SourceGitHubApp, SourceGitLabApp, SourceDockerfile, SourceImage, SourceCompose} {
		if !SourceTypeImplemented(source) {
			t.Errorf("SourceTypeImplemented(%q) = false, want true", source)
		}
	}
	for _, source := range []string{"bogus"} {
		if SourceTypeImplemented(source) {
			t.Errorf("SourceTypeImplemented(%q) = true, want false", source)
		}
	}
}

// writeFileSource is a Source fake that materializes configured files: the
// repo-backed compose tests seed the referenced compose file through it.
type writeFileSource struct {
	files map[string]string
	calls int
}

func (s *writeFileSource) Clone(_ context.Context, _ Application, dir string, _ func(string)) error {
	s.calls++
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, content := range s.files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// testComposeApp returns a pasted compose application fixture. The web
// service reads the API token from the project environment, which proves
// secrets are injected as the compose project env at render time.
func testComposeApp(userID uuid.UUID) Application {
	return Application{
		ID:            uuid.New(),
		UserID:        userID,
		ServerID:      uuid.New(),
		EnvironmentID: uuid.New(),
		Name:          "compose app",
		SourceType:    SourceCompose,
		ComposeContent: `services:
  web:
    image: example.com/app:1.0
    environment:
      TOKEN: ${API_TOKEN}
  worker:
    image: example.com/worker:1.0
`,
		ComposeService: "web",
		Port:           3000,
		HostPort:       8080,
	}
}

// TestOrchestratorComposePastedHappyPath pins the GS-8 deploy: no clone, the
// rendered document (with the injected port mapping) reaches up after one
// pull per image, and the web container is the recorded release.
func TestOrchestratorComposePastedHappyPath(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	repo := &fakeRepository{app: app}
	envVars, secrets := testEnv(t)
	repo.envVars, repo.secrets = envVars, secrets
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	src := &fakeSource{}
	node := newMockNode()
	node.composeContainers = []ComposeContainer{{ID: "web-container-id", Service: "web", State: "running", Status: "Up 5 seconds"}}
	node.listed = []*agentv1.ContainerInfo{{Id: "web-container-id", State: "running", Status: "Up 5 seconds"}}
	pub := &recordPublisher{}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     src,
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(pub),
	})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	want := []State{StateQueued, StateCloning, StateBuilding, StatePushing, StateStarting, StateRunning}
	if got := repo.persistedStates(); !statesEqual(got, want) {
		t.Errorf("states = %s, want %s", renderStates(got), renderStates(want))
	}
	if stored.State != StateRunning {
		t.Errorf("state = %s, want running", stored.State)
	}
	if stored.ContainerID != "web-container-id" {
		t.Errorf("container id = %q, want the web container", stored.ContainerID)
	}
	if src.calls != 0 {
		t.Errorf("clone calls = %d, want 0 for pasted content", src.calls)
	}
	if node.composeUpCalls != 1 {
		t.Fatalf("compose up calls = %d, want 1", node.composeUpCalls)
	}
	if len(node.composeDocuments) != 1 {
		t.Fatalf("compose documents = %d, want 1", len(node.composeDocuments))
	}
	document := node.composeDocuments[0]
	if !strings.Contains(document, "8080:3000") {
		t.Errorf("document lacks the injected 8080:3000 mapping:\n%s", document)
	}
	if !strings.Contains(document, "TOKEN: hunter2") {
		t.Errorf("document lacks the injected secret project env:\n%s", document)
	}
	if node.pullCalls != 2 {
		t.Errorf("pull calls = %d, want one per image", node.pullCalls)
	}
	if node.runCalls != 0 {
		t.Errorf("run calls = %d, want 0 for a compose deploy", node.runCalls)
	}
	if node.buildCalls != 0 {
		t.Errorf("build calls = %d, want 0 for a compose deploy", node.buildCalls)
	}
}

// TestOrchestratorComposeRepoMode pins the repo-backed fetch: the checkout
// is cloned, the referenced file is read and deployed, and the web service
// membership is enforced against the file.
func TestOrchestratorComposeRepoMode(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	app.ComposeContent = ""
	app.Provider = ""
	app.Repo = "o/r"
	app.CloneURL = "https://example.com/o/r.git"
	app.Branch = "main"
	app.ComposeFile = "deploy/compose.yml"
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	src := &writeFileSource{files: map[string]string{
		"deploy/compose.yml": "services:\n  web:\n    image: example.com/app:1.0\n",
	}}
	node := newMockNode()
	node.composeContainers = []ComposeContainer{{ID: "web-id", Service: "web", State: "running", Status: "Up 1 second"}}
	node.listed = []*agentv1.ContainerInfo{{Id: "web-id", State: "running", Status: "Up 1 second"}}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     src,
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	if stored.State != StateRunning {
		t.Fatalf("state = %s (%q), want running", stored.State, stored.Error)
	}
	if src.calls != 1 {
		t.Errorf("clone calls = %d, want 1", src.calls)
	}
	if node.composeUpCalls != 1 {
		t.Errorf("compose up calls = %d, want 1", node.composeUpCalls)
	}

	t.Run("unknown service fails", func(t *testing.T) {
		bad := app
		bad.ComposeService = "missing"
		badRepo := &fakeRepository{app: bad}
		badDep := seedDeployment(t, badRepo, bad, Deployment{Kind: KindDeploy})
		badOrchestrator := newTestOrchestrator(Config{
			Repository: badRepo,
			Source: &writeFileSource{files: map[string]string{
				"deploy/compose.yml": "services:\n  web:\n    image: example.com/app:1.0\n",
			}},
			Dial:    dialAlways(newMockNode()),
			Emitter: NewEmitter(&recordPublisher{}),
		})
		badOrchestrator.run(context.Background(), job{app: bad, dep: badDep})
		failed, ok := badRepo.deployment(badDep.ID)
		if !ok {
			t.Fatal("deployment row is gone")
		}
		if failed.State != StateFailed {
			t.Errorf("state = %s, want failed", failed.State)
		}
		if !strings.Contains(failed.Error, "missing") {
			t.Errorf("error = %q, want the unknown service named", failed.Error)
		}
	})
}

// TestOrchestratorComposeRollbackReapplies pins the GS-8 rollback: the
// run ups the target release's stored document, not the live row. Deploy
// v1, edit the application to v2, roll back to the v1 deployment: the v1
// document is applied.
func TestOrchestratorComposeRollbackReapplies(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	repo := &fakeRepository{app: app}
	envVars, secrets := testEnv(t)
	repo.envVars, repo.secrets = envVars, secrets
	first := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.composeContainers = []ComposeContainer{{ID: "web-id", Service: "web", State: "running", Status: "Up 1 second"}}
	node.listed = []*agentv1.ContainerInfo{{Id: "web-id", State: "running", Status: "Up 1 second"}}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})
	o.run(context.Background(), job{app: app, dep: first})
	v1, ok := repo.deployment(first.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	if v1.State != StateRunning {
		t.Fatalf("setup deploy state = %s, want running", v1.State)
	}
	// The stored release carries the raw document: the secret reference,
	// never the value.
	if !strings.Contains(v1.ComposeDocument, "${API_TOKEN}") {
		t.Fatalf("v1 recorded no raw document: %q", v1.ComposeDocument)
	}
	if strings.Contains(v1.ComposeDocument, "hunter2") {
		t.Fatalf("v1 stored a secret value: %q", v1.ComposeDocument)
	}

	edited := app
	edited.ComposeContent = "services:\n  web:\n    image: example.com/app:2.0\n"
	rollback := seedDeployment(t, repo, edited, Deployment{
		Kind:            KindRollback,
		RollbackFrom:    first.ID,
		ComposeDocument: v1.ComposeDocument,
		ComposeCommit:   v1.ComposeCommit,
	})
	o.run(context.Background(), job{app: edited, dep: rollback})

	stored, ok := repo.deployment(rollback.ID)
	if !ok {
		t.Fatal("rollback row is gone")
	}
	if stored.State != StateRunning {
		t.Fatalf("rollback state = %s (%q), want running", stored.State, stored.Error)
	}
	if len(node.composeDocuments) != 2 {
		t.Fatalf("compose documents = %d, want the deploy plus the rollback", len(node.composeDocuments))
	}
	if !strings.Contains(node.composeDocuments[1], "example.com/app:1.0") {
		t.Errorf("rollback applied the live file instead of the v1 document:\n%s", node.composeDocuments[1])
	}
	if strings.Contains(node.composeDocuments[1], "example.com/app:2.0") {
		t.Errorf("rollback applied the v2 edit:\n%s", node.composeDocuments[1])
	}
}

// TestOrchestratorComposeRollbackWithoutDocument pins the fail-closed edge:
// a rollback whose target recorded no document fails instead of ups-ing an
// empty project.
func TestOrchestratorComposeRollbackWithoutDocument(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	repo := &fakeRepository{app: app}
	rollback := seedDeployment(t, repo, app, Deployment{Kind: KindRollback})
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(newMockNode()),
		Emitter:    NewEmitter(&recordPublisher{}),
	})
	o.run(context.Background(), job{app: app, dep: rollback})
	stored, ok := repo.deployment(rollback.ID)
	if !ok {
		t.Fatal("rollback row is gone")
	}
	if stored.State != StateFailed {
		t.Errorf("state = %s, want failed", stored.State)
	}
}

// TestCloneSourceCompose pins the direct entry: a pasted compose app
// resolves without cloning, while an empty one fails validation.
func TestCloneSourceCompose(t *testing.T) {
	t.Run("pasted resolves without cloning", func(t *testing.T) {
		src := &fakeSource{}
		o := newTestOrchestrator(Config{Source: src})
		app := testComposeApp(uuid.New())
		if err := o.cloneSource(context.Background(), app, t.TempDir(), nil); err != nil {
			t.Fatalf("cloneSource = %v, want nil", err)
		}
		if src.calls != 0 {
			t.Errorf("clone calls = %d, want 0", src.calls)
		}
	})

	t.Run("empty fails validation", func(t *testing.T) {
		o := newTestOrchestrator(Config{Source: &fakeSource{}})
		app := testComposeApp(uuid.New())
		app.ComposeContent = ""
		if err := o.cloneSource(context.Background(), app, t.TempDir(), nil); !errors.Is(err, ErrValidation) {
			t.Fatalf("cloneSource = %v, want ErrValidation", err)
		}
	})
}

// TestValidateComposeTarget pins the submit gate: a compose application
// needs its routed service, a container port and one of the two sources.
func TestValidateComposeTarget(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Application)
		wantErr error
	}{
		{"pasted", func(a *Application) {}, nil},
		{"no service", func(a *Application) { a.ComposeService = "" }, ErrValidation},
		{"no port", func(a *Application) { a.Port = 0 }, ErrValidation},
		{"privileged host port", func(a *Application) { a.HostPort = 22 }, ErrValidation},
		{"no source", func(a *Application) { a.ComposeContent = "" }, ErrValidation},
		{"repo mode", func(a *Application) {
			a.ComposeContent = ""
			a.CloneURL = "https://example.com/o/r.git"
			a.ComposeFile = "docker-compose.yml"
		}, nil},
		{"repo mode no file", func(a *Application) {
			a.ComposeContent = ""
			a.CloneURL = "https://example.com/o/r.git"
		}, ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()
			app := testComposeApp(userID)
			tc.mutate(&app)
			err := validateDeployTarget(app)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("validateDeployTarget = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("validateDeployTarget = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestRoutesComposeFields posts the compose payload through HTTP: create
// echoes the stored document on the detail shape, the list omits it, and an
// update replaces it.
func TestRoutesComposeFields(t *testing.T) {
	userID := uuid.New()
	svc := newTestService(t, &fakeRepository{})
	srv := newRouteServer(svc, alwaysUser(userID))

	post := func(payload map[string]any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode payload: %v", err)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, applicationsPath, strings.NewReader(string(encoded))))
		return rec
	}
	base := map[string]any{
		"name":            "compose app",
		"environment_id":  uuid.New().String(),
		"provider":        "",
		"repo":            "",
		"clone_url":       "",
		"source_type":     SourceCompose,
		"compose_content": composeSafe,
		"compose_service": "web",
		"branch":          "",
		"build_pack":      "",
		"port":            3000,
		"host_port":       8080,
		"server_id":       uuid.New().String(),
	}

	t.Run("create echoes content", func(t *testing.T) {
		rec := post(base)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
		}
		assertJSONKeys(t, mustJSON(t, rec.Body.Bytes(), "application"), applicationWireKeys...)
		var envelope applicationEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if envelope.Application.ComposeContent != composeSafe {
			t.Errorf("content not echoed on create")
		}
		if envelope.Application.ComposeService != "web" {
			t.Errorf("service = %q, want web", envelope.Application.ComposeService)
		}
	})

	t.Run("dangerous content is a 400", func(t *testing.T) {
		bad := map[string]any{}
		for key, value := range base {
			bad[key] = value
		}
		bad["name"] = "bad compose"
		bad["compose_content"] = "services:\n  web:\n    image: x\n    privileged: true\n"
		if rec := post(bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
		}
	})

	createdID := func() string {
		rec := post(map[string]any{
			"name": "listed compose", "environment_id": base["environment_id"],
			"provider": "", "repo": "", "clone_url": "", "source_type": SourceCompose,
			"compose_content": composeSafe, "compose_service": "web",
			"port": 3000, "server_id": base["server_id"],
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("setup create status = %d (body %s)", rec.Code, rec.Body.String())
		}
		var envelope applicationEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return envelope.Application.ID
	}()

	t.Run("list omits content", func(t *testing.T) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, applicationsPath, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var body applicationListEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(body.Applications) == 0 {
			t.Fatal("no applications listed")
		}
		for _, item := range body.Applications {
			if item.ID == createdID && item.ComposeService != "web" {
				t.Errorf("list service = %q, want web", item.ComposeService)
			}
		}
		raw := mustJSON(t, rec.Body.Bytes(), "applications", "0")
		assertJSONKeys(t, raw, applicationListWireKeys...)
	})

	t.Run("update replaces content", func(t *testing.T) {
		update := map[string]any{
			"compose_content": "services:\n  web:\n    image: example.com/app:9.0\n",
			"compose_service": "web",
		}
		encoded, err := json.Marshal(update)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, applicationsPath+"/"+createdID, strings.NewReader(string(encoded))))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var envelope applicationEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !strings.Contains(envelope.Application.ComposeContent, "app:9.0") {
			t.Errorf("content not replaced: %q", envelope.Application.ComposeContent)
		}
	})
}

// TestServiceCreateCompose pins the write path: pasted content stores with
// the repository cleared, the branch empty and no build pack, while a
// dangerous document is a 400 that stores nothing.
func TestServiceCreateCompose(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	svc := newTestService(t, repo)
	serverID := uuid.New()

	in := validCreateInput(serverID)
	in.Provider = ""
	in.Repo = ""
	in.CloneURL = ""
	in.SourceType = SourceCompose
	in.ComposeContent = composeSafe
	in.ComposeService = "web"
	in.Branch = "main"
	in.BuildPack = "dockerfile"

	created, err := svc.CreateApplication(context.Background(), userID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ComposeContent != composeSafe {
		t.Errorf("content not stored")
	}
	if created.ComposeService != "web" {
		t.Errorf("service = %q, want web", created.ComposeService)
	}
	if created.Repo != "" || created.CloneURL != "" {
		t.Errorf("repo = %q clone = %q, want cleared", created.Repo, created.CloneURL)
	}
	if created.Branch != "" {
		t.Errorf("branch = %q, want empty for pasted compose", created.Branch)
	}
	if created.BuildPack != "" {
		t.Errorf("build pack = %q, want empty for compose", created.BuildPack)
	}

	t.Run("dangerous document rejected", func(t *testing.T) {
		bad := in
		bad.Name = "bad app"
		bad.ComposeContent = "services:\n  web:\n    image: x\n    privileged: true\n"
		if _, err := svc.CreateApplication(context.Background(), userID, bad); !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})

	t.Run("nul env value rejected", func(t *testing.T) {
		bad := in
		bad.Name = "nul env"
		bad.Env = []EnvEntry{{Key: "EVIL", Value: "a\x00b"}}
		if _, err := svc.CreateApplication(context.Background(), userID, bad); !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		bad.Name = "nul escape env"
		bad.Env = []EnvEntry{{Key: "EVIL", Value: "a\u0000b"}}
		if _, err := svc.CreateApplication(context.Background(), userID, bad); !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})

	t.Run("update switches modes", func(t *testing.T) {
		content := "services:\n  web:\n    image: example.com/app:9.0\n"
		updated, err := svc.UpdateApplication(context.Background(), userID, created.ID, UpdateApplicationInput{
			ComposeContent: &content,
		})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if updated.ComposeContent != content {
			t.Errorf("content not replaced")
		}
	})
}

// TestServiceRollbackCompose pins the rollback queue boundary: a running
// compose release with a stored document rolls back to that document, while
// nothing released — or a release that recorded no document — is a 400.
func TestServiceRollbackCompose(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	if _, err := svc.Rollback(context.Background(), userID, app.ID, uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty history err = %v, want ErrValidation", err)
	}
	bare := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning})
	if _, err := svc.Rollback(context.Background(), userID, app.ID, bare.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("documentless release err = %v, want ErrValidation", err)
	}
	recorded := seedDeployment(t, repo, app, Deployment{
		Kind: KindDeploy, State: StateRunning, ComposeDocument: "services:\n  web:\n    image: x\n",
	})
	rollback, err := svc.Rollback(context.Background(), userID, app.ID, recorded.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rollback.Kind != KindRollback || rollback.State != StateQueued {
		t.Errorf("rollback = %+v, want a queued rollback", rollback)
	}
	if rollback.ImageTag != "" || rollback.RegistryImage != "" {
		t.Errorf("rollback carries image fields %q/%q, want none for compose", rollback.ImageTag, rollback.RegistryImage)
	}
	if rollback.ComposeDocument != "services:\n  web:\n    image: x\n" {
		t.Errorf("rollback carries %q, want the target release's document", rollback.ComposeDocument)
	}
}

// TestComposeGuardErrorsRedacted pins N4: a guard rejection on the
// rendered document quotes the substituted secret value, so the message
// must be redacted before it reaches the deployment row. The bind is
// valid while the reference is unresolved and escapes the managed
// directory once the secret (which contains a slash) substitutes in.
func TestComposeGuardErrorsRedacted(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	app.ComposeContent = "services:\n  web:\n    image: example.com/app:1.0\n    volumes:\n      - /var/lib/gotham/volumes/" +
		app.ID.String() + "/${API_TOKEN}:/d\n"
	repo := &fakeRepository{app: app}
	sealed, err := providers.SealSecret(testSecretKey, "ab/sekret-value")
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}
	repo.envVars = []EnvVar{{Key: "FOO", Value: "bar"}}
	repo.secrets = []Secret{{Key: "API_TOKEN", Ciphertext: sealed}}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})
	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	if stored.State != StateFailed {
		t.Fatalf("state = %s, want failed", stored.State)
	}
	if strings.Contains(stored.Error, "sekret-value") {
		t.Errorf("error leaks the secret: %q", stored.Error)
	}
	if !strings.Contains(stored.Error, "<redacted>") {
		t.Errorf("error = %q, want the redaction marker", stored.Error)
	}
	if node.composeUpCalls != 0 {
		t.Errorf("compose up calls = %d, want 0 (rejected before the node)", node.composeUpCalls)
	}
}

// TestServiceDeleteComposeDown pins the teardown: deleting a compose
// application downs its project through the agent, best effort.
func TestServiceDeleteComposeDown(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	repo := &fakeRepository{app: app}
	node := newMockNode()
	svc := newNodeService(t, repo, node)

	if err := svc.DeleteApplication(context.Background(), userID, app.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if node.composeDownCalls != 1 {
		t.Errorf("compose down calls = %d, want 1", node.composeDownCalls)
	}
}

// TestServiceDeleteComposeProbesUnverifiedNode pins the teardown gate: an
// old agent that does not report enforcement still gets its project torn
// down (teardown stays best effort), and the probe ran first.
func TestServiceDeleteComposeProbesUnverifiedNode(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	repo := &fakeRepository{app: app}
	node := newMockNode()
	node.composeOldAgent = true
	svc := newNodeService(t, repo, node)

	if err := svc.DeleteApplication(context.Background(), userID, app.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if node.composeValidateCalls == 0 {
		t.Error("capability probes = 0, want the teardown probe to run")
	}
	if node.composeDownCalls != 1 {
		t.Errorf("compose down calls = %d, want 1 (teardown proceeds unverified)", node.composeDownCalls)
	}
}

// TestComposeNodeErrorsRedacted pins F3: a node error that echoes a project
// secret value (the compose CLI quotes substituted values) must not reach
// the deployment row. The seeded secret hunter2 travels only into the
// rendered document; the stored error carries the redaction marker.
func TestComposeNodeErrorsRedacted(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	repo := &fakeRepository{app: app}
	envVars, secrets := testEnv(t)
	repo.envVars, repo.secrets = envVars, secrets
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.composeUpErr = errors.New("invalid hostPort: hunter2")
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})
	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	if stored.State != StateFailed {
		t.Fatalf("state = %s, want failed", stored.State)
	}
	if strings.Contains(stored.Error, "hunter2") {
		t.Errorf("error leaks the secret: %q", stored.Error)
	}
	if !strings.Contains(stored.Error, "<redacted>") {
		t.Errorf("error = %q, want the redaction marker", stored.Error)
	}
}

// TestServiceCreateComposeHostile pins the write gate against the review's
// exploit repros: driver_opts binds, secrets file mounts, CAP_-prefixed
// capabilities and namespace joins are all 400s that store nothing.
func TestServiceCreateComposeHostile(t *testing.T) {
	hostiles := []struct {
		name    string
		content string
	}{
		{"driver opts bind", "services:\n  web:\n    image: x\n    volumes:\n      - data:/mnt\nvolumes:\n  data:\n    driver_opts:\n      type: none\n      o: bind\n      device: /\n"},
		{"secrets file", "services:\n  web:\n    image: x\n    secrets:\n      - s\nsecrets:\n  s:\n    file: /etc/hostname\n"},
		{"cap prefix", "services:\n  web:\n    image: x\n    cap_add:\n      - CAP_SYS_ADMIN\n"},
		{"parent bind", "services:\n  web:\n    image: x\n    volumes:\n      - ..:/mnt\n"},
		{"hidden bind", "services:\n  web:\n    image: x\n    volumes:\n      - .hidden:/mnt\n"},
		{"home bind", "services:\n  web:\n    image: x\n    volumes:\n      - ~:/mnt\n"},
		{"network container", "services:\n  web:\n    image: x\n    network_mode: container:other\n"},
		{"security opt", "services:\n  web:\n    image: x\n    security_opt:\n      - seccomp=unconfined\n"},
		{"volumes from", "services:\n  web:\n    image: x\n    volumes_from:\n      - container:other:rw\n"},
		{"external object", "services:\n  web:\n    image: x\n    volumes:\n      - shared:/data\nvolumes:\n  shared:\n    external:\n      name: gotham-app-other-data\n"},
		{"bare env inherits agent", "services:\n  web:\n    image: x\n    environment:\n      - INHERIT_ME\n"},
		{"gotham label", "services:\n  web:\n    image: x\n    labels:\n      gotham.app_id: 00000000-0000-0000-0000-000000000000\n"},
	}
	for _, tc := range hostiles {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()
			repo := &fakeRepository{}
			svc := newTestService(t, repo)
			in := validCreateInput(uuid.New())
			in.Provider = ""
			in.Repo = ""
			in.CloneURL = ""
			in.SourceType = SourceCompose
			in.ComposeContent = tc.content
			in.ComposeService = "web"
			in.Branch = ""
			in.BuildPack = ""
			in.Port = 3000
			if _, err := svc.CreateApplication(context.Background(), userID, in); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}

// TestPreviewComposeBinds pins F4: a preview of a compose application with
// managed binds is created with the binds rewritten to the preview's own
// directory, and deploying it ups the rewritten document.
func TestPreviewComposeBinds(t *testing.T) {
	userID := uuid.New()
	base := testComposeApp(userID)
	baseID := uuid.New()
	base.ID = baseID
	baseDir := "/var/lib/gotham/volumes/" + baseID.String()
	base.ComposeContent = "services:\n  web:\n    image: example.com/app:1.0\n    volumes:\n      - " + baseDir + "/data:/data\n"
	repo := &fakeRepository{app: base}
	svc := newTestService(t, repo)

	preview, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name:       "preview-1",
		BaseDomain: "preview-1.example.com",
	})
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	if preview.ID == uuid.Nil || preview.ID == base.ID {
		t.Fatalf("preview id = %s, want a fresh id", preview.ID)
	}
	previewDir := "/var/lib/gotham/volumes/" + preview.ID.String()
	if !strings.Contains(preview.ComposeContent, previewDir+"/data") {
		t.Errorf("preview content lacks the preview bind:\n%s", preview.ComposeContent)
	}
	if strings.Contains(preview.ComposeContent, baseDir) {
		t.Errorf("preview content still mounts the base directory:\n%s", preview.ComposeContent)
	}

	// The preview deploys through its own project with the rewritten binds.
	previewRepo := &fakeRepository{app: preview}
	dep := seedDeployment(t, previewRepo, preview, Deployment{Kind: KindDeploy})
	node := newMockNode()
	node.composeContainers = []ComposeContainer{{ID: "preview-web", Service: "web", State: "running", Status: "Up 1 second"}}
	node.listed = []*agentv1.ContainerInfo{{Id: "preview-web", State: "running", Status: "Up 1 second"}}
	o := newTestOrchestrator(Config{
		Repository: previewRepo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})
	o.run(context.Background(), job{app: preview, dep: dep})
	stored, ok := previewRepo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	if stored.State != StateRunning {
		t.Fatalf("preview deploy state = %s (%q), want running", stored.State, stored.Error)
	}
	if len(node.composeDocuments) != 1 || !strings.Contains(node.composeDocuments[0], previewDir+"/data") {
		t.Errorf("preview up missed the rewritten bind: %v", node.composeDocuments)
	}
}

// TestComposeCapabilityProbe pins N2: the run refuses a node that accepts
// the canary (a pre-confinement agent) before any document reaches it, and
// proceeds on a node that refuses it with a validation error. The canary
// itself is valid compose the allowlist refuses.
func TestComposeCapabilityProbe(t *testing.T) {
	newRun := func(node *mockNode) Deployment {
		userID := uuid.New()
		app := testComposeApp(userID)
		repo := &fakeRepository{app: app}
		envVars, secrets := testEnv(t)
		repo.envVars, repo.secrets = envVars, secrets
		dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
		o := newTestOrchestrator(Config{
			Repository: repo,
			Source:     &fakeSource{},
			Dial:       dialAlways(node),
			Emitter:    NewEmitter(&recordPublisher{}),
		})
		o.run(context.Background(), job{app: app, dep: dep})
		stored, ok := repo.deployment(dep.ID)
		if !ok {
			t.Fatal("deployment row is gone")
		}
		return stored
	}

	t.Run("old agent fails closed", func(t *testing.T) {
		node := newMockNode()
		node.composeOldAgent = true
		stored := newRun(node)
		if stored.State != StateFailed {
			t.Fatalf("state = %s, want failed", stored.State)
		}
		if !strings.Contains(stored.Error, "upgrade the agent") {
			t.Errorf("error = %q, want the upgrade instruction", stored.Error)
		}
		if node.composeUpCalls != 0 {
			t.Errorf("compose up calls = %d, want 0 (no document reached the node)", node.composeUpCalls)
		}
	})

	t.Run("capable node proceeds", func(t *testing.T) {
		node := newMockNode()
		node.composeContainers = []ComposeContainer{{ID: "web-id", Service: "web", State: "running", Status: "Up 1 second"}}
		node.listed = []*agentv1.ContainerInfo{{Id: "web-id", State: "running", Status: "Up 1 second"}}
		stored := newRun(node)
		if stored.State != StateRunning {
			t.Fatalf("state = %s (%q), want running", stored.State, stored.Error)
		}
		if node.composeValidateCalls != 1 {
			t.Errorf("capability probes = %d, want 1", node.composeValidateCalls)
		}
	})
}

// TestComposeRollbackRedactsCurrentEnv pins L2: a rollback renders the
// stored raw document with the current environment, so a node error echoing
// the current secret value is redacted by the current env map.
func TestComposeRollbackRedactsCurrentEnv(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	repo := &fakeRepository{app: app}
	envVars, secrets := testEnv(t)
	repo.envVars, repo.secrets = envVars, secrets
	raw := app.ComposeContent
	first := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ComposeDocument: raw})

	node := newMockNode()
	node.composeUpErr = errors.New("invalid hostPort: hunter2")
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})
	rollback := seedDeployment(t, repo, app, Deployment{Kind: KindRollback, RollbackFrom: first.ID, ComposeDocument: raw})
	o.run(context.Background(), job{app: app, dep: rollback})

	stored, ok := repo.deployment(rollback.ID)
	if !ok {
		t.Fatal("rollback row is gone")
	}
	if stored.State != StateFailed {
		t.Fatalf("state = %s, want failed", stored.State)
	}
	if strings.Contains(stored.Error, "hunter2") {
		t.Errorf("rollback error leaks the secret: %q", stored.Error)
	}
	if !strings.Contains(stored.Error, "<redacted>") {
		t.Errorf("rollback error = %q, want the redaction marker", stored.Error)
	}
}

// TestComposeDefaultEscapeRejected pins N1 at the deploy layer: a default
// that resolves outside the managed directory (`${OPT:-..}` with OPT
// unset) passes the raw allowlist but fails the rendered re-validation,
// before anything reaches the node.
func TestComposeDefaultEscapeRejected(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	managed := "/var/lib/gotham/volumes/" + app.ID.String()
	app.ComposeContent = "services:\n  web:\n    image: example.com/app:1.0\n    volumes:\n      - " + managed + "/${OPT:-..}:/mnt\n"
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})
	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	if stored.State != StateFailed {
		t.Fatalf("state = %s, want failed", stored.State)
	}
	if !strings.Contains(stored.Error, "services.web.volumes") {
		t.Errorf("error = %q, want the offending path named", stored.Error)
	}
	if node.composeUpCalls != 0 {
		t.Errorf("compose up calls = %d, want 0 (rejected before the node)", node.composeUpCalls)
	}
}
