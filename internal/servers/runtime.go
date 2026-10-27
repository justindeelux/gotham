package servers

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const runtimeObserveTimeout = 30 * time.Second

// RuntimeObserver consumes accepted container inventory. It is separate from
// IncidentRecorder because it needs the full observation, and separate from
// StateNotifier so a slow detector can never delay the push acknowledgement.
type RuntimeObserver interface {
	Observe(context.Context, NodeObservation)
	NodeUnknown(context.Context, uuid.UUID, time.Time)
	NodeDeleted(context.Context, uuid.UUID)
}

func (s *ServerService) SetRuntimeObserver(observer RuntimeObserver) {
	s.runtimeMu.Lock()
	s.runtime = observer
	s.runtimeMu.Unlock()
}

// enqueueRuntime schedules detection after the observation is stored. One
// worker per node holds at most one accepted generation: a burst overwrites
// the pending generation so an older report is never evaluated after a newer
// one, and there is never one goroutine per observation or per container.
func (s *ServerService) enqueueRuntime(obs NodeObservation) {
	if s == nil || obs.ServerID == uuid.Nil {
		return
	}
	s.runtimeMu.Lock()
	if s.runtime == nil || s.runtimeClosed {
		s.runtimeMu.Unlock()
		return
	}
	if s.runtimeCtx == nil {
		s.runtimeCtx, s.runtimeCancel = context.WithCancel(context.Background())
	}
	if s.runtimeLatest == nil {
		s.runtimeLatest = map[uuid.UUID]NodeObservation{}
		s.runtimeRunning = map[uuid.UUID]bool{}
	}
	s.runtimeLatest[obs.ServerID] = obs
	if s.runtimeRunning[obs.ServerID] {
		s.runtimeMu.Unlock()
		return
	}
	s.runtimeRunning[obs.ServerID] = true
	ctx := s.runtimeCtx
	s.runtimeWG.Add(1)
	s.runtimeMu.Unlock()
	go s.runRuntimeQueue(ctx, obs.ServerID)
}

func (s *ServerService) runRuntimeQueue(ctx context.Context, serverID uuid.UUID) {
	defer s.runtimeWG.Done()
	for {
		s.runtimeMu.Lock()
		obs, ok := s.runtimeLatest[serverID]
		if ok {
			delete(s.runtimeLatest, serverID)
		} else {
			s.runtimeRunning[serverID] = false
			s.runtimeMu.Unlock()
			return
		}
		observer := s.runtime
		s.runtimeMu.Unlock()
		if observer == nil {
			continue
		}
		runCtx, cancel := context.WithTimeout(ctx, runtimeObserveTimeout)
		observer.Observe(runCtx, obs)
		cancel()
	}
}

// Close stops queued runtime evaluations and waits for the in-flight worker.
func (s *ServerService) Close() {
	if s == nil {
		return
	}
	s.runtimeMu.Lock()
	s.runtimeClosed = true
	cancel := s.runtimeCancel
	s.runtimeCancel = nil
	s.runtimeCtx = nil
	s.runtimeMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.runtimeWG.Wait()
}
