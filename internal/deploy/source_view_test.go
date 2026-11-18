package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func TestApplicationSourceDockerfileMatchesDeployedFile(t *testing.T) {
	app := Application{
		ID:                uuid.New(),
		SourceType:        SourceDockerfile,
		DockerfileContent: "FROM alpine:3.20\nRUN echo hello\n",
	}
	dir := t.TempDir()
	if err := writeDockerfileContext(dir, app.DockerfileContent); err != nil {
		t.Fatalf("write Dockerfile context: %v", err)
	}
	written, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}

	s := &Service{Orchestrator: newTestOrchestrator(Config{Repository: &fakeRepository{app: app}})}
	source, err := s.applicationSource(context.Background(), app)
	if err != nil {
		t.Fatalf("applicationSource = %v, want nil", err)
	}
	if source.Kind != SourceDockerfile {
		t.Errorf("kind = %q, want %q", source.Kind, SourceDockerfile)
	}
	if source.Raw != app.DockerfileContent {
		t.Errorf("raw = %q, want the stored content", source.Raw)
	}
	if source.Deployable != string(written) {
		t.Errorf("deployable = %q, want the deployed file %q", source.Deployable, written)
	}
}

func TestApplicationSourceComposeMatchesDeployedDocument(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	app.ComposeContent = `services:
  web:
    image: example.com/app:1.0
    environment:
      DATABASE_URL: postgres://app:${POSTGRES_PASSWORD}@db:5432/app
`
	repo := &fakeRepository{app: app}
	repo.envVars = []EnvVar{{Key: "APP_URL", Value: "https://app.example.com"}}
	sealed, err := providers.SealSecret(testSecretKey, "db-password-value")
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}
	repo.secrets = []Secret{{Key: "POSTGRES_PASSWORD", Ciphertext: sealed}}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

	node := newMockNode()
	node.composeContainers = []ComposeContainer{{ID: "web-id", Service: "web", State: "running", Status: "Up 1 second"}}
	node.listed = []*agentv1.ContainerInfo{{Id: "web-id", State: "running", Status: "Up 1 second"}}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})
	o.run(context.Background(), job{app: app, dep: dep})
	if len(node.composeDocuments) != 1 {
		t.Fatalf("compose documents = %d, want 1", len(node.composeDocuments))
	}
	deployed := node.composeDocuments[0]

	s := &Service{Orchestrator: o}
	source, err := s.applicationSource(context.Background(), app)
	if err != nil {
		t.Fatalf("applicationSource = %v, want nil", err)
	}
	if source.Kind != SourceCompose {
		t.Errorf("kind = %q, want %q", source.Kind, SourceCompose)
	}
	if source.Raw != app.ComposeContent {
		t.Errorf("raw = %q, want the stored content", source.Raw)
	}
	want := strings.ReplaceAll(deployed, "db-password-value", "<redacted>")
	if source.Deployable != want {
		t.Errorf("deployable =\n%s\nwant the deployed document with the secret masked:\n%s", source.Deployable, want)
	}
	if strings.Contains(source.Deployable, "db-password-value") {
		t.Errorf("deployable holds the secret value:\n%s", source.Deployable)
	}
	if !strings.Contains(source.Deployable, "APP_URL") {
		t.Errorf("deployable is missing the injected environment:\n%s", source.Deployable)
	}
}

func TestApplicationSourceEmptyForImageAndBuildPack(t *testing.T) {
	cases := []struct {
		name string
		app  Application
	}{
		{"image", Application{ID: uuid.New(), SourceType: SourceImage, ImageRef: "nginx:1.27"}},
		{"buildpack", Application{ID: uuid.New(), SourceType: SourceGitPublic, BuildPack: "railpack"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{Orchestrator: newTestOrchestrator(Config{Repository: &fakeRepository{}})}
			source, err := s.applicationSource(context.Background(), tc.app)
			if err != nil {
				t.Fatalf("applicationSource = %v, want nil", err)
			}
			if source.Kind != "" || source.Raw != "" || source.Deployable != "" {
				t.Errorf("source = %+v, want an empty view", source)
			}
		})
	}
}
