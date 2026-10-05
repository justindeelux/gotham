package projects

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// seedResourceEnv creates a team, a project with two environments and a node
// on the scratch database and returns their IDs.
func seedResourceEnv(t *testing.T, ctx context.Context, st interface {
	CreateTeam(context.Context, sqlc.CreateTeamParams) (sqlc.Team, error)
	CreateProject(context.Context, sqlc.CreateProjectParams) (sqlc.Project, error)
	CreateEnvironment(context.Context, sqlc.CreateEnvironmentParams) (sqlc.Environment, error)
	CreateServer(context.Context, sqlc.CreateServerParams) (sqlc.Server, error)
},
) (teamID, projectID, productionID, stagingID, serverID uuid.UUID) {
	t.Helper()
	suffix := time.Now().UnixNano()

	team, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("counter-%d", suffix),
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:     pgtype.UUID{Bytes: uuid.New(), Valid: true},
		TeamID: team.ID,
		Name:   "shop",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	production, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ProjectID: project.ID,
		Name:      "production",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	staging, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ProjectID: project.ID,
		Name:      "staging",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment (staging): %v", err)
	}
	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("counter-node-%d", suffix),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	return uuidOf(team.ID), uuidOf(project.ID), uuidOf(production.ID), uuidOf(staging.ID), uuidOf(server.ID)
}

// TestStoreCounterTalliesResources attaches one workload of each kind to
// production and asserts the environment and project counts; staging stays
// empty and previews count toward the totals.
func TestStoreCounterTalliesResources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := newScratchStore(t)
	counter := StoreCounter{Store: st}
	teamID, projectID, productionID, stagingID, serverID := seedResourceEnv(t, ctx, st)
	owner, err := st.CreateUser(ctx, fmt.Sprintf("counter-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	ownerID := uuidOf(owner.ID)

	user := pgtype.UUID{Bytes: ownerID, Valid: true}
	team := pgtype.UUID{Bytes: teamID, Valid: true}
	server := pgtype.UUID{Bytes: serverID, Valid: true}
	production := pgtype.UUID{Bytes: productionID, Valid: true}
	if _, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: user, TeamID: team, ServerID: server, EnvironmentID: production,
		Name: "web", CloneUrl: "https://github.com/acme/demo.git", Branch: "main", BuildPack: "dockerfile",
	}); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if _, err := st.CreateService(ctx, sqlc.CreateServiceParams{
		ID: user, UserID: user, TeamID: team, ServerID: server, EnvironmentID: production,
		Name: "worker", Status: "creating", ComposeYaml: "services: {}", Env: []byte("{}"),
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if _, err := st.CreateDatabase(ctx, sqlc.CreateDatabaseParams{
		ID: user, UserID: user, TeamID: team, ServerID: server, EnvironmentID: production,
		Name: "db", Engine: "postgres", Status: "creating", StoragePath: "gotham-db-x",
	}); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	productionCounts, err := counter.CountEnvironmentResources(ctx, productionID)
	if err != nil {
		t.Fatalf("count production: %v", err)
	}
	if productionCounts != (ResourceCounts{Applications: 1, Services: 1, Databases: 1}) {
		t.Fatalf("production counts = %+v, want one of each", productionCounts)
	}
	stagingCounts, err := counter.CountEnvironmentResources(ctx, stagingID)
	if err != nil {
		t.Fatalf("count staging: %v", err)
	}
	if stagingCounts != (ResourceCounts{}) {
		t.Fatalf("staging counts = %+v, want zeros", stagingCounts)
	}
	projectCounts, err := counter.CountProjectResources(ctx, projectID)
	if err != nil {
		t.Fatalf("count project: %v", err)
	}
	if projectCounts != (ResourceCounts{Applications: 1, Services: 1, Databases: 1}) {
		t.Fatalf("project counts = %+v, want one of each", projectCounts)
	}
}
