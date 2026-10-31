package cleanup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/cronexpr"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
)

// CenterStore persists schedules and history and lists exclusions. The
// control-plane store satisfies it; tests can supply only this surface.
type CenterStore interface {
	ListCleanupSchedules(ctx context.Context) ([]store.CleanupSchedule, error)
	GetCleanupSchedule(ctx context.Context, id uuid.UUID) (store.CleanupSchedule, error)
	CreateCleanupSchedule(ctx context.Context, schedule store.CleanupSchedule) (store.CleanupSchedule, error)
	UpdateCleanupSchedule(ctx context.Context, schedule store.CleanupSchedule) (store.CleanupSchedule, error)
	DeleteCleanupSchedule(ctx context.Context, id uuid.UUID) error
	ListDueCleanupSchedules(ctx context.Context, now time.Time) ([]store.CleanupSchedule, error)
	MarkCleanupScheduleRun(
		ctx context.Context, id uuid.UUID, ranAt, next time.Time,
	) (store.CleanupSchedule, error)
	ListCleanupRuns(ctx context.Context, filter store.CleanupRunFilter) ([]store.CleanupRunView, error)
	GetCleanupRun(ctx context.Context, id uuid.UUID) (store.CleanupRunView, error)
	ListCleanupRunItems(ctx context.Context, runID uuid.UUID) ([]store.CleanupRunItem, error)
	ListCleanupExclusions(ctx context.Context) ([]store.CleanupExclusion, error)
	CreateCleanupExclusion(
		ctx context.Context, exclusion store.CleanupExclusion,
	) (store.CleanupExclusion, error)
	DeleteCleanupExclusion(ctx context.Context, id uuid.UUID) error
}

// ScheduleNodeResult is one node's outcome within a scheduled run.
type ScheduleNodeResult struct {
	ServerID       string
	ServerName     string
	Status         string
	Reason         string
	ReclaimedBytes int64
	Categories     map[string]int64
}

// ScheduleRunSummary is what a scheduled run accomplished; the notifier turns
// it into one success or failure event.
type ScheduleRunSummary struct {
	ScheduleID   uuid.UUID
	ScheduleName string
	Nodes        []ScheduleNodeResult
}

// Failed reports whether any node failed.
func (s ScheduleRunSummary) Failed() bool {
	for _, node := range s.Nodes {
		if node.Status == StatusFailed {
			return true
		}
	}
	return false
}

// TotalBytes is the sum of the bytes every node freed.
func (s ScheduleRunSummary) TotalBytes() int64 {
	total := int64(0)
	for _, node := range s.Nodes {
		total += node.ReclaimedBytes
	}
	return total
}

// ScheduleNotifier receives the outcome of a scheduled run.
type ScheduleNotifier interface {
	CleanupFinished(ctx context.Context, summary ScheduleRunSummary)
}

// ScheduleInput is the owner's create/edit request for a schedule.
type ScheduleInput struct {
	Name        string
	ServerIDs   []uuid.UUID
	Categories  []string
	Cron        string
	MinAgeHours int
	// Enabled defaults to true on create and stays unchanged when nil on edit.
	Enabled *bool
}

const (
	maxScheduleName    = 80
	maxScheduleServers = 50
	minScheduleAge     = 1
	maxScheduleAge     = 8760
)

// ListSchedules lists every schedule by name.
func (s *Service) ListSchedules(ctx context.Context) ([]store.CleanupSchedule, error) {
	if s.center == nil {
		return nil, ErrUnavailable
	}
	return s.center.ListCleanupSchedules(ctx)
}

// CreateSchedule validates and stores a schedule. Dangerous categories are
// refused here and filtered again at run time.
func (s *Service) CreateSchedule(
	ctx context.Context, actor uuid.UUID, input ScheduleInput,
) (store.CleanupSchedule, error) {
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	schedule, err := s.validatedSchedule(ctx, uuid.New(), input, enabled)
	if err != nil {
		return store.CleanupSchedule{}, err
	}
	schedule.CreatedAt = s.now()
	schedule.UpdatedAt = schedule.CreatedAt
	if actor != uuid.Nil {
		schedule.CreatedBy = &actor
	}
	if s.center == nil {
		return store.CleanupSchedule{}, ErrUnavailable
	}
	return s.center.CreateCleanupSchedule(ctx, schedule)
}

// UpdateSchedule replaces a schedule's editable fields.
func (s *Service) UpdateSchedule(
	ctx context.Context, id uuid.UUID, input ScheduleInput,
) (store.CleanupSchedule, error) {
	if s.center == nil {
		return store.CleanupSchedule{}, ErrUnavailable
	}
	existing, err := s.center.GetCleanupSchedule(ctx, id)
	if err != nil {
		return store.CleanupSchedule{}, notFound(err)
	}
	enabled := existing.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	schedule, err := s.validatedSchedule(ctx, id, input, enabled)
	if err != nil {
		return store.CleanupSchedule{}, err
	}
	return s.center.UpdateCleanupSchedule(ctx, schedule)
}

// DeleteSchedule removes a schedule; its history stays with a nil schedule.
func (s *Service) DeleteSchedule(ctx context.Context, id uuid.UUID) error {
	if s.center == nil {
		return ErrUnavailable
	}
	if _, err := s.center.GetCleanupSchedule(ctx, id); err != nil {
		return notFound(err)
	}
	if err := s.center.DeleteCleanupSchedule(ctx, id); err != nil {
		return notFound(err)
	}
	return nil
}

// NextCronRun resolves the next slot of a cron expression for the schedule
// form, so a custom expression shows its next run before saving.
func (s *Service) NextCronRun(expression string) (time.Time, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return time.Time{}, fmt.Errorf("%w: cron expression is required", ErrValidation)
	}
	next, err := cronexpr.Next(expression, s.now(), s.loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return next, nil
}

// ListRuns returns cleanup history newest first.
func (s *Service) ListRuns(ctx context.Context, filter store.CleanupRunFilter) ([]store.CleanupRunView, error) {
	if s.center == nil {
		return nil, ErrUnavailable
	}
	return s.center.ListCleanupRuns(ctx, filter)
}

// GetRun returns one run with its item outcomes.
func (s *Service) GetRun(
	ctx context.Context, id uuid.UUID,
) (store.CleanupRunView, []store.CleanupRunItem, error) {
	if s.center == nil {
		return store.CleanupRunView{}, nil, ErrUnavailable
	}
	run, err := s.center.GetCleanupRun(ctx, id)
	if err != nil {
		return store.CleanupRunView{}, nil, notFound(err)
	}
	items, err := s.center.ListCleanupRunItems(ctx, id)
	if err != nil {
		return store.CleanupRunView{}, nil, err
	}
	return run, items, nil
}

// RunScheduleNow starts a schedule immediately in the background and refuses a
// second concurrent run of the same schedule.
func (s *Service) RunScheduleNow(ctx context.Context, id uuid.UUID) error {
	if s.center == nil {
		return ErrUnavailable
	}
	schedule, err := s.center.GetCleanupSchedule(ctx, id)
	if err != nil {
		return notFound(err)
	}
	if !s.claimSchedule(id) {
		return ErrScheduleRunning
	}
	go func() {
		defer s.releaseSchedule(id)
		s.executeSchedule(context.WithoutCancel(ctx), schedule)
	}()
	return nil
}

func (s *Service) validatedSchedule(
	ctx context.Context, id uuid.UUID, input ScheduleInput, enabled bool,
) (store.CleanupSchedule, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > maxScheduleName {
		return store.CleanupSchedule{}, fmt.Errorf("%w: name must be 1 to 80 characters", ErrValidation)
	}
	serverIDs := dedupeUUIDs(input.ServerIDs)
	if len(serverIDs) < 1 || len(serverIDs) > maxScheduleServers {
		return store.CleanupSchedule{}, fmt.Errorf("%w: server_ids must contain 1 to 50 nodes", ErrValidation)
	}
	if s.registry == nil {
		return store.CleanupSchedule{}, fmt.Errorf("%w: node registry is unavailable", ErrValidation)
	}
	for _, serverID := range serverIDs {
		if _, err := s.registry.Get(ctx, serverID); err != nil {
			return store.CleanupSchedule{}, fmt.Errorf("%w: %s", ErrInvalidServerID, serverID)
		}
	}
	categories := dedupeStrings(input.Categories)
	if len(categories) < 1 {
		return store.CleanupSchedule{}, fmt.Errorf("%w: choose at least one category", ErrValidation)
	}
	for _, category := range categories {
		if !KnownCategory(category) {
			return store.CleanupSchedule{}, fmt.Errorf("%w: %q", ErrInvalidCategory, category)
		}
		if CategoryTier(category) == TierDangerous {
			return store.CleanupSchedule{}, fmt.Errorf(
				"%w: %s cannot be cleaned on a schedule; run it manually", ErrDangerousCategory, category)
		}
	}
	cron := strings.TrimSpace(input.Cron)
	if err := cronexpr.Validate(cron); err != nil {
		return store.CleanupSchedule{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	next, err := cronexpr.Next(cron, s.now(), s.loc)
	if err != nil {
		return store.CleanupSchedule{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	minAge := input.MinAgeHours
	if minAge == 0 {
		minAge = 24
	}
	if minAge < minScheduleAge || minAge > maxScheduleAge {
		return store.CleanupSchedule{}, fmt.Errorf("%w: min_age_hours must be between 1 and 8760", ErrValidation)
	}
	return store.CleanupSchedule{
		ID:          id,
		Name:        name,
		ServerIDs:   serverIDs,
		Categories:  categories,
		Cron:        cron,
		MinAgeHours: minAge,
		Enabled:     enabled,
		NextRunAt:   next,
	}, nil
}

// executeSchedule scans each node with the schedule's minimum age, keeps the
// scheduled categories and cleans them. It always records skipped nodes.
func (s *Service) executeSchedule(ctx context.Context, schedule store.CleanupSchedule) ScheduleRunSummary {
	summary := ScheduleRunSummary{ScheduleID: schedule.ID, ScheduleName: schedule.Name}
	exclusions, err := s.loadExclusions(ctx)
	if err != nil {
		s.logger.Error("cleanup: schedule could not read exclusions",
			"schedule_id", schedule.ID.String(), "error", err)
	}
	categories := make(map[string]bool, len(schedule.Categories))
	for _, category := range schedule.Categories {
		if CategoryTier(category) == TierDangerous {
			continue
		}
		categories[category] = true
	}
	minAge := time.Duration(schedule.MinAgeHours) * time.Hour
	for _, serverID := range schedule.ServerIDs {
		node := ScheduleNodeResult{ServerID: serverID.String(), ServerName: s.serverName(ctx, serverID)}
		raw := serverID.String()
		if err != nil {
			node.Status, node.Reason = StatusFailed, err.Error()
			summary.Nodes = append(summary.Nodes, node)
			continue
		}
		scan := s.scanNode(ctx, raw, minAge, exclusions)
		if scan.Status != StatusOK {
			node.Status, node.Reason = scan.Status, scan.Reason
			if scan.Reason == servers.ErrNotFound.Error() {
				// A deleted node is skipped, not failed: its history cascaded
				// away with it, and cleanup_runs.server_id would violate its FK.
				node.Status, node.Reason = StatusSkipped, ReasonNotFound
			} else {
				s.recordSkipped(ctx, schedule.ID, serverID, node.Status, node.Reason)
			}
			summary.Nodes = append(summary.Nodes, node)
			continue
		}
		ids := make([]string, 0, len(scan.Items))
		for _, item := range scan.Items {
			if categories[item.Category] {
				ids = append(ids, item.ID)
			}
		}
		if len(ids) == 0 {
			node.Status = StatusOK
			summary.Nodes = append(summary.Nodes, node)
			continue
		}
		result := s.clean(ctx, uuid.Nil, TriggerSchedule, &schedule.ID, raw, ids, minAge)
		node.Status, node.Reason = result.Status, result.Reason
		node.Categories = map[string]int64{}
		for _, item := range result.Items {
			node.ReclaimedBytes += item.ReclaimedBytes
			node.Categories[item.Category] += item.ReclaimedBytes
		}
		summary.Nodes = append(summary.Nodes, node)
	}
	s.notifySchedule(ctx, summary)
	return summary
}

func (s *Service) recordSkipped(
	ctx context.Context, scheduleID, serverID uuid.UUID, status, reason string,
) {
	startedAt := s.now()
	s.record(ctx, uuid.Nil, TriggerSchedule, &scheduleID, serverID, startedAt,
		CleanResult{ServerID: serverID.String(), Status: status, Reason: reason})
}

func (s *Service) notifySchedule(ctx context.Context, summary ScheduleRunSummary) {
	if s.notifier == nil {
		return
	}
	s.notifier.CleanupFinished(context.WithoutCancel(ctx), summary)
}

func (s *Service) serverName(ctx context.Context, serverID uuid.UUID) string {
	if s.registry == nil {
		return serverID.String()
	}
	server, err := s.registry.Get(ctx, serverID)
	if err != nil || server == nil || server.Name == "" {
		return serverID.String()
	}
	return server.Name
}

func (s *Service) claimSchedule(id uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight[id] {
		return false
	}
	s.inflight[id] = true
	return true
}

func (s *Service) releaseSchedule(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, id)
}

func dedupeUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	unique := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
