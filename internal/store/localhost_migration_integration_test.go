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
		name, ip, hint, wantError, nodeID string
		collisionState, collisionRef      string
		ssh                               string
		collision, ambiguous              bool
		ignore                            bool
		unregistered                      bool
		startVersion                      int64
	}{
		{name: "saved identity after hostname change", hint: "original-host-agent"},
		{name: "unambiguous loopback fallback", ip: "127.0.0.1"},
		{name: "unknown address requires explicit identity", wantError: "GOTHAM_LEGACY_LOCAL_NODE_ID"},
		{name: "multiple loopback candidates", ip: "127.0.0.1", ambiguous: true, wantError: "cannot identify"},
		{name: "localhost collision", hint: "original-host-agent", collision: true, wantError: "reconcile"},
		{name: "saved identity missing", hint: "missing-agent", wantError: "not found"},
		{name: "custom identity loopback", nodeID: "test-node-1", ip: "127.0.0.1"},
		{name: "custom identity after migration 49", nodeID: "test-node-1", ip: "127.0.0.1", startVersion: 50},
		{name: "custom identity local agent address", nodeID: "test-node-1", ip: "127.0.0.1:9443", startVersion: 50},
		{name: "custom identity IPv6", nodeID: "test-node-1", ip: "::1", startVersion: 50},
		{name: "custom identity ambiguity", nodeID: "test-node-1", ip: "127.0.0.1", ambiguous: true, startVersion: 50, wantError: "reconcile nodes"},
		{name: "ambiguity preserves placeholder", nodeID: "test-node-1", ip: "127.0.0.1", ambiguous: true, collision: true, startVersion: 50, wantError: "reconcile nodes"},
		{name: "test box empty pending localhost", nodeID: "test-node-1", ip: "127.0.0.1", collision: true, startVersion: 50},
		{name: "registered localhost", nodeID: "test-node-1", ip: "127.0.0.1", collision: true, collisionState: "registered", startVersion: 50, wantError: "reconcile their workloads"},
		{name: "heartbeated localhost", nodeID: "test-node-1", ip: "127.0.0.1", collision: true, collisionState: "heartbeated", startVersion: 50, wantError: "reconcile their workloads"},
		{name: "localhost app", nodeID: "test-node-1", ip: "127.0.0.1", collision: true, collisionRef: "application", startVersion: 50, wantError: "references in applications"},
		{name: "localhost database", nodeID: "test-node-1", ip: "127.0.0.1", collision: true, collisionRef: "database", startVersion: 50, wantError: "references in databases"},
		{name: "localhost service", nodeID: "test-node-1", ip: "127.0.0.1", collision: true, collisionRef: "service", startVersion: 50, wantError: "references in services"},
		{name: "localhost routers", nodeID: "test-node-1", ip: "127.0.0.1", collision: true, collisionRef: "proxy", startVersion: 50, wantError: "references in proxy_config_versions"},
		{name: "localhost metrics", nodeID: "test-node-1", ip: "127.0.0.1", collision: true, collisionRef: "metrics", startVersion: 50, wantError: "references in server_metrics"},
		{name: "uppercase localhost", nodeID: "test-node-1", ip: "LOCALHOST:9443", startVersion: 50},
		{name: "agent scheme", nodeID: "test-node-1", ip: "tcp://LOCALHOST:9443", startVersion: 50},
		{name: "loopback subnet", nodeID: "test-node-1", ip: "127.0.0.2", startVersion: 50},
		{name: "mapped loopback", nodeID: "test-node-1", ip: "::ffff:127.0.0.1", startVersion: 50},
		{name: "bracketed loopback", nodeID: "test-node-1", ip: "[::1]", startVersion: 50},
		{name: "agent chosen IP", nodeID: "127.0.0.1", ip: "127.0.0.1", startVersion: 50, ignore: true},
		{name: "agent chosen address without IP", nodeID: "localhost:9443", startVersion: 50, ignore: true},
		{name: "invalid loopback address", nodeID: "test-node-1", ip: "127.0.0.999", startVersion: 50, ignore: true},
		{name: "SSH user", nodeID: "tunnel-agent", ip: "127.0.0.1", startVersion: 50, ssh: "user", ignore: true},
		{name: "SSH key", nodeID: "tunnel-agent", ip: "127.0.0.1", startVersion: 50, ssh: "key", ignore: true},
		{name: "SSH password", nodeID: "tunnel-agent", ip: "127.0.0.1", startVersion: 50, ssh: "password", ignore: true},
		{name: "not agent registered", nodeID: "tunnel-agent", ip: "127.0.0.1", startVersion: 50, unregistered: true, ignore: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("GOTHAM_LEGACY_LOCAL_NODE_ID", scenario.hint)
			if scenario.nodeID == "" {
				scenario.nodeID = "original-host-agent"
			}

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
			if scenario.startVersion == 0 {
				scenario.startVersion = 48
			}
			if _, err := provider.UpTo(ctx, scenario.startVersion); err != nil {
				t.Fatalf("migrate to %d: %v", scenario.startVersion, err)
			}

			var serverID, userID, appID, deployID, projectID, environmentID string
			for _, seed := range []struct {
				query string
				args  []any
				dest  *string
			}{
				{`INSERT INTO servers (name, ip, ssh_user, node_id, status, last_seen, os) VALUES ('old', $2, '', $1, 'ready', now(), 'linux') RETURNING id`, []any{scenario.nodeID, scenario.ip}, &serverID},
				{`INSERT INTO users (email) VALUES ('localhost-migration@example.com') RETURNING id`, nil, &userID},
			} {
				if err := db.QueryRowContext(ctx, seed.query, seed.args...).Scan(seed.dest); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO teams (id,name,is_personal) VALUES ($1,'owner',true)`, userID); err != nil {
				t.Fatal(err)
			}
			var protectedTeam any
			if scenario.ssh != "" || scenario.unregistered {
				protectedTeam = userID
				if _, err := db.ExecContext(ctx, `UPDATE servers SET team_id=$1 WHERE id=$2`, userID, serverID); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario.ssh {
			case "user":
				_, err = db.ExecContext(ctx, `UPDATE servers SET ssh_user='root' WHERE id=$1`, serverID)
			case "password":
				_, err = db.ExecContext(ctx, `UPDATE servers SET encrypted_password='sealed' WHERE id=$1`, serverID)
			case "key":
				_, err = db.ExecContext(ctx, `WITH key AS (INSERT INTO private_keys (name,encrypted_key) VALUES ('key','sealed') RETURNING id) UPDATE servers SET ssh_key_id=key.id FROM key WHERE servers.id=$1`, serverID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario.unregistered {
				if _, err := db.ExecContext(ctx, `UPDATE servers SET os=NULL, last_seen=NULL, status='pending' WHERE id=$1`, serverID); err != nil {
					t.Fatal(err)
				}
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
			if _, err := db.ExecContext(ctx, `INSERT INTO databases (name,user_id,team_id,server_id,environment_id,engine) VALUES ('db',$1,$1,$2,$3,'postgres')`, userID, serverID, environmentID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO services (name,user_id,team_id,server_id,environment_id,compose_yaml) VALUES ('service',$1,$1,$2,$3,'services: {}')`, userID, serverID, environmentID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO servers (name,ip,ssh_user,node_id) VALUES ('remote','192.0.2.5','','unrelated-agent')`); err != nil {
				t.Fatal(err)
			}

			if scenario.collision {
				var placeholderID string
				if err := db.QueryRowContext(ctx, `INSERT INTO servers (name,ip,ssh_user,node_id) VALUES ('localhost','127.0.0.1','','localhost') RETURNING id`).Scan(&placeholderID); err != nil {
					t.Fatal(err)
				}
				switch scenario.collisionState {
				case "registered":
					_, err = db.ExecContext(ctx, `UPDATE servers SET os='linux' WHERE id=$1`, placeholderID)
				case "heartbeated":
					_, err = db.ExecContext(ctx, `UPDATE servers SET last_seen=now() WHERE id=$1`, placeholderID)
				}
				if err != nil {
					t.Fatal(err)
				}
				switch scenario.collisionRef {
				case "application":
					_, err = db.ExecContext(ctx, `INSERT INTO applications (name,user_id,team_id,server_id,environment_id) VALUES ('placeholder',$1,$1,$2,$3)`, userID, placeholderID, environmentID)
				case "database":
					_, err = db.ExecContext(ctx, `INSERT INTO databases (name,user_id,team_id,server_id,environment_id,engine) VALUES ('placeholder',$1,$1,$2,$3,'postgres')`, userID, placeholderID, environmentID)
				case "service":
					_, err = db.ExecContext(ctx, `INSERT INTO services (name,user_id,team_id,server_id,environment_id,compose_yaml) VALUES ('placeholder',$1,$1,$2,$3,'services: {}')`, userID, placeholderID, environmentID)
				case "proxy":
					_, err = db.ExecContext(ctx, `INSERT INTO proxy_config_versions (server_id,files,content_hash) VALUES ($1,'{"routers":"preserved"}','hash')`, placeholderID)
				case "metrics":
					_, err = db.ExecContext(ctx, `INSERT INTO server_metrics (server_id,recorded_at,cpu_usage,mem_usage,disk_usage,net_rx_bps,net_tx_bps,disk_read_bps,disk_write_bps,container_count) VALUES ($1,now(),0,0,0,0,0,0,0,0)`, placeholderID)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario.ambiguous {
				if _, err := db.ExecContext(ctx, `INSERT INTO servers (name,ip,ssh_user,node_id,os) VALUES ('other','127.0.0.1','','other-agent','linux')`); err != nil {
					t.Fatal(err)
				}
			}
			_, err = provider.Up(ctx)
			if scenario.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), scenario.wantError) {
					t.Fatalf("migration error=%v; want %s", err, scenario.wantError)
				}
				var unchanged bool
				if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM servers s JOIN applications a ON a.server_id=s.id JOIN deployments d ON d.application_id=a.id JOIN proxy_config_versions p ON p.server_id=s.id WHERE s.id=$1 AND s.node_id=$2)`, serverID, scenario.nodeID).Scan(&unchanged); err != nil || !unchanged {
					t.Fatalf("failed migration lost workloads: %v", err)
				}
				if scenario.collision {
					var exists bool
					if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM servers WHERE node_id='localhost')`).Scan(&exists); err != nil || !exists {
						t.Fatalf("failed migration removed localhost: exists=%v, err=%v", exists, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario.ignore {
				var unchanged bool
				if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM servers WHERE id=$1 AND node_id=$2 AND ip=$3 AND ($4::uuid IS NULL OR team_id=$4))`, serverID, scenario.nodeID, scenario.ip, protectedTeam).Scan(&unchanged); err != nil || !unchanged {
					t.Fatalf("agent-chosen identity changed: preserved=%v, err=%v", unchanged, err)
				}
				return
			}
			if _, err := db.ExecContext(ctx, store.ReconcileLocalhostSQL); err != nil {
				t.Fatalf("rerun reconciliation: %v", err)
			}

			var preserved bool
			if err := db.QueryRowContext(ctx, `SELECT EXISTS (
 SELECT 1 FROM servers s JOIN applications a ON a.server_id=s.id
 JOIN deployments d ON d.application_id=a.id
 JOIN proxy_config_versions p ON p.server_id=s.id
 JOIN databases db ON db.server_id=s.id
 JOIN services svc ON svc.server_id=s.id
 WHERE s.id=$1 AND s.node_id='localhost' AND s.ip='127.0.0.1'
 AND s.status='ready' AND s.last_seen IS NOT NULL
 AND a.id=$2 AND d.id=$3 AND p.files='{"routers":"preserved"}'::jsonb)`, serverID, appID, deployID).Scan(&preserved); err != nil {
				t.Fatal(err)
			}
			if !preserved {
				t.Fatal("migration lost node identity or workload references")
			}
			var count int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM servers WHERE node_id <> 'unrelated-agent'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("duplicate local node: count=%d, err=%v", count, err)
			}
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM servers WHERE node_id='unrelated-agent'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("remote node changed: count=%d, err=%v", count, err)
			}
		})
	}
}
