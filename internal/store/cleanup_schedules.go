package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CleanupSchedule is an unattended cleanup plan over a set of nodes.
type CleanupSchedule struct {
	ID          uuid.UUID
	Name        string
	ServerIDs   []uuid.UUID
	Categories  []string
	Cron        string
	MinAgeHours int
	Enabled     bool
	NextRunAt   time.Time
	LastRunAt   *time.Time
	CreatedBy   *uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CleanupExclusion protects one item from every cleanup trigger. A nil ServerID
// protects it on every node.
type CleanupExclusion struct {
	ID        uuid.UUID
	ServerID  *uuid.UUID
	Category  string
	Match     string
	Note      string
	CreatedBy *uuid.UUID
	CreatedAt time.Time
}

func cleanupScheduleFromRow(row sqlc.CleanupSchedule) CleanupSchedule {
	schedule := CleanupSchedule{
		ID:          uuid.UUID(row.ID.Bytes),
		Name:        row.Name,
		ServerIDs:   make([]uuid.UUID, 0, len(row.ServerIds)),
		Categories:  append([]string(nil), row.Categories...),
		Cron:        row.Cron,
		MinAgeHours: int(row.MinAgeHours),
		Enabled:     row.Enabled,
		NextRunAt:   row.NextRunAt.Time,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
	for _, id := range row.ServerIds {
		schedule.ServerIDs = append(schedule.ServerIDs, uuid.UUID(id.Bytes))
	}
	if row.LastRunAt.Valid {
		last := row.LastRunAt.Time
		schedule.LastRunAt = &last
	}
	if row.CreatedBy.Valid {
		createdBy := uuid.UUID(row.CreatedBy.Bytes)
		schedule.CreatedBy = &createdBy
	}
	return schedule
}

func cleanupExclusionFromRow(row sqlc.CleanupExclusion) CleanupExclusion {
	exclusion := CleanupExclusion{
		ID:        uuid.UUID(row.ID.Bytes),
		Category:  row.Category,
		Match:     row.Match,
		Note:      row.Note,
		CreatedAt: row.CreatedAt.Time,
	}
	if row.ServerID.Valid {
		serverID := uuid.UUID(row.ServerID.Bytes)
		exclusion.ServerID = &serverID
	}
	if row.CreatedBy.Valid {
		createdBy := uuid.UUID(row.CreatedBy.Bytes)
		exclusion.CreatedBy = &createdBy
	}
	return exclusion
}

// ListCleanupSchedules lists every schedule by name.
func (s *Store) ListCleanupSchedules(ctx context.Context) ([]CleanupSchedule, error) {
	rows, err := s.queries.ListCleanupSchedules(ctx)
	if err != nil {
		return nil, err
	}
	schedules := make([]CleanupSchedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, cleanupScheduleFromRow(row))
	}
	return schedules, nil
}

// GetCleanupSchedule reads one schedule; sql.ErrNoRows means it is gone.
func (s *Store) GetCleanupSchedule(ctx context.Context, id uuid.UUID) (CleanupSchedule, error) {
	row, err := s.queries.GetCleanupSchedule(ctx, uuidToPG(id))
	if err != nil {
		return CleanupSchedule{}, err
	}
	return cleanupScheduleFromRow(row), nil
}

// CreateCleanupSchedule stores a new schedule.
func (s *Store) CreateCleanupSchedule(ctx context.Context, schedule CleanupSchedule) (CleanupSchedule, error) {
	serverIDs := make([]pgtype.UUID, 0, len(schedule.ServerIDs))
	for _, id := range schedule.ServerIDs {
		serverIDs = append(serverIDs, uuidToPG(id))
	}
	createdBy := pgtype.UUID{}
	if schedule.CreatedBy != nil {
		createdBy = uuidToPG(*schedule.CreatedBy)
	}
	row, err := s.queries.CreateCleanupSchedule(ctx, sqlc.CreateCleanupScheduleParams{
		ID:          uuidToPG(schedule.ID),
		Name:        schedule.Name,
		ServerIds:   serverIDs,
		Categories:  schedule.Categories,
		Cron:        schedule.Cron,
		MinAgeHours: int32(schedule.MinAgeHours),
		Enabled:     schedule.Enabled,
		NextRunAt:   pgtype.Timestamptz{Time: schedule.NextRunAt, Valid: true},
		CreatedBy:   createdBy,
	})
	if err != nil {
		return CleanupSchedule{}, err
	}
	return cleanupScheduleFromRow(row), nil
}

// UpdateCleanupSchedule replaces the editable fields of a schedule.
func (s *Store) UpdateCleanupSchedule(ctx context.Context, schedule CleanupSchedule) (CleanupSchedule, error) {
	serverIDs := make([]pgtype.UUID, 0, len(schedule.ServerIDs))
	for _, id := range schedule.ServerIDs {
		serverIDs = append(serverIDs, uuidToPG(id))
	}
	row, err := s.queries.UpdateCleanupSchedule(ctx, sqlc.UpdateCleanupScheduleParams{
		ID:          uuidToPG(schedule.ID),
		Name:        schedule.Name,
		ServerIds:   serverIDs,
		Categories:  schedule.Categories,
		Cron:        schedule.Cron,
		MinAgeHours: int32(schedule.MinAgeHours),
		Enabled:     schedule.Enabled,
		NextRunAt:   pgtype.Timestamptz{Time: schedule.NextRunAt, Valid: true},
	})
	if err != nil {
		return CleanupSchedule{}, err
	}
	return cleanupScheduleFromRow(row), nil
}

// DeleteCleanupSchedule removes a schedule; its runs keep history with a NULL
// schedule_id.
func (s *Store) DeleteCleanupSchedule(ctx context.Context, id uuid.UUID) error {
	return s.queries.DeleteCleanupSchedule(ctx, uuidToPG(id))
}

// ListDueCleanupSchedules lists enabled schedules whose slot has passed.
func (s *Store) ListDueCleanupSchedules(ctx context.Context, now time.Time) ([]CleanupSchedule, error) {
	rows, err := s.queries.ListDueCleanupSchedules(ctx, pgtype.Timestamptz{Time: now, Valid: true})
	if err != nil {
		return nil, err
	}
	schedules := make([]CleanupSchedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, cleanupScheduleFromRow(row))
	}
	return schedules, nil
}

// MarkCleanupScheduleRun advances the schedule before its work starts, so a
// crash cannot re-fire the same slot.
func (s *Store) MarkCleanupScheduleRun(
	ctx context.Context, id uuid.UUID, ranAt, next time.Time,
) (CleanupSchedule, error) {
	row, err := s.queries.MarkCleanupScheduleRun(ctx, sqlc.MarkCleanupScheduleRunParams{
		ID:        uuidToPG(id),
		LastRunAt: pgtype.Timestamptz{Time: ranAt, Valid: true},
		NextRunAt: pgtype.Timestamptz{Time: next, Valid: true},
	})
	if err != nil {
		return CleanupSchedule{}, err
	}
	return cleanupScheduleFromRow(row), nil
}

// CleanupRunFilter narrows the history list.
type CleanupRunFilter struct {
	ServerID *uuid.UUID
	Before   *time.Time
	Limit    int
}

// CleanupRunView is one history row with the schedule and actor names it needs.
type CleanupRunView struct {
	CleanupRun
	ScheduleName string
	ActorEmail   string
	Categories   map[string]int64
}

// ListCleanupRuns returns runs newest first, with per-category totals.
func (s *Store) ListCleanupRuns(ctx context.Context, filter CleanupRunFilter) ([]CleanupRunView, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	params := sqlc.ListCleanupRunsParams{RowLimit: int32(limit)}
	if filter.ServerID != nil {
		params.ServerID = uuidToPG(*filter.ServerID)
	}
	if filter.Before != nil {
		params.Before = pgtype.Timestamptz{Time: *filter.Before, Valid: true}
	}
	rows, err := s.queries.ListCleanupRuns(ctx, params)
	if err != nil {
		return nil, err
	}
	views := make([]CleanupRunView, 0, len(rows))
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		view := CleanupRunView{CleanupRun: cleanupRunFromRow(sqlc.CleanupRun{
			ID: row.ID, ServerID: row.ServerID, ActorUserID: row.ActorUserID,
			Trigger: row.Trigger, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
			ReclaimedBytes: row.ReclaimedBytes, ScheduleID: row.ScheduleID,
			Status: row.Status, Reason: row.Reason,
		})}
		if row.ScheduleName != nil {
			view.ScheduleName = *row.ScheduleName
		}
		if row.ActorEmail != nil {
			view.ActorEmail = *row.ActorEmail
		}
		views = append(views, view)
		ids = append(ids, row.ID)
	}
	if len(ids) == 0 {
		return views, nil
	}
	totals, err := s.queries.ListCleanupRunCategoryTotals(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]*CleanupRunView, len(views))
	for i := range views {
		views[i].Categories = map[string]int64{}
		byID[views[i].ID] = &views[i]
	}
	for _, total := range totals {
		if view, ok := byID[uuid.UUID(total.RunID.Bytes)]; ok {
			view.Categories[total.Category] += total.ReclaimedBytes
		}
	}
	return views, nil
}

// GetCleanupRun reads one run with its per-category totals.
func (s *Store) GetCleanupRun(ctx context.Context, id uuid.UUID) (CleanupRunView, error) {
	row, err := s.queries.GetCleanupRun(ctx, uuidToPG(id))
	if err != nil {
		return CleanupRunView{}, err
	}
	view := CleanupRunView{CleanupRun: cleanupRunFromRow(sqlc.CleanupRun{
		ID: row.ID, ServerID: row.ServerID, ActorUserID: row.ActorUserID,
		Trigger: row.Trigger, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		ReclaimedBytes: row.ReclaimedBytes, ScheduleID: row.ScheduleID,
		Status: row.Status, Reason: row.Reason,
	})}
	if row.ScheduleName != nil {
		view.ScheduleName = *row.ScheduleName
	}
	if row.ActorEmail != nil {
		view.ActorEmail = *row.ActorEmail
	}
	items, err := s.ListCleanupRunItems(ctx, id)
	if err != nil {
		return CleanupRunView{}, err
	}
	view.Categories = map[string]int64{}
	for _, item := range items {
		view.Categories[item.Category] += item.ReclaimedBytes
	}
	return view, nil
}

func cleanupRunFromRow(row sqlc.CleanupRun) CleanupRun {
	run := CleanupRun{
		ID:             uuid.UUID(row.ID.Bytes),
		ServerID:       uuid.UUID(row.ServerID.Bytes),
		Trigger:        row.Trigger,
		StartedAt:      row.StartedAt.Time,
		FinishedAt:     row.FinishedAt.Time,
		ReclaimedBytes: row.ReclaimedBytes,
		Status:         row.Status,
		Reason:         row.Reason,
	}
	if row.ActorUserID.Valid {
		actor := uuid.UUID(row.ActorUserID.Bytes)
		run.ActorUserID = &actor
	}
	if row.ScheduleID.Valid {
		scheduleID := uuid.UUID(row.ScheduleID.Bytes)
		run.ScheduleID = &scheduleID
	}
	return run
}

// ListCleanupRunItems lists the item outcomes of one run.
func (s *Store) ListCleanupRunItems(ctx context.Context, runID uuid.UUID) ([]CleanupRunItem, error) {
	rows, err := s.queries.ListCleanupRunItems(ctx, uuidToPG(runID))
	if err != nil {
		return nil, err
	}
	items := make([]CleanupRunItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, CleanupRunItem{
			ItemID:         row.ItemID,
			Category:       row.Category,
			Name:           row.Name,
			Status:         row.Status,
			Message:        row.Message,
			ReclaimedBytes: row.ReclaimedBytes,
		})
	}
	return items, nil
}

// ListCleanupExclusions lists every exclusion (global and per-node).
func (s *Store) ListCleanupExclusions(ctx context.Context) ([]CleanupExclusion, error) {
	rows, err := s.queries.ListCleanupExclusions(ctx)
	if err != nil {
		return nil, err
	}
	exclusions := make([]CleanupExclusion, 0, len(rows))
	for _, row := range rows {
		exclusions = append(exclusions, cleanupExclusionFromRow(row))
	}
	return exclusions, nil
}

// CreateCleanupExclusion inserts an exclusion, returning the existing row when
// the same server/category/match is already protected.
func (s *Store) CreateCleanupExclusion(ctx context.Context, exclusion CleanupExclusion) (CleanupExclusion, error) {
	serverID := pgtype.UUID{}
	if exclusion.ServerID != nil {
		serverID = uuidToPG(*exclusion.ServerID)
	}
	createdBy := pgtype.UUID{}
	if exclusion.CreatedBy != nil {
		createdBy = uuidToPG(*exclusion.CreatedBy)
	}
	row, err := s.queries.InsertCleanupExclusion(ctx, sqlc.InsertCleanupExclusionParams{
		ID:        uuidToPG(exclusion.ID),
		ServerID:  serverID,
		Category:  exclusion.Category,
		Match:     exclusion.Match,
		Note:      exclusion.Note,
		CreatedBy: createdBy,
	})
	if err == nil {
		return cleanupExclusionFromRow(row), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CleanupExclusion{}, err
	}
	existing, err := s.queries.GetCleanupExclusionByKey(ctx, sqlc.GetCleanupExclusionByKeyParams{
		Category: exclusion.Category, Match: exclusion.Match, ServerID: serverID,
	})
	if err != nil {
		return CleanupExclusion{}, err
	}
	return cleanupExclusionFromRow(existing), nil
}

// DeleteCleanupExclusion removes one exclusion.
func (s *Store) DeleteCleanupExclusion(ctx context.Context, id uuid.UUID) error {
	return s.queries.DeleteCleanupExclusion(ctx, uuidToPG(id))
}
