package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

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
	if role, ok := ws.ConnectionRole(ctx); ok {
		if role == auth.RoleOwner {
			return nil
		}
		return errAccessDenied
	}
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

	resource, err := s.realtimeChannelResource(ctx, channel)
	if err != nil {
		return errAccessDenied
	}
	return s.requireRealtimeGrant(ctx, userID, resource)
}

type realtimeResource struct {
	kind string
	id   uuid.UUID
}

func (s *Server) requireRealtimeGrant(ctx context.Context, userID uuid.UUID, resource realtimeResource) error {
	if s.persistence == nil || s.persistence.DB == nil {
		return errAccessDenied
	}
	allowed, err := s.persistence.GrantedResourceIDs(ctx, userID, resource.kind, []uuid.UUID{resource.id})
	if err != nil || !allowed[resource.id] {
		return errAccessDenied
	}
	return nil
}

// Resolve immutable container labels once per socket; join grants to the live
// workload relationship so moving a workload cannot preserve its old access.
func (s *Server) newRealtimeChannelAuthorizer() func(context.Context, string, uuid.UUID) error {
	resources := make(map[string]realtimeResource)
	ownerChannels := make(map[string]bool)
	type decision struct {
		version uint64
		until   time.Time
	}
	decisions := make(map[string]decision)
	return func(ctx context.Context, channel string, userID uuid.UUID) error {
		if strings.HasPrefix(channel, "notifications:") {
			id, err := uuid.Parse(strings.TrimPrefix(channel, "notifications:"))
			if err != nil || id != userID {
				return errAccessDenied
			}
			return nil
		}
		if s.requireOwnerContext(ctx, userID) == nil {
			if !ownerChannels[channel] {
				if err := s.authorizeRealtimeChannel(ctx, channel, userID); err != nil {
					return err
				}
				ownerChannels[channel] = true
			}
			return nil
		}
		version := ws.AuthorizationVersion(ctx)
		if cached := decisions[channel]; version != 0 && cached.version == version && time.Now().Before(cached.until) {
			return nil
		}
		resource, resolved := resources[channel]
		if !resolved {
			var err error
			resource, err = s.realtimeChannelResource(ctx, channel)
			if err != nil {
				return errAccessDenied
			}
		}
		if err := s.requireRealtimeGrant(ctx, userID, resource); err != nil {
			return err
		}
		resources[channel] = resource
		decisions[channel] = decision{version: version, until: time.Now().Add(ws.AuthorizationInterval)}
		return nil
	}
}

func (s *Server) realtimeChannelResource(ctx context.Context, channel string) (realtimeResource, error) {
	if s.persistence == nil || s.persistence.DB == nil {
		return realtimeResource{}, errAccessDenied
	}
	if strings.HasPrefix(channel, "tasks:") {
		projectID, err := uuid.Parse(strings.TrimPrefix(channel, "tasks:"))
		if err != nil {
			return realtimeResource{}, errAccessDenied
		}
		return realtimeResource{kind: "project", id: projectID}, nil
	}
	parts := strings.Split(channel, ":")
	if len(parts) != 3 || parts[0] != "logs" {
		return realtimeResource{}, errAccessDenied
	}
	serverID, err := uuid.Parse(parts[1])
	if err != nil {
		return realtimeResource{}, errAccessDenied
	}
	var resource realtimeResource
	err = s.persistence.DB.QueryRow(ctx, `SELECT 'application',a.id
 FROM deployments d JOIN applications a ON a.id=d.application_id
 WHERE a.server_id=$1 AND (d.id::text=$2 OR (d.container_id=$2 AND d.container_id<>''))
 UNION SELECT 'database',d.id FROM databases d
 WHERE d.server_id=$1 AND d.container_id=$2 AND d.deleted_at IS NULL AND d.container_id<>''`, serverID, parts[2]).Scan(&resource.kind, &resource.id)
	if err != nil {
		return s.runtimeContainerResource(ctx, serverID, parts[2])
	}
	return resource, nil
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
	resource, err := s.runtimeContainerResource(ctx, serverID, containerID)
	if err != nil {
		return errAccessDenied
	}
	return s.requireRealtimeGrant(ctx, userID, resource)
}

func (s *Server) runtimeContainerResource(ctx context.Context, serverID uuid.UUID, containerID string) (realtimeResource, error) {
	dialer, ok := s.servers.(containerDialer)
	if !ok {
		return realtimeResource{}, errAccessDenied
	}
	client, err := dialer.DialDockerClient(ctx, serverID)
	if err != nil {
		return realtimeResource{}, errAccessDenied
	}
	defer func() { _ = client.Close() }()
	inventory, err := client.ListContainers(ctx, &agentv1.ListContainersRequest{All: true})
	if err != nil {
		return realtimeResource{}, errAccessDenied
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
				return realtimeResource{}, errAccessDenied
			}
			id, err = uuid.Parse(strings.TrimPrefix(project, services.ProjectPrefix))
			if err != nil {
				return realtimeResource{}, errAccessDenied
			}
		}
		var resource realtimeResource
		err = s.persistence.DB.QueryRow(ctx, `SELECT 'application',a.id
 FROM applications a
 WHERE a.id=$1 AND a.server_id=$2
 UNION SELECT 'service',s.id
 FROM services s
 WHERE s.id=$1 AND s.server_id=$2 AND s.deleted_at IS NULL`, id, serverID).Scan(&resource.kind, &resource.id)
		if err == nil {
			return resource, nil
		}
		return realtimeResource{}, errAccessDenied
	}
	return realtimeResource{}, errAccessDenied
}
