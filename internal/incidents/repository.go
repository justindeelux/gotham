package incidents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type Cursor struct {
	At time.Time
	ID uuid.UUID
}

type ListFilter struct {
	UserID          uuid.UUID
	IsOwner         bool
	IncludeResolved bool
	Lifecycle       *Lifecycle
	Severity        *Severity
	Cursor          *Cursor
	Limit           int
}

type EventFilter struct {
	IncidentID uuid.UUID
	Cursor     *Cursor
	Limit      int
}

type Event struct {
	ID            uuid.UUID
	IncidentID    uuid.UUID
	Kind          string
	FromLifecycle Lifecycle
	ToLifecycle   Lifecycle
	FromSeverity  Severity
	ToSeverity    Severity
	Reason        string
	ActorID       uuid.UUID
	Detail        string
	CreatedAt     time.Time
}

type DeliveryIntent struct {
	ID         uuid.UUID
	IncidentID uuid.UUID
	Transition string
	Severity   Severity
	State      string
	Attempts   int
	LastError  string
	CreatedAt  time.Time
}

// Tx is the write surface of one transition transaction: select/create the
// incident, update it, append history and commit the delivery intent together.
type Tx interface {
	ActiveByIdentity(ctx context.Context, identity string) (Incident, error)
	GetForUpdate(ctx context.Context, id uuid.UUID) (Incident, error)
	Create(ctx context.Context, incident Incident) (Incident, error)
	Update(ctx context.Context, incident Incident) (Incident, error)
	Acknowledge(ctx context.Context, id, userID uuid.UUID, at time.Time) (Incident, error)
	Snooze(ctx context.Context, id uuid.UUID, until, at time.Time) (Incident, error)
	ClearExpiredSnooze(ctx context.Context, id uuid.UUID, notifiedUntil, at time.Time) (Incident, error)
	CreateEvent(ctx context.Context, event Event) error
	CreateDelivery(ctx context.Context, intent DeliveryIntent) error
	PruneEvents(ctx context.Context, incidentID uuid.UUID, keep int) error
}

type Repository interface {
	Transact(ctx context.Context, fn func(Tx) error) error
	Get(ctx context.Context, id uuid.UUID) (Incident, error)
	List(ctx context.Context, filter ListFilter) ([]Incident, error)
	ListEvents(ctx context.Context, filter EventFilter) ([]Event, error)
	ListActiveNodeIncidents(ctx context.Context) ([]Incident, error)
	ListDeliveries(ctx context.Context, incidentID uuid.UUID, limit int) ([]DeliveryIntent, error)
	ClaimPendingDeliveries(ctx context.Context, limit, maxAttempts int) ([]DeliveryIntent, error)
	ReclaimStaleDeliveries(ctx context.Context) (int64, error)
	MarkDelivery(ctx context.Context, intentID, incidentID uuid.UUID, state, hold, transition string, severity Severity, attempts int, errorText string, at time.Time) error
	ClearExpiredSnooze(ctx context.Context, id uuid.UUID, notifiedUntil, at time.Time) (Incident, error)
	ListExpiredSnoozes(ctx context.Context, now time.Time, limit int) ([]Incident, error)
	PruneResolved(ctx context.Context, before time.Time) (int64, error)
}

type storeRepository struct{ st *store.Store }

func newStoreRepository(st *store.Store) *storeRepository { return &storeRepository{st: st} }

func (r *storeRepository) Transact(ctx context.Context, fn func(Tx) error) error {
	return r.st.InTx(ctx, func(tx *store.Store) error {
		return fn(&storeTx{st: tx})
	})
}

func (r *storeRepository) Get(ctx context.Context, id uuid.UUID) (Incident, error) {
	row, err := r.st.GetIncident(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Incident{}, ErrNotFound
		}
		return Incident{}, fmt.Errorf("incidents: get: %w", err)
	}
	return incidentFromRow(row), nil
}

func (r *storeRepository) List(ctx context.Context, filter ListFilter) ([]Incident, error) {
	params := sqlc.ListIncidentsParams{
		IsOwner:         filter.IsOwner,
		UserID:          pgUUID(filter.UserID),
		IncludeResolved: filter.IncludeResolved,
		RowLimit:        int32(filter.Limit),
	}
	if filter.Lifecycle != nil {
		value := string(*filter.Lifecycle)
		params.LifecycleFilter = &value
	}
	if filter.Severity != nil {
		value := string(*filter.Severity)
		params.SeverityFilter = &value
	}
	if filter.Cursor != nil {
		params.CursorAt = pgtype.Timestamptz{Time: filter.Cursor.At, Valid: true}
		params.CursorID = pgUUID(filter.Cursor.ID)
	}
	rows, err := r.st.ListIncidents(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("incidents: list: %w", err)
	}
	return incidentsFromRows(rows), nil
}

func (r *storeRepository) ListEvents(ctx context.Context, filter EventFilter) ([]Event, error) {
	params := sqlc.ListIncidentEventsParams{
		IncidentID: pgUUID(filter.IncidentID),
		RowLimit:   int32(filter.Limit),
	}
	if filter.Cursor != nil {
		params.CursorAt = pgtype.Timestamptz{Time: filter.Cursor.At, Valid: true}
		params.CursorID = pgUUID(filter.Cursor.ID)
	}
	rows, err := r.st.ListIncidentEvents(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("incidents: list history: %w", err)
	}
	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, eventFromRow(row))
	}
	return events, nil
}

func (r *storeRepository) ListActiveNodeIncidents(ctx context.Context) ([]Incident, error) {
	rows, err := r.st.ListActiveNodeIncidents(ctx)
	if err != nil {
		return nil, fmt.Errorf("incidents: list node incidents: %w", err)
	}
	return incidentsFromRows(rows), nil
}

func (r *storeRepository) ListDeliveries(ctx context.Context, incidentID uuid.UUID, limit int) ([]DeliveryIntent, error) {
	rows, err := r.st.ListIncidentDeliveries(ctx, sqlc.ListIncidentDeliveriesParams{
		IncidentID: pgUUID(incidentID),
		Limit:      int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("incidents: list deliveries: %w", err)
	}
	return intentsFromRows(rows), nil
}

func (r *storeRepository) ClaimPendingDeliveries(ctx context.Context, limit, maxAttempts int) ([]DeliveryIntent, error) {
	rows, err := r.st.ClaimPendingIncidentDeliveries(ctx, sqlc.ClaimPendingIncidentDeliveriesParams{
		Limit:    int32(limit),
		Attempts: int32(maxAttempts),
	})
	if err != nil {
		return nil, fmt.Errorf("incidents: claim pending deliveries: %w", err)
	}
	return intentsFromRows(rows), nil
}

func (r *storeRepository) ReclaimStaleDeliveries(ctx context.Context) (int64, error) {
	return r.st.ReclaimStaleIncidentDeliveries(ctx)
}

func (r *storeRepository) MarkDelivery(ctx context.Context, intentID, incidentID uuid.UUID, state, hold, transition string, severity Severity, attempts int, errorText string, at time.Time) error {
	return r.st.InTx(ctx, func(tx *store.Store) error {
		if err := tx.MarkIncidentDelivery(ctx, sqlc.MarkIncidentDeliveryParams{
			ID:        pgUUID(intentID),
			State:     state,
			LastError: errorText,
			UpdatedAt: pgtype.Timestamptz{Time: at, Valid: true},
		}); err != nil {
			return fmt.Errorf("incidents: mark delivery: %w", err)
		}
		if err := tx.SetIncidentDelivery(ctx, sqlc.SetIncidentDeliveryParams{
			ID:                   pgUUID(incidentID),
			DeliveryState:        state,
			DeliveryHold:         hold,
			DeliveryEvent:        transition,
			DeliveryAttempts:     int32(attempts),
			DeliveryError:        errorText,
			LastNotifiedSeverity: string(severity),
			UpdatedAt:            pgtype.Timestamptz{Time: at, Valid: true},
		}); err != nil {
			return fmt.Errorf("incidents: set delivery state: %w", err)
		}
		return nil
	})
}

func (r *storeRepository) ClearExpiredSnooze(ctx context.Context, id uuid.UUID, notifiedUntil, at time.Time) (Incident, error) {
	row, err := r.st.ClearExpiredSnooze(ctx, sqlc.ClearExpiredSnoozeParams{
		ID:                  pgUUID(id),
		SnoozeNotifiedUntil: pgtype.Timestamptz{Time: notifiedUntil, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: at, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Incident{}, ErrNotFound
		}
		return Incident{}, fmt.Errorf("incidents: clear snooze: %w", err)
	}
	return incidentFromRow(row), nil
}

func (r *storeRepository) ListExpiredSnoozes(ctx context.Context, now time.Time, limit int) ([]Incident, error) {
	rows, err := r.st.ListExpiredSnoozes(ctx, sqlc.ListExpiredSnoozesParams{
		SnoozedUntil: pgtype.Timestamptz{Time: now, Valid: true},
		Limit:        int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("incidents: list expired snoozes: %w", err)
	}
	return incidentsFromRows(rows), nil
}

func (r *storeRepository) PruneResolved(ctx context.Context, before time.Time) (int64, error) {
	return r.st.PruneResolvedIncidents(ctx, pgtype.Timestamptz{Time: before, Valid: true})
}

type storeTx struct{ st *store.Store }

func (t *storeTx) ActiveByIdentity(ctx context.Context, identity string) (Incident, error) {
	row, err := t.st.GetActiveIncidentByIdentity(ctx, identity)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Incident{}, ErrNotFound
		}
		return Incident{}, fmt.Errorf("incidents: active by identity: %w", err)
	}
	return incidentFromRow(row), nil
}

func (t *storeTx) GetForUpdate(ctx context.Context, id uuid.UUID) (Incident, error) {
	row, err := t.st.GetIncidentForUpdate(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Incident{}, ErrNotFound
		}
		return Incident{}, fmt.Errorf("incidents: get for update: %w", err)
	}
	return incidentFromRow(row), nil
}

func (t *storeTx) Acknowledge(ctx context.Context, id, userID uuid.UUID, at time.Time) (Incident, error) {
	row, err := t.st.AcknowledgeIncident(ctx, sqlc.AcknowledgeIncidentParams{
		ID:             pgUUID(id),
		AcknowledgedAt: pgtype.Timestamptz{Time: at, Valid: true},
		AcknowledgedBy: pgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Incident{}, ErrNotFound
		}
		return Incident{}, fmt.Errorf("incidents: acknowledge: %w", err)
	}
	return incidentFromRow(row), nil
}

func (t *storeTx) Snooze(ctx context.Context, id uuid.UUID, until, at time.Time) (Incident, error) {
	row, err := t.st.SnoozeIncident(ctx, sqlc.SnoozeIncidentParams{
		ID:           pgUUID(id),
		SnoozedUntil: pgtype.Timestamptz{Time: until, Valid: true},
		UpdatedAt:    pgtype.Timestamptz{Time: at, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Incident{}, ErrNotFound
		}
		return Incident{}, fmt.Errorf("incidents: snooze: %w", err)
	}
	return incidentFromRow(row), nil
}

func (t *storeTx) ClearExpiredSnooze(ctx context.Context, id uuid.UUID, notifiedUntil, at time.Time) (Incident, error) {
	row, err := t.st.ClearExpiredSnooze(ctx, sqlc.ClearExpiredSnoozeParams{
		ID:                  pgUUID(id),
		SnoozeNotifiedUntil: pgtype.Timestamptz{Time: notifiedUntil, Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: at, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Incident{}, ErrNotFound
		}
		return Incident{}, fmt.Errorf("incidents: clear snooze: %w", err)
	}
	return incidentFromRow(row), nil
}

func (t *storeTx) Create(ctx context.Context, incident Incident) (Incident, error) {
	row, err := t.st.CreateIncident(ctx, sqlc.CreateIncidentParams{
		ID:                   pgUUID(incident.ID),
		IdentityKey:          incident.Identity,
		DetectorKey:          incident.Detector,
		ScopeKind:            incident.ScopeKind,
		ScopeID:              pgUUID(incident.ScopeID),
		NodeID:               pgUUID(incident.NodeID),
		ProjectID:            pgUUID(incident.ProjectID),
		ResourceType:         incident.ResourceType,
		ResourceID:           pgUUID(incident.ResourceID),
		ResourceName:         incident.ResourceName,
		Severity:             string(incident.Severity),
		Lifecycle:            string(incident.Lifecycle),
		Coverage:             string(incident.Coverage),
		FirstObservedAt:      ts(incident.FirstObservedAt),
		LastObservedAt:       ts(incident.LastObservedAt),
		LastValidAt:          tsPtr(incident.LastValidAt),
		ConfirmedAt:          tsPtr(incident.ConfirmedAt),
		RecoveryStartedAt:    tsPtr(incident.RecoveryStartedAt),
		ResolvedAt:           tsPtr(incident.ResolvedAt),
		CloseReason:          incident.CloseReason,
		AcknowledgedAt:       tsPtr(incident.AcknowledgedAt),
		AcknowledgedBy:       pgUUID(incident.AcknowledgedBy),
		SnoozedUntil:         tsPtr(incident.SnoozedUntil),
		SnoozeNotifiedUntil:  tsPtr(incident.SnoozeNotifiedUntil),
		DeliveryState:        incident.DeliveryState,
		DeliveryHold:         incident.DeliveryHold,
		DeliveryEvent:        incident.DeliveryEvent,
		DeliveryAttempts:     int32(incident.DeliveryAttempts),
		DeliveryError:        incident.DeliveryError,
		LastNotifiedSeverity: string(incident.LastNotifiedSeverity),
		Evidence:             incident.Evidence,
		CreatedAt:            ts(incident.CreatedAt),
		UpdatedAt:            ts(incident.UpdatedAt),
		SeveritySince:        tsPtr(incident.SeveritySince),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The partial unique index won a concurrent insert; the caller
			// re-reads the active row with its row lock.
			return Incident{}, ErrConflict
		}
		return Incident{}, fmt.Errorf("incidents: create: %w", err)
	}
	return incidentFromRow(row), nil
}

func (t *storeTx) Update(ctx context.Context, incident Incident) (Incident, error) {
	row, err := t.st.UpdateIncident(ctx, sqlc.UpdateIncidentParams{
		ID:                   pgUUID(incident.ID),
		Severity:             string(incident.Severity),
		Lifecycle:            string(incident.Lifecycle),
		Coverage:             string(incident.Coverage),
		FirstObservedAt:      ts(incident.FirstObservedAt),
		LastObservedAt:       ts(incident.LastObservedAt),
		LastValidAt:          tsPtr(incident.LastValidAt),
		ConfirmedAt:          tsPtr(incident.ConfirmedAt),
		RecoveryStartedAt:    tsPtr(incident.RecoveryStartedAt),
		ResolvedAt:           tsPtr(incident.ResolvedAt),
		CloseReason:          incident.CloseReason,
		SnoozeNotifiedUntil:  tsPtr(incident.SnoozeNotifiedUntil),
		DeliveryState:        incident.DeliveryState,
		DeliveryHold:         incident.DeliveryHold,
		DeliveryEvent:        incident.DeliveryEvent,
		DeliveryAttempts:     int32(incident.DeliveryAttempts),
		DeliveryError:        incident.DeliveryError,
		LastNotifiedSeverity: string(incident.LastNotifiedSeverity),
		Evidence:             incident.Evidence,
		UpdatedAt:            ts(incident.UpdatedAt),
		SeveritySince:        tsPtr(incident.SeveritySince),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Incident{}, ErrNotFound
		}
		return Incident{}, fmt.Errorf("incidents: update: %w", err)
	}
	return incidentFromRow(row), nil
}

func (t *storeTx) CreateEvent(ctx context.Context, event Event) error {
	return t.st.CreateIncidentEvent(ctx, sqlc.CreateIncidentEventParams{
		ID:            pgUUID(event.ID),
		IncidentID:    pgUUID(event.IncidentID),
		Kind:          event.Kind,
		FromLifecycle: string(event.FromLifecycle),
		ToLifecycle:   string(event.ToLifecycle),
		FromSeverity:  string(event.FromSeverity),
		ToSeverity:    string(event.ToSeverity),
		Reason:        event.Reason,
		ActorID:       pgUUID(event.ActorID),
		Detail:        event.Detail,
		CreatedAt:     ts(event.CreatedAt),
	})
}

func (t *storeTx) CreateDelivery(ctx context.Context, intent DeliveryIntent) error {
	return t.st.CreateIncidentDeliveryIntent(ctx, sqlc.CreateIncidentDeliveryIntentParams{
		ID:         pgUUID(intent.ID),
		IncidentID: pgUUID(intent.IncidentID),
		Transition: intent.Transition,
		Severity:   string(intent.Severity),
		CreatedAt:  ts(intent.CreatedAt),
	})
}

func (t *storeTx) PruneEvents(ctx context.Context, incidentID uuid.UUID, keep int) error {
	_, err := t.st.PruneIncidentEvents(ctx, sqlc.PruneIncidentEventsParams{
		IncidentID: pgUUID(incidentID),
		Limit:      int32(keep),
	})
	return err
}

// ErrConflict signals a concurrent insert on the open-identity index.
var ErrConflict = errors.New("incidents: concurrent transition")

func incidentFromRow(row sqlc.Incident) Incident {
	return Incident{
		ID:                   uuidFromPG(row.ID),
		Identity:             row.IdentityKey,
		Detector:             row.DetectorKey,
		ScopeKind:            row.ScopeKind,
		ScopeID:              uuidFromPG(row.ScopeID),
		NodeID:               uuidFromPG(row.NodeID),
		ProjectID:            uuidFromPG(row.ProjectID),
		ResourceType:         row.ResourceType,
		ResourceID:           uuidFromPG(row.ResourceID),
		ResourceName:         row.ResourceName,
		Severity:             Severity(row.Severity),
		Lifecycle:            Lifecycle(row.Lifecycle),
		Coverage:             Coverage(row.Coverage),
		FirstObservedAt:      row.FirstObservedAt.Time.UTC(),
		LastObservedAt:       row.LastObservedAt.Time.UTC(),
		LastValidAt:          timeFromPG(row.LastValidAt),
		ConfirmedAt:          timeFromPG(row.ConfirmedAt),
		RecoveryStartedAt:    timeFromPG(row.RecoveryStartedAt),
		ResolvedAt:           timeFromPG(row.ResolvedAt),
		CloseReason:          row.CloseReason,
		AcknowledgedAt:       timeFromPG(row.AcknowledgedAt),
		AcknowledgedBy:       uuidFromPG(row.AcknowledgedBy),
		SnoozedUntil:         timeFromPG(row.SnoozedUntil),
		SnoozeNotifiedUntil:  timeFromPG(row.SnoozeNotifiedUntil),
		DeliveryState:        row.DeliveryState,
		DeliveryHold:         row.DeliveryHold,
		DeliveryEvent:        row.DeliveryEvent,
		DeliveryAttempts:     int(row.DeliveryAttempts),
		DeliveryError:        row.DeliveryError,
		LastNotifiedSeverity: Severity(row.LastNotifiedSeverity),
		Evidence:             row.Evidence,
		CreatedAt:            row.CreatedAt.Time.UTC(),
		UpdatedAt:            row.UpdatedAt.Time.UTC(),
		SeveritySince:        timeFromPG(row.SeveritySince),
	}
}

func incidentsFromRows(rows []sqlc.Incident) []Incident {
	out := make([]Incident, 0, len(rows))
	for _, row := range rows {
		out = append(out, incidentFromRow(row))
	}
	return out
}

func eventFromRow(row sqlc.IncidentEvent) Event {
	return Event{
		ID:            uuidFromPG(row.ID),
		IncidentID:    uuidFromPG(row.IncidentID),
		Kind:          row.Kind,
		FromLifecycle: Lifecycle(row.FromLifecycle),
		ToLifecycle:   Lifecycle(row.ToLifecycle),
		FromSeverity:  Severity(row.FromSeverity),
		ToSeverity:    Severity(row.ToSeverity),
		Reason:        row.Reason,
		ActorID:       uuidFromPG(row.ActorID),
		Detail:        row.Detail,
		CreatedAt:     row.CreatedAt.Time.UTC(),
	}
}

func intentsFromRows(rows []sqlc.IncidentDeliveryIntent) []DeliveryIntent {
	out := make([]DeliveryIntent, 0, len(rows))
	for _, row := range rows {
		out = append(out, DeliveryIntent{
			ID:         uuidFromPG(row.ID),
			IncidentID: uuidFromPG(row.IncidentID),
			Transition: row.Transition,
			Severity:   Severity(row.Severity),
			State:      row.State,
			Attempts:   int(row.Attempts),
			LastError:  row.LastError,
			CreatedAt:  row.CreatedAt.Time.UTC(),
		})
	}
	return out
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func uuidFromPG(value pgtype.UUID) uuid.UUID {
	if !value.Valid {
		return uuid.Nil
	}
	return uuid.UUID(value.Bytes)
}

func ts(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

func tsPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func timeFromPG(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}
