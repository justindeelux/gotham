package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateIncident(ctx context.Context, params sqlc.CreateIncidentParams) (sqlc.Incident, error) {
	return s.queries.CreateIncident(ctx, params)
}

func (s *Store) GetActiveIncidentByIdentity(ctx context.Context, identity string) (sqlc.Incident, error) {
	return s.queries.GetActiveIncidentByIdentity(ctx, identity)
}

func (s *Store) GetIncident(ctx context.Context, id pgtype.UUID) (sqlc.Incident, error) {
	return s.queries.GetIncident(ctx, id)
}

func (s *Store) GetIncidentForUpdate(ctx context.Context, id pgtype.UUID) (sqlc.Incident, error) {
	return s.queries.GetIncidentForUpdate(ctx, id)
}

func (s *Store) ListIncidents(ctx context.Context, params sqlc.ListIncidentsParams) ([]sqlc.Incident, error) {
	return s.queries.ListIncidents(ctx, params)
}

func (s *Store) ListResolvedIncidents(ctx context.Context, params sqlc.ListResolvedIncidentsParams) ([]sqlc.Incident, error) {
	return s.queries.ListResolvedIncidents(ctx, params)
}

func (s *Store) CountActiveIncidents(ctx context.Context, params sqlc.CountActiveIncidentsParams) (int64, error) {
	return s.queries.CountActiveIncidents(ctx, params)
}

func (s *Store) ListActiveNodeIncidents(ctx context.Context) ([]sqlc.Incident, error) {
	return s.queries.ListActiveNodeIncidents(ctx)
}

func (s *Store) UpdateIncident(ctx context.Context, params sqlc.UpdateIncidentParams) (sqlc.Incident, error) {
	return s.queries.UpdateIncident(ctx, params)
}

func (s *Store) AcknowledgeIncident(ctx context.Context, params sqlc.AcknowledgeIncidentParams) (sqlc.Incident, error) {
	return s.queries.AcknowledgeIncident(ctx, params)
}

func (s *Store) SnoozeIncident(ctx context.Context, params sqlc.SnoozeIncidentParams) (sqlc.Incident, error) {
	return s.queries.SnoozeIncident(ctx, params)
}

func (s *Store) ClearExpiredSnooze(ctx context.Context, params sqlc.ClearExpiredSnoozeParams) (sqlc.Incident, error) {
	return s.queries.ClearExpiredSnooze(ctx, params)
}

func (s *Store) ListExpiredSnoozes(ctx context.Context, params sqlc.ListExpiredSnoozesParams) ([]sqlc.Incident, error) {
	return s.queries.ListExpiredSnoozes(ctx, params)
}

func (s *Store) CreateIncidentEvent(ctx context.Context, params sqlc.CreateIncidentEventParams) error {
	return s.queries.CreateIncidentEvent(ctx, params)
}

func (s *Store) ListIncidentEvents(ctx context.Context, params sqlc.ListIncidentEventsParams) ([]sqlc.IncidentEvent, error) {
	return s.queries.ListIncidentEvents(ctx, params)
}

func (s *Store) PruneIncidentEvents(ctx context.Context, params sqlc.PruneIncidentEventsParams) (int64, error) {
	return s.queries.PruneIncidentEvents(ctx, params)
}

func (s *Store) CreateIncidentDeliveryIntent(ctx context.Context, params sqlc.CreateIncidentDeliveryIntentParams) error {
	return s.queries.CreateIncidentDeliveryIntent(ctx, params)
}

func (s *Store) ClaimPendingIncidentDeliveries(ctx context.Context, params sqlc.ClaimPendingIncidentDeliveriesParams) ([]sqlc.IncidentDeliveryIntent, error) {
	return s.queries.ClaimPendingIncidentDeliveries(ctx, params)
}

func (s *Store) ReclaimStaleIncidentDeliveries(ctx context.Context) (int64, error) {
	return s.queries.ReclaimStaleIncidentDeliveries(ctx)
}

func (s *Store) ListIncidentDeliveries(ctx context.Context, params sqlc.ListIncidentDeliveriesParams) ([]sqlc.IncidentDeliveryIntent, error) {
	return s.queries.ListIncidentDeliveries(ctx, params)
}

func (s *Store) MarkIncidentDelivery(ctx context.Context, params sqlc.MarkIncidentDeliveryParams) error {
	return s.queries.MarkIncidentDelivery(ctx, params)
}

func (s *Store) SetIncidentDelivery(ctx context.Context, params sqlc.SetIncidentDeliveryParams) error {
	return s.queries.SetIncidentDelivery(ctx, params)
}

func (s *Store) PruneResolvedIncidents(ctx context.Context, before pgtype.Timestamptz) (int64, error) {
	return s.queries.PruneResolvedIncidents(ctx, before)
}
