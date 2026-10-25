package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func replayTestStore(t *testing.T) (*Store, *Store) {
	t.Helper()
	dsn := os.Getenv("GOTHAM_TEST_DSN")
	if dsn == "" {
		t.Skip("set GOTHAM_TEST_DSN to an isolated PostgreSQL instance")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/postgres"
	admin, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("replay_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE " + name + " WITH (FORCE)") })
	u.Path = "/" + name
	ctx := context.Background()
	if err := Migrate(ctx, u.String(), MigrateUp); err != nil {
		t.Fatal(err)
	}
	first, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	second, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	return New(first), New(second)
}

func replayContext(t *testing.T, principal uuid.UUID, op string, target uuid.UUID, key string, input any) context.Context {
	t.Helper()
	ctx := requestreplay.With(context.Background(), &requestreplay.Request{Principal: principal, Operation: op, Target: target, Key: key})
	if err := requestreplay.Fingerprint(ctx, "stable-server-key", input); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func seedReplayResources(t *testing.T, st *Store) (uuid.UUID, sqlc.Project, sqlc.Environment, sqlc.Server) {
	t.Helper()
	ctx := context.Background()
	user, err := st.CreateUser(ctx, uuid.NewString()+"@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	project, env, err := st.CreateProjectWithEnvironment(ctx, sqlc.CreateProjectParams{ID: replayUUID(uuid.New()), Name: "base"}, replayUUID(uuid.New()), "production")
	if err != nil {
		t.Fatal(err)
	}
	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{Name: "node", Ip: "127.0.0.1", Port: 22, SshUser: "root"})
	if err != nil {
		t.Fatal(err)
	}
	return userID, project, env, server
}

func TestRequestReplayConcurrentAllBoundaries(t *testing.T) {
	st, other := replayTestStore(t)
	user, project, env, server := seedReplayResources(t, st)
	appParams := func() sqlc.CreateApplicationParams {
		return sqlc.CreateApplicationParams{ID: replayUUID(uuid.New()), UserID: replayUUID(user), EnvironmentID: env.ID, ServerID: server.ID, Name: "image", SourceType: "image", ImageRef: "busybox:latest"}
	}
	app, err := st.CreateApplicationWithConfig(context.Background(), appParams(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, table string
		target      uuid.UUID
		create      func(context.Context, *Store) (uuid.UUID, error)
	}{
		{"create_project", "projects", uuid.Nil, func(ctx context.Context, s *Store) (uuid.UUID, error) {
			p, _, e := s.CreateProjectWithEnvironment(ctx, sqlc.CreateProjectParams{ID: replayUUID(uuid.New()), Name: "accepted"}, replayUUID(uuid.New()), "production")
			return uuid.UUID(p.ID.Bytes), e
		}},
		{"create_environment", "environments", uuid.UUID(project.ID.Bytes), func(ctx context.Context, s *Store) (uuid.UUID, error) {
			e, err := s.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{ID: replayUUID(uuid.New()), ProjectID: project.ID, Name: "staging"})
			return uuid.UUID(e.ID.Bytes), err
		}},
		{"create_application", "applications", uuid.Nil, func(ctx context.Context, s *Store) (uuid.UUID, error) {
			p := appParams()
			p.Name = "accepted"
			a, e := s.CreateApplicationWithConfig(ctx, p, []sqlc.InsertEnvVarParams{{Key: "HELLO", Value: "private-config"}}, nil, nil)
			return uuid.UUID(a.ID.Bytes), e
		}},
		{"deploy", "deployments", uuid.UUID(app.ID.Bytes), func(ctx context.Context, s *Store) (uuid.UUID, error) {
			d, e := s.CreateDeployment(ctx, sqlc.CreateDeploymentParams{ApplicationID: app.ID, Kind: "deploy", State: "queued"})
			return uuid.UUID(d.ID.Bytes), e
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var before int
			if err := st.DB.QueryRow(context.Background(), "SELECT count(*) FROM "+tc.table).Scan(&before); err != nil {
				t.Fatal(err)
			}
			type answer struct {
				id     uuid.UUID
				err    error
				result *requestreplay.Result
			}
			results := make(chan answer, 8)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					ctx := replayContext(t, user, tc.name, tc.target, "same-key", "canonical-input")
					s := st
					if i%2 == 1 {
						s = other
					}
					id, err := tc.create(ctx, s)
					results <- answer{id, err, requestreplay.ResultFrom(ctx)}
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)
			var accepted uuid.UUID
			first := 0
			for a := range results {
				if a.err != nil {
					t.Fatal(a.err)
				}
				if accepted == uuid.Nil {
					accepted = a.id
				}
				if a.id != accepted || a.result == nil {
					t.Fatalf("unstable result: %+v", a)
				}
				if !a.result.Replayed {
					first++
				}
			}
			if first != 1 {
				t.Fatalf("first acceptances=%d", first)
			}
			var after int
			_ = st.DB.QueryRow(context.Background(), "SELECT count(*) FROM "+tc.table).Scan(&after)
			if after != before+1 {
				t.Fatalf("rows %d -> %d", before, after)
			}
			ctx := replayContext(t, user, tc.name, tc.target, "same-key", "canonical-input")
			id, e := tc.create(ctx, other)
			if e != nil || id != accepted || !requestreplay.Replayed(ctx) {
				t.Fatalf("response loss recovery: %v %v", id, e)
			}
			changed := replayContext(t, user, tc.name, tc.target, "same-key", "changed-input")
			if _, e := tc.create(changed, st); !errors.Is(e, requestreplay.ErrConflict) {
				t.Fatalf("changed payload: %v", e)
			}
		})
	}
	var rows string
	if err := st.DB.QueryRow(context.Background(), "SELECT json_agg(r)::text FROM request_receipts r").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"canonical-input", "private-config", "stable-server-key"} {
		if strings.Contains(rows, secret) {
			t.Fatalf("receipt leaked %q", secret)
		}
	}
}

func TestRequestReplayRollbackExpiryIsolationAndDeletion(t *testing.T) {
	st, _ := replayTestStore(t)
	user, _, _, _ := seedReplayResources(t, st)
	ctx := context.Background()
	create := func(c context.Context, name string) (sqlc.Project, error) {
		p, _, err := st.CreateProjectWithEnvironment(c, sqlc.CreateProjectParams{ID: replayUUID(uuid.New()), Name: name}, replayUUID(uuid.New()), "production")
		return p, err
	}
	request := func(key string) context.Context {
		return replayContext(t, user, "create_project", uuid.Nil, key, "body")
	}
	for _, deferred := range []bool{false, true} {
		trigger := `CREATE TRIGGER fail_receipt BEFORE INSERT ON request_receipts FOR EACH ROW EXECUTE FUNCTION reject_receipt()`
		if deferred {
			trigger = `CREATE CONSTRAINT TRIGGER fail_receipt AFTER INSERT ON request_receipts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_receipt()`
		}
		if _, err := st.DB.Exec(ctx, `CREATE FUNCTION reject_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected'; END $$; `+trigger); err != nil {
			t.Fatal(err)
		}
		c := request("rollback")
		if _, err := create(c, "rollback"); err == nil || requestreplay.ResultFrom(c) != nil {
			t.Fatalf("failure accepted: %v", err)
		}
		var count int
		_ = st.DB.QueryRow(ctx, "SELECT count(*) FROM projects WHERE name='rollback'").Scan(&count)
		if count != 0 {
			t.Fatal("resource survived failed receipt/commit")
		}
		if _, err := st.DB.Exec(ctx, "DROP TRIGGER fail_receipt ON request_receipts; DROP FUNCTION reject_receipt()"); err != nil {
			t.Fatal(err)
		}
	}
	c := request("resource-failure")
	if _, err := create(c, "base"); err == nil {
		t.Fatal("expected unique resource failure")
	}
	var count int
	_ = st.DB.QueryRow(ctx, "SELECT count(*) FROM request_receipts").Scan(&count)
	if count != 0 {
		t.Fatal("receipt survived resource rollback")
	}
	first, err := create(request("isolation"), "first")
	if err != nil {
		t.Fatal(err)
	}
	otherPrincipal := replayContext(t, uuid.New(), "create_project", uuid.Nil, "isolation", "body")
	if _, err := create(otherPrincipal, "second"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(ctx, "DELETE FROM projects WHERE id=$1", first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := create(request("isolation"), "first"); !errors.Is(err, requestreplay.ErrMissing) {
		t.Fatalf("deleted replay: %v", err)
	}
	if _, err := st.DB.Exec(ctx, "UPDATE request_receipts SET accepted_at=clock_timestamp()-interval '25 hours'"); err != nil {
		t.Fatal(err)
	}
	if _, err := create(request("isolation"), "first"); !errors.Is(err, requestreplay.ErrExpired) {
		t.Fatalf("expiry: %v", err)
	}
	if n, err := st.CleanupRequestReceipts(ctx); err != nil || n != 0 {
		t.Fatalf("premature cleanup %d %v", n, err)
	}
	if _, err := st.DB.Exec(ctx, "UPDATE request_receipts SET accepted_at=clock_timestamp()-interval '8 days'"); err != nil {
		t.Fatal(err)
	}
	if n, err := st.CleanupRequestReceipts(ctx); err != nil || n != 2 {
		t.Fatalf("cleanup %d %v", n, err)
	}
	if _, err := create(request("isolation"), "after-cleanup"); err != nil {
		t.Fatalf("unknown after cleanup: %v", err)
	}
}

func TestRequestReplayBoundedPending(t *testing.T) {
	st, _ := replayTestStore(t)
	user, _, _, _ := seedReplayResources(t, st)
	c := replayContext(t, user, "create_project", uuid.Nil, "locked", "body")
	r := requestreplay.From(c)
	tx, err := st.DB.Begin(c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	identity, _ := json.Marshal(receiptArgs(r))
	if _, err := tx.Exec(c, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", string(identity)); err != nil {
		t.Fatal(err)
	}
	_, _, err = st.CreateProjectWithEnvironment(c, sqlc.CreateProjectParams{ID: replayUUID(uuid.New()), Name: "pending"}, pgtype.UUID{Bytes: uuid.New(), Valid: true}, "production")
	if !errors.Is(err, requestreplay.ErrProgress) {
		t.Fatalf("pending: %v", err)
	}
	var count int
	_ = st.DB.QueryRow(c, "SELECT count(*) FROM request_receipts").Scan(&count)
	if count != 0 {
		t.Fatal("permanent pending receipt")
	}
}

func TestRequestReplayCleanupBound(t *testing.T) {
	st, _ := replayTestStore(t)
	ctx := context.Background()
	principal := uuid.New()
	_, err := st.DB.Exec(ctx, `INSERT INTO request_receipts(principal_id,operation,target_id,request_key,fingerprint,resource_id,related_id,accepted_at)
 SELECT $1,'create_project',$1,'old-'||i,'safe',$1,$1,clock_timestamp()-interval '8 days' FROM generate_series(1,205) i`, principal)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []int64{100, 100, 5, 0} {
		n, err := st.CleanupRequestReceipts(ctx)
		if err != nil || n != want {
			t.Fatalf("bounded cleanup=%d want=%d err=%v", n, want, err)
		}
	}
}

func TestRequestReplayResourceLockTimeout(t *testing.T) {
	st, _ := replayTestStore(t)
	user, project, _, _ := seedReplayResources(t, st)
	ctx := context.Background()
	holder, err := st.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, "SELECT id FROM projects WHERE id=$1 FOR UPDATE", project.ID); err != nil {
		t.Fatal(err)
	}
	request := func() context.Context {
		return replayContext(t, user, "create_environment", uuid.UUID(project.ID.Bytes), "resource-lock", "body")
	}
	params := sqlc.CreateEnvironmentParams{ID: replayUUID(uuid.New()), ProjectID: project.ID, Name: "blocked"}
	c := request()
	if _, err := st.CreateEnvironment(c, params); !errors.Is(err, requestreplay.ErrProgress) {
		t.Fatalf("resource lock timeout=%v, want request_in_progress", err)
	}
	if requestreplay.ResultFrom(c) != nil {
		t.Fatal("timed-out resource was accepted")
	}
	var receipts, resources int
	if err := st.DB.QueryRow(ctx, "SELECT count(*) FROM request_receipts WHERE request_key='resource-lock'").Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRow(ctx, "SELECT count(*) FROM environments WHERE name='blocked'").Scan(&resources); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 || resources != 0 {
		t.Fatalf("timeout left receipt/resource rows: %d/%d", receipts, resources)
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEnvironment(request(), params); err != nil {
		t.Fatalf("same-key retry after unlock: %v", err)
	}
}
