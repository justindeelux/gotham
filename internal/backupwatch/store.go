package backupwatch

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Source is the persisted evidence read by the observer and the API.
type Source interface {
	ProtectionSchedules(context.Context) ([]ScheduleEvidence, error)
	ActiveProtectionIncidents(context.Context) ([]string, error)
	Resource(context.Context, string, uuid.UUID) (ResourceInfo, error)
}

type ResourceInfo struct {
	Type string
	ID   uuid.UUID
	Name string
}

type storeSource struct {
	st *store.Store
}

func newStoreSource(st *store.Store) *storeSource {
	return &storeSource{st: st}
}

// NewStoreSource adapts the SQL store to the evidence source.
func NewStoreSource(st *store.Store) Source {
	return newStoreSource(st)
}

func (s *storeSource) ProtectionSchedules(ctx context.Context) ([]ScheduleEvidence, error) {
	rows, err := s.st.ListBackupProtectionSchedules(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ScheduleEvidence, 0, len(rows))
	for _, row := range rows {
		evidence := ScheduleEvidence{
			ScheduleID:      uuidFromPG(row.ID),
			ResourceType:    row.ResourceType,
			ResourceID:      uuidFromPG(row.ResourceID),
			ResourceName:    row.ResourceName,
			ProjectID:       uuidFromPG(row.ProjectID),
			Cron:            row.Cron,
			TargetID:        uuidFromPG(row.TargetID),
			TargetName:      row.TargetName,
			Enabled:         row.Enabled,
			NextRunAt:       timeFromPG(row.NextRunAt),
			LastSlotAt:      timeFromPG(row.LastSlotAt),
			LastRunAt:       timeFromPG(row.LastRunAt),
			TargetChangedAt: timeFromPG(row.TargetChangedAt),
			MaxRunSeconds:   int(row.MaxRunSeconds),
		}
		if row.SuccessCreatedAt.Valid {
			success := timeFromPG(row.SuccessCreatedAt)
			evidence.LastSuccessAt = &success
		}
		if row.SuccessID.Valid {
			evidence.LastSuccessID = uuidFromPG(row.SuccessID)
		}
		if row.RunID.Valid {
			running := timeFromPG(row.RunCreatedAt)
			evidence.RunningID = uuidFromPG(row.RunID)
			evidence.RunningSince = &running
		}
		out = append(out, evidence)
	}
	return out, nil
}

func (s *storeSource) ActiveProtectionIncidents(ctx context.Context) ([]string, error) {
	rows, err := s.st.ListActiveBackupProtectionIncidents(ctx)
	if err != nil {
		return nil, err
	}
	identities := make([]string, 0, len(rows))
	for _, row := range rows {
		identities = append(identities, row.IdentityKey)
	}
	return identities, nil
}

// Resource resolves a live resource of any supported type. Deleted resources
// and unknown ids return ErrNotFound.
func (s *storeSource) Resource(ctx context.Context, resourceType string, id uuid.UUID) (ResourceInfo, error) {
	pgID := pgUUID(id)
	var name string
	var err error
	switch resourceType {
	case ResourceDatabase:
		var row sqlc.Database
		row, err = s.st.GetDatabase(ctx, pgID)
		name = row.Name
	case ResourceApplication:
		var row sqlc.Application
		row, err = s.st.GetApplication(ctx, pgID)
		name = row.Name
	case ResourceService:
		var row sqlc.Service
		row, err = s.st.GetService(ctx, pgID)
		name = row.Name
	default:
		return ResourceInfo{}, ErrNotFound
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResourceInfo{}, ErrNotFound
		}
		return ResourceInfo{}, err
	}
	return ResourceInfo{Type: resourceType, ID: id, Name: name}, nil
}

func uuidFromPG(value pgtype.UUID) uuid.UUID {
	if !value.Valid {
		return uuid.Nil
	}
	return uuid.UUID(value.Bytes)
}

func timeFromPG(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
