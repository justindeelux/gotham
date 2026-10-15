package servers

import (
	"context"
	"fmt"
)

// localhostAgentServerName separates the local agent certificate from CP listener SANs.
const localhostAgentServerName = "localhost.agent.gotham.invalid"

// EnsureLocalhost repairs the mandatory registry entry without replacing its
// UUID, inventory or heartbeat status. Readiness still requires a live agent.
func (s *ServerService) EnsureLocalhost(ctx context.Context) error {
	_, err := s.store.DB.Exec(ctx, `
		INSERT INTO servers (name, ip, port, ssh_user, node_id)
		VALUES ('localhost', '127.0.0.1', 22, '', 'localhost')
		ON CONFLICT (node_id) DO UPDATE
		SET name = 'localhost', ip = '127.0.0.1', team_id = NULL, updated_at = now()`)
	if err != nil {
		return fmt.Errorf("ensure localhost node: %w", err)
	}
	return nil
}
