package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestServerHostPendingMigrationMovesInstanceRecord proves an in-flight
// instance-wide network trial survives the upgrade: the old single record is
// carried to the localhost node with its revert deadline instead of being
// dropped, so the banner and the host's own timer stay in step.
func TestServerHostPendingMigrationMovesInstanceRecord(t *testing.T) {
	ctx, dsn, provider := backupCoreScratch(t)

	if _, err := provider.UpTo(ctx, 83); err != nil {
		t.Fatalf("migrate to 83: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	serverID := uuid.New()
	if _, err := conn.Exec(ctx,
		`INSERT INTO servers(id,name,ip,ssh_user,status,node_id) VALUES($1,'localhost','127.0.0.1','root','ready','localhost')`,
		serverID); err != nil {
		t.Fatalf("insert localhost server: %v", err)
	}
	deadline := time.Now().UTC().Add(90 * time.Second).Truncate(time.Microsecond)
	pending := `{"previous":{"dns_servers":[],"ipv4":{"mode":"dhcp"},"ipv6":{"enabled":false,"mode":"dhcp"}},` +
		`"proposed":{"dns_servers":["9.9.9.9"],"ipv4":{"mode":"dhcp"},"ipv6":{"enabled":false,"mode":"dhcp"}}}`
	if _, err := conn.Exec(ctx,
		`UPDATE instance_settings SET timezone = 'Asia/Ho_Chi_Minh', pending_network = $1::jsonb, pending_deadline = $2 WHERE id = 1`,
		pending, deadline); err != nil {
		t.Fatalf("seed pending network: %v", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	var stored []byte
	var gotDeadline time.Time
	if err := conn.QueryRow(ctx,
		`SELECT pending, deadline FROM server_host_pending WHERE server_id = $1`, serverID).
		Scan(&stored, &gotDeadline); err != nil {
		t.Fatalf("moved pending record: %v", err)
	}
	if gotDeadline.Sub(deadline).Abs() > time.Millisecond {
		t.Fatalf("deadline not preserved: got %s want %s", gotDeadline, deadline)
	}
	if !strings.Contains(string(stored), "9.9.9.9") {
		t.Fatalf("pending values not preserved: %s", stored)
	}

	// The control plane's own zone is what schedules run in; the move must not
	// touch it.
	var timezone string
	if err := conn.QueryRow(ctx, `SELECT timezone FROM instance_settings WHERE id = 1`).Scan(&timezone); err != nil {
		t.Fatalf("read timezone: %v", err)
	}
	if timezone != "Asia/Ho_Chi_Minh" {
		t.Fatalf("existing timezone lost: %q", timezone)
	}

	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM server_host_pending`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("want exactly the localhost record, got %d", count)
	}
}
