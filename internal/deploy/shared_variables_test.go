package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

func sealShared(t *testing.T, plain string) string {
	t.Helper()
	sealed, err := providers.SealSecret(testSecretKey, plain)
	if err != nil {
		t.Fatalf("seal shared secret: %v", err)
	}
	return sealed
}

func mergedEnv(t *testing.T, vars []EnvVar, secrets []Secret) map[string]string {
	t.Helper()
	out := make(map[string]string, len(vars)+len(secrets))
	for _, v := range vars {
		out[v.Key] = v.Value
	}
	for _, s := range secrets {
		opened, err := providers.OpenSecret(testSecretKey, s.Ciphertext)
		if err != nil {
			t.Fatalf("open merged secret %s: %v", s.Key, err)
		}
		out[s.Key] = opened
	}
	return out
}

func TestMergeSharedVariablesPrecedence(t *testing.T) {
	project := []SharedVariable{
		{Key: "SHARED", Value: "project"},
		{Key: "ENV_WINS", Value: "project"},
		{Key: "APP_WINS", Value: "project"},
		{Key: "SECRET_FLIP", Ciphertext: sealShared(t, "project-secret"), Secret: true},
		{Key: "PLAIN_FLIP", Value: "project-plain"},
	}
	environment := []SharedVariable{
		{Key: "ENV_WINS", Value: "environment"},
		{Key: "SECRET_FLIP", Value: "env-plain"},
		{Key: "PLAIN_FLIP", Ciphertext: sealShared(t, "env-secret"), Secret: true},
	}
	appVars := []EnvVar{{Key: "APP_WINS", Value: "application"}}
	appSecrets := []Secret{{Key: "APP_SECRET", Ciphertext: sealShared(t, "app-secret")}}

	vars, secrets := mergeSharedVariables(project, environment, appVars, appSecrets)
	got := mergedEnv(t, vars, secrets)
	want := map[string]string{
		"SHARED":      "project",
		"ENV_WINS":    "environment",
		"APP_WINS":    "application",
		"SECRET_FLIP": "env-plain",
		"PLAIN_FLIP":  "env-secret",
		"APP_SECRET":  "app-secret",
	}
	if len(got) != len(want) {
		t.Fatalf("merged = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("merged[%s] = %q, want %q", key, got[key], value)
		}
	}
	for _, s := range secrets {
		if s.Key == "PLAIN_FLIP" && s.Ciphertext != environment[2].Ciphertext {
			t.Errorf("shared ciphertext was re-sealed: %q", s.Ciphertext)
		}
	}
}

func TestMergeApplicationOverridesShared(t *testing.T) {
	sealed := sealShared(t, "shared-secret")
	vars, secrets := mergeSharedVariables(
		[]SharedVariable{
			{Key: "HIDE_SECRET", Ciphertext: sealed, Secret: true},
			{Key: "HIDE_PLAIN", Value: "shared-plain"},
		},
		nil,
		[]EnvVar{{Key: "HIDE_SECRET", Value: "app-plain"}},
		[]Secret{{Key: "HIDE_PLAIN", Ciphertext: sealShared(t, "app-secret")}},
	)
	got := mergedEnv(t, vars, secrets)
	if got["HIDE_SECRET"] != "app-plain" || got["HIDE_PLAIN"] != "app-secret" {
		t.Fatalf("merged = %v, want the application values to win", got)
	}
	if len(vars) != 1 || len(secrets) != 1 {
		t.Fatalf("vars = %+v secrets = %+v, want one of each", vars, secrets)
	}
}

func TestMergeSharedVariablesEmptyScopes(t *testing.T) {
	appVars := []EnvVar{{Key: "FOO", Value: "bar"}}
	appSecrets := []Secret{{Key: "TOKEN", Ciphertext: sealShared(t, "hunter2")}}
	vars, secrets := mergeSharedVariables(nil, nil, appVars, appSecrets)
	if len(vars) != 1 || vars[0] != appVars[0] {
		t.Fatalf("vars = %+v, want %+v", vars, appVars)
	}
	if len(secrets) != 1 || secrets[0].Ciphertext != appSecrets[0].Ciphertext {
		t.Fatalf("secrets = %+v, want the application secret untouched", secrets)
	}
}

func seedSharedApp(app Application, projectID uuid.UUID, project, environment []SharedVariable, repo *fakeRepository) Application {
	app.ProjectID = projectID
	repo.sharedProject = map[uuid.UUID][]SharedVariable{projectID: project}
	repo.sharedEnv = map[uuid.UUID][]SharedVariable{app.EnvironmentID: environment}
	return app
}

func runToStart(t *testing.T, repo *fakeRepository, app Application) []string {
	t.Helper()
	node := newMockNode()
	dep := seedDeployment(t, repo, app, Deployment{Kind: KindDeploy})
	o := newTestOrchestrator(Config{Repository: repo, Source: &fakeSource{}, Dial: dialAlways(node)})
	o.run(context.Background(), job{app: app, dep: dep})
	if stored, _ := repo.deployment(dep.ID); stored.State != StateRunning {
		t.Fatalf("state = %s, want running", stored.State)
	}
	if node.runCalls != 1 || len(node.requests) != 1 {
		t.Fatalf("run calls = %d, want 1", node.runCalls)
	}
	return node.requests[0].GetEnv()
}

func TestDeployMergesSharedVariables(t *testing.T) {
	projectID := uuid.New()
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	envVars, secrets := testEnv(t)
	repo.envVars, repo.secrets = envVars, secrets
	app = seedSharedApp(app, projectID,
		[]SharedVariable{
			{Key: "SHARED", Value: "project"},
			{Key: "OVERRIDDEN", Value: "project"},
			{Key: "DB_PASSWORD", Ciphertext: sealShared(t, "project-db-pass"), Secret: true},
		},
		[]SharedVariable{
			{Key: "OVERRIDDEN", Value: "environment"},
			{Key: "ENV_ONLY", Value: "yes"},
		}, repo)

	env := strings.Join(runToStart(t, repo, app), "\n")
	for _, want := range []string{
		"FOO=bar", "API_TOKEN=hunter2",
		"SHARED=project", "OVERRIDDEN=environment", "ENV_ONLY=yes",
		"DB_PASSWORD=project-db-pass",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("run env = %q, want %q", env, want)
		}
	}
}

func TestPreviewDeployMergesSharedVariables(t *testing.T) {
	projectID := uuid.New()
	app := testApplication(uuid.New())
	app.IsPreview = true
	repo := &fakeRepository{app: app}
	app = seedSharedApp(app, projectID,
		[]SharedVariable{{Key: "SHARED", Value: "project"}},
		[]SharedVariable{{Key: "ENV_ONLY", Value: "yes"}}, repo)

	env := strings.Join(runToStart(t, repo, app), "\n")
	for _, want := range []string{"SHARED=project", "ENV_ONLY=yes"} {
		if !strings.Contains(env, want) {
			t.Errorf("preview run env = %q, want %q", env, want)
		}
	}
}

func TestSharedSecretWithBrokenCiphertextFailsClosed(t *testing.T) {
	projectID := uuid.New()
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	app = seedSharedApp(app, projectID,
		[]SharedVariable{{Key: "DB_PASSWORD", Ciphertext: "not-a-sealed-value", Secret: true}},
		nil, repo)

	vars, secrets := mergeSharedVariables(
		repo.sharedProject[projectID], repo.sharedEnv[app.EnvironmentID], nil, nil)
	if len(secrets) != 1 || secrets[0].Ciphertext != "not-a-sealed-value" {
		t.Fatalf("merge must pass the broken ciphertext through, got %+v", secrets)
	}
	_ = vars
	dep := Deployment{
		ID:            uuid.New(),
		ApplicationID: app.ID,
		Kind:          KindDeploy,
		State:         StateStarting,
		ImageTag:      "gotham/app:tag",
		RegistryImage: "127.0.0.1:5000/gotham/app:tag",
	}
	if _, err := buildRunRequest(app, dep, vars, secrets, nil, testSecretKey, nil, 0); err == nil {
		t.Fatal("buildRunRequest with a broken shared secret succeeded, want ErrValidation")
	} else if got := err.Error(); strings.Contains(got, "not-a-sealed-value") || !strings.Contains(got, "DB_PASSWORD") {
		t.Fatalf("error = %q, want the key name without the ciphertext", got)
	}
}

func TestListSharedVariablesRejectsZeroScope(t *testing.T) {
	repo := &storeRepository{}
	if _, _, err := repo.ListSharedVariables(context.Background(), uuid.Nil, uuid.New()); !errors.Is(err, ErrValidation) {
		t.Fatalf("zero project = %v, want ErrValidation", err)
	}
	if _, _, err := repo.ListSharedVariables(context.Background(), uuid.New(), uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("zero environment = %v, want ErrValidation", err)
	}
}
