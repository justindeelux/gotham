package incidents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/store"
)

const (
	maxDeliveryAttempts = 5
	deliveryBatch       = 50
	maxSnooze           = 30 * 24 * time.Hour
	sweepInterval       = 30 * time.Second
)

// Access resolves the caller's current role and project grants. It is read on
// every list, detail and mutation instead of trusting a cached decision.
type Access interface {
	Role(ctx context.Context, userID uuid.UUID) (string, error)
	HasProjectGrant(ctx context.Context, userID, projectID uuid.UUID) (bool, error)
}

type storeAccess struct{ st *store.Store }

func (a storeAccess) Role(ctx context.Context, userID uuid.UUID) (string, error) {
	user, err := a.st.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		return "", err
	}
	return user.Role, nil
}

func (a storeAccess) HasProjectGrant(ctx context.Context, userID, projectID uuid.UUID) (bool, error) {
	return a.st.HasProjectGrant(ctx, userID, projectID)
}

type Config struct {
	Store      *store.Store
	Repository Repository
	Notifier   Notifier
	Access     Access
	Logger     *slog.Logger
	Now        func() time.Time
	Retention  time.Duration
	PageSize   int
}

type Service struct {
	repo      Repository
	notifier  Notifier
	access    Access
	logger    *slog.Logger
	now       func() time.Time
	retention time.Duration
	pageSize  int
}

func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	repo := cfg.Repository
	if repo == nil && cfg.Store != nil {
		repo = newStoreRepository(cfg.Store)
	}
	access := cfg.Access
	if access == nil && cfg.Store != nil {
		access = storeAccess{st: cfg.Store}
	}
	retention := cfg.Retention
	if retention <= 0 {
		retention = DefaultRetention
	}
	pageSize := cfg.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	return &Service{
		repo:      repo,
		notifier:  cfg.Notifier,
		access:    access,
		logger:    logger,
		now:       now,
		retention: retention,
		pageSize:  pageSize,
	}
}

func (s *Service) ready() error {
	if s == nil || s.repo == nil {
		return errors.New("incidents: repository is not configured")
	}
	return nil
}

// Report applies one detector observation and commits the resulting
// transition, history row and local delivery intent in one transaction.
func (s *Service) Report(ctx context.Context, obs Observation) (*Incident, Transition, error) {
	if err := s.ready(); err != nil {
		return nil, Transition{}, err
	}
	if err := obs.validate(); err != nil {
		return nil, Transition{}, err
	}
	now := s.now().UTC()

	var result *Incident
	var transition Transition
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		transition = Transition{}
		err = s.repo.Transact(ctx, func(tx Tx) error {
			active, err := tx.ActiveByIdentity(ctx, obs.Identity)
			var inc *Incident
			if err == nil {
				inc = &active
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
			previousLifecycle, previousSeverity := Lifecycle(""), Severity("")
			if inc != nil {
				previousLifecycle, previousSeverity = inc.Lifecycle, inc.Severity
			}
			next, tr := advance(inc, obs, now)
			transition = tr
			if next == nil {
				result = nil
				return nil
			}
			if inc == nil {
				created, createErr := tx.Create(ctx, *next)
				if createErr != nil {
					return createErr
				}
				*next = created
			} else {
				updated, updateErr := tx.Update(ctx, *next)
				if updateErr != nil {
					return updateErr
				}
				*next = updated
			}

			if tr.Event != "" {
				if err := tx.CreateEvent(ctx, Event{
					ID:            uuid.New(),
					IncidentID:    next.ID,
					Kind:          eventKind(tr.Event),
					FromLifecycle: previousLifecycle,
					ToLifecycle:   next.Lifecycle,
					FromSeverity:  previousSeverity,
					ToSeverity:    next.Severity,
					Reason:        tr.Reason,
					CreatedAt:     now,
				}); err != nil {
					return err
				}
			}

			switch {
			case tr.notify() && obs.SuppressDelivery:
				next.DeliveryState = DeliverySuppressed
				next.DeliveryEvent = tr.Event
				next.DeliveryHold = suppressApproval(obs.SuppressReason)
				updated, err := tx.Update(ctx, *next)
				if err != nil {
					return err
				}
				*next = updated
			case tr.notify():
				if err := tx.CreateDelivery(ctx, DeliveryIntent{
					ID:         uuid.New(),
					IncidentID: next.ID,
					Transition: tr.Event,
					Severity:   next.Severity,
					State:      DeliveryPending,
					CreatedAt:  now,
				}); err != nil {
					return err
				}
			}

			if err := tx.PruneEvents(ctx, next.ID, MaxHistory); err != nil {
				return err
			}
			result = next
			return nil
		})
		if errors.Is(err, ErrConflict) {
			continue
		}
		break
	}
	if err != nil {
		return nil, transition, fmt.Errorf("incidents: report: %w", err)
	}
	if result != nil && transition.notify() && result.DeliveryState == DeliveryPending {
		s.dispatch(ctx)
	}
	return result, transition, nil
}

func suppressApproval(reason string) string {
	if reason == "" {
		return "delivery suppressed by the producer"
	}
	return reason
}

func eventKind(transition string) string {
	switch transition {
	case TransitionOpened:
		return "opened"
	case TransitionSeverityChanged:
		return "severity_changed"
	case TransitionRecovered:
		return "recovered"
	default:
		return transition
	}
}

// Close resolves an active incident because its detector, rule or resource
// went away. It never emits a recovery notification.
func (s *Service) Close(ctx context.Context, identity, reason string) error {
	if err := s.ready(); err != nil {
		return err
	}
	if identity == "" || reason == "" {
		return fmt.Errorf("%w: identity and reason are required", ErrValidation)
	}
	now := s.now().UTC()
	return s.repo.Transact(ctx, func(tx Tx) error {
		inc, err := tx.ActiveByIdentity(ctx, identity)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			return err
		}
		return closeIncident(ctx, tx, &inc, reason, now)
	})
}

func closeIncident(ctx context.Context, tx Tx, inc *Incident, reason string, now time.Time) error {
	if !inc.Active() {
		return nil
	}
	previous := inc.Lifecycle
	inc.Lifecycle = LifecycleResolved
	inc.ResolvedAt = timePtr(now)
	inc.CloseReason = reason
	inc.RecoveryStartedAt = nil
	inc.UpdatedAt = now
	if _, err := tx.Update(ctx, *inc); err != nil {
		return err
	}
	return tx.CreateEvent(ctx, Event{
		ID:            uuid.New(),
		IncidentID:    inc.ID,
		Kind:          "closed",
		FromLifecycle: previous,
		ToLifecycle:   LifecycleResolved,
		Reason:        reason,
		CreatedAt:     now,
	})
}

func (s *Service) Acknowledge(ctx context.Context, userID, incidentID uuid.UUID) (Incident, error) {
	if err := s.ready(); err != nil {
		return Incident{}, err
	}
	now := s.now().UTC()
	var result Incident
	err := s.repo.Transact(ctx, func(tx Tx) error {
		inc, err := tx.GetForUpdate(ctx, incidentID)
		if err != nil {
			return err
		}
		if !inc.Active() {
			return ErrNotFound
		}
		if err := s.authorize(ctx, userID, inc); err != nil {
			return err
		}
		updated, err := tx.Acknowledge(ctx, incidentID, userID, now)
		if err != nil {
			return err
		}
		result = updated
		return tx.CreateEvent(ctx, Event{
			ID: uuid.New(), IncidentID: incidentID, Kind: "acknowledged",
			FromLifecycle: inc.Lifecycle, ToLifecycle: inc.Lifecycle,
			ActorID: userID, CreatedAt: now,
		})
	})
	return result, err
}

func (s *Service) Snooze(ctx context.Context, userID, incidentID uuid.UUID, until time.Time) (Incident, error) {
	if err := s.ready(); err != nil {
		return Incident{}, err
	}
	now := s.now().UTC()
	until = until.UTC()
	if until.Before(now) || until.After(now.Add(maxSnooze)) {
		return Incident{}, fmt.Errorf("%w: until must be within the next 30 days", ErrValidation)
	}
	var result Incident
	err := s.repo.Transact(ctx, func(tx Tx) error {
		inc, err := tx.GetForUpdate(ctx, incidentID)
		if err != nil {
			return err
		}
		if !inc.Active() {
			return ErrNotFound
		}
		if err := s.authorize(ctx, userID, inc); err != nil {
			return err
		}
		updated, err := tx.Snooze(ctx, incidentID, until, now)
		if err != nil {
			return err
		}
		result = updated
		return tx.CreateEvent(ctx, Event{
			ID: uuid.New(), IncidentID: incidentID, Kind: "snoozed",
			FromLifecycle: inc.Lifecycle, ToLifecycle: inc.Lifecycle,
			ActorID: userID, Detail: until.Format(time.RFC3339), CreatedAt: now,
		})
	})
	return result, err
}

// authorize returns ErrNotFound for a member who cannot see the incident's
// project, so a guessed ID does not disclose another project's incident.
func (s *Service) authorize(ctx context.Context, userID uuid.UUID, inc Incident) error {
	if s.access == nil {
		return ErrForbidden
	}
	if userID == uuid.Nil {
		return ErrNotFound
	}
	role, err := s.access.Role(ctx, userID)
	if err != nil {
		return fmt.Errorf("incidents: resolve role: %w", err)
	}
	if role == auth.RoleOwner {
		return nil
	}
	if inc.ProjectID == uuid.Nil {
		return ErrNotFound
	}
	allowed, err := s.access.HasProjectGrant(ctx, userID, inc.ProjectID)
	if err != nil {
		return fmt.Errorf("incidents: resolve grant: %w", err)
	}
	if !allowed {
		return ErrNotFound
	}
	return nil
}

func (s *Service) isOwner(ctx context.Context, userID uuid.UUID) (bool, error) {
	if s.access == nil || userID == uuid.Nil {
		return false, nil
	}
	role, err := s.access.Role(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("incidents: resolve role: %w", err)
	}
	return role == auth.RoleOwner, nil
}

func (s *Service) Get(ctx context.Context, userID, incidentID uuid.UUID) (IncidentView, error) {
	if err := s.ready(); err != nil {
		return IncidentView{}, err
	}
	inc, err := s.repo.Get(ctx, incidentID)
	if err != nil {
		return IncidentView{}, err
	}
	if err := s.authorize(ctx, userID, inc); err != nil {
		return IncidentView{}, err
	}
	grouping, err := s.nodeGrouping(ctx)
	if err != nil {
		return IncidentView{}, err
	}
	owner, err := s.isOwner(ctx, userID)
	if err != nil {
		return IncidentView{}, err
	}
	return view(inc, grouping, owner), nil
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, filter ListParams) (Page, error) {
	if err := s.ready(); err != nil {
		return Page{}, err
	}
	owner, err := s.isOwner(ctx, userID)
	if err != nil {
		return Page{}, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = s.pageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	rows, err := s.repo.List(ctx, ListFilter{
		UserID:          userID,
		IsOwner:         owner,
		IncludeResolved: filter.IncludeResolved,
		Lifecycle:       filter.Lifecycle,
		Severity:        filter.Severity,
		Cursor:          filter.Cursor,
		// Read one extra row to know whether another page exists without a
		// count that could disclose hidden rows.
		Limit: limit + 1,
	})
	if err != nil {
		return Page{}, err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeCursor(Cursor{At: last.UpdatedAt, ID: last.ID})
	}
	grouping, err := s.nodeGrouping(ctx)
	if err != nil {
		return Page{}, err
	}
	views := make([]IncidentView, 0, len(rows))
	for _, inc := range rows {
		views = append(views, view(inc, grouping, owner))
	}
	return Page{Incidents: views, NextCursor: next}, nil
}

func (s *Service) History(ctx context.Context, userID, incidentID uuid.UUID, filter HistoryParams) ([]Event, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	inc, err := s.repo.Get(ctx, incidentID)
	if err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, userID, inc); err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = s.pageSize
	}
	if limit > MaxPageSize+1 {
		limit = MaxPageSize + 1
	}
	return s.repo.ListEvents(ctx, EventFilter{IncidentID: incidentID, Cursor: filter.Cursor, Limit: limit})
}

func (s *Service) Deliveries(ctx context.Context, userID, incidentID uuid.UUID) ([]DeliveryIntent, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	inc, err := s.repo.Get(ctx, incidentID)
	if err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, userID, inc); err != nil {
		return nil, err
	}
	return s.repo.ListDeliveries(ctx, incidentID, 10)
}

func (s *Service) nodeGrouping(ctx context.Context) (map[uuid.UUID]Incident, error) {
	rows, err := s.repo.ListActiveNodeIncidents(ctx)
	if err != nil {
		return nil, err
	}
	grouping := make(map[uuid.UUID]Incident, len(rows))
	for _, inc := range rows {
		if inc.NodeID != uuid.Nil {
			grouping[inc.NodeID] = inc
		}
	}
	return grouping, nil
}

// Sweep retries local handoffs, expires snoozes without falsifying health and
// prunes resolved history. Active incidents are never pruned.
func (s *Service) Sweep(ctx context.Context) error {
	if err := s.ready(); err != nil {
		return err
	}
	now := s.now().UTC()
	if _, err := s.repo.PruneResolved(ctx, now.Add(-s.retention)); err != nil {
		return fmt.Errorf("incidents: prune: %w", err)
	}
	if err := s.expireSnoozes(ctx, now); err != nil {
		return err
	}
	s.dispatch(ctx)
	return nil
}

func (s *Service) expireSnoozes(ctx context.Context, now time.Time) error {
	rows, err := s.repo.ListExpiredSnoozes(ctx, now, deliveryBatch)
	if err != nil {
		return err
	}
	for _, inc := range rows {
		err := s.repo.Transact(ctx, func(tx Tx) error {
			updated, err := tx.ClearExpiredSnooze(ctx, inc.ID, *inc.SnoozedUntil, now)
			if err != nil {
				return err
			}
			if updated.AcknowledgedAt != nil || !fresh(updated, now) {
				// The condition is acknowledged or no longer freshly
				// confirmed: clear the snooze silently.
				return nil
			}
			if err := tx.CreateEvent(ctx, Event{
				ID: uuid.New(), IncidentID: inc.ID, Kind: "snooze_expired",
				FromLifecycle: inc.Lifecycle, ToLifecycle: inc.Lifecycle,
				Reason: "condition still confirmed", CreatedAt: now,
			}); err != nil {
				return err
			}
			return tx.CreateDelivery(ctx, DeliveryIntent{
				ID: uuid.New(), IncidentID: inc.ID, Transition: TransitionReminder,
				Severity: inc.Severity, State: DeliveryPending, CreatedAt: now,
			})
		})
		if err != nil {
			return fmt.Errorf("incidents: expire snooze: %w", err)
		}
	}
	return nil
}

func fresh(inc Incident, now time.Time) bool {
	return inc.Coverage == CoverageFresh && inc.LastValidAt != nil && now.Sub(*inc.LastValidAt) <= DefaultMaxGap
}

func (s *Service) dispatch(ctx context.Context) {
	if s.notifier == nil {
		return
	}
	if _, err := s.repo.ReclaimStaleDeliveries(ctx); err != nil {
		s.logger.Warn("incidents: reclaim stale deliveries", "error", err)
	}
	intents, err := s.repo.ClaimPendingDeliveries(ctx, deliveryBatch, maxDeliveryAttempts)
	if err != nil {
		s.logger.Warn("incidents: claim pending deliveries", "error", err)
		return
	}
	for _, intent := range intents {
		s.deliver(ctx, intent)
	}
}

func (s *Service) deliver(ctx context.Context, intent DeliveryIntent) {
	inc, err := s.repo.Get(ctx, intent.IncidentID)
	if err != nil {
		if markErr := s.repo.MarkDelivery(ctx, intent.ID, intent.IncidentID, DeliverySuppressed, "incident not found", intent.Transition, "", 0, "", s.now().UTC()); markErr != nil {
			s.logger.Warn("incidents: drop delivery for missing incident", "error", markErr)
		}
		return
	}
	now := s.now().UTC()
	hold := holdReason(inc, intent, now)
	if hold != "" {
		if err := s.repo.MarkDelivery(ctx, intent.ID, intent.IncidentID, DeliverySuppressed, hold, intent.Transition, inc.LastNotifiedSeverity, 0, "", now); err != nil {
			s.logger.Warn("incidents: record suppressed delivery", "error", err)
		}
		return
	}
	delivery := Delivery{
		IncidentID:   inc.ID,
		Transition:   intent.Transition,
		Severity:     intent.Severity,
		Detector:     inc.Detector,
		Name:         inc.ResourceName,
		Evidence:     inc.Evidence,
		ResourceType: inc.ResourceType,
		ResourceID:   inc.ResourceID,
		ProjectID:    inc.ProjectID,
		NodeID:       inc.NodeID,
		At:           now,
	}
	if delivery.Name == "" {
		delivery.Name = inc.Detector
	}
	if err := s.notifier.NotifyIncident(ctx, delivery); err != nil {
		if markErr := s.repo.MarkDelivery(ctx, intent.ID, intent.IncidentID, DeliveryFailed, "", intent.Transition, inc.LastNotifiedSeverity, 1, err.Error(), now); markErr != nil {
			s.logger.Warn("incidents: record failed delivery", "error", markErr)
		}
		return
	}
	if err := s.repo.MarkDelivery(ctx, intent.ID, intent.IncidentID, DeliveryDispatched, "", intent.Transition, intent.Severity, 1, "", now); err != nil {
		s.logger.Warn("incidents: record dispatched delivery", "error", err)
	}
}

func holdReason(inc Incident, intent DeliveryIntent, now time.Time) string {
	if !inc.Active() && intent.Transition != TransitionRecovered {
		return "incident is resolved"
	}
	if intent.Transition == TransitionRecovered {
		return ""
	}
	if inc.AcknowledgedAt != nil {
		return "acknowledged"
	}
	if inc.SnoozedUntil != nil && now.Before(*inc.SnoozedUntil) {
		return "snoozed until " + inc.SnoozedUntil.UTC().Format(time.RFC3339)
	}
	switch inc.Coverage {
	case CoverageExpected:
		return "expected maintenance"
	case CoverageUnknown, CoverageStale:
		return "coverage " + string(inc.Coverage)
	}
	return ""
}

// Run drives snooze expiry, local retry and retention until ctx ends.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := s.Sweep(sweepCtx); err != nil {
				s.logger.Warn("incidents: sweep", "error", err)
			}
			cancel()
		}
	}
}

// ListParams and HistoryParams are the bounded read filters exposed by the API.
type ListParams struct {
	IncludeResolved bool
	Lifecycle       *Lifecycle
	Severity        *Severity
	Cursor          *Cursor
	Limit           int
}

type HistoryParams struct {
	Cursor *Cursor
	Limit  int
}
