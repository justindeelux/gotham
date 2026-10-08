package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestNormalizeSourceType keeps pre-GS-2 callers on their behaviour: an
// explicit type passes through, an empty one derives from the provider like
// the 00037 backfill.
func TestNormalizeSourceType(t *testing.T) {
	cases := []struct {
		sourceType string
		provider   string
		want       string
	}{
		{"", "github", SourceGitHubApp},
		{"", "gitlab", SourceGitLabApp},
		{"", "public", SourceGitPublic},
		{"", "gitea", SourceGitPublic},
		{"", "", SourceGitPublic},
		{SourceGitPrivate, "github", SourceGitPrivate},
		{SourceDockerfile, "", SourceDockerfile},
	}
	for _, tc := range cases {
		if got := NormalizeSourceType(tc.sourceType, tc.provider); got != tc.want {
			t.Errorf("NormalizeSourceType(%q, %q) = %q, want %q",
				tc.sourceType, tc.provider, got, tc.want)
		}
	}
}

// TestValidSourceType accepts the seven GS-2 types plus the legacy empty
// value, and rejects anything else.
func TestValidSourceType(t *testing.T) {
	for _, s := range []string{
		"", SourceGitPublic, SourceGitPrivate, SourceGitHubApp,
		SourceGitLabApp, SourceDockerfile, SourceCompose, SourceImage,
	} {
		if !ValidSourceType(s) {
			t.Errorf("ValidSourceType(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"git", "svn", "GIT_PUBLIC", "tarball"} {
		if ValidSourceType(s) {
			t.Errorf("ValidSourceType(%q) = true, want false", s)
		}
	}
}

// TestValidateSourcePerType checks the per-type creation requirements:
// git-backed types need a cloneable URL (provider-backed ones also name
// the repository), while Dockerfile/Compose/image sources carry no fetch
// fields until GS-7..GS-9.
func TestValidateSourcePerType(t *testing.T) {
	base := Application{
		Name:      "demo",
		Branch:    "main",
		BuildPack: "dockerfile",
	}
	cases := []struct {
		name       string
		sourceType string
		repo       string
		cloneURL   string
		wantErr    bool
	}{
		{"public git", SourceGitPublic, "", "https://github.com/acme/demo.git", false},
		{"legacy empty behaves like public git", "", "", "https://github.com/acme/demo.git", false},
		{"public git without URL", SourceGitPublic, "", "", true},
		{"private git", SourceGitPrivate, "", "git@github.com:acme/demo.git", false},
		{"private git without URL", SourceGitPrivate, "", "", true},
		{"github app", SourceGitHubApp, "acme/demo", "git@github.com:acme/demo.git", false},
		{"github app without repo", SourceGitHubApp, "", "git@github.com:acme/demo.git", true},
		{"github app without URL", SourceGitHubApp, "acme/demo", "", true},
		{"gitlab app", SourceGitLabApp, "acme/demo", "git@gitlab.com:acme/demo.git", false},
		{"dockerfile", SourceDockerfile, "", "", false},
		{"compose", SourceCompose, "", "", false},
		{"image", SourceImage, "", "", false},
		{"unknown type", "tarball", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := base
			app.SourceType = tc.sourceType
			app.Repo = tc.repo
			app.CloneURL = tc.cloneURL
			err := validateApplication(app, true)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateApplication = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want it to wrap ErrValidation", err)
			}
		})
	}
}

// TestCreateApplicationSourceType covers the service boundary: an unknown
// type is a 400, while a not-yet-implemented type stores fine (the
// orchestrator fails its deploy closed) and an empty type normalizes from
// the provider.
func TestCreateApplicationSourceType(t *testing.T) {
	t.Run("unknown type is rejected", func(t *testing.T) {
		repo := &fakeRepository{}
		svc := newTestService(t, repo)
		in := validCreateInput(uuid.New())
		in.SourceType = "tarball"
		if _, err := svc.CreateApplication(context.Background(), uuid.New(), in); !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})

	t.Run("empty type normalizes from the provider", func(t *testing.T) {
		repo := &fakeRepository{}
		svc := newTestService(t, repo)
		userID := uuid.New()
		in := validCreateInput(uuid.New())
		in.SourceType = ""
		in.Provider = "github"
		created, err := svc.CreateApplication(context.Background(), userID, in)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if created.SourceType != SourceGitHubApp {
			t.Errorf("source type = %q, want %q", created.SourceType, SourceGitHubApp)
		}
	})

	t.Run("unimplemented type stores and keeps its type", func(t *testing.T) {
		repo := &fakeRepository{}
		svc := newTestService(t, repo)
		userID := uuid.New()
		in := validCreateInput(uuid.New())
		in.SourceType = SourceDockerfile
		created, err := svc.CreateApplication(context.Background(), userID, in)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if created.SourceType != SourceDockerfile {
			t.Errorf("source type = %q, want %q", created.SourceType, SourceDockerfile)
		}
	})
}

// TestCloneSourceBranching pins the GS-2 orchestrator switch: git-backed
// types (and legacy empty rows) clone, Dockerfile/Compose/image fail with
// the typed error, and anything else is a validation error.
func TestCloneSourceBranching(t *testing.T) {
	t.Run("git types clone", func(t *testing.T) {
		for _, sourceType := range []string{
			"", SourceGitPublic, SourceGitPrivate, SourceGitHubApp, SourceGitLabApp,
		} {
			src := &fakeSource{}
			o := newTestOrchestrator(Config{Source: src})
			app := testApplication(uuid.New())
			app.SourceType = sourceType
			if err := o.cloneSource(context.Background(), app, t.TempDir(), nil); err != nil {
				t.Errorf("cloneSource(%q): %v", sourceType, err)
			}
			if src.calls != 1 {
				t.Errorf("cloneSource(%q): clone calls = %d, want 1", sourceType, src.calls)
			}
		}
	})

	t.Run("unimplemented types fail closed", func(t *testing.T) {
		for _, sourceType := range []string{SourceDockerfile, SourceCompose, SourceImage} {
			src := &fakeSource{}
			o := newTestOrchestrator(Config{Source: src})
			app := testApplication(uuid.New())
			app.SourceType = sourceType
			err := o.cloneSource(context.Background(), app, t.TempDir(), nil)
			if !errors.Is(err, ErrSourceNotImplemented) {
				t.Errorf("cloneSource(%q) = %v, want ErrSourceNotImplemented", sourceType, err)
			}
			if src.calls != 0 {
				t.Errorf("cloneSource(%q): clone calls = %d, want 0", sourceType, src.calls)
			}
		}
	})

	t.Run("unknown type is a validation error", func(t *testing.T) {
		o := newTestOrchestrator(Config{Source: &fakeSource{}})
		app := testApplication(uuid.New())
		app.SourceType = "tarball"
		if err := o.cloneSource(context.Background(), app, t.TempDir(), nil); !errors.Is(err, ErrValidation) {
			t.Errorf("cloneSource(tarball) = %v, want ErrValidation", err)
		}
	})
}
