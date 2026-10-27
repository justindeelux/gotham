package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/notifications"
	"github.com/justindeelux/gotham/internal/runtimewatch"
	"github.com/justindeelux/gotham/internal/servers"
)

// incidentAuth keeps the normal read/deploy token scope gate; the incidents
// service resolves the caller's role and current project grants on every read
// and mutation, so node/global incidents stay owner-only.
func (s *Server) incidentAuth() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.RequireAuth(requireResourceScopes(next))
	}
}

type incidentNotifier struct {
	notify notifications.NotificationService
}

func (n incidentNotifier) NotifyIncident(_ context.Context, delivery incidents.Delivery) error {
	enqueuer, ok := n.notify.(interface {
		Enqueue(notifications.Event) error
	})
	if !ok {
		return nil
	}
	key := notifications.EventKey("")
	if strings.HasPrefix(delivery.Detector, "runtime_") {
		switch delivery.Transition {
		case "recovered":
			key = notifications.EventRuntimeRecovery
		default:
			key = notifications.EventRuntimeFailure
		}
	}
	return enqueuer.Enqueue(notifications.Event{
		Kind:         notifications.EventIncident,
		Outcome:      notifications.OutcomeFailure,
		Action:       delivery.Transition,
		Severity:     string(delivery.Severity),
		IncidentID:   delivery.IncidentID,
		ResourceType: delivery.ResourceType,
		ResourceID:   delivery.ResourceID,
		Name:         delivery.Name,
		Detail:       delivery.Evidence,
		At:           delivery.At,
		KeyOverride:  key,
	})
}

// incidentConnectivity mirrors the authoritative 30-second offline signal into
// the lifecycle while the existing server_offline/server_online notification
// remains the single user-visible connectivity message.
type incidentConnectivity struct {
	svc     *incidents.Service
	runtime *runtimewatch.Service
	logger  *slog.Logger
}

func (r incidentConnectivity) NodeDeleted(ctx context.Context, serverID uuid.UUID) {
	if r.svc == nil || serverID == uuid.Nil {
		return
	}
	if err := r.svc.Close(ctx, incidents.Identity("connectivity", "node", serverID), "node_deleted"); err != nil && r.logger != nil {
		r.logger.Warn("incidents: close deleted node incident", "server_id", serverID.String(), "error", err)
	}
}

func (r incidentConnectivity) NodeConnectivity(ctx context.Context, change servers.StateChange) {
	if r.svc == nil || change.ServerID == uuid.Nil {
		return
	}
	_, _, err := r.svc.Report(ctx, incidents.Observation{
		Identity:         incidents.Identity("connectivity", "node", change.ServerID),
		Detector:         "connectivity",
		ScopeKind:        incidents.ScopeNode,
		ScopeID:          change.ServerID,
		NodeID:           change.ServerID,
		ResourceType:     "node",
		ResourceID:       change.ServerID,
		ResourceName:     change.Name,
		At:               change.At,
		Valid:            true,
		Unhealthy:        !change.Online,
		Severity:         incidents.SeverityCritical,
		Evidence:         change.Host,
		SuppressDelivery: true,
		SuppressReason:   "connectivity uses the existing node notification",
	})
	if err != nil && r.logger != nil {
		r.logger.Warn("incidents: record node connectivity", "server_id", change.ServerID.String(), "error", err)
	}
	if r.runtime != nil && !change.Online {
		r.runtime.NodeUnknown(ctx, change.ServerID, change.At)
	}
}
