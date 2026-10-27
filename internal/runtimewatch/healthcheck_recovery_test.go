package runtimewatch

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(delta)
}

type transitionReporter struct {
	svc    *incidents.Service
	mu     sync.Mutex
	events []string
}

func (r *transitionReporter) Report(ctx context.Context, obs incidents.Observation) (*incidents.Incident, incidents.Transition, error) {
	inc, transition, err := r.svc.Report(ctx, obs)
	if transition.Event != "" {
		r.mu.Lock()
		r.events = append(r.events, transition.Event)
		r.mu.Unlock()
	}
	return inc, transition, err
}

func (r *transitionReporter) Close(ctx context.Context, identity, reason string) error {
	return r.svc.Close(ctx, identity, reason)
}

func (r *transitionReporter) lastEvent() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return ""
	}
	return r.events[len(r.events)-1]
}

func seedPlainApplication(t *testing.T, st *store.Store) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	user, err := st.CreateUser(ctx, fmt.Sprintf("runtime-health-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("runtime-health-%d", suffix),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM projects WHERE id = $1", project.ID)
	})
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ProjectID: project.ID,
		Name:      "production",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name: fmt.Sprintf("runtime-health-node-%d", suffix), Ip: "127.0.0.1", Port: 22, SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	serverID := uuid.UUID(serverRow.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID)
	})
	appRow, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		ID:            pgtype.UUID{Bytes: uuid.New(), Valid: true},
		UserID:        user.ID,
		ServerID:      serverRow.ID,
		EnvironmentID: environment.ID,
		Name:          fmt.Sprintf("runtime-health-app-%d", suffix),
		SourceType:    "image",
		Provider:      "image",
		Branch:        "main",
		BuildPack:     "auto",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	return uuid.UUID(appRow.ID.Bytes), serverID
}

func TestNoHealthcheckRunningRecoversStoppedIncident(t *testing.T) {
	st := integrationStore(t)
	ctx := context.Background()
	appID, serverID := seedPlainApplication(t, st)
	clock := &fakeClock{at: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	lifecycle := incidents.NewService(incidents.Config{Store: st, Now: clock.Now})
	reporter := &transitionReporter{svc: lifecycle}
	svc := NewService(Config{Store: st, Incidents: reporter, Now: clock.Now})

	containerID := strings.Repeat("a", 64)
	observe := func(state, health string) {
		t.Helper()
		svc.Observe(ctx, servers.NodeObservation{
			ServerID:   serverID,
			ObservedAt: clock.Now(),
			Inventory: servers.ContainerInventory{
				Complete: true,
				Containers: []servers.ContainerObservation{{
					ID:      containerID,
					State:   state,
					Health:  health,
					AppID:   appID.String(),
					Managed: true,
				}},
			},
		})
	}

	observe("running", "")
	clock.Advance(15 * time.Second)
	observe("exited", "")
	for i := 0; i < 4; i++ {
		clock.Advance(15 * time.Second)
		observe("exited", "")
	}
	if got := reporter.lastEvent(); got != incidents.TransitionOpened {
		t.Fatalf("stopped container transition = %q, want opened", got)
	}

	observe("running", "")
	for i := 0; i < 5; i++ {
		clock.Advance(15 * time.Second)
		observe("running", "")
	}
	if got := reporter.lastEvent(); got != incidents.TransitionRecovered {
		t.Fatalf("recovered container transition = %q, want recovered", got)
	}

	rows, err := st.ListRuntimeMonitorReplicaStates(ctx, sqlc.ListRuntimeMonitorReplicaStatesParams{
		ResourceType: ResourceApplication,
		ResourceID:   pgUUID(appID),
	})
	if err != nil || len(rows) != 1 {
		t.Fatalf("replica rows = %d, %v", len(rows), err)
	}
	row := rows[0]
	if row.State != "running" || row.Health != "unknown" || row.Reason != "no_healthcheck" {
		t.Fatalf("summary row = %s/%s/%s, want running/unknown/no_healthcheck", row.State, row.Health, row.Reason)
	}
	summary, _ := aggregateRows(summaryRows(rows))
	if summary != StateUnknown {
		t.Fatalf("summary = %q, want unknown", summary)
	}

	// A native healthcheck that stays unhealthy must not resolve its incident.
	recovered := len(reporter.events)
	clock.Advance(15 * time.Second)
	observe("running", "unhealthy")
	for i := 0; i < 6; i++ {
		clock.Advance(15 * time.Second)
		observe("running", "unhealthy")
	}
	reporter.mu.Lock()
	after := append([]string(nil), reporter.events...)
	reporter.mu.Unlock()
	if len(after) == recovered || after[len(after)-1] != incidents.TransitionOpened {
		t.Fatalf("unhealthy transition = %v, want a new opened incident", after[recovered:])
	}
	for _, event := range after[recovered:] {
		if event == incidents.TransitionRecovered {
			t.Fatalf("native unhealthy incident recovered: %v", after[recovered:])
		}
	}
}
