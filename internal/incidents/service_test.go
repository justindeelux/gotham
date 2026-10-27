package incidents

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepo struct {
	incidents map[uuid.UUID]*Incident
	events    []Event
	intents   map[uuid.UUID]*DeliveryIntent
	grants    map[uuid.UUID]map[uuid.UUID]bool
	pruned    []uuid.UUID
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		incidents: map[uuid.UUID]*Incident{},
		intents:   map[uuid.UUID]*DeliveryIntent{},
		grants:    map[uuid.UUID]map[uuid.UUID]bool{},
	}
}

func (f *fakeRepo) Transact(ctx context.Context, fn func(Tx) error) error {
	backup := map[uuid.UUID]Incident{}
	for id, inc := range f.incidents {
		backup[id] = *inc
	}
	eventBackup := append([]Event(nil), f.events...)
	intentBackup := map[uuid.UUID]DeliveryIntent{}
	for id, intent := range f.intents {
		intentBackup[id] = *intent
	}
	if err := fn(&fakeTx{repo: f}); err != nil {
		f.incidents = map[uuid.UUID]*Incident{}
		for id, inc := range backup {
			copied := inc
			f.incidents[id] = &copied
		}
		f.events = eventBackup
		f.intents = map[uuid.UUID]*DeliveryIntent{}
		for id, intent := range intentBackup {
			copied := intent
			f.intents[id] = &copied
		}
		return err
	}
	return nil
}

func (f *fakeRepo) Get(_ context.Context, id uuid.UUID) (Incident, error) {
	if inc, ok := f.incidents[id]; ok {
		return *inc, nil
	}
	return Incident{}, ErrNotFound
}

func (f *fakeRepo) List(_ context.Context, filter ListFilter) ([]Incident, error) {
	rows := make([]Incident, 0, len(f.incidents))
	for _, inc := range f.incidents {
		if !filter.IncludeResolved && inc.Lifecycle == LifecycleResolved {
			continue
		}
		if filter.Lifecycle != nil && inc.Lifecycle != *filter.Lifecycle {
			continue
		}
		if filter.Severity != nil && inc.Severity != *filter.Severity {
			continue
		}
		if !filter.IsOwner {
			if inc.ProjectID == uuid.Nil || !f.grants[filter.UserID][inc.ProjectID] {
				continue
			}
		}
		rows = append(rows, *inc)
	}
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].UpdatedAt.Equal(rows[j].UpdatedAt) {
			return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
		}
		return rows[i].ID.String() > rows[j].ID.String()
	})
	if filter.Cursor != nil {
		trimmed := rows[:0]
		for _, row := range rows {
			if row.UpdatedAt.Before(filter.Cursor.At) || (row.UpdatedAt.Equal(filter.Cursor.At) && row.ID.String() < filter.Cursor.ID.String()) {
				trimmed = append(trimmed, row)
			}
		}
		rows = trimmed
	}
	if filter.Limit > 0 && len(rows) > filter.Limit {
		rows = rows[:filter.Limit]
	}
	return rows, nil
}

func (f *fakeRepo) ListEvents(_ context.Context, filter EventFilter) ([]Event, error) {
	out := make([]Event, 0, len(f.events))
	for _, event := range f.events {
		if event.IncidentID == filter.IncidentID {
			out = append(out, event)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (f *fakeRepo) ListActiveNodeIncidents(context.Context) ([]Incident, error) {
	out := make([]Incident, 0)
	for _, inc := range f.incidents {
		if inc.NodeID != uuid.Nil && inc.Active() {
			out = append(out, *inc)
		}
	}
	return out, nil
}

func (f *fakeRepo) ListDeliveries(_ context.Context, incidentID uuid.UUID, limit int) ([]DeliveryIntent, error) {
	out := make([]DeliveryIntent, 0)
	for _, intent := range f.intents {
		if intent.IncidentID == incidentID {
			out = append(out, *intent)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeRepo) ClaimPendingDeliveries(_ context.Context, limit, maxAttempts int) ([]DeliveryIntent, error) {
	out := make([]DeliveryIntent, 0)
	ids := make([]uuid.UUID, 0, len(f.intents))
	for id := range f.intents {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := f.intents[ids[i]], f.intents[ids[j]]
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.ID.String() < right.ID.String()
	})
	for _, id := range ids {
		intent := f.intents[id]
		if (intent.State == DeliveryPending || intent.State == DeliveryFailed) && intent.Attempts < maxAttempts {
			intent.State = DeliverySending
			out = append(out, *intent)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeRepo) ReclaimStaleDeliveries(context.Context) (int64, error) {
	return 0, nil
}

func (f *fakeRepo) MarkDelivery(_ context.Context, intentID, incidentID uuid.UUID, state, hold, transition string, severity Severity, attempts int, errorText string, at time.Time) error {
	var touched *Incident
	if intent, ok := f.intents[intentID]; ok {
		intent.State = state
		intent.LastError = errorText
		intent.Attempts += attempts
	} else {
		return ErrNotFound
	}
	if inc, ok := f.incidents[incidentID]; ok {
		inc.DeliveryState = state
		inc.DeliveryHold = hold
		inc.DeliveryEvent = transition
		inc.DeliveryAttempts += attempts
		inc.DeliveryError = errorText
		if state == DeliveryDispatched {
			inc.LastNotifiedSeverity = severity
		}
		inc.UpdatedAt = at
		touched = inc
	}
	_ = touched
	return nil
}

func (f *fakeRepo) ClearExpiredSnooze(_ context.Context, id uuid.UUID, notifiedUntil, at time.Time) (Incident, error) {
	inc, ok := f.incidents[id]
	if !ok {
		return Incident{}, ErrNotFound
	}
	inc.SnoozedUntil = nil
	inc.SnoozeNotifiedUntil = timePtr(notifiedUntil)
	inc.UpdatedAt = at
	return *inc, nil
}

func (f *fakeRepo) ListExpiredSnoozes(_ context.Context, now time.Time, limit int) ([]Incident, error) {
	out := make([]Incident, 0)
	for _, inc := range f.incidents {
		if inc.Lifecycle == LifecycleFiring && inc.SnoozedUntil != nil && !inc.SnoozedUntil.After(now) {
			out = append(out, *inc)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeRepo) PruneResolved(_ context.Context, before time.Time) (int64, error) {
	var count int64
	for id, inc := range f.incidents {
		if inc.Lifecycle == LifecycleResolved && inc.ResolvedAt != nil && inc.ResolvedAt.Before(before) {
			delete(f.incidents, id)
			f.pruned = append(f.pruned, id)
			count++
		}
	}
	return count, nil
}

type fakeTx struct{ repo *fakeRepo }

func (t *fakeTx) ActiveByIdentity(_ context.Context, identity string) (Incident, error) {
	for _, inc := range t.repo.incidents {
		if inc.Identity == identity && inc.Active() {
			return *inc, nil
		}
	}
	return Incident{}, ErrNotFound
}

func (t *fakeTx) GetForUpdate(_ context.Context, id uuid.UUID) (Incident, error) {
	return t.repo.Get(context.Background(), id)
}

func (t *fakeTx) Create(_ context.Context, inc Incident) (Incident, error) {
	for _, existing := range t.repo.incidents {
		if existing.Identity == inc.Identity && existing.Active() {
			return Incident{}, ErrConflict
		}
	}
	copied := inc
	t.repo.incidents[inc.ID] = &copied
	return inc, nil
}

func (t *fakeTx) Update(_ context.Context, inc Incident) (Incident, error) {
	if _, ok := t.repo.incidents[inc.ID]; !ok {
		return Incident{}, ErrNotFound
	}
	copied := inc
	t.repo.incidents[inc.ID] = &copied
	return inc, nil
}

func (t *fakeTx) Acknowledge(_ context.Context, id, userID uuid.UUID, at time.Time) (Incident, error) {
	inc, ok := t.repo.incidents[id]
	if !ok || !inc.Active() {
		return Incident{}, ErrNotFound
	}
	inc.AcknowledgedAt = timePtr(at)
	inc.AcknowledgedBy = userID
	inc.UpdatedAt = at
	return *inc, nil
}

func (t *fakeTx) Snooze(_ context.Context, id uuid.UUID, until, at time.Time) (Incident, error) {
	inc, ok := t.repo.incidents[id]
	if !ok || !inc.Active() {
		return Incident{}, ErrNotFound
	}
	inc.SnoozedUntil = timePtr(until)
	inc.SnoozeNotifiedUntil = nil
	inc.UpdatedAt = at
	return *inc, nil
}

func (t *fakeTx) ClearExpiredSnooze(ctx context.Context, id uuid.UUID, notifiedUntil, at time.Time) (Incident, error) {
	return t.repo.ClearExpiredSnooze(ctx, id, notifiedUntil, at)
}

func (t *fakeTx) CreateEvent(_ context.Context, event Event) error {
	t.repo.events = append(t.repo.events, event)
	return nil
}

func (t *fakeTx) CreateDelivery(_ context.Context, intent DeliveryIntent) error {
	copied := intent
	intent.State = DeliveryPending
	copied.State = DeliveryPending
	t.repo.intents[intent.ID] = &copied
	return nil
}

func (t *fakeTx) PruneEvents(context.Context, uuid.UUID, int) error { return nil }

type fakeAccess struct {
	owner  bool
	grants map[uuid.UUID]bool
}

func (a *fakeAccess) Role(context.Context, uuid.UUID) (string, error) {
	if a.owner {
		return "owner", nil
	}
	return "member", nil
}

func (a *fakeAccess) HasProjectGrant(_ context.Context, _, projectID uuid.UUID) (bool, error) {
	return a.grants[projectID], nil
}

type fakeNotifier struct {
	deliveries []Delivery
	fail       error
}

func (n *fakeNotifier) NotifyIncident(_ context.Context, delivery Delivery) error {
	n.deliveries = append(n.deliveries, delivery)
	return n.fail
}

func testService(t *testing.T, repo *fakeRepo, access Access, notifier Notifier) (*Service, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := NewService(Config{
		Repository: repo,
		Access:     access,
		Notifier:   notifier,
		Now:        func() time.Time { return now },
		Retention:  DefaultRetention,
	})
	return svc, &now
}

func reportOpened(t *testing.T, svc *Service, now *time.Time, projectID uuid.UUID) Incident {
	t.Helper()
	identity := "app:1"
	if projectID != uuid.Nil {
		identity = "app:" + projectID.String()
	}
	obs := Observation{
		Identity: identity, Detector: "app.health", ProjectID: projectID,
		ResourceType: "application", ResourceID: uuid.New(), ResourceName: "shop",
		At: *now, Valid: true, Unhealthy: true, Severity: SeverityWarning,
	}
	inc, tr, err := svc.Report(context.Background(), obs)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if inc == nil || tr.Event != TransitionOpened {
		t.Fatalf("Report = %+v %q; want opened", inc, tr.Event)
	}
	return *inc
}

func TestServiceOpeningAndRecoveryDispatchOnce(t *testing.T) {
	repo := newFakeRepo()
	notifier := &fakeNotifier{}
	svc, now := testService(t, repo, &fakeAccess{owner: true}, notifier)
	inc := reportOpened(t, svc, now, uuid.Nil)

	if len(notifier.deliveries) != 1 || notifier.deliveries[0].Transition != TransitionOpened {
		t.Fatalf("deliveries = %+v; want one opening", notifier.deliveries)
	}

	// A duplicate report must not open again or add another intent.
	obs := Observation{
		Identity: "app:1", Detector: "app.health", At: *now, Valid: true, Unhealthy: true,
		Severity: SeverityWarning,
	}
	if _, tr, err := svc.Report(context.Background(), obs); err != nil || tr.Event != "" {
		t.Fatalf("duplicate = %q %v", tr.Event, err)
	}
	if len(notifier.deliveries) != 1 {
		t.Fatalf("duplicate delivered again: %d", len(notifier.deliveries))
	}

	*now = now.Add(10 * time.Second)
	healthy := Observation{
		Identity: "app:1", Detector: "app.health", At: *now, Valid: true, Unhealthy: false,
	}
	recovered, tr, err := svc.Report(context.Background(), healthy)
	if err != nil || tr.Event != TransitionRecovered {
		t.Fatalf("recovery = %q %v", tr.Event, err)
	}
	if recovered.Lifecycle != LifecycleResolved || recovered.CloseReason != "recovered" {
		t.Fatalf("recovered = %s %q", recovered.Lifecycle, recovered.CloseReason)
	}
	if len(notifier.deliveries) != 2 || notifier.deliveries[1].Transition != TransitionRecovered {
		t.Fatalf("deliveries = %+v; want one opening and one recovery", notifier.deliveries)
	}
	if inc.ID != recovered.ID {
		t.Fatalf("recovery created a new incident")
	}
}

func TestServiceFailedHandoffStaysRetryable(t *testing.T) {
	repo := newFakeRepo()
	notifier := &fakeNotifier{fail: errors.New("queue is full")}
	svc, now := testService(t, repo, &fakeAccess{owner: true}, notifier)
	inc := reportOpened(t, svc, now, uuid.Nil)

	intents, _ := repo.ListDeliveries(context.Background(), inc.ID, 10)
	if len(intents) != 1 || intents[0].State != DeliveryFailed {
		t.Fatalf("intents = %+v; want a visible failed handoff", intents)
	}
	stored, _ := repo.Get(context.Background(), inc.ID)
	if stored.DeliveryState != DeliveryFailed || stored.DeliveryError == "" {
		t.Fatalf("incident delivery = %s %q", stored.DeliveryState, stored.DeliveryError)
	}

	notifier.fail = nil
	if err := svc.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	stored, _ = repo.Get(context.Background(), inc.ID)
	if stored.DeliveryState != DeliveryDispatched {
		t.Fatalf("retry left delivery = %s", stored.DeliveryState)
	}
}

func TestServiceAckAndSnoozeSuppressWithoutChangingHealth(t *testing.T) {
	repo := newFakeRepo()
	notifier := &fakeNotifier{}
	access := &fakeAccess{owner: true}
	svc, now := testService(t, repo, access, notifier)
	inc := reportOpened(t, svc, now, uuid.Nil)
	user := uuid.New()

	if _, err := svc.Acknowledge(context.Background(), user, inc.ID); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	*now = now.Add(10 * time.Second)
	escalate := Observation{
		Identity: "app:1", Detector: "app.health", At: *now, Valid: true, Unhealthy: true,
		Severity: SeverityCritical,
	}
	updated, tr, err := svc.Report(context.Background(), escalate)
	if err != nil || tr.Event != TransitionSeverityChanged {
		t.Fatalf("severity = %q %v", tr.Event, err)
	}
	if updated.Severity != SeverityCritical || updated.Lifecycle != LifecycleFiring {
		t.Fatalf("ack falsified health: %+v", updated)
	}
	if len(notifier.deliveries) != 1 {
		t.Fatalf("acknowledged incident delivered again: %d", len(notifier.deliveries))
	}
	stored, _ := repo.Get(context.Background(), inc.ID)
	if stored.DeliveryHold != "acknowledged" {
		t.Fatalf("hold = %q; want acknowledged", stored.DeliveryHold)
	}

	*now = now.Add(10 * time.Second)
	until := now.Add(time.Hour)
	if _, err := svc.Snooze(context.Background(), user, inc.ID, until); err != nil {
		t.Fatalf("Snooze: %v", err)
	}
	stored, _ = repo.Get(context.Background(), inc.ID)
	if stored.SnoozedUntil == nil || !stored.Active() {
		t.Fatalf("snoozed incident is not visibly active: %+v", stored)
	}
}

func TestServiceAuthorizationOwnerMemberAndRevoked(t *testing.T) {
	repo := newFakeRepo()
	ownerID, memberID := uuid.New(), uuid.New()
	granted, other := uuid.New(), uuid.New()
	repo.grants[memberID] = map[uuid.UUID]bool{granted: true}

	ownerSvc, now := testService(t, repo, &fakeAccess{owner: true}, nil)
	projectIncident := reportOpened(t, ownerSvc, now, granted)
	nodeObs := Observation{
		Identity: "node:1", Detector: "connectivity", NodeID: uuid.New(), ScopeKind: ScopeNode,
		At: *now, Valid: true, Unhealthy: true, Severity: SeverityCritical, ConfirmFor: 0,
		SuppressDelivery: true,
	}
	nodeIncident, _, err := ownerSvc.Report(context.Background(), nodeObs)
	if err != nil {
		t.Fatalf("node Report: %v", err)
	}

	memberSvc, _ := testService(t, repo, &fakeAccess{grants: repo.grants[memberID]}, nil)
	page, err := memberSvc.List(context.Background(), memberID, ListParams{})
	if err != nil {
		t.Fatalf("member List: %v", err)
	}
	if len(page.Incidents) != 1 || page.Incidents[0].ID != projectIncident.ID {
		t.Fatalf("member saw %+v; want only the granted project incident", page.Incidents)
	}
	if _, err := memberSvc.Get(context.Background(), memberID, nodeIncident.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("member read node incident: %v", err)
	}
	if _, err := memberSvc.Acknowledge(context.Background(), memberID, projectIncident.ID); err != nil {
		t.Fatalf("member ack granted project: %v", err)
	}

	otherIncident := reportOpened(t, ownerSvc, now, other)
	if _, err := memberSvc.Get(context.Background(), memberID, otherIncident.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("member read unrelated project: %v", err)
	}
	if _, err := memberSvc.Acknowledge(context.Background(), memberID, otherIncident.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("member mutated unrelated project: %v", err)
	}

	delete(repo.grants[memberID], granted)
	if _, err := memberSvc.Get(context.Background(), memberID, projectIncident.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked grant still readable: %v", err)
	}
	ownerPage, err := ownerSvc.List(context.Background(), ownerID, ListParams{})
	if err != nil || len(ownerPage.Incidents) != 3 {
		t.Fatalf("owner list = %d %v; want every incident", len(ownerPage.Incidents), err)
	}
}

func TestServiceRetainsResolvedHistoryAndNeverPrunesActive(t *testing.T) {
	repo := newFakeRepo()
	svc, now := testService(t, repo, &fakeAccess{owner: true}, nil)
	resolved := reportOpened(t, svc, now, uuid.Nil)
	*now = now.Add(10 * time.Second)
	healthy := Observation{Identity: "app:1", Detector: "app.health", At: *now, Valid: true}
	if _, tr, err := svc.Report(context.Background(), healthy); err != nil || tr.Event != TransitionRecovered {
		t.Fatalf("recovery: %q %v", tr.Event, err)
	}

	activeObs := Observation{
		Identity: "app:2", Detector: "app.health", ProjectID: uuid.New(), At: *now,
		Valid: true, Unhealthy: true, ConfirmFor: 0, Severity: SeverityWarning,
	}
	active, _, err := svc.Report(context.Background(), activeObs)
	if err != nil {
		t.Fatalf("active Report: %v", err)
	}

	*now = now.Add(DefaultRetention + time.Hour)
	if err := svc.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := repo.Get(context.Background(), resolved.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolved incident older than retention survived: %v", err)
	}
	if _, err := repo.Get(context.Background(), active.ID); err != nil {
		t.Fatalf("active incident was pruned: %v", err)
	}
}

func TestCloseResolvesWithoutRecoveryNotification(t *testing.T) {
	repo := newFakeRepo()
	notifier := &fakeNotifier{}
	svc, now := testService(t, repo, &fakeAccess{owner: true}, notifier)
	inc := reportOpened(t, svc, now, uuid.Nil)

	if err := svc.Close(context.Background(), inc.Identity, "detector disabled"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	stored, err := repo.Get(context.Background(), inc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Lifecycle != LifecycleResolved || stored.CloseReason != "detector disabled" {
		t.Fatalf("closed = %s %q", stored.Lifecycle, stored.CloseReason)
	}
	if len(notifier.deliveries) != 1 {
		t.Fatalf("closing emitted a false recovery: %+v", notifier.deliveries)
	}
}
