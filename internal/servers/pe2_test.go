package servers

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestDeleteRefusesServerWithResources is the PE-2 delete guard: a node with
// applications, services or databases answers 409 naming the blocking
// resources, and deletes cleanly once they are gone.
func TestDeleteRefusesServerWithResources(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	server, err := service.Add(ctx, uuid.New(), fmt.Sprintf("guarded-%d", suffix), "127.0.0.1", 22, "root", uuid.Nil, "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM applications WHERE server_id = $1", pgUUID(server.ID))
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM services WHERE server_id = $1", pgUUID(server.ID))
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM databases WHERE server_id = $1", pgUUID(server.ID))
		_ = st.DeleteServer(cleanupCtx, pgUUID(server.ID))
	})

	team, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("guard-%d", suffix),
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM teams WHERE id = $1", team.ID)
	})
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:     pgtype.UUID{Bytes: uuid.New(), Valid: true},
		TeamID: team.ID,
		Name:   "shop",
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
	user, err := st.CreateUser(ctx, fmt.Sprintf("guard-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	if _, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: user.ID, TeamID: team.ID, ServerID: pgUUID(server.ID), EnvironmentID: environment.ID,
		Name: "web", CloneUrl: "https://github.com/acme/demo.git", Branch: "main", BuildPack: "dockerfile",
	}); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if _, err := st.CreateService(ctx, sqlc.CreateServiceParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, UserID: user.ID, TeamID: team.ID,
		ServerID: pgUUID(server.ID), EnvironmentID: environment.ID,
		Name: "worker", Status: "creating", ComposeYaml: "services: {}", Env: []byte("{}"),
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if _, err := st.CreateDatabase(ctx, sqlc.CreateDatabaseParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, UserID: user.ID, TeamID: team.ID,
		ServerID: pgUUID(server.ID), EnvironmentID: environment.ID,
		Name: "db", Engine: "postgres", Status: "creating", StoragePath: "gotham-db-guard",
	}); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	err = service.Delete(ctx, server.ID)
	if !isConflict(err) {
		t.Fatalf("Delete = %v, want ErrConflict", err)
	}
	for _, name := range []string{"web", "worker", "db"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("conflict body %q does not name %q", err.Error(), name)
		}
	}

	if _, err := st.DB.Exec(ctx, "DELETE FROM applications WHERE server_id = $1", pgUUID(server.ID)); err != nil {
		t.Fatalf("delete applications: %v", err)
	}
	if _, err := st.DB.Exec(ctx, "DELETE FROM services WHERE server_id = $1", pgUUID(server.ID)); err != nil {
		t.Fatalf("delete services: %v", err)
	}
	if _, err := st.DB.Exec(ctx, "DELETE FROM databases WHERE server_id = $1", pgUUID(server.ID)); err != nil {
		t.Fatalf("delete databases: %v", err)
	}
	if err := service.Delete(ctx, server.ID); err != nil {
		t.Fatalf("Delete after clearing resources: %v", err)
	}
}

func isConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrConflict.Error())
}
