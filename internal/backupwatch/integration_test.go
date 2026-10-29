package backupwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type protectionSeed struct {
	userID        pgtype.UUID
	projectID     uuid.UUID
	databaseID    uuid.UUID
	targetID      uuid.UUID
	serverID      pgtype.UUID
	environmentID pgtype.UUID
}

func seedProtectionDatabase(t *testing.T, st *store.Store) protectionSeed {
	t.Helper()
	ctx := context.Background()
	user, err := st.CreateUser(ctx, fmt.Sprintf("bw-it-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Name: fmt.Sprintf("bw-it-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, ProjectID: project.ID, Name: "production",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name: fmt.Sprintf("bw-it-node-%d", time.Now().UnixNano()), Ip: "127.0.0.1", Port: 22, SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	seed := protectionSeed{
		userID: user.ID, projectID: uuid.UUID(project.ID.Bytes),
		databaseID: uuid.New(), targetID: uuid.New(),
		serverID: server.ID, environmentID: environment.ID,
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO backup_targets (id, user_id, name, kind, bucket)
		VALUES ($1, $2, 'offsite', 's3', 'bucket')`, seed.targetID, seed.userID); err != nil {
		t.Fatalf("insert target: %v", err)
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO databases (id, user_id, server_id, environment_id, name, engine, status)
		VALUES ($1, $2, $3, $4, 'orders', 'postgres', 'running')`,
		seed.databaseID, seed.userID, server.ID, environment.ID); err != nil {
		t.Fatalf("insert database: %v", err)
	}
	return seed
}

func insertProtectionSchedule(t *testing.T, st *store.Store, seed protectionSeed, nextRun, lastSlot time.Time) uuid.UUID {
	t.Helper()
	return insertResourceSchedule(t, st, "database_id", seed.databaseID, seed.targetID, nextRun, lastSlot)
}

func insertResourceSchedule(
	t *testing.T, st *store.Store, column string, resourceID, targetID uuid.UUID, nextRun, lastSlot time.Time,
) uuid.UUID {
	t.Helper()
	scheduleID := uuid.New()
	var lastSlotArg any
	if !lastSlot.IsZero() {
		lastSlotArg = lastSlot
	}
	if _, err := st.DB.Exec(context.Background(), `INSERT INTO backup_schedules
		(id, `+column+`, cron, target_id, enabled, next_run_at, last_slot_at, max_run_seconds)
		VALUES ($1, $2, '0 2 * * *', $3, true, $4, $5, 7200)`,
		scheduleID, resourceID, targetID, nextRun, lastSlotArg); err != nil {
		t.Fatalf("insert schedule: %v", err)
	}
	return scheduleID
}

func seedProtectionApplication(t *testing.T, st *store.Store, seed protectionSeed) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := st.DB.Exec(context.Background(), `INSERT INTO applications
		(id, user_id, server_id, environment_id, name)
		VALUES ($1, $2, $3, $4, 'storefront')`, id, seed.userID, seed.serverID, seed.environmentID); err != nil {
		t.Fatalf("insert application: %v", err)
	}
	return id
}

func seedProtectionService(t *testing.T, st *store.Store, seed protectionSeed) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := st.DB.Exec(context.Background(), `INSERT INTO services
		(id, user_id, server_id, environment_id, name, compose_yaml)
		VALUES ($1, $2, $3, $4, 'watchtower', 'services: {}')`, id, seed.userID, seed.serverID, seed.environmentID); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	return id
}

func newProtectionService(t *testing.T, st *store.Store, now func() time.Time) (*Service, *incidents.Service) {
	t.Helper()
	incidentSvc := incidents.NewService(incidents.Config{
		Store:  st,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    now,
	})
	service := NewService(Config{
		Store:     NewStoreSource(st),
		Incidents: incidentSvc,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:       now,
	})
	return service, incidentSvc
}

func openOverdueIncident(t *testing.T, seed protectionSeed, scheduleID uuid.UUID, incidentSvc *incidents.Service, at time.Time) {
	t.Helper()
	_, transition, err := incidentSvc.Report(context.Background(), incidents.Observation{
		Identity:     Identity(ResourceDatabase, scheduleID),
		Detector:     Detector,
		ScopeKind:    incidents.ScopeProject,
		ScopeID:      seed.projectID,
		ProjectID:    seed.projectID,
		ResourceType: ResourceDatabase,
		ResourceID:   seed.databaseID,
		ResourceName: "orders",
		At:           at,
		Valid:        true,
		Unhealthy:    true,
		Severity:     incidents.SeverityWarning,
		Evidence:     "integration overdue",
	})
	if err != nil {
		t.Fatalf("open incident: %v", err)
	}
	if transition.Event != incidents.TransitionOpened {
		t.Fatalf("transition = %q, want opened", transition.Event)
	}
}

func getProtection(t *testing.T, resource, resourceID string, service *Service) (int, Summary) {
	t.Helper()
	router := chi.NewRouter()
	Mount(router, func(next http.Handler) http.Handler { return next }, service)
	request := httptest.NewRequest(http.MethodGet,
		"/v1/"+resource+"/"+resourceID+"/backup-protection", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	var body struct {
		Protection Summary `json:"protection"`
	}
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v (%s)", err, recorder.Body.String())
		}
	}
	return recorder.Code, body.Protection
}

func TestProtectionHTTPStates(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	t.Run("no schedule is not configured", func(t *testing.T) {
		st := scratchStore(t)
		seed := seedProtectionDatabase(t, st)
		service, _ := newProtectionService(t, st, func() time.Time { return now })
		code, summary := getProtection(t, "databases", seed.databaseID.String(), service)
		if code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
		if summary.Status != StatusNotConfigured || len(summary.Schedules) != 0 {
			t.Fatalf("summary = %+v", summary)
		}
	})

	t.Run("schedule not yet due is scheduled", func(t *testing.T) {
		st := scratchStore(t)
		seed := seedProtectionDatabase(t, st)
		insertProtectionSchedule(t, st, seed, now.Add(time.Hour), time.Time{})
		service, _ := newProtectionService(t, st, func() time.Time { return now })
		code, summary := getProtection(t, "databases", seed.databaseID.String(), service)
		if code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
		if summary.Status != StatusScheduled || len(summary.Schedules) != 1 ||
			summary.Schedules[0].Status != StatusScheduled {
			t.Fatalf("summary = %+v", summary)
		}
	})

	t.Run("overdue with an open incident", func(t *testing.T) {
		st := scratchStore(t)
		seed := seedProtectionDatabase(t, st)
		slot := now.Add(-2 * time.Hour)
		scheduleID := insertProtectionSchedule(t, st, seed, slot, slot)
		service, incidentSvc := newProtectionService(t, st, func() time.Time { return now })
		openOverdueIncident(t, seed, scheduleID, incidentSvc, now)
		code, summary := getProtection(t, "databases", seed.databaseID.String(), service)
		if code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
		if summary.Status != StatusOverdue || len(summary.Schedules) != 1 ||
			summary.Schedules[0].Status != StatusOverdue {
			t.Fatalf("summary = %+v", summary)
		}
	})
}

func TestObserverSweepOpensAndResolvesOverdueIncident(t *testing.T) {
	ctx := context.Background()
	st := scratchStore(t)
	seed := seedProtectionDatabase(t, st)
	now := time.Now().UTC().Truncate(time.Second)
	slot := now.Add(-2 * time.Hour)
	scheduleID := insertProtectionSchedule(t, st, seed, slot, slot)
	service, _ := newProtectionService(t, st, func() time.Time { return now })

	service.tick(ctx)
	service.tick(ctx)
	active, err := st.ListActiveBackupProtectionIncidents(ctx)
	if err != nil {
		t.Fatalf("list active incidents: %v", err)
	}
	if len(active) != 1 || active[0].IdentityKey != Identity(ResourceDatabase, scheduleID) {
		t.Fatalf("active incidents = %+v", active)
	}

	if _, err := st.DB.Exec(ctx, `INSERT INTO backups
		(id, database_id, type, status, target_id, created_at, finished_at)
		VALUES ($1, $2, 'manual', 'completed', $3, $4, $4)`,
		uuid.New(), seed.databaseID, seed.targetID, now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert qualifying backup: %v", err)
	}
	now = now.Add(time.Minute)
	service.tick(ctx)
	active, err = st.ListActiveBackupProtectionIncidents(ctx)
	if err != nil {
		t.Fatalf("list active incidents after success: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("incident did not resolve: %+v", active)
	}
}

func TestProtectionHTTPRoutesCoverApplicationsAndServices(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	st := scratchStore(t)
	seed := seedProtectionDatabase(t, st)
	applicationID := seedProtectionApplication(t, st, seed)
	serviceID := seedProtectionService(t, st, seed)
	insertResourceSchedule(t, st, "application_id", applicationID, seed.targetID, now.Add(time.Hour), time.Time{})
	insertResourceSchedule(t, st, "service_id", serviceID, seed.targetID, now.Add(time.Hour), time.Time{})
	insertProtectionSchedule(t, st, seed, now.Add(time.Hour), time.Time{})
	service, _ := newProtectionService(t, st, func() time.Time { return now })

	for _, tc := range []struct {
		resource string
		id       uuid.UUID
		wantType string
	}{
		{"applications", applicationID, ResourceApplication},
		{"services", serviceID, ResourceService},
		{"databases", seed.databaseID, ResourceDatabase},
	} {
		code, summary := getProtection(t, tc.resource, tc.id.String(), service)
		if code != http.StatusOK {
			t.Fatalf("%s status = %d", tc.resource, code)
		}
		if summary.ResourceType != tc.wantType || summary.ResourceID != tc.id.String() {
			t.Fatalf("%s summary = %+v", tc.resource, summary)
		}
		if len(summary.Schedules) != 1 || summary.Schedules[0].Status != StatusScheduled {
			t.Fatalf("%s schedules = %+v", tc.resource, summary.Schedules)
		}
		if tc.wantType == ResourceDatabase {
			if summary.DatabaseID != tc.id.String() {
				t.Fatalf("database_id = %q", summary.DatabaseID)
			}
		} else if summary.DatabaseID != "" {
			t.Fatalf("%s exposed database_id %q", tc.resource, summary.DatabaseID)
		}
	}

	code, _ := getProtection(t, "applications", uuid.New().String(), service)
	if code != http.StatusNotFound {
		t.Fatalf("unknown application = %d, want 404", code)
	}
}

func TestObserverRaisesAndResolvesApplicationAndServiceIncidents(t *testing.T) {
	ctx := context.Background()
	st := scratchStore(t)
	seed := seedProtectionDatabase(t, st)
	applicationID := seedProtectionApplication(t, st, seed)
	serviceID := seedProtectionService(t, st, seed)
	now := time.Now().UTC().Truncate(time.Second)
	slot := now.Add(-2 * time.Hour)
	applicationSchedule := insertResourceSchedule(t, st, "application_id", applicationID, seed.targetID, slot, slot)
	serviceSchedule := insertResourceSchedule(t, st, "service_id", serviceID, seed.targetID, slot, slot)
	service, _ := newProtectionService(t, st, func() time.Time { return now })

	service.tick(ctx)
	active, err := st.ListActiveBackupProtectionIncidents(ctx)
	if err != nil {
		t.Fatalf("list active incidents: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("active incidents = %+v", active)
	}
	for _, tc := range []struct {
		scheduleID uuid.UUID
		wantType   string
		wantID     uuid.UUID
		wantName   string
	}{
		{applicationSchedule, ResourceApplication, applicationID, "storefront"},
		{serviceSchedule, ResourceService, serviceID, "watchtower"},
	} {
		incident, err := st.GetActiveIncidentByIdentity(ctx, Identity(tc.wantType, tc.scheduleID))
		if err != nil {
			t.Fatalf("read %s incident: %v", tc.wantType, err)
		}
		if incident.ResourceType != tc.wantType || uuid.UUID(incident.ResourceID.Bytes) != tc.wantID ||
			incident.ResourceName != tc.wantName {
			t.Fatalf("%s incident = %+v", tc.wantType, incident)
		}
		if uuid.UUID(incident.ProjectID.Bytes) != seed.projectID {
			t.Fatalf("%s incident project = %s", tc.wantType, uuid.UUID(incident.ProjectID.Bytes))
		}
	}

	for _, tc := range []struct {
		column string
		id     uuid.UUID
	}{{"application_id", applicationID}, {"service_id", serviceID}} {
		if _, err := st.DB.Exec(ctx, `INSERT INTO backups
			(id, `+tc.column+`, type, status, target_id, created_at, finished_at)
			VALUES ($1, $2, 'manual', 'completed', $3, $4, $4)`,
			uuid.New(), tc.id, seed.targetID, now.Add(-time.Hour)); err != nil {
			t.Fatalf("insert %s backup: %v", tc.column, err)
		}
	}
	now = now.Add(time.Minute)
	service.tick(ctx)
	active, err = st.ListActiveBackupProtectionIncidents(ctx)
	if err != nil {
		t.Fatalf("list active incidents after success: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("incidents did not resolve: %+v", active)
	}
}
