package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext("00049_localhost_node.go", migrateLocalhostNode, nil)
}

// A Go migration uses the control-plane hostname, never the database host's.
// Only the local installer's old default is renamed; remote nodes stay intact.
func migrateLocalhostNode(ctx context.Context, tx *sql.Tx) error {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return fmt.Errorf("cannot identify legacy localhost node: hostname %q: %v", host, err)
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE servers SET node_id = 'localhost', name = 'localhost',
		    ip = '127.0.0.1', team_id = NULL, updated_at = now()
		WHERE node_id = $1
		  AND NOT EXISTS (SELECT 1 FROM servers WHERE node_id = 'localhost')`, host+"-agent")
	return err
}
