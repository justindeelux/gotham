package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/server/ws"
	"github.com/justindeelux/gotham/internal/services"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// errAccessDenied is a refusal a caller may retry only after its role changes.
var errAccessDenied = errors.New("access denied")

// authorizeLogSubscription handles the owner-only node-level authorizer.
// Member subscriptions use authorizeRealtimeChannel to resolve the container.
func (s *Server) authorizeLogSubscription(ctx context.Context, serverID, userID uuid.UUID) error {
	if s.servers == nil {
		return errors.New("ws: server registry is not configured")
	}
	if _, err := s.servers.Get(ctx, serverID); err != nil {
		return err
	}
	return s.requireOwnerContext(ctx, userID)
}

// authorizeTaskSubscription uses the same persisted grants as HTTP requests.
func (s *Server) authorizeTaskSubscription(ctx context.Context, projectID string, userID uuid.UUID) error {
	if err := s.requireOwnerContext(ctx, userID); err == nil {
		return nil
	}
	if s.canAccessResource(ctx, userID, "project", projectID) {
		return nil
	}
	return errAccessDenied
}

func (s *Server) requireOwnerContext(ctx context.Context, userID uuid.UUID) error {
	if s.auth == nil {
		return errAccessDenied
	}
	user, err := s.auth.Me(ctx, userID)
	if err != nil {
		return err
	}
	if user.Role != auth.RoleOwner {
		return errAccessDenied
	}
	return nil
}

func (s *Server) readScopeAuth(next http.Handler) http.Handler {
	return s.RequireAuth(RequireScopes(auth.ScopeRead)(next))
}

func (s *Server) adminScopeAuth(next http.Handler) http.Handler {
	return s.RequireAuth(RequireScopes(auth.ScopeAdmin)(next))
}

func (s *Server) resourceScopeAuth(next http.Handler) http.Handler {
	return s.RequireAuth(requireResourceScopes(next))
}

// Unknown channels are operator-only; a guessed Redis channel is not a grant.
func (s *Server) authorizeRealtimeChannel(ctx context.Context, channel string, userID uuid.UUID) error {
	if err := s.requireOwnerContext(ctx, userID); err == nil {
		if parts := strings.Split(channel, ":"); len(parts) == 3 && parts[0] == "logs" {
			serverID, err := uuid.Parse(parts[1])
			if err != nil {
				return errAccessDenied
			}
			return s.authorizeLogSubscription(ctx, serverID, userID)
		}
		return nil
	}
	if strings.HasPrefix(channel, "tasks:") {
		return s.authorizeTaskSubscription(ctx, strings.TrimPrefix(channel, "tasks:"), userID)
	}
	parts := strings.Split(channel, ":")
	if len(parts) != 3 || parts[0] != "logs" || s.persistence == nil || s.persistence.DB == nil {
		return errAccessDenied
	}
	serverID, err := uuid.Parse(parts[1])
	if err != nil {
		return errAccessDenied
	}
	var projectID uuid.UUID
	err = s.persistence.DB.QueryRow(ctx, `SELECT e.project_id
 FROM deployments d
 JOIN applications a ON a.id=d.application_id
 JOIN environments e ON e.id=a.environment_id
 WHERE a.server_id=$1 AND (d.id::text=$2 OR (d.container_id=$2 AND d.container_id<>''))
 UNION SELECT e.project_id
 FROM databases d
 JOIN environments e ON e.id=d.environment_id
 WHERE d.server_id=$1 AND d.container_id=$2 AND d.deleted_at IS NULL AND d.container_id<>''`, serverID, parts[2]).Scan(&projectID)
	if err != nil {
		return s.authorizeRuntimeContainer(ctx, serverID, parts[2], userID)
	}
	if s.canAccessResource(ctx, userID, "project", projectID.String()) {
		return nil
	}
	return errAccessDenied
}

func (s *Server) filterProjectFrame(ctx context.Context, userID uuid.UUID, payload []byte) []byte {
	if s.requireOwnerContext(ctx, userID) == nil {
		return payload
	}
	var frame ws.Message
	if json.Unmarshal(payload, &frame) != nil {
		return nil
	}
	if strings.HasPrefix(frame.Channel, "tasks:") && frame.Data != "" {
		var data map[string]any
		if json.Unmarshal([]byte(frame.Data), &data) != nil {
			return nil
		}
		redactOperatorResources(data)
		encoded, err := json.Marshal(data)
		if err != nil {
			return nil
		}
		frame.Data = string(encoded)
		result, err := json.Marshal(frame)
		if err != nil {
			return nil
		}
		return result
	}
	return payload
}

// Compose container IDs live on the agent. Resolve their immutable labels and
// check the persisted workload and node before accepting a subscription.
func (s *Server) authorizeRuntimeContainer(ctx context.Context, serverID uuid.UUID, containerID string, userID uuid.UUID) error {
	dialer, ok := s.servers.(containerDialer)
	if !ok {
		return errAccessDenied
	}
	client, err := dialer.DialDockerClient(ctx, serverID)
	if err != nil {
		return errAccessDenied
	}
	defer func() { _ = client.Close() }()
	inventory, err := client.ListContainers(ctx, &agentv1.ListContainersRequest{All: true})
	if err != nil {
		return errAccessDenied
	}
	for _, container := range inventory.GetContainers() {
		if container.GetId() != containerID {
			continue
		}
		labels := container.GetLabels()
		id, err := uuid.Parse(labels["gotham.app_id"])
		if err != nil {
			project := labels["com.docker.compose.project"]
			if !strings.HasPrefix(project, services.ProjectPrefix) {
				return errAccessDenied
			}
			id, err = uuid.Parse(strings.TrimPrefix(project, services.ProjectPrefix))
			if err != nil {
				return errAccessDenied
			}
		}
		var projectID uuid.UUID
		err = s.persistence.DB.QueryRow(ctx, `SELECT e.project_id
 FROM applications a
 JOIN environments e ON e.id=a.environment_id
 WHERE a.id=$1 AND a.server_id=$2
 UNION SELECT e.project_id
 FROM services s
 JOIN environments e ON e.id=s.environment_id
 WHERE s.id=$1 AND s.server_id=$2 AND s.deleted_at IS NULL`, id, serverID).Scan(&projectID)
		if err == nil && s.canAccessResource(ctx, userID, "project", projectID.String()) {
			return nil
		}
		return errAccessDenied
	}
	return errAccessDenied
}
