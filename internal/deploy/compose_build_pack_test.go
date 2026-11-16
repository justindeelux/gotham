package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/builds"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const composeBuildPackFile = "deploy/compose.yml"

func composeBuildPackInput(serverID uuid.UUID) CreateApplicationInput {
	in := validCreateInput(serverID)
	in.SourceType = SourceGitPublic
	in.Provider = ""
	in.BuildPack = string(builds.EngineCompose)
	in.ComposeFile = ""
	in.ComposeService = "web"
	return in
}

func TestServiceCreateComposeBuildPack(t *testing.T) {
	cases := []struct {
		name       string
		sourceType string
		provider   string
		repo       string
		cloneURL   string
	}{
		{"public git", SourceGitPublic, "", "acme/demo", "https://github.com/acme/demo.git"},
		{"private git", SourceGitPrivate, "", "acme/demo", "ssh://git@git.internal/acme/demo.git"},
		{"github app", SourceGitHubApp, "github", "acme/demo", "https://github.com/acme/demo.git"},
		{"gitlab app", SourceGitLabApp, "gitlab", "acme/demo", "https://gitlab.com/acme/demo.git"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{}
			svc := newTestService(t, repo)
			in := composeBuildPackInput(uuid.New())
			in.SourceType = tc.sourceType
			in.Provider = tc.provider
			in.Repo = tc.repo
			in.CloneURL = tc.cloneURL

			created, err := svc.CreateApplication(context.Background(), uuid.New(), in)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if created.BuildPack != string(builds.EngineCompose) {
				t.Errorf("build pack = %q, want %q", created.BuildPack, builds.EngineCompose)
			}
			if created.ComposeFile != DefaultComposeFile {
				t.Errorf("compose file = %q, want the default %q", created.ComposeFile, DefaultComposeFile)
			}
			if created.ComposeService != "web" {
				t.Errorf("compose service = %q, want web", created.ComposeService)
			}
			if created.ComposeContent != "" {
				t.Errorf("compose content = %q, want empty for a repository-backed app", created.ComposeContent)
			}
		})
	}

	t.Run("rejects invalid compose fields", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*CreateApplicationInput)
		}{
			{"no web service", func(in *CreateApplicationInput) { in.ComposeService = "" }},
			{"pasted content", func(in *CreateApplicationInput) { in.ComposeContent = composeSafe }},
			{"absolute file", func(in *CreateApplicationInput) { in.ComposeFile = "/etc/docker-compose.yml" }},
			{"escaping file", func(in *CreateApplicationInput) { in.ComposeFile = "../docker-compose.yml" }},
			{"dockerfile source", func(in *CreateApplicationInput) {
				in.SourceType = SourceDockerfile
				in.Provider = ""
				in.Repo = ""
				in.CloneURL = ""
				in.DockerfileContent = "FROM alpine:3.20\n"
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := newTestService(t, &fakeRepository{})
				in := composeBuildPackInput(uuid.New())
				tc.mutate(&in)
				if _, err := svc.CreateApplication(context.Background(), uuid.New(), in); !errors.Is(err, ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation", err)
				}
			})
		}
	})
}

func TestServiceUpdateComposeBuildPack(t *testing.T) {
	newRepoApp := func() Application {
		return Application{
			ID:            uuid.New(),
			UserID:        uuid.New(),
			ServerID:      uuid.New(),
			EnvironmentID: uuid.New(),
			Name:          "demo app",
			SourceType:    SourceGitHubApp,
			Provider:      "github",
			Repo:          "acme/demo",
			CloneURL:      "https://github.com/acme/demo.git",
			Branch:        "main",
		}
	}

	t.Run("switching the pack defaults the compose file", func(t *testing.T) {
		app := newRepoApp()
		svc := newTestService(t, &fakeRepository{app: app})
		pack := string(builds.EngineCompose)
		service := "web"
		updated, err := svc.UpdateApplication(context.Background(), app.UserID, app.ID, UpdateApplicationInput{
			BuildPack:      &pack,
			ComposeService: &service,
		})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if updated.ComposeFile != DefaultComposeFile || updated.ComposeService != "web" {
			t.Errorf("compose = (%q, %q), want the default file and web service",
				updated.ComposeFile, updated.ComposeService)
		}
	})

	t.Run("pasted content is refused", func(t *testing.T) {
		app := newRepoApp()
		app.BuildPack = string(builds.EngineCompose)
		app.ComposeFile = DefaultComposeFile
		app.ComposeService = "web"
		svc := newTestService(t, &fakeRepository{app: app})
		content := composeSafe
		_, err := svc.UpdateApplication(context.Background(), app.UserID, app.ID,
			UpdateApplicationInput{ComposeContent: &content})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})

	t.Run("legacy compose source refuses the pack", func(t *testing.T) {
		app := testComposeApp(uuid.New())
		svc := newTestService(t, &fakeRepository{app: app})
		pack := string(builds.EngineCompose)
		_, err := svc.UpdateApplication(context.Background(), app.UserID, app.ID,
			UpdateApplicationInput{BuildPack: &pack})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})
}

func TestOrchestratorComposeBuildPackSources(t *testing.T) {
	cases := []struct {
		name       string
		sourceType string
		provider   string
		repo       string
		cloneURL   string
		wantToken  bool
		wantKey    bool
		wantCred   bool
	}{
		{"public git", SourceGitPublic, "", "acme/demo", "https://github.com/acme/demo.git", false, false, false},
		{"private git", SourceGitPrivate, "", "acme/demo", "https://git.internal/acme/demo.git", false, false, true},
		{"github app", SourceGitHubApp, "github", "acme/demo", "https://github.com/acme/demo.git", true, false, false},
		{"gitlab app", SourceGitLabApp, "gitlab", "acme/demo", "https://gitlab.com/acme/demo.git", false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()
			app := Application{
				ID:             uuid.New(),
				UserID:         userID,
				ServerID:       uuid.New(),
				EnvironmentID:  uuid.New(),
				Name:           "compose app",
				SourceType:     tc.sourceType,
				Provider:       tc.provider,
				Repo:           tc.repo,
				CloneURL:       tc.cloneURL,
				Branch:         "main",
				BuildPack:      string(builds.EngineCompose),
				ComposeFile:    composeBuildPackFile,
				ComposeService: "web",
			}
			if tc.sourceType == SourceGitHubApp {
				app.GitHubAppID = uuid.New()
			}
			if err := validateDeployTarget(app); err != nil {
				t.Fatalf("validateDeployTarget: %v", err)
			}
			repo := &fakeRepository{app: app}
			dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})

			keys := &staticKeyResolver{}
			if tc.wantKey {
				keys.pem = "test-deploy-key"
			}
			tokens := &staticTokenResolver{tokenURL: "https://x-access-token:fresh-install-token@github.com/acme/demo.git"}
			creds := &staticCredResolver{username: "git"}
			if tc.wantCred {
				creds.token = "private-token"
			}
			var gotArgv, gotEnv []string
			run := func(_ context.Context, argv, env []string) ([]byte, error) {
				gotArgv, gotEnv = argv, env
				dest := argv[len(argv)-1]
				if err := os.MkdirAll(filepath.Join(dest, "deploy"), 0o755); err != nil {
					return nil, err
				}
				return nil, os.WriteFile(filepath.Join(dest, composeBuildPackFile),
					[]byte("services:\n  web:\n    image: example.com/app:1.0\n"), 0o644)
			}
			src := gitSource{keys: keys, appTokens: tokens, creds: creds, run: run}
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
			if node.composeUpCalls != 1 {
				t.Errorf("compose up calls = %d, want 1", node.composeUpCalls)
			}
			if node.runCalls != 0 {
				t.Errorf("run calls = %d, want 0 for a compose deploy", node.runCalls)
			}
			if !strings.Contains(stored.ComposeDocument, "example.com/app:1.0") {
				t.Errorf("stored compose document = %q, want the repository file", stored.ComposeDocument)
			}

			joined := strings.Join(gotArgv, " ")
			switch {
			case tc.wantToken:
				if tokens.calls != 1 {
					t.Errorf("token resolver calls = %d, want 1", tokens.calls)
				}
				if !strings.Contains(joined, "fresh-install-token@github.com") {
					t.Errorf("clone argv = %v, want the GitHub App token URL", gotArgv)
				}
			case tc.wantKey:
				if keys.calls != 1 {
					t.Errorf("deploy key lookups = %d, want 1", keys.calls)
				}
				if !strings.Contains(joined, "ssh://git@gitlab.com/acme/demo.git") {
					t.Errorf("clone argv = %v, want the GitLab App keyed SSH URL", gotArgv)
				}
				if !hasEntry(gotEnv, "GIT_SSH_COMMAND") {
					t.Errorf("clone env lacks the deploy key SSH command")
				}
			case tc.wantCred:
				if creds.calls != 1 {
					t.Errorf("git credential lookups = %d, want 1", creds.calls)
				}
				if !hasEntry(gotEnv, "GIT_ASKPASS") {
					t.Errorf("clone env lacks the stored HTTPS credential")
				}
			default:
				if hasEntry(gotEnv, "GIT_ASKPASS") || hasEntry(gotEnv, "GIT_SSH_COMMAND") {
					t.Errorf("public clone carried credentials")
				}
			}
		})
	}
}

func TestOrchestratorComposeBuildPackRollback(t *testing.T) {
	userID := uuid.New()
	app := Application{
		ID:             uuid.New(),
		UserID:         userID,
		ServerID:       uuid.New(),
		EnvironmentID:  uuid.New(),
		Name:           "compose app",
		SourceType:     SourceGitPublic,
		Repo:           "acme/demo",
		CloneURL:       "https://github.com/acme/demo.git",
		Branch:         "main",
		BuildPack:      string(builds.EngineCompose),
		ComposeFile:    composeBuildPackFile,
		ComposeService: "web",
	}
	repo := &fakeRepository{app: app}
	first := seedDeployment(t, repo, app, Deployment{
		Kind:            KindDeploy,
		State:           StateRunning,
		ComposeDocument: "services:\n  web:\n    image: example.com/app:1.0\n",
		ComposeCommit:   "v1",
	})
	rollback := seedDeployment(t, repo, app, Deployment{
		Kind:            KindRollback,
		RollbackFrom:    first.ID,
		ComposeDocument: first.ComposeDocument,
		ComposeCommit:   first.ComposeCommit,
	})
	src := &writeFileSource{files: map[string]string{
		composeBuildPackFile: "services:\n  web:\n    image: example.com/app:2.0\n",
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

	o.run(context.Background(), job{app: app, dep: rollback})

	stored, ok := repo.deployment(rollback.ID)
	if !ok {
		t.Fatal("rollback row is gone")
	}
	if stored.State != StateRunning {
		t.Fatalf("rollback state = %s (%q), want running", stored.State, stored.Error)
	}
	if src.calls != 0 {
		t.Errorf("clone calls = %d, want 0: the rollback applies the stored document", src.calls)
	}
	if len(node.composeDocuments) != 1 {
		t.Fatalf("compose documents = %d, want 1", len(node.composeDocuments))
	}
	if !strings.Contains(node.composeDocuments[0], "example.com/app:1.0") {
		t.Errorf("rollback applied the live file instead of the stored document:\n%s", node.composeDocuments[0])
	}
}
