package deploy

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/runtimewatch"
)

// RuntimeMonitor lets deploy actions tell the detector about an intentional
// stop/start without importing the monitoring implementation into the deploy
// package.
type RuntimeMonitor interface {
	SetDesiredState(ctx context.Context, resourceType string, resourceID uuid.UUID, desired string, grace time.Duration) error
	SetSettings(ctx context.Context, resourceType string, resourceID uuid.UUID, enabled *bool, graceSeconds *int) error
	CloseResource(ctx context.Context, resourceType string, resourceID uuid.UUID, reason string)
}

func (s *Service) setDesiredState(ctx context.Context, appID uuid.UUID, desired string) {
	if s == nil || s.runtimeMonitor == nil {
		return
	}
	if err := s.runtimeMonitor.SetDesiredState(ctx, runtimewatch.ResourceApplication, appID, desired, 0); err != nil && s.logger != nil {
		s.logger.Warn("deploy: record runtime desired state", "application_id", appID.String(), "error", err)
	}
}
