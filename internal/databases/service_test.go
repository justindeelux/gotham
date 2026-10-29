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

func newTestService(repo *fakeRepository, cs containers.ContainerService) *Service {
	return NewService(Config{
		Repository:    repo,
		Containers:    cs,
		Secret:        testSecret,
		Logger:        discardLogger(),
		HealthTimeout: 300 * time.Millisecond,
		HealthPoll:    2 * time.Millisecond,
	})
}

// waitForDatabaseStatus polls until the background provision reaches want.
// Create now returns before the container exists, so tests follow the same
// poll the API clients do.
func waitForDatabaseStatus(t *testing.T, svc *Service, owner, databaseID uuid.UUID, want Status) Database {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last Database
	var lastErr error
	for time.Now().Before(deadline) {
		last, lastErr = svc.Get(context.Background(), owner, databaseID)
		if lastErr == nil && last.Status == want {
			return last
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("database %s never reached %q: status=%q err=%v",
		databaseID, want, last.Status, lastErr)
	return Database{}
}

func createDatabaseAndWait(t *testing.T, svc *Service, owner uuid.UUID, req CreateRequest) (Database, Credentials) {
	t.Helper()
	created, credentials, err := svc.Create(context.Background(), owner, req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return waitForDatabaseStatus(t, svc, owner, created.ID, StatusRunning), credentials
}

func waitForCondition(t *testing.T, description string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

// blockingContainers holds the provision inside Run until the provision context
// is canceled, so a test can observe the row while it is still creating.
type blockingContainers struct {
	*fakeContainers
	entered chan struct{}
}

func (b *blockingContainers) Run(ctx context.Context, _ uuid.UUID, _ containers.RunOptions) (string, error) {
	close(b.entered)
	<-ctx.Done()
	return "", ctx.Err()
}

func TestCreateReturnsWhileProvisioning(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &blockingContainers{fakeContainers: &fakeContainers{}, entered: make(chan struct{})}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	database, credentials, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if database.Status != StatusCreating {
		t.Errorf("status = %q, want creating", database.Status)
	}
	if credentials.Password == "" {
		t.Error("Create returned no credentials")
	}
	<-cs.entered

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	row := waitForDatabaseStatus(t, svc, owner, database.ID, StatusError)
	if row.StatusMessage != interruptedProvisionMessage {
		t.Errorf("status_message = %q, want %q", row.StatusMessage, interruptedProvisionMessage)
	}
}

func TestStartupRecoveryMarksCreatingAsInterrupted(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, &fakeContainers{})
	owner := uuid.New()
	row := repo.seed(Database{
		UserID: owner, ServerID: uuid.New(), EnvironmentID: uuid.New(),
		Name: "stuck", Engine: EnginePostgres, Status: StatusCreating,
	})

	svc.recoverInterruptedProvisioning()

	got, err := svc.Get(context.Background(), owner, row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusError || got.StatusMessage != interruptedProvisionMessage {
		t.Errorf("row = %+v, want error with %q", got, interruptedProvisionMessage)
	}
}

func TestStatusMessageRedactsCredentials(t *testing.T) {
	credentials := Credentials{
		Username: "orders", Password: "secret-password",
		Database: "orders", RootPassword: "root-secret",
	}
	message := statusMessageFor(
		errors.New("run failed: PASSWORD=secret-password ROOT=root-secret"),
		credentials,
	)
	if strings.Contains(message, "secret-password") || strings.Contains(message, "root-secret") {
		t.Fatalf("status message leaks a credential: %q", message)
	}
	if !strings.Contains(message, "<redacted>") {
		t.Errorf("status message = %q, want the redaction marker", message)
	}
}

func TestCreateEachEngine(t *testing.T) {
	for _, engineName := range EngineNames() {
		t.Run(engineName, func(t *testing.T) {
			repo := newFakeRepository()
			serverID := repo.seedServer()
			envID, _ := repo.seedEnvironment()
			cs := &fakeContainers{runID: "container-" + engineName}
			svc := newTestService(repo, cs)
			userID := uuid.New()

			database, credentials := createDatabaseAndWait(t, svc, userID, CreateRequest{
				Name:          "db-" + engineName,
				Engine:        engineName,
				Version:       "",
				EnvironmentID: envID,
				ServerID:      serverID,
				PublicPort:    20000,
			})

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

			if !envContains(run.Env, credentials.Password) {
				t.Error("the run payload does not carry the generated password")
			}
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

func TestCreateInternalDatabasePublishesNoPort(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)

	createDatabaseAndWait(t, svc, uuid.New(), CreateRequest{
		Name:          "cache",
		Engine:        EngineRedis,
		EnvironmentID: envID, ServerID: serverID,
	})
	if ports := cs.lastRun().Ports; len(ports) != 0 {
		t.Errorf("ports = %v, want none", ports)
	}
}

func TestCreateValidation(t *testing.T) {
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
		noEnv      bool
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
			name:  "missing environment",
			req:   CreateRequest{Name: "db", Engine: EnginePostgres},
			noEnv: true,
			want:  ErrValidation,
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
			envID, _ := repo.seedEnvironment()
			svc := newTestService(repo, &fakeContainers{})
			if tt.disable {
				t.Setenv(FeatureEnv, "false")
			}

			req := tt.req
			if !tt.noEnv {
				req.EnvironmentID = envID
			}
			switch tt.serverMode {
			case serverAbsent:
				req.ServerID = uuid.Nil
			case serverUnknown:
				req.ServerID = uuid.New()
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

func TestCreateRejectsDuplicateName(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)
	userID := uuid.New()

	createDatabaseAndWait(t, svc, userID, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
	_, _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "orders", Engine: EngineMySQL, EnvironmentID: envID, ServerID: serverID,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Create error = %v, want ErrConflict", err)
	}
}

func TestCreateAgentFailureKeepsAnErrorRow(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runErr: containers.ErrAgentUnavailable}
	svc := newTestService(repo, cs)
	userID := uuid.New()

	created, _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	row := waitForDatabaseStatus(t, svc, userID, created.ID, StatusError)
	if row.ContainerID != "" {
		t.Errorf("container_id = %q, want empty when the run failed", row.ContainerID)
	}
	if row.StatusMessage == "" {
		t.Error("status_message is empty, want a readable reason")
	}

	list, err := svc.List(context.Background(), userID, DatabaseFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("databases = %d, want the failed row to be visible", len(list))
	}
	if list[0].Status != StatusError {
		t.Errorf("status = %q, want error", list[0].Status)
	}
}

func TestCreateHealthcheckTimeout(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{suppressRunning: true}
	svc := newTestService(repo, cs)
	userID := uuid.New()

	created, _, err := svc.Create(context.Background(), userID, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	row := waitForDatabaseStatus(t, svc, userID, created.ID, StatusError)
	if !strings.Contains(row.StatusMessage, "did not become ready") {
		t.Errorf("status_message = %q, want the healthcheck reason", row.StatusMessage)
	}
	waitForCondition(t, "the unhealthy container removed", func() bool {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		return len(cs.removes) == 1
	})
}

func TestAccessByID(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()
	created, _ := createDatabaseAndWait(t, svc, owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})

	unknown := uuid.New()
	if _, err := svc.Get(context.Background(), owner, unknown); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Get(context.Background(), owner, uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Errorf("nil id: error = %v, want ErrValidation", err)
	}
	if _, err := svc.Get(context.Background(), owner, created.ID); err != nil {
		t.Errorf("owner Get: %v", err)
	}
}

func TestListExcludesOtherUsersAndDeletedRows(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)
	owner := uuid.New()
	stranger := uuid.New()

	empty, err := svc.List(context.Background(), owner, DatabaseFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty list = %#v, want a non-nil empty slice", empty)
	}

	createDatabaseAndWait(t, svc, stranger, CreateRequest{
		Name: "theirs", Engine: EngineRedis, EnvironmentID: envID, ServerID: serverID,
	})
	mine, _ := createDatabaseAndWait(t, svc, owner, CreateRequest{
		Name: "mine", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})

	list, err := svc.List(context.Background(), owner, DatabaseFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != mine.ID {
		t.Fatalf("list = %+v, want only the owner's database", list)
	}

	if err := svc.Delete(context.Background(), owner, mine.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, err = svc.List(context.Background(), owner, DatabaseFilter{})
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("list after delete = %+v, want empty", list)
	}
}

func TestDeleteStopsRemovesAndKeepsTheVolume(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _ := createDatabaseAndWait(t, svc, owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
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

func TestDeleteFailsWhenTheAgentCannotRemove(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runErr: nil, removeErr: errors.New("agent exploded")}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _ := createDatabaseAndWait(t, svc, owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
	if err := svc.Delete(context.Background(), owner, created.ID); err == nil {
		t.Fatal("Delete should fail when the container cannot be removed")
	}
	if _, err := svc.Get(context.Background(), owner, created.ID); err != nil {
		t.Errorf("row must stay live after a failed delete: %v", err)
	}
}

func TestDeleteWithoutContainer(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, &fakeContainers{})
	owner := uuid.New()
	row := repo.seed(Database{
		UserID: owner, ServerID: uuid.New(), EnvironmentID: uuid.New(), Name: "broken",
		Engine: EnginePostgres, Status: StatusError,
	})

	if err := svc.Delete(context.Background(), owner, row.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(context.Background(), owner, row.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: %v, want ErrNotFound", err)
	}
}

func TestDeleteSucceedsWhenTheServerIsGone(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	cs.setRunning("container-1")
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _ := createDatabaseAndWait(t, svc, owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
	cs.removeErr = containers.ErrServerNotFound

	if err := svc.Delete(context.Background(), owner, created.ID); err != nil {
		t.Fatalf("Delete with a gone server: %v", err)
	}
	if _, err := svc.Get(context.Background(), owner, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: %v, want ErrNotFound", err)
	}
}

func TestCreateRemovesContainerWhenTheRowCannotBeUpdated(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	cs.setRunning("container-1")
	svc := newTestService(repo, cs)

	repo.updateErr = errors.New("database is down")
	if _, _, err := svc.Create(context.Background(), uuid.New(), CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	waitForCondition(t, "the unmanageable container removed", func() bool {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		return len(cs.removes) == 1 && cs.removes[0] == "container-1"
	})
}

func TestLifecycleStatuses(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _ := createDatabaseAndWait(t, svc, owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})

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

func TestLifecycleAgentFailureIsMapped(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _ := createDatabaseAndWait(t, svc, owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})

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

func TestLifecycleRequiresContainer(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, &fakeContainers{})
	owner := uuid.New()
	row := repo.seed(Database{
		UserID: owner, ServerID: uuid.New(), EnvironmentID: uuid.New(), Name: "broken",
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

func TestCredentialsRoundTrip(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, credentials := createDatabaseAndWait(t, svc, owner, CreateRequest{
		Name: "orders", Engine: EngineMySQL, EnvironmentID: envID, ServerID: serverID,
	})
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

func TestUpdateRename(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	first, _ := createDatabaseAndWait(t, svc, owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
	second, _ := createDatabaseAndWait(t, svc, owner, CreateRequest{
		Name: "billing", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})

	renamed, err := svc.Update(context.Background(), owner, first.ID, UpdateRequest{Name: ptr("warehouse")})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if renamed.Name != "warehouse" {
		t.Errorf("name = %q, want warehouse", renamed.Name)
	}
	if renamed.Engine != EnginePostgres || renamed.ContainerID != first.ContainerID {
		t.Errorf("Update changed more than the name: %+v", renamed)
	}

	if _, err := svc.Update(context.Background(), owner, second.ID, UpdateRequest{Name: ptr("warehouse")}); !errors.Is(err, ErrConflict) {
		t.Errorf("colliding rename error = %v, want ErrConflict", err)
	}
	if _, err := svc.Update(context.Background(), owner, first.ID, UpdateRequest{Name: ptr("not a name")}); !errors.Is(err, ErrValidation) {
		t.Errorf("invalid rename error = %v, want ErrValidation", err)
	}
}

func TestServiceWithoutDependencies(t *testing.T) {
	svc := NewService(Config{Logger: discardLogger()})
	if _, _, err := svc.Create(context.Background(), uuid.New(), CreateRequest{Name: "db", Engine: EnginePostgres}); err == nil {
		t.Error("Create without a repository must fail")
	}
	if _, err := svc.List(context.Background(), uuid.New(), DatabaseFilter{}); err == nil {
		t.Error("List without a repository must fail")
	}

	withoutContainers := NewService(Config{Repository: newFakeRepository(), Logger: discardLogger()})
	if _, err := withoutContainers.List(context.Background(), uuid.New(), DatabaseFilter{}); err == nil {
		t.Error("List without a container service must fail")
	}
}

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

func TestCreatePullsImageBeforeRun(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)

	createDatabaseAndWait(t, svc, uuid.New(), CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
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
	want := []string{"pull", "run"}
	if len(cs.calls) < len(want) || cs.calls[0] != want[0] || cs.calls[1] != want[1] {
		t.Fatalf("calls = %v, want pull before run", cs.calls)
	}
}

func TestCreatePullFailureLeavesTerminalErrorRow(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{pullErr: containers.ErrAgentUnavailable}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	createdID := created.ID
	row := waitForDatabaseStatus(t, svc, owner, createdID, StatusError)
	if row.StatusMessage == "" {
		t.Error("status_message is empty, want the pull failure reason")
	}
	if len(cs.runs) != 0 {
		t.Fatalf("runs = %d, want none after a failed pull", len(cs.runs))
	}
	repo.mu.Lock()
	secrets := len(repo.secrets[createdID])
	repo.mu.Unlock()
	if secrets != 0 {
		t.Errorf("stored %d credentials despite a failed pull", secrets)
	}
}

func TestCreateCleanupUsesRecordedIDs(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	cs.setRunning("container-1")
	svc := newTestService(repo, cs)

	repo.updateErr = errors.New("database is down")
	if _, _, err := svc.Create(context.Background(), uuid.New(), CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	waitForCondition(t, "the unmanageable container removed", func() bool {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		return len(cs.removes) == 1 && cs.removes[0] == "container-1" &&
			len(cs.removeServers) == 1 && cs.removeServers[0] == serverID
	})
}

func TestDeleteDuringProvisionRemovesLateContainer(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()
	ctx := context.Background()

	var databaseID uuid.UUID
	repo.afterCreate = func(d Database) { databaseID = d.ID }
	cs.afterRun = func() {
		if err := svc.Delete(ctx, owner, databaseID); err != nil {
			t.Errorf("Delete during provisioning: %v", err)
		}
	}

	if _, _, err := svc.Create(ctx, owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	waitForCondition(t, "the late container removed", func() bool {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		return len(cs.removes) == 1 && cs.removes[0] == "container-1" &&
			len(cs.removeServers) == 1 && cs.removeServers[0] == serverID
	})
	if _, err := repo.GetDatabase(ctx, databaseID); !errors.Is(err, ErrNotFound) {
		t.Errorf("row lookup = %v, want the row to stay deleted", err)
	}
}

func TestSoftDeletedRowRejectsProvisioningWrites(t *testing.T) {
	repo := newFakeRepository()
	owner := uuid.New()
	row := repo.seed(Database{
		UserID: owner, ServerID: uuid.New(), EnvironmentID: uuid.New(), Name: "orders",
		Engine: EnginePostgres, Status: StatusCreating,
	})
	ctx := context.Background()
	if _, err := repo.SoftDeleteDatabase(ctx, row.ID); err != nil {
		t.Fatalf("SoftDeleteDatabase: %v", err)
	}

	if _, err := repo.UpdateDatabaseContainer(ctx, row.ID, "late"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDatabaseContainer after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateDatabaseStatus(ctx, row.ID, StatusRunning, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDatabaseStatus after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateDatabaseName(ctx, row.ID, "late"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDatabaseName after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetDatabase(ctx, row.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDatabase = %v, want the row to stay deleted", err)
	}
}

func TestConcurrentRenameAndLifecycleDoNotClobber(t *testing.T) {
	for i := 0; i < 50; i++ {
		repo := newFakeRepository()
		serverID := repo.seedServer()
		envID, _ := repo.seedEnvironment()
		cs := &fakeContainers{runID: "container-1"}
		svc := newTestService(repo, cs)
		owner := uuid.New()
		ctx := context.Background()

		created, _ := createDatabaseAndWait(t, svc, owner, CreateRequest{
			Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
		})

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := svc.Update(ctx, owner, created.ID, UpdateRequest{Name: ptr("renamed")}); err != nil {
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

func TestCreateCredentialFailureRollsBackAndMarksError(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	var createdID uuid.UUID
	repo.afterCreate = func(d Database) { createdID = d.ID }
	repo.secretFailAt = 2
	repo.secretFail = errors.New("credentials table is down")

	_, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
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

func TestCreateRejectsPublicPortInUse(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	owner := uuid.New()
	repo.seed(Database{
		UserID: owner, EnvironmentID: envID, ServerID: serverID, Name: "taken",
		Engine: EnginePostgres, Status: StatusRunning, PublicPort: 5433,
	})
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)

	_, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID, PublicPort: 5433,
	})
	if !errors.Is(err, ErrPortConflict) {
		t.Fatalf("Create error = %v, want ErrPortConflict", err)
	}
	if cs.pulls != 0 || len(cs.runs) != 0 {
		t.Errorf("create touched the node (pulls %d, runs %d) despite a port conflict", cs.pulls, len(cs.runs))
	}
	list, err := svc.List(context.Background(), owner, DatabaseFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("databases = %d, want the port-conflict create to leave no row", len(list))
	}
}

func TestCreateMapsDockerPortConflict(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runErr: containers.ErrPortConflict}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID, PublicPort: 5433,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	row := waitForDatabaseStatus(t, svc, owner, created.ID, StatusError)
	if row.Status != StatusError {
		t.Fatalf("row = %+v, want the error state", row)
	}
	if len(cs.removes) != 0 {
		t.Errorf("removes = %v, want none (the failed run left no container)", cs.removes)
	}
}

func TestCreateStatusWriteTransientFailureKeepsContainer(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	var createdID uuid.UUID
	repo.afterCreate = func(d Database) { createdID = d.ID }
	repo.statusErrFor = StatusRunning
	repo.statusErr = errors.New("transient status write failure")

	if _, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	row := waitForDatabaseStatus(t, svc, owner, createdID, StatusError)
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

func TestCreateStatusWriteFencedByDeleteRemovesContainer(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)

	repo.statusErrFor = StatusRunning
	repo.statusErr = ErrNotFound

	if _, _, err := svc.Create(context.Background(), uuid.New(), CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForCondition(t, "the fenced container removed", func() bool {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		return len(cs.removes) == 1 && cs.removes[0] == "container-1"
	})
}

func TestProvisionSurvivesRequestCancellation(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1"}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	ctx, cancel := context.WithCancel(context.Background())
	created, _, err := svc.Create(ctx, owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	cancel()

	row := waitForDatabaseStatus(t, svc, owner, created.ID, StatusRunning)
	if row.ContainerID != "container-1" {
		t.Errorf("container_id = %q, want the provision to finish after the request ends", row.ContainerID)
	}
}

func TestCloseCancelsProvisioningAndMarksInterrupted(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{runID: "container-1", suppressRunning: true}
	svc := newTestService(repo, cs)
	owner := uuid.New()

	created, _, err := svc.Create(context.Background(), owner, CreateRequest{
		Name: "orders", Engine: EnginePostgres, EnvironmentID: envID, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	row, err := svc.Get(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != StatusError || row.StatusMessage != interruptedProvisionMessage {
		t.Errorf("row = %+v, want error with the interrupted message", row)
	}
}

func TestDeleteRemovesContainerPersistedAfterRead(t *testing.T) {
	repo := newFakeRepository()
	serverID := repo.seedServer()
	envID, _ := repo.seedEnvironment()
	cs := &fakeContainers{}
	svc := newTestService(repo, cs)
	owner := uuid.New()
	row := repo.seed(Database{
		UserID: owner, EnvironmentID: envID, ServerID: serverID, Name: "orders",
		Engine: EnginePostgres, Status: StatusCreating,
	})
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

func envContains(env []string, value string) bool {
	for _, entry := range env {
		if strings.HasSuffix(entry, "="+value) {
			return true
		}
	}
	return false
}
