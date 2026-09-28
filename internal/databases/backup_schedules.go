package databases

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Schedule surfaces: the cron CRUD of BackupService.

// ListSchedules implements BackupService.
func (m *BackupManager) ListSchedules(ctx context.Context, userID, databaseID uuid.UUID) ([]BackupSchedule, error) {
	if err := m.backupsReady(); err != nil {
		return nil, err
	}
	if _, err := m.database(ctx, userID, databaseID); err != nil {
		return nil, err
	}
	schedules, err := m.backups.ListBackupSchedulesByDatabase(ctx, databaseID)
	if err != nil {
		return nil, err
	}
	if schedules == nil {
		return []BackupSchedule{}, nil
	}
	return schedules, nil
}

// CreateSchedule implements BackupService. The expression is validated and
// the first run computed before anything is written, so an invalid cron
// never reaches the table.
func (m *BackupManager) CreateSchedule(ctx context.Context, userID, databaseID uuid.UUID, req ScheduleRequest) (BackupSchedule, error) {
	if _, err := m.database(ctx, userID, databaseID); err != nil {
		return BackupSchedule{}, err
	}
	cron := strings.TrimSpace(req.Cron)
	next, err := nextCronTime(cron, m.now(), time.Local)
	if err != nil {
		return BackupSchedule{}, err
	}
	targetID := uuid.Nil
	if req.TargetID != nil {
		if targetID, err = m.ownedTargetID(ctx, userID, *req.TargetID); err != nil {
			return BackupSchedule{}, err
		}
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	now := m.now()
	schedule := BackupSchedule{
		ID:         uuid.New(),
		DatabaseID: databaseID,
		Cron:       cron,
		TargetID:   targetID,
		Enabled:    enabled,
		NextRunAt:  next,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	created, err := m.backups.CreateBackupSchedule(ctx, schedule)
	if err != nil {
		return BackupSchedule{}, err
	}
	m.logger.Info("databases: backup schedule created",
		"schedule_id", created.ID.String(), "database_id", databaseID.String(),
		"cron", cron, "next_run_at", created.NextRunAt.Format(time.RFC3339))
	return created, nil
}

// UpdateSchedule implements BackupService. The cron expression and the
// computed next run are always recomputed, so a schedule cannot drift from
// its expression. TargetID is tri-state: nil keeps the stored target, an
// empty string clears it (runs go to the local backup directory) and an id
// replaces it with an owned target.
func (m *BackupManager) UpdateSchedule(ctx context.Context, userID, databaseID, scheduleID uuid.UUID, req ScheduleRequest) (BackupSchedule, error) {
	if _, err := m.database(ctx, userID, databaseID); err != nil {
		return BackupSchedule{}, err
	}
	schedule, err := m.backups.GetBackupSchedule(ctx, scheduleID)
	if err != nil {
		return BackupSchedule{}, err
	}
	if schedule.DatabaseID != databaseID {
		return BackupSchedule{}, ErrNotFound
	}
	cron := strings.TrimSpace(req.Cron)
	next, err := nextCronTime(cron, m.now(), time.Local)
	if err != nil {
		return BackupSchedule{}, err
	}
	targetID := schedule.TargetID
	switch {
	case req.TargetID == nil:
		// Field absent: keep the stored target. nil can never clear.
	case strings.TrimSpace(*req.TargetID) == "":
		// Explicit clear: scheduled runs return to the local directory.
		targetID = uuid.Nil
	default:
		if targetID, err = m.ownedTargetID(ctx, userID, *req.TargetID); err != nil {
			return BackupSchedule{}, err
		}
	}
	enabled := schedule.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	schedule.Cron = cron
	schedule.TargetID = targetID
	schedule.Enabled = enabled
	schedule.NextRunAt = next
	schedule.UpdatedAt = m.now()
	return m.backups.UpdateBackupSchedule(ctx, schedule)
}

// DeleteSchedule implements BackupService.
func (m *BackupManager) DeleteSchedule(ctx context.Context, userID, databaseID, scheduleID uuid.UUID) error {
	if _, err := m.database(ctx, userID, databaseID); err != nil {
		return err
	}
	schedule, err := m.backups.GetBackupSchedule(ctx, scheduleID)
	if err != nil {
		return err
	}
	if schedule.DatabaseID != databaseID {
		return ErrNotFound
	}
	if _, err := m.backups.DeleteBackupSchedule(ctx, scheduleID); err != nil {
		return err
	}
	return nil
}
