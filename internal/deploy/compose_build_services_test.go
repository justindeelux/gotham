package deploy

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/builds"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func readContextTarFiles(t *testing.T, data []byte) map[string]string {
	t.Helper()
	files := map[string]string{}
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatalf("read context tar: %v", err)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read context entry: %v", err)
		}
		files[header.Name] = string(content)
	}
}

const composeBuildServicesFile = `services:
  db:
    image: postgres:16-alpine
  web:
    build:
      context: .
      target: web
      args:
        NODE_ENV: production
    ports:
      - "3000:3000"
    depends_on:
      - db
`

func composeBuildServicesApp() Application {
	return Application{
		ID:             uuid.New(),
		UserID:         uuid.New(),
		ServerID:       uuid.New(),
		EnvironmentID:  uuid.New(),
		Name:           "built compose app",
		SourceType:     SourceGitPublic,
		Repo:           "acme/demo",
		CloneURL:       "https://github.com/acme/demo.git",
		Branch:         "main",
		BuildPack:      string(builds.EngineCompose),
		ComposeFile:    composeBuildPackFile,
		ComposeService: "web",
	}
}

func composeBuildServicesOrchestrator(t *testing.T, app Application, node *mockNode, files map[string]string) *Orchestrator {
	t.Helper()
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
	node.composeContainers = []ComposeContainer{{ID: "web-id", Service: "web", State: "running", Status: "Up 1 second"}}
	node.listed = []*agentv1.ContainerInfo{{Id: "web-id", State: "running", Status: "Up 1 second"}}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &writeFileSource{files: files},
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
	return o
}

func TestOrchestratorBuildsComposeServices(t *testing.T) {
	app := composeBuildServicesApp()
	node := newMockNode()
	composeBuildServicesOrchestrator(t, app, node, map[string]string{
		composeBuildPackFile:   composeBuildServicesFile,
		"deploy/Dockerfile":    "FROM node:22-slim AS web\n",
		"deploy/.dockerignore": "nothing\n",
	})

	if node.buildCalls != 1 {
		t.Fatalf("build calls = %d, want 1 (only the service that declares build)", node.buildCalls)
	}
	meta := node.metas[0]
	if meta.Target != "web" {
		t.Errorf("build target = %q, want web", meta.Target)
	}
	if meta.Tag != "gotham/"+app.ID.String()+":"+meta.DeployID+"-web" {
		t.Errorf("build tag = %q, want the per-service release tag", meta.Tag)
	}
	if meta.BuildArgs["NODE_ENV"] != "production" {
		t.Errorf("build args = %v, want NODE_ENV from the compose directive", meta.BuildArgs)
	}
	if len(node.contexts[0]) == 0 {
		t.Error("build context tar is empty")
	}
	if files := readContextTarFiles(t, node.contexts[0]); len(files) == 0 {
		t.Error("build context tar has no entries")
	}
	if node.composeUpCalls != 1 {
		t.Fatalf("compose up calls = %d, want 1", node.composeUpCalls)
	}
	document := node.composeDocuments[0]
	if strings.Contains(document, "build:") {
		t.Errorf("compose document still carries a build directive:\n%s", document)
	}
	if !strings.Contains(document, "image: gotham/"+app.ID.String()+":"+meta.DeployID+"-web") {
		t.Errorf("compose document does not run the built image:\n%s", document)
	}
	if !strings.Contains(document, "postgres:16-alpine") {
		t.Errorf("compose document lost the pulled image:\n%s", document)
	}
}

func TestOrchestratorComposeBuildContextIsRelativeToTheComposeFile(t *testing.T) {
	app := composeBuildServicesApp()
	app.ComposeFile = "stack/compose.yaml"
	node := newMockNode()
	composeBuildServicesOrchestrator(t, app, node, map[string]string{
		"stack/compose.yaml":   "services:\n  web:\n    build:\n      context: app\n      target: web\n    ports:\n      - \"8000:8000\"\n",
		"stack/app/Dockerfile": "FROM python:3.12 AS web\n",
		"stack/app/main.py":    "print('hi')\n",
		"elsewhere/Dockerfile": "FROM scratch\n",
	})

	if node.buildCalls != 1 {
		t.Fatalf("build calls = %d, want 1", node.buildCalls)
	}
	files := readContextTarFiles(t, node.contexts[0])
	if _, ok := files["main.py"]; !ok {
		t.Error("context is missing a file from the compose file's directory")
	}
	if _, ok := files["Dockerfile"]; !ok {
		t.Error("context is missing the Dockerfile")
	}
}

func TestOrchestratorComposeBuildRollbackReusesImages(t *testing.T) {
	app := composeBuildServicesApp()
	first := seedDeployment(t, &fakeRepository{app: app}, app, Deployment{
		Kind:            KindDeploy,
		State:           StateRunning,
		ComposeDocument: composeBuildServicesFile,
		ComposeCommit:   "v1",
	})
	repo := &fakeRepository{app: app}
	rollback := seedDeployment(t, repo, app, Deployment{
		Kind:            KindRollback,
		RollbackFrom:    first.ID,
		ComposeDocument: composeBuildServicesFile,
		ComposeCommit:   first.ComposeCommit,
	})
	node := newMockNode()
	node.composeContainers = []ComposeContainer{{ID: "web-id", Service: "web", State: "running", Status: "Up 1 second"}}
	node.listed = []*agentv1.ContainerInfo{{Id: "web-id", State: "running", Status: "Up 1 second"}}
	o := newTestOrchestrator(Config{
		Repository: repo,
		Source:     &writeFileSource{files: map[string]string{}},
		Dial:       dialAlways(node),
		Emitter:    NewEmitter(&recordPublisher{}),
	})

	o.run(context.Background(), job{app: app, dep: rollback})

	stored, ok := repo.deployment(rollback.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	if stored.State != StateRunning {
		t.Fatalf("state = %s (%q), want running", stored.State, stored.Error)
	}
	if node.buildCalls != 0 {
		t.Errorf("build calls = %d, want 0: a rollback runs the images its release built", node.buildCalls)
	}
	want := "gotham/" + app.ID.String() + ":" + first.ID.String() + "-web"
	if !strings.Contains(node.composeDocuments[0], "image: "+want) {
		t.Errorf("rollback document does not run %q:\n%s", want, node.composeDocuments[0])
	}
}

func TestValidateComposeSourceRejectsPastedBuild(t *testing.T) {
	app := Application{
		ID:             uuid.New(),
		SourceType:     SourceCompose,
		ComposeContent: "services:\n  web:\n    build: .\n",
		ComposeService: "web",
	}
	err := validateComposeSource(app)
	if err == nil || !strings.Contains(err.Error(), "pasted compose cannot build") {
		t.Fatalf("validateComposeSource = %v, want a pasted-build rejection", err)
	}
}
