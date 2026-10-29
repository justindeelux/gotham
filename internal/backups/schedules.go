package backups

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (m *BackupManager) ListSchedules(ctx context.Context, userID uuid.UUID, ref ResourceRef) ([]BackupSchedule, error) {
	if err := m.backupsReady(); err != nil {
		return nil, err
	}
	if !ref.Valid() {
		schedules, err := m.backups.ListBackupSchedules(ctx, userID)
		if err != nil {
			return nil, err
		}
		if schedules == nil {
			return []BackupSchedule{}, nil
		}
		return schedules, nil
	}
	resource, _, err := m.resolve(ctx, ref)
	if err != nil {
		return nil, err
	}
	schedules, err := m.backups.ListBackupSchedulesByResource(ctx, resource.Ref())
	if err != nil {
		return nil, err
	}
	if schedules == nil {
		return []BackupSchedule{}, nil
	}
	return schedules, nil
}

func (m *BackupManager) CreateSchedule(ctx context.Context, userID uuid.UUID, req ScheduleRequest) (BackupSchedule, error) {
	resource, _, err := m.resolve(ctx, req.Resource)
	if err != nil {
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
		if err := m.targetOwnedByResource(ctx, targetID, resource.OwnerID); err != nil {
			return BackupSchedule{}, err
		}
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	maxRunSeconds, err := normalizeMaxRunSeconds(req.MaxRunSeconds, DefaultMaxBackupRunSeconds)
	if err != nil {
		return BackupSchedule{}, err
	}
	keepLast, err := normalizeKeepLast(req.KeepLast, DefaultKeepLastScheduledBackups)
	if err != nil {
		return BackupSchedule{}, err
	}
	now := m.now()
	schedule := BackupSchedule{
		ID:            uuid.New(),
		ResourceRef:   resource.Ref(),
		Cron:          cron,
		TargetID:      targetID,
		Enabled:       enabled,
		NextRunAt:     next,
		MaxRunSeconds: maxRunSeconds,
		KeepLast:      keepLast,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	created, err := m.backups.CreateBackupSchedule(ctx, schedule)
	if err != nil {
		return BackupSchedule{}, err
	}
	m.logger.Info("backups: backup schedule created",
		"schedule_id", created.ID.String(), "resource", resource.Ref().String(),
		"cron", cron, "next_run_at", created.NextRunAt.Format(time.RFC3339))
	return created, nil
}

func (m *BackupManager) UpdateSchedule(ctx context.Context, userID, scheduleID uuid.UUID, req ScheduleRequest) (BackupSchedule, error) {
	schedule, err := m.backups.GetBackupSchedule(ctx, scheduleID)
	if err != nil {
		return BackupSchedule{}, err
	}
	if req.Resource.Valid() && req.Resource != schedule.ResourceRef {
		return BackupSchedule{}, ErrNotFound
	}
	resource, _, err := m.resolve(ctx, schedule.ResourceRef)
	if err != nil {
		return BackupSchedule{}, err
	}
	cron := strings.TrimSpace(req.Cron)
	next, err := nextCronTime(cron, m.now(), time.Local)
	if err != nil {
		return BackupSchedule{}, err
	}
	targetID := schedule.TargetID
	switch {
	case req.TargetID == nil:
	case strings.TrimSpace(*req.TargetID) == "":
		targetID = uuid.Nil
	default:
		if targetID, err = m.ownedTargetID(ctx, userID, *req.TargetID); err != nil {
			return BackupSchedule{}, err
		}
		if err := m.targetOwnedByResource(ctx, targetID, resource.OwnerID); err != nil {
			return BackupSchedule{}, err
		}
	}
	enabled := schedule.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	maxRunSeconds, err := normalizeMaxRunSeconds(req.MaxRunSeconds, schedule.MaxRunSeconds)
	if err != nil {
		return BackupSchedule{}, err
	}
	keepLast := schedule.KeepLast
	if req.KeepLast != nil {
		keepLast, err = normalizeKeepLast(req.KeepLast, 0)
		if err != nil {
			return BackupSchedule{}, err
		}
	}
	now := m.now()
	// An edit never erases a missed slot: keep the due time as observed
	// scheduling evidence so the protection incident is not resolved by
	// changing the cron or target.
	if !schedule.NextRunAt.IsZero() && !schedule.NextRunAt.After(now) &&
		(schedule.LastSlotAt.IsZero() || schedule.NextRunAt.After(schedule.LastSlotAt)) {
		schedule.LastSlotAt = schedule.NextRunAt
	}
	if targetID != schedule.TargetID {
		schedule.TargetChangedAt = now
	}
	schedule.Cron = cron
	schedule.TargetID = targetID
	schedule.Enabled = enabled
	schedule.NextRunAt = next
	schedule.MaxRunSeconds = maxRunSeconds
	schedule.KeepLast = keepLast
	schedule.UpdatedAt = now
	return m.backups.UpdateBackupSchedule(ctx, schedule)
}

func (m *BackupManager) DeleteSchedule(ctx context.Context, userID, scheduleID uuid.UUID) error {
	schedule, err := m.backups.GetBackupSchedule(ctx, scheduleID)
	if err != nil {
		return err
	}
	if _, _, err := m.resolve(ctx, schedule.ResourceRef); err != nil {
		return err
	}
	if _, err := m.backups.DeleteBackupSchedule(ctx, scheduleID); err != nil {
		return err
	}
	return nil
}
