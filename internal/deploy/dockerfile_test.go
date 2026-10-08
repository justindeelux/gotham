package deploy

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestValidateDockerfileContent checks the GS-7 creation gate: pasted text is
// required, capped at 64 KiB, and must contain a FROM instruction. Nothing is
// executed; deeper validation happens on the node at build time.
func TestValidateDockerfileContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr error
	}{
		{"minimal", "FROM alpine:3.20\n", nil},
		{"lowercase from", "from scratch\n", nil},
		{"comments and blanks before FROM", "# comment\n\nFROM scratch\n", nil},
		{"parser directive before FROM", "# syntax=docker/dockerfile:1\nFROM scratch\n", nil},
		{"multistage", "FROM golang:1.22 AS build\nRUN go build ./...\nFROM scratch\nCOPY --from=build /app /app\n", nil},
		{"arg before FROM", "ARG VERSION=latest\nFROM alpine:${VERSION}\n", nil},
		{"empty", "", ErrValidation},
		{"blank", "  \n # only a comment\n", ErrValidation},
		{"no FROM", "RUN echo hi\nCOPY . /app\n", ErrValidation},
		{"FROM as suffix does not count", "RUN ECHO FROM\n", ErrValidation},
		{"nul byte", "FROM alpine:3.20\nRUN echo \x00\n", ErrValidation},
		{"invalid utf-8", "FROM alpine:3.20\n# \xff\xfe\n", ErrValidation},
		{"oversize", "FROM scratch\n" + strings.Repeat("x", MaxDockerfileBytes), ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDockerfileContent(tc.content)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateDockerfileContent = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateDockerfileContent = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestValidateBuildArgs checks the --build-arg collection gate: bounded
// count, container-variable keys, bounded values.
func TestValidateBuildArgs(t *testing.T) {
	if err := ValidateBuildArgs(nil); err != nil {
		t.Fatalf("nil args = %v, want nil", err)
	}
	if err := ValidateBuildArgs(map[string]string{"APP_ENV": "production"}); err != nil {
		t.Fatalf("valid args = %v, want nil", err)
	}
	if err := ValidateBuildArgs(map[string]string{"": "x"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty key = %v, want ErrValidation", err)
	}
	if err := ValidateBuildArgs(map[string]string{"A=B": "x"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad key = %v, want ErrValidation", err)
	}
	if err := ValidateBuildArgs(map[string]string{"K": "a\x00b"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("nul value = %v, want ErrValidation", err)
	}
	if err := ValidateBuildArgs(map[string]string{"K": "\xff\xfe"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid utf-8 value = %v, want ErrValidation", err)
	}
	if err := ValidateBuildArgs(map[string]string{"K": strings.Repeat("x", MaxBuildArgValueBytes+1)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("oversize value = %v, want ErrValidation", err)
	}
	many := make(map[string]string, MaxBuildArgs+1)
	for i := 0; i <= MaxBuildArgs; i++ {
		many["K"+strings.Repeat("X", 10)+string(rune('0'+i%10))+strings.Repeat("Y", i)] = "v"
	}
	if err := ValidateBuildArgs(many); !errors.Is(err, ErrValidation) {
		t.Fatalf("too many args = %v, want ErrValidation", err)
	}
}

// TestCloneSourceDockerfile pins the GS-7 fetch step: no clone runs, the
// stored text lands as the only file, invalid content fails before anything
// is written, and a retry clears a stale checkout first.
func TestCloneSourceDockerfile(t *testing.T) {
	const content = "FROM alpine:3.20\nCMD [\"echo\", \"hi\"]\n"

	t.Run("materializes content", func(t *testing.T) {
		src := &fakeSource{}
		o := newTestOrchestrator(Config{Source: src})
		app := testApplication(uuid.New())
		app.SourceType = SourceDockerfile
		app.DockerfileContent = content
		dir := t.TempDir()
		var logs []string
		if err := o.cloneSource(context.Background(), app, dir, func(s string) { logs = append(logs, s) }); err != nil {
			t.Fatalf("cloneSource = %v, want nil", err)
		}
		if src.calls != 0 {
			t.Errorf("clone calls = %d, want 0", src.calls)
		}
		data, err := os.ReadFile(dir + "/Dockerfile")
		if err != nil {
			t.Fatalf("read Dockerfile: %v", err)
		}
		if string(data) != content {
			t.Errorf("Dockerfile = %q, want %q", data, content)
		}
		if len(logs) == 0 {
			t.Error("no log lines emitted")
		}
	})

	t.Run("invalid content fails before cloning", func(t *testing.T) {
		src := &fakeSource{}
		o := newTestOrchestrator(Config{Source: src})
		app := testApplication(uuid.New())
		app.SourceType = SourceDockerfile
		app.DockerfileContent = "RUN echo hi\n"
		if err := o.cloneSource(context.Background(), app, t.TempDir(), nil); !errors.Is(err, ErrValidation) {
			t.Fatalf("cloneSource = %v, want ErrValidation", err)
		}
		if src.calls != 0 {
			t.Errorf("clone calls = %d, want 0", src.calls)
		}
	})

	t.Run("retry clears a stale checkout", func(t *testing.T) {
		o := newTestOrchestrator(Config{Source: &fakeSource{}})
		app := testApplication(uuid.New())
		app.SourceType = SourceDockerfile
		app.DockerfileContent = content
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/stale.txt", []byte("stale"), 0o644); err != nil {
			t.Fatalf("seed stale file: %v", err)
		}
		if err := o.cloneSource(context.Background(), app, dir, nil); err != nil {
			t.Fatalf("cloneSource = %v, want nil", err)
		}
		if _, err := os.Stat(dir + "/stale.txt"); !os.IsNotExist(err) {
			t.Error("stale.txt survived the materialization")
		}
	})
}

// TestBuildDockerfileSource pins the GS-7 build step: build-pack detection
// is skipped, the Dockerfile engine is forced, the stored --build-arg pairs
// ride the BuildImage meta, and the streamed context carries exactly the
// stored text as its only file.
func TestBuildDockerfileSource(t *testing.T) {
	const content = "FROM alpine:3.20\nARG APP_ENV\nCMD [\"echo\", \"hi\"]\n"
	app := testApplication(uuid.New())
	app.SourceType = SourceDockerfile
	app.DockerfileContent = content
	app.BuildPack = "railpack" // ignored: detection is skipped for dockerfiles
	app.BuildArgs = map[string]string{"APP_ENV": "production"}
	repo := &fakeRepository{app: app}
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
	node := newMockNode()
	o := newTestOrchestrator(Config{Repository: repo, Dial: dialAlways(node)})

	o.run(context.Background(), job{app: app, dep: dep})

	stored, ok := repo.deployment(dep.ID)
	if !ok {
		t.Fatal("deployment row is gone")
	}
	if stored.State != StateRunning {
		t.Fatalf("state = %s, want running (error %q)", stored.State, stored.Error)
	}
	if len(node.metas) != 1 {
		t.Fatalf("build calls = %d, want 1", len(node.metas))
	}
	meta := node.metas[0]
	// An empty engine means a Dockerfile build on the node (the
	// ImageBuildOptions contract); the Dockerfile path names the file.
	if meta.Engine != "" && meta.Engine != "dockerfile" {
		t.Errorf("engine = %q, want empty or dockerfile", meta.Engine)
	}
	if meta.Dockerfile != "Dockerfile" {
		t.Errorf("dockerfile = %q, want Dockerfile", meta.Dockerfile)
	}
	if meta.BuildArgs["APP_ENV"] != "production" {
		t.Errorf("build args = %v, want APP_ENV=production", meta.BuildArgs)
	}
	if len(node.contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(node.contexts))
	}
	names, contents := readTar(t, node.contexts[0])
	if len(names) != 1 || names[0] != "Dockerfile" {
		t.Fatalf("context files = %v, want [Dockerfile]", names)
	}
	if contents["Dockerfile"] != content {
		t.Errorf("context Dockerfile = %q, want %q", contents["Dockerfile"], content)
	}
}

// TestCreateApplicationDockerfileNormalizes pins finding-7 behavior: stale
// repo fields are cleared for dockerfile rows, and raw build args are
// validated before normalization, exactly like the update path.
func TestCreateApplicationDockerfileNormalizes(t *testing.T) {
	newInput := func() CreateApplicationInput {
		in := validCreateInput(uuid.New())
		in.SourceType = SourceDockerfile
		in.Provider = ""
		in.Repo = "acme/stale"
		in.CloneURL = "https://github.com/acme/stale.git"
		in.DockerfileContent = "FROM alpine:3.20\n"
		in.BuildArgs = map[string]string{"APP_ENV": "production"}
		return in
	}

	t.Run("clears stale repo fields", func(t *testing.T) {
		repo := &fakeRepository{}
		svc := newTestService(t, repo)
		created, err := svc.CreateApplication(context.Background(), uuid.New(), newInput())
		if err != nil {
			t.Fatalf("create = %v, want nil", err)
		}
		if created.Repo != "" || created.CloneURL != "" {
			t.Errorf("repo = %q, clone_url = %q, want both cleared", created.Repo, created.CloneURL)
		}
		if created.DockerfileContent != "FROM alpine:3.20\n" {
			t.Errorf("content = %q, want the pasted text", created.DockerfileContent)
		}
	})

	t.Run("rejects raw build args like update does", func(t *testing.T) {
		repo := &fakeRepository{}
		svc := newTestService(t, repo)
		in := newInput()
		in.BuildArgs = map[string]string{"": "x"}
		if _, err := svc.CreateApplication(context.Background(), uuid.New(), in); !errors.Is(err, ErrValidation) {
			t.Fatalf("create = %v, want ErrValidation", err)
		}
	})
}

// TestUpdateApplicationDockerfile pins the detail-page edit path: stored
// text and args change together, other source types refuse both, and invalid
// content is rejected without touching the row.
func TestUpdateApplicationDockerfile(t *testing.T) {
	const content = "FROM alpine:3.20\n"

	newDockerApp := func() (Application, *Service) {
		app := testApplication(uuid.New())
		app.SourceType = SourceDockerfile
		app.DockerfileContent = content
		repo := &fakeRepository{app: app}
		return app, newTestService(t, repo)
	}

	t.Run("edits text and args", func(t *testing.T) {
		app, svc := newDockerApp()
		next := "FROM alpine:3.21\n"
		args := map[string]string{"APP_ENV": "staging"}
		updated, err := svc.UpdateApplication(context.Background(), app.UserID, app.ID, UpdateApplicationInput{
			DockerfileContent: &next,
			BuildArgs:         &args,
		})
		if err != nil {
			t.Fatalf("update = %v, want nil", err)
		}
		if updated.DockerfileContent != next {
			t.Errorf("content = %q, want %q", updated.DockerfileContent, next)
		}
		if updated.BuildArgs["APP_ENV"] != "staging" {
			t.Errorf("args = %v, want APP_ENV=staging", updated.BuildArgs)
		}
		stored, err := svc.GetApplication(context.Background(), app.UserID, app.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if stored.DockerfileContent != next {
			t.Errorf("stored content = %q, want %q", stored.DockerfileContent, next)
		}
	})

	t.Run("invalid content is rejected", func(t *testing.T) {
		app, svc := newDockerApp()
		bad := "RUN echo hi\n"
		if _, err := svc.UpdateApplication(context.Background(), app.UserID, app.ID, UpdateApplicationInput{
			DockerfileContent: &bad,
		}); !errors.Is(err, ErrValidation) {
			t.Fatalf("update = %v, want ErrValidation", err)
		}
		stored, err := svc.GetApplication(context.Background(), app.UserID, app.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if stored.DockerfileContent != content {
			t.Error("stored content changed on a failed update")
		}
	})

	t.Run("git apps refuse dockerfile fields", func(t *testing.T) {
		app := testApplication(uuid.New())
		repo := &fakeRepository{app: app}
		svc := newTestService(t, repo)
		next := "FROM alpine:3.21\n"
		if _, err := svc.UpdateApplication(context.Background(), app.UserID, app.ID, UpdateApplicationInput{
			DockerfileContent: &next,
		}); !errors.Is(err, ErrValidation) {
			t.Fatalf("update = %v, want ErrValidation", err)
		}
	})
}

// TestPreviewCopiesDockerfileFields pins the intended inheritance: a preview
// of a dockerfile base builds the same stored text with the same build args
// (build-time values, not sealed secrets, which stay excluded).
func TestPreviewCopiesDockerfileFields(t *testing.T) {
	userID := uuid.New()
	base := testApplication(userID)
	base.TeamID = uuid.New()
	base.SourceType = SourceDockerfile
	base.Provider = ""
	base.Repo = ""
	base.CloneURL = ""
	base.DockerfileContent = "FROM alpine:3.20\n"
	base.BuildArgs = map[string]string{"APP_ENV": "production"}
	repo := seedBaseForPreview(t, base)
	svc := newTestService(t, repo)

	created, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name:   "demo app-pr-7",
		Branch: "feat/x",
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}
	if created.DockerfileContent != base.DockerfileContent {
		t.Errorf("content = %q, want %q", created.DockerfileContent, base.DockerfileContent)
	}
	if created.BuildArgs["APP_ENV"] != "production" {
		t.Errorf("build args = %v, want APP_ENV=production", created.BuildArgs)
	}
	if !created.IsPreview {
		t.Error("the clone is not marked is_preview")
	}
}

// TestValidateDeployTargetDockerfile pins the submit gate: a dockerfile app
// deploys without any clone URL, and empty stored text fails the deploy
// instead of the build.
func TestValidateDeployTargetDockerfile(t *testing.T) {
	app := testApplication(uuid.New())
	app.SourceType = SourceDockerfile
	app.CloneURL = ""
	app.DockerfileContent = "FROM alpine:3.20\n"
	if err := validateDeployTarget(app); err != nil {
		t.Fatalf("validateDeployTarget = %v, want nil", err)
	}
	app.DockerfileContent = ""
	if err := validateDeployTarget(app); !errors.Is(err, ErrValidation) {
		t.Fatalf("validateDeployTarget = %v, want ErrValidation", err)
	}
}

// TestRedeployAfterEditBuildsLatestText pins the detail-page contract: after
// the stored text changes, the next deploy streams the new text to the node.
func TestRedeployAfterEditBuildsLatestText(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	app.SourceType = SourceDockerfile
	app.Provider = ""
	app.Repo = ""
	app.CloneURL = ""
	app.DockerfileContent = "FROM alpine:3.20\n"
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)
	node := newMockNode()
	o := newTestOrchestrator(Config{Repository: repo, Dial: dialAlways(node)})

	first := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
	o.run(context.Background(), job{app: app, dep: first})

	next := "FROM alpine:3.21\n"
	updated, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{
		DockerfileContent: &next,
	})
	if err != nil {
		t.Fatalf("update = %v, want nil", err)
	}
	second := seedDeployment(t, repo, updated, Deployment{Kind: KindDeploy})
	o.run(context.Background(), job{app: updated, dep: second})

	if len(node.contexts) != 2 {
		t.Fatalf("build contexts = %d, want 2", len(node.contexts))
	}
	_, contents := readTar(t, node.contexts[1])
	if contents["Dockerfile"] != next {
		t.Errorf("redeploy context = %q, want %q", contents["Dockerfile"], next)
	}
	stored, ok := repo.deployment(second.ID)
	if !ok || stored.State != StateRunning {
		t.Fatalf("second deploy state = %v, want running", stored.State)
	}
}

// readTar lists the regular files of a tar context by name.
func readTar(t *testing.T, data []byte) ([]string, map[string]string) {
	t.Helper()
	var names []string
	contents := map[string]string{}
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		names = append(names, header.Name)
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read tar body: %v", err)
		}
		contents[header.Name] = string(body)
	}
	return names, contents
}
