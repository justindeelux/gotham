package databases

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

// newTestService wires a service over the fakes with a short health window, so
// failure paths resolve in milliseconds instead of engine timeouts.
func newTestService(repo *fakeRepository, cs *fakeContainers) *Service {
	return NewService(Config{
		Repository:    repo,
		Containers:    cs,
		Secret:        testSecret,
		Logger:        discardLogger(),
		HealthTimeout: 300 * time.Millisecond,
		HealthPoll:    2 * time.Millisecond,
	})
}

// TestCreateEachEngine provisions every supported engine and asserts the same
// contract: a running row, a container payload carrying the image, the sealed
// environment and the volume, and credentials that are readable but never
// stored in the clear.
func TestCreateEachEngine(t *testing.T) {
	for _, engineName := range EngineNames() {
		t.Run(engineName, func(t *testing.T) {
			repo := newFakeRepository()
			serverID := repo.seedServer()
			cs := &fakeContainers{runID: "container-" + engineName}
			svc := newTestService(repo, cs)
			userID := uuid.New()

			database, credentials, err := svc.Create(context.Background(), userID, CreateRequest{
				Name:       "db-" + engineName,
				Engine:     engineName,
				Version:    "",
				ServerID:   serverID,
				PublicPort: 20000,
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			if database.UserID != userID {
				t.Errorf("user_id = %s, want %s", database.UserID, userID)
			}
			if database.Status != StatusRunning {
				t.Errorf("status = %q, want running", database.Status)
			}
			if database.Engine != engineName {
				t.Errorf("engine = %q, want %q", database.Engine, engineName)
			}
			if database.ContainerID != "container-"+engineName {
				t.Errorf("container_id = %q", database.ContainerID)
			}
			if database.StoragePath != VolumeName(database.ID) {
				t.Errorf("storage_path = %q, want %q", database.StoragePath, VolumeName(database.ID))
			}
			if database.PublicPort != 20000 {
				t.Errorf("public_port = %d, want 20000", database.PublicPort)
			}

			run := cs.lastRun()
			engine, _ := LookupEngine(engineName)
			if run.Image != engine.Image("") {
				t.Errorf("image = %q, want %q", run.Image, engine.Image(""))
			}
			mount := engine.VolumeSpec().MountPath
			if len(run.Volumes) != 1 || run.Volumes[0] != database.StoragePath+":"+mount {
				t.Errorf("volumes = %v, want [%s:%s]", run.Volumes, database.StoragePath, mount)
			}
			wantPort := engine.PortSpec().Internal
			wantPortSpec := "20000:" + strconv.FormatInt(int64(wantPort), 10)
			if len(run.Ports) != 1 || run.Ports[0] != wantPortSpec {
				t.Errorf("ports = %v, want [%s]", run.Ports, wantPortSpec)
			}
			if run.Labels[labelDatabaseID] != database.ID.String() {
				t.Errorf("labels = %v, want the database id label", run.Labels)
			}

			// The payload carries the plaintext credential exactly once...
			if !envContains(run.Env, credentials.Password) {
				t.Error("the run payload does not carry the generated password")
			}
			// ...while the repository holds ciphertext only.
			secrets, err := repo.ListSecrets(context.Background(), database.ID)
			if err != nil {
				t.Fatalf("ListSecrets: %v", err)
			}
			if len(secrets) == 0 {
				t.Fatal("no credentials were stored")
			}
			for _, secret := range secrets {
				if strings.Contains(secret.Ciphertext, credentials.Password) {
					t.Errorf("secret %q stores the password in the clear", secret.Key)
				}
			}
			opened, err := openCredentials(testSecret, secrets)
			if err != nil {
				t.Fatalf("openCredentials: %v", err)
			}
			if opened.Password != credentials.Password || opened.Username != credentials.Username {
				t.Errorf("opened credentials = %+v, want %+v", opened, credentials)
			}
			if engineName == EngineMySQL || engineName == EngineMariaDB {
				if credentials.RootPassword == "" {
					t.Error("mysql/mariadb must generate a root password")
				}
				if !envContains(run.Env, credentials.RootPassword) {
					t.Error("the run payload does not carry the root password")
				}
			} else if credentials.RootPassword != "" {
				t.Errorf("%s should not generate a root password", engineName)
			}
		})
	}
}

// TestCreateInternalDatabasePublishesNoPort: an internal database has no host
// binding at all.
func TestCreateInternalDatabasePublishesNoPort(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)

	_, _, err := svc.Create(context.Background(), uuid.New(), CreateRequest{
		Name:     "cache",
		Engine:   EngineRedis,
		ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ports := cs.lastRun().Ports; len(ports) != 0 {
		t.Errorf("ports = %v, want none", ports)
	}
}

// TestCreateValidation covers every rejected request shape.
func TestCreateValidation(t *testing.T) {
	// serverMode decides which server the request points at: at the default
	// (valid) the failure under test is not the server resolution.
	const (
		serverValid   = "valid"
		serverAbsent  = "absent"
		serverUnknown = "unknown"
	)

	tests := []struct {
		name       string
		req        CreateRequest
		serverMode string
		disable    bool
		want       error
	}{
		{
			name: "unknown engine",
			req:  CreateRequest{Name: "db", Engine: "oracle"},
			want: ErrValidation,
		},
		{
			name: "missing engine",
			req:  CreateRequest{Name: "db"},
			want: ErrValidation,
		},
		{
			name: "empty name",
			req:  CreateRequest{Name: "  ", Engine: EnginePostgres},
			want: ErrValidation,
		},
		{
			name: "name with spaces",
			req:  CreateRequest{Name: "my db", Engine: EnginePostgres},
			want: ErrValidation,
		},
		{
			name: "unsafe version",
			req:  CreateRequest{Name: "db", Engine: EnginePostgres, Version: "16:alpine"},
			want: ErrValidation,
		},
		{
			name:       "missing server",
			req:        CreateRequest{Name: "db", Engine: EnginePostgres},
			serverMode: serverAbsent,
			want:       ErrValidation,
		},
		{
			name:       "unknown server",
			req:        CreateRequest{Name: "db", Engine: EnginePostgres},
			serverMode: serverUnknown,
			want:       ErrServerNotFound,
		},
		{
			name: "public port too large",
			req:  CreateRequest{Name: "db", Engine: EnginePostgres, PublicPort: 70000},
			want: ErrValidation,
		},
		{
			name: "negative public port",
			req:  CreateRequest{Name: "db", Engine: EnginePostgres, PublicPort: -1},
			want: ErrValidation,
		},
		{
			name:    "feature disabled",
			req:     CreateRequest{Name: "db", Engine: EnginePostgres},
			disable: true,
			want:    ErrDisabled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			serverID := repo.seedServer()
			svc := newTestService(repo, &fakeContainers{})
			if tt.disable {
				t.Setenv(FeatureEnv, "false")
			}

			req := tt.req
			switch tt.serverMode {
			case serverAbsent:
				req.ServerID = uuid.Nil
			case serverUnknown:
				req.ServerID = uuid.New() // never seeded
			default:
				req.ServerID = serverID
			}

			_, _, err := svc.Create(context.Background(), uuid.New(), req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Create error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestCreateRejectsDuplicateName: the partial unique index answers 409.
func TestCreateRejectsDuplicateName(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)
	userID := uuid.New()

	if _, _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "orders", Engine: EngineMySQL, ServerID: serverID,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Create error = %v, want ErrConflict", err)
	}
}

// TestCreateAgentFailureKeepsAnErrorRow: a failed provision leaves a visible
// row in the error state instead of silently vanishing.
func TestCreateAgentFailureKeepsAnErrorRow(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runErr: containers.ErrAgentUnavailable}
	svc := newTestService(repo, cs)
	userID := uuid.New()

	_, _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	})
	if !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("Create error = %v, want ErrAgentUnavailable", err)
	}

	list, err := svc.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("databases = %d, want the failed row to be visible", len(list))
	}
	if list[0].Status != StatusError {
		t.Errorf("status = %q, want error", list[0].Status)
	}
	if list[0].ContainerID != "" {
		t.Errorf("container_id = %q, want empty when the run failed", list[0].ContainerID)
	}
}

// TestCreateHealthcheckTimeout: a container that never reports running fails
// the provision with ErrHealthcheck and an error row.
func TestCreateHealthcheckTimeout(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{suppressRunning: true}
	svc := newTestService(repo, cs)
	userID := uuid.New()

	_, _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	})
	if !errors.Is(err, ErrHealthcheck) {
		t.Fatalf("Create error = %v, want ErrHealthcheck", err)
	}
	list, err := svc.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Status != StatusError {
		t.Fatalf("databases = %+v, want one row in the error state", list)
	}
}

// TestOwnershipIsolation: every read and mutation of another user's database
// answers ErrNotFound so IDs cannot be probed.
func TestOwnershipIsolation(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()
	stranger := uuid.New()

	created, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	cs.setRunning(created.ContainerID)

	unknown := uuid.New()
	scenarios := map[string]error{
		"get": errOf(func() error { _, err := svc.Get(context.Background(), stranger, created.ID); return err }),
		"credentials": errOf(func() error {
			_, err := svc.Credentials(context.Background(), stranger, created.ID)
			return err
		}),
		"update": errOf(func() error {
			_, err := svc.Update(context.Background(), stranger, created.ID, UpdateRequest{Name: "stolen"})
			return err
		}),
		"delete":  errOf(func() error { return svc.Delete(context.Background(), stranger, created.ID) }),
		"start":   errOf(func() error { _, err := svc.Start(context.Background(), stranger, created.ID); return err }),
		"stop":    errOf(func() error { _, err := svc.Stop(context.Background(), stranger, created.ID); return err }),
		"restart": errOf(func() error { _, err := svc.Restart(context.Background(), stranger, created.ID); return err }),
	}
	for name, err := range scenarios {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s by another user: error = %v, want ErrNotFound", name, err)
		}
	}

	if _, err := svc.Get(context.Background(), stranger, unknown); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Get(context.Background(), owner, uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Errorf("nil id: error = %v, want ErrValidation", err)
	}
	if _, err := svc.Get(context.Background(), owner, created.ID); err != nil {
		t.Errorf("owner Get: %v", err)
	}

	// The stranger's failed deletes must not have touched the row.
	still, err := svc.Get(context.Background(), owner, created.ID)
	if err != nil || still.Status == StatusDeleting {
		t.Errorf("row after failed deletes: %+v (err %v)", still, err)
	}
}

// TestListExcludesOtherUsersAndDeletedRows.
func TestListExcludesOtherUsersAndDeletedRows(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)
	owner := uuid.New()
	stranger := uuid.New()

	empty, err := svc.List(context.Background(), owner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty list = %#v, want a non-nil empty slice", empty)
	}

	if _, _, err := svc.Create(context.Background(), stranger, CreateRequest{
		Name: "theirs", Engine: EngineRedis, ServerID: serverID,
	}); err != nil {
		t.Fatalf("Create (stranger): %v", err)
	}
	mine, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "mine", Engine: EnginePostgres, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create (owner): %v", err)
	}

	list, err := svc.List(context.Background(), owner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != mine.ID {
		t.Fatalf("list = %+v, want only the owner's database", list)
	}

	if err := svc.Delete(context.Background(), owner, mine.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, err = svc.List(context.Background(), owner)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("list after delete = %+v, want empty", list)
	}
}

// TestDeleteStopsRemovesAndKeepsTheVolume is the soft-delete contract: the
// container goes, the row is hidden, the named volume survives.
func TestDeleteStopsRemovesAndKeepsTheVolume(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	volume := created.StoragePath

	if err := svc.Delete(context.Background(), owner, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if cs.stops != 1 {
		t.Errorf("stops = %d, want 1", cs.stops)
	}
	if len(cs.removes) != 1 || cs.removes[0] != created.ContainerID {
		t.Errorf("removes = %v, want [%s]", cs.removes, created.ContainerID)
	}

	repo.mu.Lock()
	row := repo.databases[created.ID]
	repo.mu.Unlock()
	if row.DeletedAt.IsZero() {
		t.Error("deleted_at was not set")
	}
	if row.Status != StatusDeleting {
		t.Errorf("status = %q, want deleting", row.Status)
	}
	if row.StoragePath != volume {
		t.Errorf("volume = %q, want the retained %q", row.StoragePath, volume)
	}
	if row.ContainerID != created.ContainerID {
		t.Errorf("container_id = %q, want it recorded for the purge", row.ContainerID)
	}

	if _, err := svc.Get(context.Background(), owner, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: %v, want ErrNotFound", err)
	}
	if err := svc.Delete(context.Background(), owner, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete: %v, want ErrNotFound", err)
	}
}

// TestDeleteFailsWhenTheAgentCannotRemove: a failed removal keeps the row
// live so the operator can retry instead of losing track of a running
// container.
func TestDeleteFailsWhenTheAgentCannotRemove(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runErr: nil, removeErr: errors.New("agent exploded")}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Delete(context.Background(), owner, created.ID); err == nil {
		t.Fatal("Delete should fail when the container cannot be removed")
	}
	if _, err := svc.Get(context.Background(), owner, created.ID); err != nil {
		t.Errorf("row must stay live after a failed delete: %v", err)
	}
}

// TestDeleteWithoutContainer: a row whose container never came up is still
// deletable (it is what a failed provision leaves behind).
func TestDeleteWithoutContainer(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, &fakeContainers{})
	owner := uuid.New()
	row := repo.seed(Database{
		UserID: owner, ServerID: uuid.New(), Name: "broken",
		Engine: EnginePostgres, Status: StatusError,
	})

	if err := svc.Delete(context.Background(), owner, row.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(context.Background(), owner, row.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: %v, want ErrNotFound", err)
	}
}

// TestDeleteSucceedsWhenTheServerIsGone: deleting a server nulls the FK on its
// databases, and the agent then knows no such node. The row must still
// soft-delete instead of wedging on ErrServerNotFound forever.
func TestDeleteSucceedsWhenTheServerIsGone(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	cs.setRunning("container-1")
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	cs.removeErr = containers.ErrServerNotFound

	if err := svc.Delete(context.Background(), owner, created.ID); err != nil {
		t.Fatalf("Delete with a gone server: %v", err)
	}
	if _, err := svc.Get(context.Background(), owner, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: %v, want ErrNotFound", err)
	}
}

// TestCreateRemovesContainerWhenTheRowCannotBeUpdated: when the agent starts a
// container but its ID cannot be persisted, the row can never manage it again,
// so the container is removed instead of becoming an orphan.
func TestCreateRemovesContainerWhenTheRowCannotBeUpdated(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	cs.setRunning("container-1")
	svc := newTestService(repo, cs)

	repo.updateErr = errors.New("database is down")
	if _, _, err := svc.Create(context.Background(), uuid.New(), CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	}); err == nil {
		t.Fatal("Create should fail when the row cannot be updated")
	}

	cs.mu.Lock()
	removes := append([]string(nil), cs.removes...)
	cs.mu.Unlock()
	if len(removes) != 1 || removes[0] != "container-1" {
		t.Fatalf("removes = %v, want the unmanageable container removed", removes)
	}
}

// TestLifecycleStatuses walks start/stop/restart and their container calls.
func TestLifecycleStatuses(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	cs.setRunning(created.ContainerID)

	stopped, err := svc.Stop(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.Status != StatusStopped {
		t.Errorf("status after stop = %q, want stopped", stopped.Status)
	}

	started, err := svc.Start(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if started.Status != StatusRunning {
		t.Errorf("status after start = %q, want running", started.Status)
	}

	restarted, err := svc.Restart(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if restarted.Status != StatusRunning {
		t.Errorf("status after restart = %q, want running", restarted.Status)
	}
	if cs.starts != 1 || cs.stops != 1 || cs.restarts != 1 {
		t.Errorf("container calls = start %d, stop %d, restart %d; want 1/1/1",
			cs.starts, cs.stops, cs.restarts)
	}
}

// TestLifecycleAgentFailureIsMapped: an unreachable agent surfaces as
// ErrAgentUnavailable on every verb.
func TestLifecycleAgentFailureIsMapped(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	cs.setRunning(created.ContainerID)

	cs.startErr = containers.ErrAgentUnavailable
	cs.stopErr = containers.ErrAgentUnavailable
	cs.restartErr = containers.ErrAgentUnavailable

	if _, err := svc.Start(context.Background(), owner, created.ID); !errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("Start error = %v, want ErrAgentUnavailable", err)
	}
	if _, err := svc.Stop(context.Background(), owner, created.ID); !errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("Stop error = %v, want ErrAgentUnavailable", err)
	}
	if _, err := svc.Restart(context.Background(), owner, created.ID); !errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("Restart error = %v, want ErrAgentUnavailable", err)
	}
}

// TestLifecycleRequiresContainer guards verbs on a row that never provisioned.
func TestLifecycleRequiresContainer(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, &fakeContainers{})
	owner := uuid.New()
	row := repo.seed(Database{
		UserID: owner, ServerID: uuid.New(), Name: "broken",
		Engine: EnginePostgres, Status: StatusError,
	})

	for _, call := range []struct {
		name string
		fn   func() error
	}{
		{"start", func() error { _, err := svc.Start(context.Background(), owner, row.ID); return err }},
		{"stop", func() error { _, err := svc.Stop(context.Background(), owner, row.ID); return err }},
		{"restart", func() error { _, err := svc.Restart(context.Background(), owner, row.ID); return err }},
	} {
		if err := call.fn(); !errors.Is(err, ErrValidation) {
			t.Errorf("%s without a container: error = %v, want ErrValidation", call.name, err)
		}
	}
}

// TestCredentialsRoundTrip: what Create returned is what the credentials
// endpoint decrypts later.
func TestCredentialsRoundTrip(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, credentials, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EngineMySQL, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	opened, err := svc.Credentials(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if opened != credentials {
		t.Errorf("credentials = %+v, want %+v", opened, credentials)
	}
	if opened.RootPassword == "" {
		t.Error("root password did not survive the round trip")
	}
}

// TestUpdateRename covers the single mutable field.
func TestUpdateRename(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	first, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "billing", Engine: EnginePostgres, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	renamed, err := svc.Update(context.Background(), owner, first.ID, UpdateRequest{Name: "warehouse"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if renamed.Name != "warehouse" {
		t.Errorf("name = %q, want warehouse", renamed.Name)
	}
	if renamed.Engine != EnginePostgres || renamed.ContainerID != first.ContainerID {
		t.Errorf("Update changed more than the name: %+v", renamed)
	}

	if _, err := svc.Update(context.Background(), owner, second.ID, UpdateRequest{Name: "warehouse"}); !errors.Is(err, ErrConflict) {
		t.Errorf("colliding rename error = %v, want ErrConflict", err)
	}
	if _, err := svc.Update(context.Background(), owner, first.ID, UpdateRequest{Name: "not a name"}); !errors.Is(err, ErrValidation) {
		t.Errorf("invalid rename error = %v, want ErrValidation", err)
	}
}

// TestServiceWithoutDependencies fails clearly instead of panicking.
func TestServiceWithoutDependencies(t *testing.T) {
	svc := NewService(Config{Logger: discardLogger()})
	if _, _, err := svc.Create(context.Background(), uuid.New(), CreateRequest{Name: "db", Engine: EnginePostgres}); err == nil {
		t.Error("Create without a repository must fail")
	}
	if _, err := svc.List(context.Background(), uuid.New()); err == nil {
		t.Error("List without a repository must fail")
	}

	withoutContainers := NewService(Config{Repository: newFakeRepository(), Logger: discardLogger()})
	if _, err := withoutContainers.List(context.Background(), uuid.New()); err == nil {
		t.Error("List without a container service must fail")
	}
}

// TestNewDefaultServiceReturnsNilWithoutDependencies: the server wiring passes
// the result to Mount unconditionally.
func TestNewDefaultServiceReturnsNilWithoutDependencies(t *testing.T) {
	if svc := NewDefaultService(Config{Secret: testSecret}); svc != nil {
		t.Error("no repository and no container service must yield a nil service")
	}
	if svc := NewDefaultService(Config{Repository: newFakeRepository(), Secret: testSecret}); svc != nil {
		t.Error("no container service must yield a nil service")
	}

	t.Setenv(FeatureEnv, "false")
	svc := NewDefaultService(Config{
		Repository: newFakeRepository(),
		Containers: &fakeContainers{},
		Secret:     testSecret,
	})
	if svc != nil {
		t.Error("FEATURE_DATABASES=false must yield a nil service")
	}
}

// ---------------------------------------------------------------------------
// FX-8a regression tests: provisioning image pull, cleanup ids, delete/create
// fencing, column-scoped writes, credential rollback and public-port conflicts.
// ---------------------------------------------------------------------------

// TestCreatePullsImageBeforeRun (D1-2): a fresh node has no engine image, so
// create must pull it before running the container.
func TestCreatePullsImageBeforeRun(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)

	if _, _, err := svc.Create(context.Background(), uuid.New(), CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if cs.pulls != 1 {
		t.Fatalf("pulls = %d, want 1", cs.pulls)
	}
	engine, _ := LookupEngine(EnginePostgres)
	pullImage := ""
	if len(cs.runs) > 0 {
		pullImage = cs.runs[0].Image
	}
	if got := cs.lastRun().Image; got != engine.Image("") || pullImage == "" {
		t.Fatalf("run image = %q, want %q", got, engine.Image(""))
	}
	// Pull must happen before Run: a run against a missing image would fail.
	want := []string{"pull", "run"}
	if len(cs.calls) < len(want) || cs.calls[0] != want[0] || cs.calls[1] != want[1] {
		t.Fatalf("calls = %v, want pull before run", cs.calls)
	}
}

// TestCreatePullFailureLeavesTerminalErrorRow (D1-2): a failed pull must not
// leave a permanently-creating row, and it must not write credentials for a
// database that will never run.
func TestCreatePullFailureLeavesTerminalErrorRow(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{pullErr: containers.ErrAgentUnavailable}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	var createdID uuid.UUID
	repo.afterCreate = func(d Database) { createdID = d.ID }

	_, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	})
	if !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("Create error = %v, want ErrAgentUnavailable", err)
	}
	if len(cs.runs) != 0 {
		t.Fatalf("runs = %d, want none after a failed pull", len(cs.runs))
	}
	row, err := svc.Get(context.Background(), owner, createdID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != StatusError {
		t.Errorf("status = %q, want a terminal error, not creating", row.Status)
	}
	repo.mu.Lock()
	secrets := len(repo.secrets[createdID])
	repo.mu.Unlock()
	if secrets != 0 {
		t.Errorf("stored %d credentials despite a failed pull", secrets)
	}
}

// TestCreateCleanupUsesRecordedIDs (D1-4): when the container-id write fails,
// cleanup must remove the container using the node id we actually hold, never
// a zeroed row.
func TestCreateCleanupUsesRecordedIDs(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	cs.setRunning("container-1")
	svc := newTestService(repo, cs)

	repo.updateErr = errors.New("database is down")
	if _, _, err := svc.Create(context.Background(), uuid.New(), CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	}); err == nil {
		t.Fatal("Create should fail when the row cannot be updated")
	}

	cs.mu.Lock()
	removes := append([]string(nil), cs.removes...)
	servers := append([]uuid.UUID(nil), cs.removeServers...)
	cs.mu.Unlock()
	if len(removes) != 1 || removes[0] != "container-1" {
		t.Fatalf("removes = %v, want the unmanageable container removed", removes)
	}
	if len(servers) != 1 || servers[0] != serverID {
		t.Fatalf("remove servers = %v, want the real node %s (a zeroed row = bug)", servers, serverID)
	}
}

// TestDeleteDuringProvisionRemovesLateContainer (D1-5): delete that wins while
// provisioning is in flight must leave no container behind and the row must
// stay deleted. The create path's late write is fenced and its cleanup removes
// the container the agent already started.
func TestDeleteDuringProvisionRemovesLateContainer(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()
	ctx := context.Background()

	var databaseID uuid.UUID
	repo.afterCreate = func(d Database) { databaseID = d.ID }
	// afterRun fires once the agent returned a container id but before create
	// persisted it: the classic interleaving the finding describes.
	cs.afterRun = func() {
		if err := svc.Delete(ctx, owner, databaseID); err != nil {
			t.Errorf("Delete during provisioning: %v", err)
		}
	}

	if _, _, err := svc.Create(ctx, owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	}); err == nil {
		t.Fatal("Create should fail after delete removed the row")
	}

	cs.mu.Lock()
	removes := append([]string(nil), cs.removes...)
	servers := append([]uuid.UUID(nil), cs.removeServers...)
	cs.mu.Unlock()
	if len(removes) != 1 || removes[0] != "container-1" {
		t.Fatalf("removes = %v, want the late container removed", removes)
	}
	if len(servers) != 1 || servers[0] != serverID {
		t.Fatalf("remove servers = %v, want %s", servers, serverID)
	}
	if _, err := repo.GetDatabase(ctx, databaseID); !errors.Is(err, ErrNotFound) {
		t.Errorf("row lookup = %v, want the row to stay deleted", err)
	}
}

// TestSoftDeletedRowRejectsProvisioningWrites (D1-5): the fence itself — after
// a soft delete every provisioning write reports no row.
func TestSoftDeletedRowRejectsProvisioningWrites(t *testing.T) {
	repo := newFakeRepository()
	owner := uuid.New()
	row := repo.seed(Database{
		UserID: owner, ServerID: uuid.New(), Name: "orders",
		Engine: EnginePostgres, Status: StatusCreating,
	})
	ctx := context.Background()
	if _, err := repo.SoftDeleteDatabase(ctx, row.ID); err != nil {
		t.Fatalf("SoftDeleteDatabase: %v", err)
	}

	if _, err := repo.UpdateDatabaseContainer(ctx, row.ID, "late"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDatabaseContainer after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateDatabaseStatus(ctx, row.ID, StatusRunning); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDatabaseStatus after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateDatabaseName(ctx, row.ID, "late"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDatabaseName after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetDatabase(ctx, row.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDatabase = %v, want the row to stay deleted", err)
	}
}

// TestConcurrentRenameAndLifecycleDoNotClobber (D1-6): column-scoped writes
// keep a rename and a lifecycle transition from erasing each other's field or
// the container id.
func TestConcurrentRenameAndLifecycleDoNotClobber(t *testing.T) {
	for i := 0; i < 50; i++ {
		repo := newFakeRepository()
		serverID := repo.seedServer()
		cs := &fakeContainers{runID: "container-1"}
		svc := newTestService(repo, cs)
		owner := uuid.New()
		ctx := context.Background()

		created, _, err := svc.Create(ctx, owner, CreateRequest{
			Name: "orders", Engine: EnginePostgres, ServerID: serverID,
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		cs.setRunning(created.ContainerID)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := svc.Update(ctx, owner, created.ID, UpdateRequest{Name: "renamed"}); err != nil {
				t.Errorf("Update: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := svc.Stop(ctx, owner, created.ID); err != nil {
				t.Errorf("Stop: %v", err)
			}
		}()
		wg.Wait()

		repo.mu.Lock()
		row := repo.databases[created.ID]
		repo.mu.Unlock()
		if row.Name != "renamed" {
			t.Fatalf("iteration %d: name = %q, want renamed (rename clobbered)", i, row.Name)
		}
		if row.Status != StatusStopped {
			t.Fatalf("iteration %d: status = %q, want stopped (lifecycle clobbered)", i, row.Status)
		}
		if row.ContainerID != "container-1" {
			t.Fatalf("iteration %d: container_id = %q, want container-1 (erased)", i, row.ContainerID)
		}
	}
}

// TestCreateCredentialFailureRollsBackAndMarksError (D1-8): a partial
// credential write is rolled back and the row becomes terminal, never a
// permanently-creating database with incomplete secrets.
func TestCreateCredentialFailureRollsBackAndMarksError(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	var createdID uuid.UUID
	repo.afterCreate = func(d Database) { createdID = d.ID }
	// The first credential write succeeds, the second fails: a partial write.
	repo.secretFailAt = 2
	repo.secretFail = errors.New("credentials table is down")

	_, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	})
	if err == nil {
		t.Fatal("Create should fail when a credential write fails")
	}
	if len(cs.runs) != 0 {
		t.Errorf("runs = %d, want none after a credential failure", len(cs.runs))
	}

	row, err := svc.Get(context.Background(), owner, createdID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != StatusError {
		t.Errorf("status = %q, want a terminal error, not creating", row.Status)
	}
	repo.mu.Lock()
	secrets := len(repo.secrets[createdID])
	repo.mu.Unlock()
	if secrets != 0 {
		t.Errorf("secrets = %d, want the partial credential write rolled back", secrets)
	}
}

// TestCreateRejectsPublicPortInUse (D1-11): the pre-check answers 409 before a
// row or container is created.
func TestCreateRejectsPublicPortInUse(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	owner := uuid.New()
	repo.seed(Database{
		UserID: owner, ServerID: serverID, Name: "taken",
		Engine: EnginePostgres, Status: StatusRunning, PublicPort: 5433,
	})
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)

	_, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID, PublicPort: 5433,
	})
	if !errors.Is(err, ErrPortConflict) {
		t.Fatalf("Create error = %v, want ErrPortConflict", err)
	}
	if cs.pulls != 0 || len(cs.runs) != 0 {
		t.Errorf("create touched the node (pulls %d, runs %d) despite a port conflict", cs.pulls, len(cs.runs))
	}
	list, err := svc.List(context.Background(), owner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("databases = %d, want the port-conflict create to leave no row", len(list))
	}
}

// TestCreateMapsDockerPortConflict (D1-11): a Docker bind conflict surfaced by
// the agent is a typed conflict, not a 500, and leaves a terminal error row.
func TestCreateMapsDockerPortConflict(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runErr: containers.ErrPortConflict}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	_, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID, PublicPort: 5433,
	})
	if !errors.Is(err, ErrPortConflict) {
		t.Fatalf("Create error = %v, want ErrPortConflict", err)
	}
	list, err := svc.List(context.Background(), owner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Status != StatusError {
		t.Fatalf("databases = %+v, want one row in the error state", list)
	}
	if len(cs.removes) != 0 {
		t.Errorf("removes = %v, want none (the failed run left no container)", cs.removes)
	}
}

// TestCreateStatusWriteTransientFailureKeepsContainer (U1): only a fenced
// (not-found) status write means delete won. Any other status-write failure is
// transient, so the row becomes terminal error and the healthy container is
// kept recoverable instead of being destroyed by one failed UPDATE.
func TestCreateStatusWriteTransientFailureKeepsContainer(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	var createdID uuid.UUID
	repo.afterCreate = func(d Database) { createdID = d.ID }
	repo.statusErrFor = StatusRunning
	repo.statusErr = errors.New("transient status write failure")

	if _, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	}); err == nil {
		t.Fatal("Create should fail on a transient status write")
	}

	row, err := svc.Get(context.Background(), owner, createdID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != StatusError {
		t.Errorf("status = %q, want a terminal error", row.Status)
	}
	if row.ContainerID != "container-1" {
		t.Errorf("container_id = %q, want the healthy container kept", row.ContainerID)
	}
	cs.mu.Lock()
	removes := append([]string(nil), cs.removes...)
	cs.mu.Unlock()
	if len(removes) != 0 {
		t.Errorf("removes = %v, want the container kept for recovery", removes)
	}
}

// TestCreateStatusWriteFencedByDeleteRemovesContainer (U1): the fenced case —
// the status write reports ErrNotFound because delete won — still removes the
// container.
func TestCreateStatusWriteFencedByDeleteRemovesContainer(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)

	repo.statusErrFor = StatusRunning
	repo.statusErr = ErrNotFound

	if _, _, err := svc.Create(context.Background(), uuid.New(), CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Create error = %v, want ErrNotFound", err)
	}
	cs.mu.Lock()
	removes := append([]string(nil), cs.removes...)
	cs.mu.Unlock()
	if len(removes) != 1 || removes[0] != "container-1" {
		t.Fatalf("removes = %v, want the fenced container removed", removes)
	}
}

// TestCreateCleanupSurvivesRequestCancellation (U2): a client disconnect
// during provisioning must not leave a row creating or a container
// unmanaged — cleanup runs on a context detached from the request.
func TestCreateCleanupSurvivesRequestCancellation(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var createdID uuid.UUID
	repo.afterCreate = func(d Database) { createdID = d.ID }
	// Cancel the request while the container is starting: the container-id
	// write then fails with context.Canceled.
	cs.afterRun = cancel

	if _, _, err := svc.Create(ctx, owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, ServerID: serverID,
	}); err == nil {
		t.Fatal("Create should fail once the request context is canceled")
	}

	row, err := svc.Get(context.Background(), owner, createdID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != StatusError {
		t.Errorf("status = %q, want error (cleanup must ignore the cancel)", row.Status)
	}
	cs.mu.Lock()
	removes := append([]string(nil), cs.removes...)
	servers := append([]uuid.UUID(nil), cs.removeServers...)
	cs.mu.Unlock()
	if len(removes) != 1 || removes[0] != "container-1" {
		t.Errorf("removes = %v, want the started container removed despite the cancel", removes)
	}
	if len(servers) != 1 || servers[0] != serverID {
		t.Errorf("remove servers = %v, want %s", servers, serverID)
	}
}

// TestDeleteRemovesContainerPersistedAfterRead (U4): a provision that persists
// its container id between Delete's read and its soft delete is still removed
// — exactly once.
func TestDeleteRemovesContainerPersistedAfterRead(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)
	owner := uuid.New()
	row := repo.seed(Database{
		UserID: owner, ServerID: serverID, Name: "orders",
		Engine: EnginePostgres, Status: StatusCreating,
	})
	// The provision persists its container id as the delete lands; the row
	// returned by the soft delete carries it.
	repo.softDeleteContainerID = "late-container"

	if err := svc.Delete(context.Background(), owner, row.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	cs.mu.Lock()
	removes := append([]string(nil), cs.removes...)
	servers := append([]uuid.UUID(nil), cs.removeServers...)
	cs.mu.Unlock()
	if len(removes) != 1 || removes[0] != "late-container" {
		t.Fatalf("removes = %v, want exactly the late-persisted container", removes)
	}
	if len(servers) != 1 || servers[0] != serverID {
		t.Errorf("remove servers = %v, want %s", servers, serverID)
	}
	if _, err := svc.Get(context.Background(), owner, row.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get = %v, want the row to stay deleted", err)
	}
}

// errOf runs fn and returns its error, which keeps the ownership table
// readable.
func errOf(fn func() error) error { return fn() }

// envContains reports whether a KEY=VALUE list carries value. Generated values
// are base64url (no "="), so a suffix match on "=value" is exact.
func envContains(env []string, value string) bool {
	for _, entry := range env {
		if strings.HasSuffix(entry, "="+value) {
			return true
		}
	}
	return false
}
