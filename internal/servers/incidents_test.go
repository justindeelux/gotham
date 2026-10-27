package servers

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type incidentRecorderStub struct {
	svc   *incidents.Service
	err   error
	calls int
}

func (r *incidentRecorderStub) NodeConnectivity(context.Context, StateChange) {}

func (r *incidentRecorderStub) NodeDeleted(ctx context.Context, serverID uuid.UUID) {
	r.calls++
	r.err = r.svc.Close(ctx, incidents.Identity("connectivity", "node", serverID), "node_deleted")
}

type silentNotifier struct {
	mu    sync.Mutex
	count int
}

func (n *silentNotifier) NotifyIncident(context.Context, incidents.Delivery) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.count++
	return nil
}

func TestDeleteClosesNodeConnectivityIncident(t *testing.T) {
	service, st := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nodeID := addObservationNode(t, service)
	row, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	serverID := uuid.UUID(row.ID.Bytes)

	notifier := &silentNotifier{}
	incidentSvc := incidents.NewService(incidents.Config{
		Store: st, Notifier: notifier, Logger: discardLogger(),
	})
	recorder := &incidentRecorderStub{svc: incidentSvc}
	service.SetIncidentRecorder(recorder)

	incident, transition, err := incidentSvc.Report(ctx, incidents.Observation{
		Identity:         incidents.Identity("connectivity", "node", serverID),
		Detector:         "connectivity",
		ScopeKind:        incidents.ScopeNode,
		ScopeID:          serverID,
		NodeID:           serverID,
		ResourceType:     "node",
		ResourceName:     row.Name,
		At:               time.Now().UTC(),
		Valid:            true,
		Unhealthy:        true,
		Severity:         incidents.SeverityCritical,
		SuppressDelivery: true,
		SuppressReason:   "connectivity uses the existing node notification",
	})
	if err != nil || transition.Event != incidents.TransitionOpened {
		t.Fatalf("Report = %q %v", transition.Event, err)
	}
	if notifier.count != 0 {
		t.Fatalf("suppressed connectivity delivered %d notifications", notifier.count)
	}

	if err := service.Delete(ctx, serverID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if recorder.calls != 1 {
		t.Fatalf("NodeDeleted calls = %d; want 1", recorder.calls)
	}
	if recorder.err != nil {
		t.Fatalf("NodeDeleted: %v", recorder.err)
	}

	stored, err := st.GetIncident(ctx, pgtype.UUID{Bytes: incident.ID, Valid: true})
	if err != nil {
		t.Fatalf("GetIncident: %v", err)
	}
	if stored.Lifecycle != string(incidents.LifecycleResolved) {
		t.Fatalf("lifecycle = %q; want resolved", stored.Lifecycle)
	}
	if stored.CloseReason != "node_deleted" || !stored.ResolvedAt.Valid {
		t.Fatalf("close = %q at %+v; want node_deleted", stored.CloseReason, stored.ResolvedAt)
	}
	if notifier.count != 0 {
		t.Fatalf("deleting a node emitted a recovery notification: %d", notifier.count)
	}

	events, err := st.ListIncidentEvents(ctx, sqlc.ListIncidentEventsParams{
		IncidentID: pgtype.UUID{Bytes: incident.ID, Valid: true},
		RowLimit:   10,
	})
	if err != nil {
		t.Fatalf("ListIncidentEvents: %v", err)
	}
	found := false
	for _, event := range events {
		if event.Kind == "closed" && event.Reason == "node_deleted" {
			found = true
		}
	}
	if !found {
		t.Fatalf("history does not show node_deleted: %+v", events)
	}
}
