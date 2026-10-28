package store

import (
	"context"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) ListBackupProtectionSchedules(ctx context.Context) ([]sqlc.ListBackupProtectionSchedulesRow, error) {
	return s.queries.ListBackupProtectionSchedules(ctx)
}

func (s *Store) ListActiveBackupProtectionIncidents(ctx context.Context) ([]sqlc.ListActiveBackupProtectionIncidentsRow, error) {
	return s.queries.ListActiveBackupProtectionIncidents(ctx)
}
