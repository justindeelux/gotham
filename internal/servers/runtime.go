package servers

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// RuntimeObserver consumes accepted container inventory. It is separate from
// IncidentRecorder because it needs the full observation, and separate from
// StateNotifier so a slow detector can never delay the push acknowledgement.
type RuntimeObserver interface {
	Observe(context.Context, NodeObservation)
	NodeUnknown(context.Context, uuid.UUID, time.Time)
	NodeDeleted(context.Context, uuid.UUID)
}

func (s *ServerService) SetRuntimeObserver(observer RuntimeObserver) {
	s.runtime = observer
}
