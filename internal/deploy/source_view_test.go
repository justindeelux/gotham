package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

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
      LOG_LEVEL: debug-literal
`
	sealed, err := providers.SealSecret(testSecretKey, "db-password-value")
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}
	repo := &fakeRepository{app: app}
	app = seedSharedApp(app, uuid.New(),
		[]SharedVariable{{Key: "SESSION_SECRET", Value: "project-session-value"}}, nil, repo)
	repo.app = app
	repo.envVars = []EnvVar{
		{Key: "APP_URL", Value: "https://app.example.com"},
		{Key: "ADMIN_PASSWORD", Value: "plain-admin-password"},
	}
	repo.secrets = []Secret{{Key: "POSTGRES_PASSWORD", Ciphertext: sealed}}

	node := newMockNode()
	node.composeContainers = []ComposeContainer{{ID: "web-id", Service: "web", State: "running", Status: "Up 1 second"}}
	node.listed = []*agentv1.ContainerInfo{{Id: "web-id", State: "running", Status: "Up 1 second"}}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &fakeSource{},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})
	o.run(context.Background(), job{app: app, dep: seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})})
	if len(node.composeDocuments) != 1 {
		t.Fatalf("compose documents = %d, want 1", len(node.composeDocuments))
	}
	deployed := node.composeDocuments[0]
	injected := []string{"db-password-value", "https://app.example.com", "plain-admin-password", "project-session-value"}
	for _, value := range injected {
		if !strings.Contains(deployed, value) {
			t.Fatalf("deployed document is missing %q:\n%s", value, deployed)
		}
	}

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
	for _, value := range injected {
		if strings.Contains(source.Deployable, value) {
			t.Errorf("deployable holds the injected value %q:\n%s", value, source.Deployable)
		}
	}
	for _, key := range []string{"APP_URL", "ADMIN_PASSWORD", "POSTGRES_PASSWORD", "SESSION_SECRET"} {
		if !strings.Contains(source.Deployable, key) {
			t.Errorf("deployable is missing the injected key %s:\n%s", key, source.Deployable)
		}
		if !strings.Contains(source.Deployable, key+": <redacted>") {
			t.Errorf("deployable does not show %s as masked:\n%s", key, source.Deployable)
		}
	}
	if strings.Count(source.Deployable, "<redacted>") < len(injected) {
		t.Errorf("deployable holds fewer masks than injected values:\n%s", source.Deployable)
	}
	if !strings.Contains(source.Deployable, "debug-literal") {
		t.Errorf("deployable masked the literal the user wrote:\n%s", source.Deployable)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(source.Deployable), &doc); err != nil {
		t.Errorf("deployable is not valid YAML: %v", err)
	}

	// The view must not touch the deploy path: a second deploy sends the same
	// bytes as the first.
	o.run(context.Background(), job{app: app, dep: seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})})
	if len(node.composeDocuments) != 2 {
		t.Fatalf("compose documents = %d, want 2", len(node.composeDocuments))
	}
	if node.composeDocuments[1] != deployed {
		t.Errorf("second deploy document =\n%s\nwant the first, unmasked:\n%s", node.composeDocuments[1], deployed)
	}
}

func TestApplicationSourceMasksReencodedSecrets(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	app.ComposeContent = `services:
  web:
    image: example.com/app:1.0
`
	pem := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7\n-----END PRIVATE KEY-----"
	quoted := "pa'ss$word"
	sealedPEM, err := providers.SealSecret(testSecretKey, pem)
	if err != nil {
		t.Fatalf("seal PEM secret: %v", err)
	}
	sealedQuoted, err := providers.SealSecret(testSecretKey, quoted)
	if err != nil {
		t.Fatalf("seal quoted secret: %v", err)
	}
	repo := &fakeRepository{app: app}
	repo.secrets = []Secret{
		{Key: "TLS_KEY", Ciphertext: sealedPEM},
		{Key: "QUOTED", Ciphertext: sealedQuoted},
	}
	s := &Service{Orchestrator: newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}})}
	source, err := s.applicationSource(context.Background(), app)
	if err != nil {
		t.Fatalf("applicationSource = %v, want nil", err)
	}
	deployable := source.Deployable
	if !strings.Contains(deployable, "<redacted>") {
		t.Fatalf("deployable holds no mask:\n%s", deployable)
	}
	if strings.Contains(deployable, pem) {
		t.Errorf("deployable holds the multi-line secret:\n%s", deployable)
	}
	for _, line := range strings.Split(pem, "\n") {
		if line != "" && strings.Contains(deployable, line) {
			t.Errorf("deployable holds a line of the multi-line secret %q:\n%s", line, deployable)
		}
	}
	if strings.Contains(deployable, quoted) {
		t.Errorf("deployable holds the quoted secret:\n%s", deployable)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(deployable), &doc); err != nil {
		t.Errorf("deployable is not valid YAML: %v", err)
	}
}

func TestApplicationSourceLeavesShortInjectedValues(t *testing.T) {
	userID := uuid.New()
	app := testComposeApp(userID)
	app.ComposeContent = `services:
  web:
    image: example.com/app:1.0
    environment:
      DEBUG: "true"
      REPLICAS: "1"
`
	repo := &fakeRepository{app: app}
	repo.envVars = []EnvVar{
		{Key: "EMPTY", Value: ""},
		{Key: "ONE_CHAR", Value: "x"},
		{Key: "FLAG", Value: "true"},
		{Key: "COUNT", Value: "1"},
	}
	s := &Service{Orchestrator: newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}})}
	source, err := s.applicationSource(context.Background(), app)
	if err != nil {
		t.Fatalf("applicationSource = %v, want nil", err)
	}
	if strings.Contains(source.Deployable, "<redacted>") {
		t.Errorf("deployable masked a short or boolean-like value:\n%s", source.Deployable)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(source.Deployable), &doc); err != nil {
		t.Fatalf("deployable is not valid YAML: %v", err)
	}
	for _, want := range []string{"example.com/app:1.0", "DEBUG", "REPLICAS", "FLAG", "COUNT"} {
		if !strings.Contains(source.Deployable, want) {
			t.Errorf("deployable is missing %q:\n%s", want, source.Deployable)
		}
	}
	if !strings.Contains(source.Deployable, "true") {
		t.Errorf("deployable lost the boolean it held:\n%s", source.Deployable)
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
