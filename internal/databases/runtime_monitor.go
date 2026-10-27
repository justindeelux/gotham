package databases

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/runtimewatch"
)

// RuntimeMonitor records intentional database lifecycle transitions for the
// runtime detector without coupling this package to its implementation.
type RuntimeMonitor interface {
	SetDesiredState(ctx context.Context, resourceType string, resourceID uuid.UUID, desired string, grace time.Duration) error
	SetSettings(ctx context.Context, resourceType string, resourceID uuid.UUID, enabled *bool, graceSeconds *int) error
	CloseResource(ctx context.Context, resourceType string, resourceID uuid.UUID, reason string)
}

func (s *Service) setDesiredState(ctx context.Context, databaseID uuid.UUID, desired string, grace time.Duration) {
	if s == nil || s.runtimeMonitor == nil {
		return
	}
	if err := s.runtimeMonitor.SetDesiredState(ctx, runtimewatch.ResourceDatabase, databaseID, desired, grace); err != nil {
		s.logger.Warn("databases: record runtime desired state", "database_id", databaseID.String(), "error", err)
	}
}
