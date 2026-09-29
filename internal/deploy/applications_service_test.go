package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/teams"
)

// validCreateInput returns a payload every creation test starts from: a known
// server, a cloneable repository and one plain variable plus one sealed secret.
func validCreateInput(serverID uuid.UUID) CreateApplicationInput {
	return CreateApplicationInput{
		Name:       "demo app",
		Provider:   "github",
		Repo:       "acme/demo",
		CloneURL:   "https://github.com/acme/demo.git",
		Branch:     "main",
		BuildPack:  "dockerfile",
		BaseDomain: "demo.example.com",
		Port:       3000,
		HostPort:   8080,
		ServerID:   serverID,
		Env: []EnvEntry{
			{Key: "NODE_ENV", Value: "production"},
			{Key: "API_TOKEN", Value: "secret:super-secret"},
		},
		Storage: []Storage{{Name: "data", HostPath: "/data/app", ContainerPath: "/var/lib/app"}},
	}
}

// newNodeService builds a service whose agent dialer always hands out node.
func newNodeService(t *testing.T, repo *fakeRepository, node Node) *Service {
	t.Helper()
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Dial:       dialAlways(node),
	})
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func TestServiceCreateApplication(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	svc := newTestService(t, repo)
	serverID := uuid.New()

	in := validCreateInput(serverID)
	in.Name = "  demo app  "
	in.Branch = ""    // defaults to "main"
	in.BuildPack = "" // auto-detection
	in.Storage = []Storage{{Name: "data", HostPath: " /data/app ", ContainerPath: " /var/lib/app "}}

	created, err := svc.CreateApplication(context.Background(), userID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == uuid.Nil || created.UserID != userID {
		t.Fatalf("created = %+v, want an id and owner %s", created, userID)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("created_at/updated_at not assigned")
	}
	if created.Name != "demo app" {
		t.Errorf("name = %q, want the trimmed %q", created.Name, "demo app")
	}
	if created.Branch != "main" {
		t.Errorf("branch = %q, want the default branch", created.Branch)
	}
	if created.ServerID != serverID {
		t.Errorf("server_id = %s, want %s", created.ServerID, serverID)
	}

	t.Run("stores plain env vars and seals secret values", func(t *testing.T) {
		variables, err := repo.ListEnvVars(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("list env vars: %v", err)
		}
		if len(variables) != 1 || variables[0].Key != "NODE_ENV" || variables[0].Value != "production" {
			t.Fatalf("env vars = %+v, want only the plain NODE_ENV row", variables)
		}
		secrets, err := repo.ListSecrets(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("list secrets: %v", err)
		}
		if len(secrets) != 1 || secrets[0].Key != "API_TOKEN" {
			t.Fatalf("secrets = %+v, want one API_TOKEN row", secrets)
		}
		plain, err := providers.OpenSecret(testSecretKey, secrets[0].Ciphertext)
		if err != nil || plain != "super-secret" {
			t.Errorf("opened = %q, %v; want the original plaintext", plain, err)
		}
		if strings.Contains(secrets[0].Ciphertext, "super-secret") {
			t.Error("ciphertext contains the plaintext")
		}
	})

	t.Run("trims storage paths", func(t *testing.T) {
		storages, err := repo.ListStorages(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("list storages: %v", err)
		}
		if len(storages) != 1 {
			t.Fatalf("storages = %d, want 1", len(storages))
		}
		if storages[0].HostPath != "/data/app" || storages[0].ContainerPath != "/var/lib/app" {
			t.Errorf("paths = %q → %q, want trimmed absolute paths",
				storages[0].HostPath, storages[0].ContainerPath)
		}
	})

	t.Run("answers with the stored environment", func(t *testing.T) {
		entries, err := svc.GetEnv(context.Background(), userID, created.ID)
		if err != nil {
			t.Fatalf("get env: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("entries = %+v, want the plain variable and the secret reference", entries)
		}
		if entries[0].Key != "API_TOKEN" || !strings.HasPrefix(entries[0].Value, secretRefPrefix) {
			t.Errorf("secret entry = %+v, want a secret: reference", entries[0])
		}
		if entries[1].Key != "NODE_ENV" || entries[1].Value != "production" {
			t.Errorf("plain entry = %+v, want NODE_ENV=production", entries[1])
		}
	})
}

func TestServiceCreateApplicationValidation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*CreateApplicationInput, *fakeRepository, uuid.UUID)
		wantErr error
	}{
		{
			name:    "missing name",
			mutate:  func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) { in.Name = "   " },
			wantErr: ErrValidation,
		},
		{
			name: "unsupported clone scheme",
			mutate: func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) {
				in.CloneURL = "ftp://example.com/repo.git"
			},
			wantErr: ErrValidation,
		},
		{
			name:    "missing clone URL",
			mutate:  func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) { in.CloneURL = "" },
			wantErr: ErrValidation,
		},
		{
			name:    "unknown build pack",
			mutate:  func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) { in.BuildPack = "cobol" },
			wantErr: ErrValidation,
		},
		{
			name:    "negative port",
			mutate:  func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) { in.Port = -1 },
			wantErr: ErrValidation,
		},
		{
			name:    "port above 65535",
			mutate:  func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) { in.Port = 65536 },
			wantErr: ErrValidation,
		},
		{
			name:    "host port above 65535",
			mutate:  func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) { in.HostPort = 70000 },
			wantErr: ErrValidation,
		},
		{
			name:    "no server assigned",
			mutate:  func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) { in.ServerID = uuid.Nil },
			wantErr: ErrValidation,
		},
		{
			name: "unknown server",
			mutate: func(in *CreateApplicationInput, repo *fakeRepository, serverID uuid.UUID) {
				if repo.unknownServers == nil {
					repo.unknownServers = map[uuid.UUID]bool{}
				}
				repo.unknownServers[serverID] = true
			},
			wantErr: ErrServerNotFound,
		},
		{
			name: "duplicate env key",
			mutate: func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) {
				in.Env = append(in.Env, EnvEntry{Key: "NODE_ENV", Value: "test"})
			},
			wantErr: ErrValidation,
		},
		{
			name: "empty env key",
			mutate: func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) {
				in.Env = append(in.Env, EnvEntry{Key: " ", Value: "value"})
			},
			wantErr: ErrValidation,
		},
		{
			name: "env key with equals sign",
			mutate: func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) {
				in.Env = append(in.Env, EnvEntry{Key: "A=B", Value: "value"})
			},
			wantErr: ErrValidation,
		},
		{
			name: "empty secret value",
			mutate: func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) {
				in.Env = append(in.Env, EnvEntry{Key: "EMPTY", Value: "secret:"})
			},
			wantErr: ErrValidation,
		},
		{
			name: "unknown secret reference",
			mutate: func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) {
				in.Env = append(in.Env, EnvEntry{Key: "STALE", Value: secretRefPrefix + uuid.New().String()})
			},
			wantErr: ErrValidation,
		},
		{
			name: "storage with a relative host path",
			mutate: func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) {
				in.Storage = []Storage{{Name: "data", HostPath: "data", ContainerPath: "/var/lib/app"}}
			},
			wantErr: ErrValidation,
		},
		{
			name: "storage without a container path",
			mutate: func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) {
				in.Storage = []Storage{{Name: "data", HostPath: "/data"}}
			},
			wantErr: ErrValidation,
		},
		{
			name: "duplicate storage name",
			mutate: func(in *CreateApplicationInput, _ *fakeRepository, _ uuid.UUID) {
				in.Storage = append(in.Storage, Storage{Name: "data", HostPath: "/other", ContainerPath: "/var/lib/other"})
			},
			wantErr: ErrValidation,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID := uuid.New()
			serverID := uuid.New()
			repo := &fakeRepository{}
			svc := newTestService(t, repo)

			in := validCreateInput(serverID)
			tc.mutate(&in, repo, serverID)

			_, err := svc.CreateApplication(context.Background(), userID, in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if applications, _ := repo.ListApplications(context.Background(), teams.Scope{UserID: userID}); len(applications) != 0 {
				t.Errorf("stored %d applications, want none", len(applications))
			}
		})
	}
}

func TestServiceCreateApplicationRejectsDuplicateName(t *testing.T) {
	userID := uuid.New()
	serverID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)

	in := validCreateInput(serverID)
	in.Name = app.Name

	if _, err := svc.CreateApplication(context.Background(), userID, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for a name already in use", err)
	}
}

func TestServiceListApplications(t *testing.T) {
	userID := uuid.New()
	otherUser := uuid.New()
	repo := &fakeRepository{app: testApplication(userID)}
	svc := newTestService(t, repo)

	for _, name := range []string{"second", "third"} {
		in := validCreateInput(uuid.New())
		in.Name = name
		if _, err := svc.CreateApplication(context.Background(), userID, in); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	foreign := validCreateInput(uuid.New())
	foreign.Name = "someone else's app"
	if _, err := svc.CreateApplication(context.Background(), otherUser, foreign); err != nil {
		t.Fatalf("create foreign: %v", err)
	}

	applications, err := svc.ListApplications(context.Background(), userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(applications) != 3 {
		t.Fatalf("applications = %d, want the three owned by the caller", len(applications))
	}
	if applications[0].Name != "third" || applications[1].Name != "second" || applications[2].Name != "demo app" {
		t.Errorf("order = %q, %q, %q; want newest first",
			applications[0].Name, applications[1].Name, applications[2].Name)
	}
	for _, application := range applications {
		if application.UserID != userID {
			t.Errorf("application %q belongs to %s, want %s", application.Name, application.UserID, userID)
		}
	}
}

func TestServiceApplicationOwnership(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)
	otherUser := uuid.New()
	name := "renamed"

	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"get", func() error {
			_, err := svc.GetApplication(context.Background(), otherUser, app.ID)
			return err
		}},
		{"update", func() error {
			_, err := svc.UpdateApplication(context.Background(), otherUser, app.ID, UpdateApplicationInput{Name: &name})
			return err
		}},
		{"delete", func() error {
			return svc.DeleteApplication(context.Background(), otherUser, app.ID)
		}},
		{"get env", func() error {
			_, err := svc.GetEnv(context.Background(), otherUser, app.ID)
			return err
		}},
		{"replace env", func() error {
			_, err := svc.ReplaceEnv(context.Background(), otherUser, app.ID, nil)
			return err
		}},
		{"get storages", func() error {
			_, err := svc.GetStorages(context.Background(), otherUser, app.ID)
			return err
		}},
		{"replace storages", func() error {
			_, err := svc.ReplaceStorages(context.Background(), otherUser, app.ID, nil)
			return err
		}},
		{"stop", func() error {
			_, err := svc.Stop(context.Background(), otherUser, app.ID)
			return err
		}},
		{"start", func() error {
			_, err := svc.Start(context.Background(), otherUser, app.ID)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestServiceUpdateApplication(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	svc := newTestService(t, repo)
	name := "renamed app"
	branch := "release"
	port := int32(9090)
	newServer := uuid.New()

	updated, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{
		Name:   &name,
		Branch: &branch,
		Port:   &port,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != name || updated.Branch != branch || updated.Port != port {
		t.Errorf("updated = %+v, want the patched fields applied", updated)
	}
	if updated.CloneURL != app.CloneURL || updated.HostPort != app.HostPort {
		t.Errorf("unpatched fields changed: %+v", updated)
	}

	t.Run("clears the server assignment", func(t *testing.T) {
		cleared := uuid.Nil
		updated, err := svc.UpdateApplication(context.Background(), userID, app.ID, UpdateApplicationInput{ServerID: &cleared})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if updated.ServerID != uuid.Nil {
			t.Errorf("server_id = %s, want it cleared", updated.ServerID)
		}
	})

	t.Run("rejects an unknown server", func(t *testing.T) {
		if repo.unknownServers == nil {
			repo.unknownServers = map[uuid.UUID]bool{}
		}
		repo.unknownServers[newServer] = true
		if _, err := svc.UpdateApplication(context.Background(), userID, app.ID,
			UpdateApplicationInput{ServerID: &newServer}); !errors.Is(err, ErrServerNotFound) {
			t.Fatalf("err = %v, want ErrServerNotFound", err)
		}
	})

	t.Run("rejects an empty patch", func(t *testing.T) {
		if _, err := svc.UpdateApplication(context.Background(), userID, app.ID,
			UpdateApplicationInput{}); !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})

	t.Run("rejects invalid values", func(t *testing.T) {
		badPort := int32(70000)
		if _, err := svc.UpdateApplication(context.Background(), userID, app.ID,
			UpdateApplicationInput{Port: &badPort}); !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		badPack := "cobol"
		if _, err := svc.UpdateApplication(context.Background(), userID, app.ID,
			UpdateApplicationInput{BuildPack: &badPack}); !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})
}

func TestServiceDeleteApplication(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	node := newMockNode()
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: node.containerID})
	svc := newNodeService(t, repo, node)

	if err := svc.DeleteApplication(context.Background(), userID, app.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if node.stopCalls != 1 || len(node.stopped) != 1 || node.stopped[0] != node.containerID {
		t.Errorf("stopped = %v (%d calls), want the running container", node.stopped, node.stopCalls)
	}
	if _, err := svc.GetApplication(context.Background(), userID, app.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after delete err = %v, want ErrNotFound", err)
	}
}

func TestServiceDeleteApplicationWithoutNode(t *testing.T) {
	userID := uuid.New()
	app := testApplication(userID)
	repo := &fakeRepository{app: app}
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		Dial: func(context.Context, uuid.UUID) (Node, error) {
			return nil, ErrAgentUnavailable
		},
	})
	t.Cleanup(func() { _ = svc.Close() })
	seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: "abc"})

	if err := svc.DeleteApplication(context.Background(), userID, app.ID); err != nil {
		t.Fatalf("delete must not depend on the node: %v", err)
	}
	if _, err := svc.GetApplication(context.Background(), userID, app.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after delete err = %v, want ErrNotFound", err)
	}
}

func TestServiceReplaceEnv(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	svc := newTestService(t, repo)
	serverID := uuid.New()

	in := validCreateInput(serverID)
	created, err := svc.CreateApplication(context.Background(), userID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before, err := svc.GetEnv(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("get env: %v", err)
	}
	reference := ""
	for _, entry := range before {
		if entry.Key == "API_TOKEN" {
			reference = entry.Value
		}
	}
	if reference == "" {
		t.Fatal("the created secret has no reference")
	}
	stored, err := repo.ListSecrets(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}
	ciphertext := stored[0].Ciphertext

	after, err := svc.ReplaceEnv(context.Background(), userID, created.ID, []EnvEntry{
		{Key: "API_TOKEN", Value: reference}, // round-trip: re-seal nothing
		{Key: "LOG_LEVEL", Value: "debug"},
	})
	if err != nil {
		t.Fatalf("replace env: %v", err)
	}
	if len(after) != 2 || after[0].Key != "API_TOKEN" || after[1].Key != "LOG_LEVEL" {
		t.Fatalf("entries = %+v, want the replaced collection", after)
	}
	if after[0].Value != reference {
		t.Errorf("reference = %q, want the stable %q", after[0].Value, reference)
	}
	replaced, err := repo.ListSecrets(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}
	if len(replaced) != 1 || replaced[0].Ciphertext != ciphertext {
		t.Errorf("the secret was re-sealed on round-trip: %+v", replaced)
	}
	variables, err := repo.ListEnvVars(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("list env vars: %v", err)
	}
	if len(variables) != 1 || variables[0].Key != "LOG_LEVEL" {
		t.Errorf("env vars = %+v, want only the new plain row", variables)
	}

	t.Run("clears the collection", func(t *testing.T) {
		entries, err := svc.ReplaceEnv(context.Background(), userID, created.ID, nil)
		if err != nil {
			t.Fatalf("replace env: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("entries = %+v, want an empty environment", entries)
		}
		remaining, err := repo.ListSecrets(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("list secrets: %v", err)
		}
		if len(remaining) != 0 {
			t.Errorf("secrets = %+v, want them cleared with the collection", remaining)
		}
	})

	t.Run("rejects an unknown reference", func(t *testing.T) {
		_, err := svc.ReplaceEnv(context.Background(), userID, created.ID, []EnvEntry{
			{Key: "API_TOKEN", Value: secretRefPrefix + uuid.New().String()},
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})
}

func TestServiceReplaceStorages(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepository{}
	svc := newTestService(t, repo)

	created, err := svc.CreateApplication(context.Background(), userID, validCreateInput(uuid.New()))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	storages, err := svc.ReplaceStorages(context.Background(), userID, created.ID, []Storage{
		{Name: "cache", HostPath: "/data/cache", ContainerPath: "/var/cache"},
	})
	if err != nil {
		t.Fatalf("replace storages: %v", err)
	}
	if len(storages) != 1 || storages[0].Name != "cache" {
		t.Fatalf("storages = %+v, want the replaced collection", storages)
	}

	if _, err := svc.ReplaceStorages(context.Background(), userID, created.ID, nil); err != nil {
		t.Fatalf("clear storages: %v", err)
	}
	cleared, err := svc.GetStorages(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("get storages: %v", err)
	}
	if len(cleared) != 0 {
		t.Errorf("storages = %+v, want an empty volume map", cleared)
	}

	t.Run("rejects a relative path", func(t *testing.T) {
		_, err := svc.ReplaceStorages(context.Background(), userID, created.ID, []Storage{
			{Name: "cache", HostPath: "cache", ContainerPath: "/var/cache"},
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})
}

func TestServiceStopStart(t *testing.T) {
	newFixture := func(t *testing.T, states ...State) (*Service, *fakeRepository, *mockNode, Application) {
		t.Helper()
		userID := uuid.New()
		app := testApplication(userID)
		repo := &fakeRepository{app: app}
		node := newMockNode()
		// The service is built before the deployments are seeded: NewService
		// sweeps non-terminal rows (a queued deployment left by a dead control
		// plane), and the in-flight case below must survive that sweep.
		svc := newNodeService(t, repo, node)
		for _, state := range states {
			seedDeployment(t, repo, app, Deployment{
				Kind:        KindDeploy,
				State:       state,
				ContainerID: node.containerID,
			})
		}
		return svc, repo, node, app
	}

	t.Run("stops the newest deployment's container", func(t *testing.T) {
		svc, _, node, app := newFixture(t, StateRunning, StateFailed)

		deployment, err := svc.Stop(context.Background(), app.UserID, app.ID)
		if err != nil {
			t.Fatalf("stop: %v", err)
		}
		if deployment.State != StateFailed {
			t.Errorf("target state = %s, want the newest deployment", deployment.State)
		}
		if node.stopCalls != 1 || node.stopped[0] != node.containerID {
			t.Errorf("stopped = %v (%d calls), want exactly the current container", node.stopped, node.stopCalls)
		}
	})

	t.Run("starts the newest deployment's container", func(t *testing.T) {
		svc, _, node, app := newFixture(t, StateRunning)

		if _, err := svc.Start(context.Background(), app.UserID, app.ID); err != nil {
			t.Fatalf("start: %v", err)
		}
		if node.startCalls != 1 || node.started[0] != node.containerID {
			t.Errorf("started = %v (%d calls), want exactly the current container", node.started, node.startCalls)
		}
	})

	t.Run("answers 404 without a container", func(t *testing.T) {
		svc, _, _, app := newFixture(t)

		for _, run := range []struct {
			name string
			call func() (Deployment, error)
		}{
			{"stop", func() (Deployment, error) { return svc.Stop(context.Background(), app.UserID, app.ID) }},
			{"start", func() (Deployment, error) { return svc.Start(context.Background(), app.UserID, app.ID) }},
		} {
			if _, err := run.call(); !errors.Is(err, ErrNotFound) {
				t.Errorf("%s err = %v, want ErrNotFound", run.name, err)
			}
		}
	})

	t.Run("answers 409 while a deployment is in flight", func(t *testing.T) {
		svc, _, _, app := newFixture(t, StateRunning, StateQueued)

		if _, err := svc.Stop(context.Background(), app.UserID, app.ID); !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
	})

	t.Run("answers 502 when the agent is unreachable", func(t *testing.T) {
		userID := uuid.New()
		app := testApplication(userID)
		repo := &fakeRepository{app: app}
		seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: "abc"})
		svc := NewService(Config{
			Repository: repo,
			Secret:     testSecretKey,
			Logger:     discardLogger(),
			Dial: func(context.Context, uuid.UUID) (Node, error) {
				return nil, ErrAgentUnavailable
			},
		})
		t.Cleanup(func() { _ = svc.Close() })

		if _, err := svc.Stop(context.Background(), userID, app.ID); !errors.Is(err, ErrAgentUnavailable) {
			t.Fatalf("err = %v, want ErrAgentUnavailable", err)
		}
	})

	t.Run("answers 404 without a server", func(t *testing.T) {
		userID := uuid.New()
		app := testApplication(userID)
		app.ServerID = uuid.Nil
		repo := &fakeRepository{app: app}
		seedDeployment(t, repo, app, Deployment{Kind: KindDeploy, State: StateRunning, ContainerID: "abc"})
		svc := newNodeService(t, repo, newMockNode())

		if _, err := svc.Stop(context.Background(), userID, app.ID); !errors.Is(err, ErrServerNotFound) {
			t.Fatalf("err = %v, want ErrServerNotFound", err)
		}
	})

	t.Run("keeps the deployment row untouched", func(t *testing.T) {
		svc, repo, node, app := newFixture(t, StateRunning)

		if _, err := svc.Stop(context.Background(), app.UserID, app.ID); err != nil {
			t.Fatalf("stop: %v", err)
		}
		deployments, err := repo.ListDeployments(context.Background(), app.ID)
		if err != nil {
			t.Fatalf("list deployments: %v", err)
		}
		if deployments[0].State != StateRunning {
			t.Errorf("state = %s, want the release state preserved so rollback still finds it", deployments[0].State)
		}
		if node.startCalls != 0 {
			t.Errorf("start called %d times, want none", node.startCalls)
		}
	})
}
