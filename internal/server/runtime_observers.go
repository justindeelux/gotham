package server

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
)

// runtimeFanout forwards every accepted observation to all runtime consumers.
// The cleanup threshold needs observations without taking the single runtime
// observer slot away from runtime monitoring.
type runtimeFanout []servers.RuntimeObserver

func (f runtimeFanout) Observe(ctx context.Context, obs servers.NodeObservation) {
	for _, observer := range f {
		observer.Observe(ctx, obs)
	}
}

func (f runtimeFanout) NodeUnknown(ctx context.Context, serverID uuid.UUID, at time.Time) {
	for _, observer := range f {
		observer.NodeUnknown(ctx, serverID, at)
	}
}

func (f runtimeFanout) NodeDeleted(ctx context.Context, serverID uuid.UUID) {
	for _, observer := range f {
		observer.NodeDeleted(ctx, serverID)
	}
}
