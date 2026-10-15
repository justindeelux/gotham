package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext("00049_localhost_node.go", migrateLocalhostNode, nil)
}

// The installer supplies the saved agent identity; the migration process may
// run on a different host. Unknown or conflicting identities must not strand workloads.
func migrateLocalhostNode(ctx context.Context, tx *sql.Tx) error {
	legacyID := strings.TrimSpace(os.Getenv("GOTHAM_LEGACY_LOCAL_NODE_ID"))
	if legacyID == "localhost" {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT node_id, ip FROM servers
 WHERE ($1 <> '' AND node_id = $1)
 OR ($1 = '' AND node_id LIKE '%-agent' AND ip IN ('', '127.0.0.1', '::1', 'localhost'))
 FOR UPDATE`, legacyID)
	if err != nil {
		return err
	}
	var candidates []string
	var unknownAddress bool
	for rows.Next() {
		var id, ip string
		if err := rows.Scan(&id, &ip); err != nil {
			_ = rows.Close()
			return err
		}
		candidates = append(candidates, id)
		unknownAddress = unknownAddress || ip == ""
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		if legacyID != "" {
			return fmt.Errorf("saved legacy node %q not found; verify GOTHAM_LEGACY_LOCAL_NODE_ID before retrying migration 49", legacyID)
		}
		return nil
	}
	if len(candidates) > 1 || (legacyID == "" && unknownAddress) {
		return fmt.Errorf("cannot identify legacy localhost node safely; set GOTHAM_LEGACY_LOCAL_NODE_ID to the node ID in the control plane's saved agent.env")
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM servers WHERE node_id='localhost')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("both legacy node %q and localhost exist; reconcile their workloads before retrying migration 49", candidates[0])
	}
	_, err = tx.ExecContext(ctx, `UPDATE servers SET node_id='localhost', name='localhost',
 ip='127.0.0.1', team_id=NULL, updated_at=now() WHERE node_id=$1`, candidates[0])
	return err
}
