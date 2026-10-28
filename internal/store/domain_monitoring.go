package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) GetDomainMonitorSettings(ctx context.Context, domainID pgtype.UUID) (sqlc.DomainMonitorSetting, error) {
	return s.queries.GetDomainMonitorSettings(ctx, domainID)
}

func (s *Store) UpsertDomainMonitorSettings(ctx context.Context, params sqlc.UpsertDomainMonitorSettingsParams) (sqlc.DomainMonitorSetting, error) {
	return s.queries.UpsertDomainMonitorSettings(ctx, params)
}

func (s *Store) GetDomainMonitorState(ctx context.Context, domainID pgtype.UUID) (sqlc.DomainMonitorState, error) {
	return s.queries.GetDomainMonitorState(ctx, domainID)
}

func (s *Store) UpsertDomainMonitorState(ctx context.Context, params sqlc.UpsertDomainMonitorStateParams) (sqlc.DomainMonitorState, error) {
	return s.queries.UpsertDomainMonitorState(ctx, params)
}

func (s *Store) InsertDomainProbeResult(ctx context.Context, params sqlc.InsertDomainProbeResultParams) error {
	return s.queries.InsertDomainProbeResult(ctx, params)
}

func (s *Store) ListDomainProbeResults(ctx context.Context, params sqlc.ListDomainProbeResultsParams) ([]sqlc.DomainProbeResult, error) {
	return s.queries.ListDomainProbeResults(ctx, params)
}

func (s *Store) DeleteDomainProbeResultsBefore(ctx context.Context, observedAt pgtype.Timestamptz) (int64, error) {
	return s.queries.DeleteDomainProbeResultsBefore(ctx, observedAt)
}

func (s *Store) ListDomainProbeTargets(ctx context.Context) ([]sqlc.ListDomainProbeTargetsRow, error) {
	return s.queries.ListDomainProbeTargets(ctx)
}

func (s *Store) GetDomainProbeTarget(ctx context.Context, params sqlc.GetDomainProbeTargetParams) (sqlc.GetDomainProbeTargetRow, error) {
	return s.queries.GetDomainProbeTarget(ctx, params)
}

func (s *Store) ListActiveDomainIncidents(ctx context.Context) ([]sqlc.ListActiveDomainIncidentsRow, error) {
	return s.queries.ListActiveDomainIncidents(ctx)
}

func (s *Store) ResetDomainMonitorStateCompletion(ctx context.Context, params sqlc.ResetDomainMonitorStateCompletionParams) error {
	return s.queries.ResetDomainMonitorStateCompletion(ctx, params)
}
