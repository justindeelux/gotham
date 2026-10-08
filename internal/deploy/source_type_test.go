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

// TestValidateSourcePerType checks the per-type creation requirements: the
// provider slug must agree with the type, git-backed types need a cloneable
// URL (provider-backed ones also name the repository, git_private stays
// provider-less), the dockerfile type needs pasted content with a FROM
// instruction, image sources need a validated reference and no git fields,
// and compose sources need pasted content or a repository with a compose
// file plus the routed web service.
func TestValidateSourcePerType(t *testing.T) {
	base := Application{
		Name:      "demo",
		Branch:    "main",
		BuildPack: "dockerfile",
	}
	cases := []struct {
		name       string
		sourceType string
		provider   string
		repo       string
		cloneURL   string
		dockerfile string
		wantErr    error
	}{
		{"public git", SourceGitPublic, "", "", "https://github.com/acme/demo.git", "", nil},
		{"public git over git scheme", SourceGitPublic, "", "", "git://git.internal/acme/demo.git", "", nil},
		{"public git refuses ssh URL", SourceGitPublic, "", "", "ssh://git@git.internal/acme/demo.git", "", ErrValidation},
		{"public git refuses scp-like URL", SourceGitPublic, "", "", "git@git.internal:acme/demo.git", "", ErrValidation},
		{"legacy empty behaves like public git", "", "", "", "https://github.com/acme/demo.git", "", nil},
		{"legacy public sentinel", SourceGitPublic, "public", "", "https://github.com/acme/demo.git", "", nil},
		{"gitea-backed legacy app", SourceGitPublic, "gitea", "acme/demo", "https://gitea.example/acme/demo.git", "", nil},
		{"public git with github provider", SourceGitPublic, "github", "", "https://github.com/acme/demo.git", "", ErrValidation},
		{"public git with gitlab provider", SourceGitPublic, "gitlab", "", "https://github.com/acme/demo.git", "", ErrValidation},
		{"public git without URL", SourceGitPublic, "", "", "", "", ErrValidation},
		{"github app", SourceGitHubApp, "github", "acme/demo", "git@github.com:acme/demo.git", "", nil},
		{"github app with gitlab provider", SourceGitHubApp, "gitlab", "acme/demo", "git@github.com:acme/demo.git", "", ErrValidation},
		{"github app without provider", SourceGitHubApp, "", "acme/demo", "git@github.com:acme/demo.git", "", ErrValidation},
		{"github app without repo", SourceGitHubApp, "github", "", "git@github.com:acme/demo.git", "", ErrValidation},
		{"github app without URL", SourceGitHubApp, "github", "acme/demo", "", "", ErrValidation},
		{"gitlab app", SourceGitLabApp, "gitlab", "acme/demo", "git@gitlab.com:acme/demo.git", "", nil},
		{"gitlab app with github provider", SourceGitLabApp, "github", "acme/demo", "git@gitlab.com:acme/demo.git", "", ErrValidation},
		{"private git over scp-like URL", SourceGitPrivate, "", "", "git@github.com:acme/demo.git", "", nil},
		{"private git over ssh URL", SourceGitPrivate, "", "", "ssh://git@git.internal:2222/acme/demo.git", "", nil},
		{"private git over https URL", SourceGitPrivate, "", "", "https://git.internal/acme/demo.git", "", nil},
		{"private git over http URL", SourceGitPrivate, "", "", "http://git.internal/acme/demo.git", "", nil},
		{"private git refuses a provider", SourceGitPrivate, "github", "acme/demo", "git@github.com:acme/demo.git", "", ErrValidation},
		{"private git refuses embedded credentials", SourceGitPrivate, "", "", "https://user:token@git.internal/acme/demo.git", "", ErrValidation},
		{"private git refuses public-only git scheme", SourceGitPrivate, "", "", "git://git.internal/acme/demo.git", "", ErrValidation},
		{"private git without URL", SourceGitPrivate, "", "", "", "", ErrValidation},
		{"dockerfile with content", SourceDockerfile, "", "", "", "FROM alpine:3.20\n", nil},
		{"dockerfile without content", SourceDockerfile, "", "", "", "", ErrValidation},
		{"dockerfile with provider", SourceDockerfile, "github", "", "", "FROM alpine:3.20\n", ErrValidation},
		{"dockerfile without FROM", SourceDockerfile, "", "", "", "RUN echo hi\n", ErrValidation},
		{"compose without content or repo", SourceCompose, "", "", "", "", ErrValidation},
		{"image with git fields is rejected", SourceImage, "", "", "", "", ErrValidation},
		{"unknown type", "tarball", "", "", "", "", ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := base
			app.SourceType = tc.sourceType
			app.Provider = tc.provider
			app.Repo = tc.repo
			app.CloneURL = tc.cloneURL
			app.DockerfileContent = tc.dockerfile
			err := validateApplication(app, true)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("validateApplication = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("validateApplication = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestCreateApplicationSourceType covers the service boundary: an unknown
// type is a 400, a not-yet-implemented type is a 422, and an empty type
// normalizes from the provider.
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

	t.Run("all known source types are implemented", func(t *testing.T) {
		for _, sourceType := range []string{
			"", SourceGitPublic, SourceGitPrivate, SourceGitHubApp,
			SourceGitLabApp, SourceDockerfile, SourceImage, SourceCompose,
		} {
			if !SourceTypeImplemented(sourceType) {
				t.Errorf("SourceTypeImplemented(%q) = false, want true", sourceType)
			}
		}
	})

	t.Run("mismatched provider is rejected", func(t *testing.T) {
		repo := &fakeRepository{}
		svc := newTestService(t, repo)
		in := validCreateInput(uuid.New())
		in.SourceType = SourceGitLabApp
		in.Provider = "github"
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

	t.Run("github app stores with matching provider", func(t *testing.T) {
		repo := &fakeRepository{}
		svc := newTestService(t, repo)
		userID := uuid.New()
		in := validCreateInput(uuid.New())
		in.SourceType = SourceGitHubApp
		in.Provider = "github"
		created, err := svc.CreateApplication(context.Background(), userID, in)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if created.SourceType != SourceGitHubApp {
			t.Errorf("source type = %q, want %q", created.SourceType, SourceGitHubApp)
		}
	})
}

// TestCloneSourceBranching pins the GS-2 orchestrator switch: implemented
// git types (and legacy empty rows) clone through the shared cloner, and
// anything unknown is a validation error. Dockerfile, image and compose
// sources take their own fetch paths (see dockerfile_test.go,
// compose_test.go and the image deploy tests).
func TestCloneSourceBranching(t *testing.T) {
	t.Run("implemented types clone", func(t *testing.T) {
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

	t.Run("unknown type is a validation error", func(t *testing.T) {
		o := newTestOrchestrator(Config{Source: &fakeSource{}})
		app := testApplication(uuid.New())
		app.SourceType = "tarball"
		if err := o.cloneSource(context.Background(), app, t.TempDir(), nil); !errors.Is(err, ErrValidation) {
			t.Errorf("cloneSource(tarball) = %v, want ErrValidation", err)
		}
	})
}

// TestPreviewOfLegacyProviderApp pins the no-behaviour-change rule: a legacy
// base (empty type) with a provider that owns no dedicated type keeps its
// preview flow, landing on git_public with the provider untouched.
func TestPreviewOfLegacyProviderApp(t *testing.T) {
	for _, provider := range []string{"gitea", "public", ""} {
		t.Run("provider "+provider, func(t *testing.T) {
			userID := uuid.New()
			base := testApplication(userID)
			base.TeamID = uuid.New()
			base.Provider = provider
			base.SourceType = ""
			repo := seedBaseForPreview(t, base)
			svc := newTestService(t, repo)

			created, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
				Name:   "demo app-pr-1",
				Branch: "feat/x",
			})
			if err != nil {
				t.Fatalf("CreatePreviewApplication: %v", err)
			}
			if created.SourceType != SourceGitPublic || created.Provider != provider {
				t.Errorf("preview source = (%q, %q), want (git_public, %q)",
					created.SourceType, created.Provider, provider)
			}
		})
	}
}
