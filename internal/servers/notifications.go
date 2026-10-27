package servers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type StateChange struct {
	ServerID uuid.UUID
	Name     string
	Host     string
	Online   bool
	At       time.Time
}

// IncidentRecorder mirrors authoritative connectivity into the persistent
// incident lifecycle. It is separate from StateNotifier so the existing
// notification path (and its timing) stays the single user-visible
// connectivity message.
type IncidentRecorder interface {
	NodeConnectivity(context.Context, StateChange)
	NodeDeleted(context.Context, uuid.UUID)
}

func (s *ServerService) SetIncidentRecorder(recorder IncidentRecorder) {
	s.incidents = recorder
}

// PressureRecorder consumes accepted observations for host resource alerts. It
// is a separate handoff so recording an observation never fails because an
// alert could not be evaluated.
type PressureRecorder interface {
	RecordHostPressure(context.Context, uuid.UUID, string, NodeObservation)
}

func (s *ServerService) SetPressureRecorder(recorder PressureRecorder) {
	s.pressure = recorder
}

type StateNotifier interface {
	ServerStateChanged(context.Context, StateChange)
}

func (s *ServerService) SetStateNotifier(notifier StateNotifier) {
	s.notifier = notifier
}

func (s *ServerService) notifyState(ctx context.Context, row sqlc.Server, online bool) {
	if s.notifier == nil && s.incidents == nil {
		return
	}
	notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	change := StateChange{
		ServerID: uuid.UUID(row.ID.Bytes), Name: row.Name, Host: row.Ip, Online: online, At: s.now().UTC(),
	}
	if s.notifier != nil {
		s.notifier.ServerStateChanged(notifyCtx, change)
	}
	if s.incidents != nil {
		s.incidents.NodeConnectivity(notifyCtx, change)
	}
}

func (s *ServerService) RunOfflineSweep(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	s.runOfflineSweep(ctx, ticker.C)
}

func (s *ServerService) runOfflineSweep(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			s.sweepOffline(sweepCtx)
			cancel()
		}
	}
}
