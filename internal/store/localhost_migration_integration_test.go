package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/justindeelux/gotham/internal/store"
)

// TestLocalhostNodeMigration preserves workload references and leaves remote nodes alone.
func TestLocalhostNodeMigration(t *testing.T) {
	for _, scenario := range []struct {
		name, ip, hint, wantError string
		collision, ambiguous      bool
	}{
		{name: "saved identity after hostname change", hint: "original-host-agent"},
		{name: "unambiguous loopback fallback", ip: "127.0.0.1"},
		{name: "unknown address requires explicit identity", wantError: "GOTHAM_LEGACY_LOCAL_NODE_ID"},
		{name: "multiple loopback candidates", ip: "127.0.0.1", ambiguous: true, wantError: "cannot identify"},
		{name: "localhost collision", hint: "original-host-agent", collision: true, wantError: "reconcile"},
		{name: "saved identity missing", hint: "missing-agent", wantError: "not found"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("GOTHAM_LEGACY_LOCAL_NODE_ID", scenario.hint)

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			base := testDSN()
			admin, err := sql.Open("pgx", dsnForDatabase(base, "postgres"))
			if err != nil {
				t.Fatalf("open maintenance connection: %v", err)
			}
			t.Cleanup(func() { _ = admin.Close() })
			if err := admin.PingContext(ctx); err != nil {
				if testDSNExplicit() {
					t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
				}
				t.Skipf("Postgres not available: %v", err)
			}

			scratch := fmt.Sprintf("pf1_profile_%d", time.Now().UnixNano())
			if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+scratch); err != nil {
				if testDSNExplicit() {
					t.Fatalf("GOTHAM_TEST_DSN is set but a disposable database cannot be created: %v", err)
				}
				t.Skipf("cannot create a disposable database (needs CREATEDB): %v", err)
			}
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cleanupCancel()
				if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS "+scratch+" WITH (FORCE)"); err != nil {
					t.Logf("drop scratch database: %v", err)
				}
			})

			scratchDSN := dsnForDatabase(base, scratch)
			db, err := sql.Open("pgx", scratchDSN)
			if err != nil {
				t.Fatalf("open scratch database: %v", err)
			}
			defer func() { _ = db.Close() }()

			provider, err := goose.NewProvider(goose.DialectPostgres, db, store.Migrations())
			if err != nil {
				t.Fatalf("init migrations: %v", err)
			}
			// Stop at the last release before the localhost identity migration.
			if _, err := provider.UpTo(ctx, 48); err != nil {
				t.Fatalf("migrate to 00048: %v", err)
			}

			var serverID, userID, appID, deployID, projectID, environmentID string
			for _, seed := range []struct {
				query string
				args  []any
				dest  *string
			}{
				{`INSERT INTO servers (name, ip, ssh_user, node_id) VALUES ('old', $2, '', $1) RETURNING id`, []any{"original-host-agent", scenario.ip}, &serverID},
				{`INSERT INTO users (email) VALUES ('localhost-migration@example.com') RETURNING id`, nil, &userID},
			} {
				if err := db.QueryRowContext(ctx, seed.query, seed.args...).Scan(seed.dest); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO teams (id,name,is_personal) VALUES ($1,'owner',true)`, userID); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, `INSERT INTO projects (team_id,name) VALUES ($1,'project') RETURNING id`, userID).Scan(&projectID); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, `INSERT INTO environments (project_id,name) VALUES ($1,'production') RETURNING id`, projectID).Scan(&environmentID); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, `INSERT INTO applications (name,user_id,team_id,server_id,environment_id) VALUES ('app',$1,$1,$2,$3) RETURNING id`, userID, serverID, environmentID).Scan(&appID); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, `INSERT INTO deployments (application_id,state) VALUES ($1,'running') RETURNING id`, appID).Scan(&deployID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO proxy_config_versions (server_id,files,content_hash) VALUES ($1,'{"routers":"preserved"}','hash')`, serverID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO servers (name,ip,ssh_user,node_id) VALUES ('remote','192.0.2.5','','unrelated-agent')`); err != nil {
				t.Fatal(err)
			}

			if scenario.collision {
				if _, err := db.ExecContext(ctx, `INSERT INTO servers (name,ip,ssh_user,node_id) VALUES ('localhost','127.0.0.1','','localhost')`); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.ambiguous {
				if _, err := db.ExecContext(ctx, `INSERT INTO servers (name,ip,ssh_user,node_id) VALUES ('other','127.0.0.1','','other-agent')`); err != nil {
					t.Fatal(err)
				}
			}
			_, err = provider.Up(ctx)
			if scenario.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), scenario.wantError) {
					t.Fatalf("migration error=%v; want %s", err, scenario.wantError)
				}
				var unchanged bool
				if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM servers s JOIN applications a ON a.server_id=s.id JOIN deployments d ON d.application_id=a.id JOIN proxy_config_versions p ON p.server_id=s.id WHERE s.id=$1 AND s.node_id='original-host-agent')`, serverID).Scan(&unchanged); err != nil || !unchanged {
					t.Fatalf("failed migration lost workloads: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			var preserved bool
			if err := db.QueryRowContext(ctx, `SELECT EXISTS (
 SELECT 1 FROM servers s JOIN applications a ON a.server_id=s.id
 JOIN deployments d ON d.application_id=a.id
 JOIN proxy_config_versions p ON p.server_id=s.id
 WHERE s.id=$1 AND s.node_id='localhost' AND s.ip='127.0.0.1'
 AND a.id=$2 AND d.id=$3 AND p.files='{"routers":"preserved"}'::jsonb)`, serverID, appID, deployID).Scan(&preserved); err != nil {
				t.Fatal(err)
			}
			if !preserved {
				t.Fatal("migration lost node identity or workload references")
			}
			var count int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM servers WHERE node_id='unrelated-agent'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("remote node changed: count=%d, err=%v", count, err)
			}
		})
	}
}
