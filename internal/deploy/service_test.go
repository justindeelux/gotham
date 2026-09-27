package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// newTestService builds a Service over the fake repository and closes its
// worker pool when the test ends.
func newTestService(t *testing.T, repo *fakeRepository) *Service {
	t.Helper()
	svc := NewService(Config{Repository: repo, Secret: testSecretKey, Logger: discardLogger()})
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// listStored reads the stored deployments through the mutex-protected seam.
func listStored(t *testing.T, repo *fakeRepository, appID uuid.UUID) []Deployment {
	t.Helper()
	deployments, err := repo.ListDeployments(context.Background(), appID)
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	return deployments
}

func TestServiceDeployValidatesTarget(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Application)
	}{
		{"no server assigned", func(a *Application) { a.ServerID = uuid.Nil }},
		{"no clone URL", func(a *Application) { a.CloneURL = "" }},
		{"unsupported clone scheme", func(a *Application) { a.CloneURL = "ftp://example.com/repo.git" }},
		{"uncloneable clone URL", func(a *Application) { a.CloneURL = "just-a-name" }},
		{"unknown build pack", func(a *Application) { a.BuildPack = "cobol" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()
			app := testApplication(userID)
			tc.mutate(&app)
			repo := &fakeRepository{app: app}
			svc := newTestService(t, repo)

			_, err := svc.Deploy(context.Background(), userID, app.ID)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if stored := listStored(t, repo, app.ID); len(stored) != 0 {
				t.Errorf("queued %d deployments, want 0", len(stored))
			}
		})
	}
}

func TestServiceDeployOwnership(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)
	ctx := context.Background()

	if _, err := svc.Deploy(ctx, uuid.New(), app.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("other user's deploy err = %v, want ErrNotFound", err)
	}
	if _, err := svc.Deploy(ctx, userID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown application err = %v, want ErrNotFound", err)
	}
	if _, err := svc.ListDeployments(ctx, uuid.New(), app.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("other user's list err = %v, want ErrNotFound", err)
	}
	if _, err := svc.Deploy(ctx, userID, uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Errorf("nil application id err = %v, want ErrValidation", err)
	}
	if stored := listStored(t, repo, app.ID); len(stored) != 0 {
		t.Errorf("queued %d deployments, want 0", len(stored))
	}
}

func TestServiceDeployConflict(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app, createErr: ErrConflict}
	svc := newTestService(t, repo)

	if _, err := svc.Deploy(context.Background(), userID, app.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestServiceDeployQueuesDeployment(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	deployment, err := svc.Deploy(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if deployment.ID == uuid.Nil {
		t.Error("deployment id not assigned")
	}
	if deployment.State != StateQueued {
		t.Errorf("state = %s, want queued", deployment.State)
	}
	if deployment.Kind != KindDeploy {
		t.Errorf("kind = %s, want deploy", deployment.Kind)
	}
	if deployment.ApplicationID != app.ID {
		t.Errorf("application id = %s, want %s", deployment.ApplicationID, app.ID)
	}
}

func TestServiceListDeploymentsEmpty(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	svc := newTestService(t, &fakeRepository{app: app})

	deployments, err := svc.ListDeployments(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if deployments == nil {
		t.Error("list = nil, want an empty slice so the API always renders an array")
	}
	if len(deployments) != 0 {
		t.Errorf("len = %d, want 0", len(deployments))
	}
}

func TestServiceRollbackTarget(t *testing.T) {
	newRepo := func(t *testing.T) (*fakeRepository, Application, []Deployment) {
		t.Helper()
		userID := uuid.New()
		app := testApplication(userID)
		repo := &fakeRepository{app: app}
		seed := []Deployment{
			{Kind: KindDeploy, State: StateFailed, ImageTag: "gotham/app:1"},
			{Kind: KindDeploy, State: StateRunning, ImageTag: "gotham/app:2"},
			{Kind: KindDeploy, State: StateRunning, ImageTag: "gotham/app:3"},
		}
		created := make([]Deployment, 0, len(seed))
		for _, dep := range seed {
			created = append(created, seedDeployment(t, repo, app, dep))
		}
		return repo, app, created
	}

	t.Run("auto picks the previous release", func(t *testing.T) {
		repo, app, created := newRepo(t)
		svc := newTestService(t, repo)

		target, err := svc.rollbackTarget(context.Background(), app.ID, uuid.Nil)
		if err != nil {
			t.Fatalf("rollback target: %v", err)
		}
		if target.ID != created[1].ID {
			t.Errorf("target = %s (%s), want the previous release %s",
				target.ID, target.ImageTag, created[1].ID)
		}
	})

	t.Run("explicit deployment id wins", func(t *testing.T) {
		repo, app, created := newRepo(t)
		svc := newTestService(t, repo)

		target, err := svc.rollbackTarget(context.Background(), app.ID, created[2].ID)
		if err != nil {
			t.Fatalf("rollback target: %v", err)
		}
		if target.ID != created[2].ID {
			t.Errorf("target = %s, want %s", target.ID, created[2].ID)
		}
	})

	t.Run("single running release is reused", func(t *testing.T) {
		userID := uuid.New()
		app := testApplication(userID)
		repo := &fakeRepository{app: app}
		only := seedDeployment(t, repo, app, Deployment{
			Kind: KindDeploy, State: StateRunning, ImageTag: "gotham/app:only",
		})
		svc := newTestService(t, repo)

		target, err := svc.rollbackTarget(context.Background(), app.ID, uuid.Nil)
		if err != nil {
			t.Fatalf("rollback target: %v", err)
		}
		if target.ID != only.ID {
			t.Errorf("target = %s, want %s", target.ID, only.ID)
		}
	})

	t.Run("no successful deployment", func(t *testing.T) {
		userID := uuid.New()
		app := testApplication(userID)
		repo := &fakeRepository{app: app}
		seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateFailed})
		svc := newTestService(t, repo)

		if _, err := svc.rollbackTarget(context.Background(), app.ID, uuid.Nil); !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})
}

func TestServiceRollbackCopiesReleasedImage(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	target := seedDeployment(t, repo, app, Deployment{
		Kind:          KindDeploy,
		State:         StateRunning,
		ImageTag:      "gotham/app:2",
		RegistryImage: "127.0.0.1:5000/gotham/app:2",
		Digest:        "sha256:cafe",
	})
	svc := newTestService(t, repo)

	deployment, err := svc.Rollback(context.Background(), userID, app.ID, target.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if deployment.Kind != KindRollback {
		t.Errorf("kind = %s, want rollback", deployment.Kind)
	}
	if deployment.State != StateQueued {
		t.Errorf("state = %s, want queued", deployment.State)
	}
	if deployment.ImageTag != target.ImageTag {
		t.Errorf("image tag = %q, want %q", deployment.ImageTag, target.ImageTag)
	}
	if deployment.RegistryImage != target.RegistryImage {
		t.Errorf("registry image = %q, want %q", deployment.RegistryImage, target.RegistryImage)
	}
	if deployment.Digest != target.Digest {
		t.Errorf("digest = %q, want %q", deployment.Digest, target.Digest)
	}
	if deployment.RollbackFrom != target.ID {
		t.Errorf("rollback_from = %s, want %s", deployment.RollbackFrom, target.ID)
	}
}

func TestServiceRollbackRejectsUnreleasedTarget(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	target := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateFailed})
	svc := newTestService(t, repo)

	_, err := svc.Rollback(context.Background(), userID, app.ID, target.ID)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if stored := listStored(t, repo, app.ID); len(stored) != 1 {
		t.Errorf("stored %d deployments, want only the seeded one", len(stored))
	}
}

func TestServiceFeatureFlag(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"", true},
		{"   ", true},
		{"true", true},
		{"false", false},
		{"FALSE", false},
		{" false ", false},
		{"0", true},
	}
	for _, tc := range cases {
		t.Run("FEATURE_APPLICATIONS="+tc.value, func(t *testing.T) {
			t.Setenv(FeatureEnv, tc.value)
			if got := Enabled(); got != tc.want {
				t.Errorf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestServiceDisabled(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)
	t.Setenv(FeatureEnv, "false")

	if _, err := svc.Deploy(context.Background(), userID, app.ID); !errors.Is(err, ErrDisabled) {
		t.Errorf("deploy err = %v, want ErrDisabled", err)
	}
	if _, err := svc.Rollback(context.Background(), userID, app.ID, uuid.Nil); !errors.Is(err, ErrDisabled) {
		t.Errorf("rollback err = %v, want ErrDisabled", err)
	}
	if stored := listStored(t, repo, app.ID); len(stored) != 0 {
		t.Errorf("queued %d deployments while disabled, want 0", len(stored))
	}
}

func TestNewDefaultService(t *testing.T) {
	t.Run("no repository", func(t *testing.T) {
		if svc := NewDefaultService(Config{}); svc != nil {
			t.Error("service = non-nil, want nil without a database")
		}
	})

	t.Run("flag off", func(t *testing.T) {
		t.Setenv(FeatureEnv, "false")
		repo := &fakeRepository{app: testApplication(uuid.New())}
		if svc := NewDefaultService(Config{Repository: repo}); svc != nil {
			t.Error("service = non-nil, want nil while the feature is disabled")
		}
	})

	t.Run("configured", func(t *testing.T) {
		repo := &fakeRepository{app: testApplication(uuid.New())}
		svc := NewDefaultService(Config{Repository: repo, Secret: testSecretKey})
		if svc == nil {
			t.Fatal("service = nil, want a wired service")
		}
		if closer, ok := svc.(interface{ Close() error }); ok {
			t.Cleanup(func() { _ = closer.Close() })
		}
	})
}

func TestServiceWithoutRepository(t *testing.T) {
	svc := NewService(Config{})

	_, err := svc.Deploy(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("err = nil, want a configuration error")
	}
	if !strings.Contains(err.Error(), "repository is not configured") {
		t.Errorf("err = %v, want the repository configuration message", err)
	}
}

// TestServiceSweepsStaleDeployments covers the boot-time recovery sweep: a
// deployment abandoned by a previous control plane process can never resume,
// and its row would block every later deploy of the application through the
// active-deployment partial unique index. Running deployments are left alone.
func TestServiceSweepsStaleDeployments(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	stale := Deployment{
		ID:            uuid.New(),
		ApplicationID: app.ID,
		Kind:          KindDeploy,
		State:         StateBuilding,
	}
	running := Deployment{
		ID:            uuid.New(),
		ApplicationID: app.ID,
		Kind:          KindDeploy,
		State:         StateRunning,
	}
	repo := &fakeRepository{app: app, deployments: []Deployment{stale, running}}

	svc := NewService(Config{Repository: repo, Secret: testSecretKey, Logger: discardLogger()})
	t.Cleanup(func() { _ = svc.Close() })

	got, ok := repo.deployment(stale.ID)
	if !ok {
		t.Fatal("stale deployment disappeared from the repository")
	}
	if got.State != StateFailed {
		t.Errorf("stale state = %s, want %s", got.State, StateFailed)
	}
	if got.Error != staleDeploymentError {
		t.Errorf("stale error = %q, want %q", got.Error, staleDeploymentError)
	}
	if got.FinishedAt.IsZero() {
		t.Error("stale finished_at is zero, want a timestamp")
	}
	released, ok := repo.deployment(running.ID)
	if !ok {
		t.Fatal("running deployment disappeared from the repository")
	}
	if released.State != StateRunning {
		t.Errorf("running state = %s, want %s", released.State, StateRunning)
	}
}

// TestServiceSurvivesSweepFailure pins the contract that a failing recovery
// sweep is logged, not fatal: the control plane must still boot.
func TestServiceSurvivesSweepFailure(t *testing.T) {
	repo := &fakeRepository{failStaleErr: errors.New("database is down")}
	svc := NewService(Config{Repository: repo, Secret: testSecretKey, Logger: discardLogger()})
	t.Cleanup(func() { _ = svc.Close() })

	if svc == nil {
		t.Fatal("service = nil, want a service even when the sweep fails")
	}
}
