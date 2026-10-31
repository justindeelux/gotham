package cleanup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
)

const (
	minThresholdPercent  = 50
	maxThresholdPercent  = 99
	minThresholdAge      = 1
	maxThresholdAge      = 8760
	minThresholdCooldown = 1
	maxThresholdCooldown = 168
	defaultThresholdAge  = 24
	defaultCooldownHours = 6
)

// ThresholdInput is the owner's threshold rule as the HTTP API receives it.
type ThresholdInput struct {
	Enabled       bool     `json:"enabled"`
	Percent       int      `json:"percent"`
	Categories    []string `json:"categories"`
	MinAgeHours   int      `json:"min_age_hours"`
	CooldownHours int      `json:"cooldown_hours"`
}

// Threshold returns the stored disk-pressure rule.
func (s *Service) Threshold(ctx context.Context) (store.CleanupThreshold, error) {
	if s.center == nil {
		return store.CleanupThreshold{}, ErrUnavailable
	}
	return s.center.GetCleanupThreshold(ctx)
}

// UpdateThreshold validates and stores the rule. Dangerous categories are
// refused with the same validator a schedule uses.
func (s *Service) UpdateThreshold(
	ctx context.Context, actor uuid.UUID, input ThresholdInput,
) (store.CleanupThreshold, error) {
	if s.center == nil {
		return store.CleanupThreshold{}, ErrUnavailable
	}
	next, err := validatedThreshold(input)
	if err != nil {
		return store.CleanupThreshold{}, err
	}
	current, err := s.center.GetCleanupThreshold(ctx)
	if err != nil {
		return store.CleanupThreshold{}, err
	}
	if thresholdsEqual(current, next) {
		return current, nil
	}
	changes := map[string]any{"from": current, "to": next}
	if err := s.center.SaveCleanupThreshold(ctx, actor, next, changes); err != nil {
		return store.CleanupThreshold{}, err
	}
	return next, nil
}

func validatedThreshold(input ThresholdInput) (store.CleanupThreshold, error) {
	categories, err := validatedCategories(input.Categories)
	if err != nil {
		return store.CleanupThreshold{}, err
	}
	if input.Percent < minThresholdPercent || input.Percent > maxThresholdPercent {
		return store.CleanupThreshold{}, fmt.Errorf(
			"%w: percent must be between %d and %d", ErrValidation, minThresholdPercent, maxThresholdPercent)
	}
	minAge := input.MinAgeHours
	if minAge == 0 {
		minAge = defaultThresholdAge
	}
	if minAge < minThresholdAge || minAge > maxThresholdAge {
		return store.CleanupThreshold{}, fmt.Errorf(
			"%w: min_age_hours must be between %d and %d", ErrValidation, minThresholdAge, maxThresholdAge)
	}
	cooldown := input.CooldownHours
	if cooldown == 0 {
		cooldown = defaultCooldownHours
	}
	if cooldown < minThresholdCooldown || cooldown > maxThresholdCooldown {
		return store.CleanupThreshold{}, fmt.Errorf(
			"%w: cooldown_hours must be between %d and %d", ErrValidation, minThresholdCooldown, maxThresholdCooldown)
	}
	return store.CleanupThreshold{
		Enabled:       input.Enabled,
		Percent:       input.Percent,
		Categories:    categories,
		MinAgeHours:   minAge,
		CooldownHours: cooldown,
	}, nil
}

func thresholdsEqual(a, b store.CleanupThreshold) bool {
	if a.Enabled != b.Enabled || a.Percent != b.Percent ||
		a.MinAgeHours != b.MinAgeHours || a.CooldownHours != b.CooldownHours ||
		len(a.Categories) != len(b.Categories) {
		return false
	}
	for i := range a.Categories {
		if a.Categories[i] != b.Categories[i] {
			return false
		}
	}
	return true
}

// Observe is the servers.RuntimeObserver half of the threshold: it reads the
// accepted disk usage and starts at most one background run per node.
func (s *Service) Observe(ctx context.Context, obs servers.NodeObservation) {
	if s == nil || s.center == nil || obs.ServerID == uuid.Nil {
		return
	}
	if obs.Host.DiskUsage.Status != servers.MetricStatusOK {
		return
	}
	settings, err := s.center.GetCleanupThreshold(ctx)
	if err != nil {
		s.logger.Warn("cleanup: read the threshold rule failed", "error", err)
		return
	}
	if !settings.Enabled || obs.Host.DiskUsage.Value*100 < float64(settings.Percent) {
		return
	}
	s.maybeStartThreshold(ctx, obs.ServerID, settings)
}

// NodeUnknown and NodeDeleted keep the RuntimeObserver contract; the threshold
// needs no per-node cleanup state across those events.
func (s *Service) NodeUnknown(context.Context, uuid.UUID, time.Time) {}

func (s *Service) NodeDeleted(context.Context, uuid.UUID) {}

// maybeStartThreshold honours the stored cool-down and the in-flight run, then
// starts the run off the observation path.
func (s *Service) maybeStartThreshold(
	ctx context.Context, serverID uuid.UUID, settings store.CleanupThreshold,
) {
	if s.thresholdRunning(serverID) {
		return
	}
	now := s.now()
	latest, err := s.center.LatestCleanupThresholdRun(ctx, serverID)
	switch {
	case err == nil:
		cooldownEnds := latest.StartedAt.Add(time.Duration(settings.CooldownHours) * time.Hour)
		if now.Before(cooldownEnds) {
			// The first observation after a finished run tells the owner that
			// cleaning did not get the node below the threshold. A skipped run
			// never cleaned, so it must not claim that it did.
			if !now.Before(latest.FinishedAt) && latest.Status != StatusSkipped {
				s.notifyStillAbove(ctx, serverID, latest, settings.Percent)
			}
			return
		}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		s.logger.Warn("cleanup: read the last threshold run failed",
			"server_id", serverID.String(), "error", err)
		return
	}
	if !s.claimThreshold(serverID) {
		return
	}
	go func() {
		defer s.releaseThreshold(serverID)
		s.executeThreshold(context.WithoutCancel(ctx), serverID, settings)
	}()
}

// executeThreshold runs the shared schedule path for one node and notifies a
// failed run once, like a schedule does.
func (s *Service) executeThreshold(ctx context.Context, serverID uuid.UUID, settings store.CleanupThreshold) {
	exclusions, err := s.loadExclusions(ctx)
	if err != nil {
		s.logger.Error("cleanup: threshold could not read exclusions",
			"server_id", serverID.String(), "error", err)
	}
	minAge := time.Duration(settings.MinAgeHours) * time.Hour
	node := s.runNode(ctx, TriggerThreshold, nil, serverID,
		categorySet(settings.Categories), minAge, exclusions, err)
	if node.Status != StatusFailed {
		return
	}
	s.notifySchedule(ctx, ScheduleRunSummary{
		ScheduleName: node.ServerName,
		Nodes:        []ScheduleNodeResult{node},
	})
	if runID, err := uuid.Parse(node.RunID); err == nil {
		s.markThresholdNotice(serverID, runID)
	}
}

// notifyStillAbove sends one failure-style cleanup notification per threshold
// run while the node is still above the threshold.
func (s *Service) notifyStillAbove(
	ctx context.Context, serverID uuid.UUID, run store.CleanupRun, percent int,
) {
	if run.ID == uuid.Nil || !s.claimThresholdNotice(serverID, run.ID) {
		return
	}
	name := s.serverName(ctx, serverID)
	reason := fmt.Sprintf("still above %d%% after cleanup, freed %s",
		percent, thresholdBytes(run.ReclaimedBytes))
	s.notifySchedule(ctx, ScheduleRunSummary{
		ScheduleName: name,
		Nodes: []ScheduleNodeResult{{
			ServerID:   serverID.String(),
			ServerName: name,
			Status:     StatusFailed,
			Reason:     reason,
		}},
	})
}

func (s *Service) thresholdRunning(serverID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thresholdInFlight[serverID]
}

func (s *Service) claimThreshold(serverID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.thresholdInFlight[serverID] {
		return false
	}
	s.thresholdInFlight[serverID] = true
	return true
}

func (s *Service) releaseThreshold(serverID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.thresholdInFlight, serverID)
}

// claimThresholdNotice reports whether this observation is the first to tell
// the owner about the run.
func (s *Service) claimThresholdNotice(serverID, runID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.thresholdNoticed[serverID] == runID {
		return false
	}
	s.thresholdNoticed[serverID] = runID
	return true
}

func (s *Service) markThresholdNotice(serverID, runID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.thresholdNoticed[serverID] = runID
}

func thresholdBytes(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(bytes)
	for _, unit := range units {
		value /= 1024
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%.1f PB", value/1024)
}
