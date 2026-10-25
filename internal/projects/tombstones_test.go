package projects

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

type tombstoneFixture struct {
	userID      uuid.UUID
	projectID   uuid.UUID
	envID       uuid.UUID
	otherEnvID  uuid.UUID
	serverID    uuid.UUID
	serviceID   uuid.UUID
	databaseID  uuid.UUID
	databaseVol string
}

func seedTombstones(t *testing.T, ctx context.Context, st *store.Store) tombstoneFixture {
	t.Helper()
	suffix := time.Now().UnixNano()

	user, err := st.CreateUser(ctx, fmt.Sprintf("tomb-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuidOf(user.ID)

	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("tomb-node-%d", suffix),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true},

		Name: fmt.Sprintf("tomb-%d", suffix),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	mkEnv := func(name string) uuid.UUID {
		env, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
			ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
			ProjectID: project.ID,
			Name:      name,
		})
		if err != nil {
			t.Fatalf("CreateEnvironment(%s): %v", name, err)
		}
		return uuidOf(env.ID)
	}
	envID, otherEnvID := mkEnv("production"), mkEnv("staging")

	userPG := pgtype.UUID{Bytes: userID, Valid: true}
	envPG := pgtype.UUID{Bytes: envID, Valid: true}
	service, err := st.CreateService(ctx, sqlc.CreateServiceParams{
		ID: userPG, UserID: userPG, ServerID: server.ID, EnvironmentID: envPG,
		Name: "worker", Status: "creating", ComposeYaml: "services: {}", Env: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	database, err := st.CreateDatabase(ctx, sqlc.CreateDatabaseParams{
		ID: userPG, UserID: userPG, ServerID: server.ID, EnvironmentID: envPG,
		Name: "db", Engine: "postgres", Status: "creating", StoragePath: fmt.Sprintf("gotham-db-tomb-%d", suffix),
	})
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		"UPDATE services SET status = 'deleting', deleted_at = now(), updated_at = now() WHERE id = $1",
		service.ID); err != nil {
		t.Fatalf("soft-delete service: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		"UPDATE databases SET status = 'deleting', deleted_at = now(), updated_at = now() WHERE id = $1",
		database.ID); err != nil {
		t.Fatalf("soft-delete database: %v", err)
	}
	return tombstoneFixture{
		userID:      userID,
		projectID:   uuidOf(project.ID),
		envID:       envID,
		otherEnvID:  otherEnvID,
		serverID:    uuidOf(server.ID),
		serviceID:   uuidOf(service.ID),
		databaseID:  uuidOf(database.ID),
		databaseVol: database.StoragePath,
	}
}

func tombstonedRowGone(t *testing.T, ctx context.Context, st *store.Store, table string, id uuid.UUID) {
	t.Helper()
	var count int
	if err := st.DB.QueryRow(ctx,
		"SELECT count(*) FROM "+table+" WHERE id = $1", pgtype.UUID{Bytes: id, Valid: true}).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != 0 {
		t.Errorf("%s tombstone %s survived, want it purged", table, id)
	}
}

func TestDeleteEnvironmentPurgesTombstones(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := newScratchStore(t)
	fix := seedTombstones(t, ctx, st)

	svc := NewService(Config{Store: st, Counter: StoreCounter{Store: st}, Logger: discardLogger()})
	adminCtx := ctx

	counts, err := svc.counter.CountEnvironmentResources(ctx, fix.envID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if counts != (ResourceCounts{}) {
		t.Fatalf("tombstoned counts = %+v, want zeros", counts)
	}
	if err := svc.DeleteEnvironment(adminCtx, fix.userID, fix.envID); err != nil {
		t.Fatalf("DeleteEnvironment with only tombstones: %v", err)
	}
	tombstonedRowGone(t, ctx, st, "services", fix.serviceID)
	tombstonedRowGone(t, ctx, st, "databases", fix.databaseID)

	qaEnv, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ProjectID: pgtype.UUID{Bytes: fix.projectID, Valid: true},
		Name:      "qa",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment(qa): %v", err)
	}
	live, err := st.CreateService(ctx, sqlc.CreateServiceParams{
		ID:     pgtype.UUID{Bytes: uuid.New(), Valid: true},
		UserID: pgtype.UUID{Bytes: fix.userID, Valid: true},

		ServerID:      pgtype.UUID{Bytes: fix.serverID, Valid: true},
		EnvironmentID: qaEnv.ID,
		Name:          "live", Status: "creating", ComposeYaml: "services: {}", Env: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateService(live): %v", err)
	}
	_ = live
	if err := svc.DeleteEnvironment(adminCtx, fix.userID, uuidOf(qaEnv.ID)); err != ErrEnvironmentNotEmpty {
		t.Fatalf("DeleteEnvironment with a live service err = %v, want ErrEnvironmentNotEmpty", err)
	}
}

func TestDeleteProjectPurgesTombstones(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := newScratchStore(t)
	fix := seedTombstones(t, ctx, st)

	svc := NewService(Config{Store: st, Counter: StoreCounter{Store: st}, Logger: discardLogger()})
	adminCtx := ctx

	if err := svc.DeleteProject(adminCtx, fix.userID, fix.projectID); err != nil {
		t.Fatalf("DeleteProject with only tombstones: %v", err)
	}
	tombstonedRowGone(t, ctx, st, "services", fix.serviceID)
	tombstonedRowGone(t, ctx, st, "databases", fix.databaseID)
}
