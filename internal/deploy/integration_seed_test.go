package deploy

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// seedProjectEnvironment creates a team, a project, an environment and a node
// on the integration database and returns their IDs, cleaning them up at test
// end. Resource rows require an environment and a server since PE-2.
func seedProjectEnvironment(t *testing.T, ctx context.Context, st *store.Store) (teamID, envID, serverID uuid.UUID) {
	t.Helper()

	team, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("p13-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:     pgtype.UUID{Bytes: uuid.New(), Valid: true},
		TeamID: team.ID,
		Name:   fmt.Sprintf("shop-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ProjectID: project.ID,
		Name:      "production",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("p13-node-%d", time.Now().UnixNano()),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		for _, query := range []string{
			"DELETE FROM applications WHERE environment_id = $1",
			"DELETE FROM services WHERE environment_id = $1",
			"DELETE FROM databases WHERE environment_id = $1",
		} {
			if _, err := st.DB.Exec(cleanupCtx, query, environment.ID); err != nil {
				t.Logf("cleanup resources: %v", err)
			}
		}
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM teams WHERE id = $1", team.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})
	return uuidFromPG(team.ID), uuidFromPG(environment.ID), uuidFromPG(serverRow.ID)
}
