package server

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
)

type recordingObserver struct {
	observations int
	unknown      int
	deleted      int
}

func (r *recordingObserver) Observe(context.Context, servers.NodeObservation) { r.observations++ }

func (r *recordingObserver) NodeUnknown(context.Context, uuid.UUID, time.Time) { r.unknown++ }

func (r *recordingObserver) NodeDeleted(context.Context, uuid.UUID) { r.deleted++ }

func TestRuntimeFanoutReachesEveryObserver(t *testing.T) {
	monitoring := &recordingObserver{}
	cleanup := &recordingObserver{}
	fanout := runtimeFanout{monitoring, cleanup}

	fanout.Observe(context.Background(), servers.NodeObservation{ServerID: uuid.New()})
	fanout.NodeUnknown(context.Background(), uuid.New(), time.Now())
	fanout.NodeDeleted(context.Background(), uuid.New())

	for name, observer := range map[string]*recordingObserver{"monitoring": monitoring, "cleanup": cleanup} {
		if observer.observations != 1 || observer.unknown != 1 || observer.deleted != 1 {
			t.Fatalf("%s = %+v, want every call forwarded", name, observer)
		}
	}
}
