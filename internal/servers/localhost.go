package servers

import (
	"context"
	"fmt"

	"github.com/justindeelux/gotham/internal/store"
)

const localhostAgentServerName = "localhost.agent.gotham.invalid"

func (s *ServerService) EnsureLocalhost(ctx context.Context) error {
	tx, err := s.store.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ensure localhost node: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM servers WHERE node_id = 'localhost')`).Scan(&exists); err != nil {
		return fmt.Errorf("find localhost node: %w", err)
	}
	if !exists {
		if _, err := tx.Exec(ctx, store.ReconcileLocalhostSQL); err != nil {
			return fmt.Errorf("reconcile localhost node: %w", err)
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO servers (name, ip, port, ssh_user, node_id)
		VALUES ('localhost', '127.0.0.1', 22, '', 'localhost')
		ON CONFLICT (node_id) DO UPDATE
		SET name = 'localhost', ip = '127.0.0.1', updated_at = now()`)
	if err != nil {
		return fmt.Errorf("ensure localhost node: %w", err)
	}
	return tx.Commit(ctx)
}
