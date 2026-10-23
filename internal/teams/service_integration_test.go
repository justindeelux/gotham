package teams

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

const integrationDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := os.Getenv("GOTHAM_TEST_DSN")
	explicit := dsn != ""
	if !explicit {
		dsn = integrationDSN
	}
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return store.New(pool)
}

func TestSetMemberRoleSerializesConcurrentDemotions(t *testing.T) {
	st := newTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	newUser := func(label string) pgtype.UUID {
		email := fmt.Sprintf("be-8.2-f4-%s-%d@example.com", label, suffix)
		user, err := st.CreateUser(ctx, email, nil)
		if err != nil {
			t.Fatalf("create user %s: %v", label, err)
		}
		return user.ID
	}
	alice := newUser("a")
	bob := newUser("b")
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, id := range []pgtype.UUID{alice, bob} {
			if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", id); err != nil {
				t.Logf("cleanup user: %v", err)
			}
		}
	})

	team, err := st.CreateTeamWithOwner(ctx, sqlc.CreateTeamParams{
		ID:   pgUUID(uuid.New()),
		Name: fmt.Sprintf("f4-%d", suffix),
	}, alice)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := st.DeleteTeam(cleanupCtx, team.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
	})
	if _, err := st.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: team.ID,
		UserID: bob,
		Role:   string(RoleOwner),
	}); err != nil {
		t.Fatalf("add second owner: %v", err)
	}

	svc := NewService(Config{Store: st, Logger: discardLogger()})

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i, pair := range [][2]pgtype.UUID{{alice, bob}, {bob, alice}} {
		wg.Add(1)
		go func(i int, actor, target pgtype.UUID) {
			defer wg.Done()
			_, results[i] = svc.SetMemberRole(ctx,
				uuid.UUID(actor.Bytes), uuid.UUID(team.ID.Bytes), uuid.UUID(target.Bytes), RoleReadOnly)
		}(i, pair[0], pair[1])
	}
	wg.Wait()

	succeeded := 0
	for _, err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrLastOwner), errors.Is(err, ErrForbidden):
		default:
			t.Fatalf("unexpected concurrent demotion error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful demotions = %d, want exactly one (the second must hit the last-owner rule)", succeeded)
	}

	owners, err := st.CountTeamOwners(ctx, team.ID)
	if err != nil {
		t.Fatalf("count owners: %v", err)
	}
	if owners < 1 {
		t.Fatalf("owners after concurrent demotions = %d, want at least 1", owners)
	}
}

func TestDeleteTeamSerializesWithConcurrentInsert(t *testing.T) {
	st := newTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	user, err := st.CreateUser(ctx, fmt.Sprintf("be-8.2-r2-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	team, err := st.CreateTeamWithOwner(ctx, sqlc.CreateTeamParams{
		ID:   pgUUID(uuid.New()),
		Name: fmt.Sprintf("r2-%d", suffix),
	}, user.ID)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM applications WHERE team_id = $1", team.ID); err != nil {
			t.Logf("cleanup applications: %v", err)
		}
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM teams WHERE id = $1", team.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	svc := NewService(Config{Store: st, Logger: discardLogger()})

	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:     pgUUID(uuid.New()),
		TeamID: team.ID,
		Name:   fmt.Sprintf("r2-%d", suffix),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgUUID(uuid.New()),
		ProjectID: project.ID,
		Name:      "production",
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("r2-node-%d", suffix),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})

	tx, err := st.DB.Begin(ctx)
	if err != nil {
		t.Fatalf("begin insert tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`INSERT INTO applications (user_id, team_id, server_id, environment_id, name, clone_url, branch, build_pack)
		 VALUES ($1, $2, $3, $4, $5, '', 'main', 'dockerfile')`,
		user.ID, team.ID, serverRow.ID, environment.ID, "race-app"); err != nil {
		t.Fatalf("insert racing application: %v", err)
	}

	deleted := make(chan error, 1)
	go func() { deleted <- svc.Delete(ctx, uuid.UUID(user.ID.Bytes), uuid.UUID(team.ID.Bytes)) }()

	time.Sleep(200 * time.Millisecond)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit racing insert: %v", err)
	}

	if err := <-deleted; !errors.Is(err, ErrTeamNotEmpty) {
		t.Fatalf("delete during a concurrent insert = %v, want ErrTeamNotEmpty", err)
	}

	var rows int
	if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM applications WHERE team_id = $1`, team.ID).Scan(&rows); err != nil {
		t.Fatalf("count applications: %v", err)
	}
	if rows != 1 {
		t.Fatalf("application rows after the refused delete = %d, want 1 (never cascaded)", rows)
	}
}
