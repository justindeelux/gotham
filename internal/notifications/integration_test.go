package notifications

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

func newIntegrationService(t *testing.T, st *store.Store, channels ...Channel) (*Service, map[uuid.UUID]*fakeNotifier) {
	t.Helper()
	service := NewService(Config{
		Store:       st,
		Secret:      testSecret,
		Logger:      discardLogger(),
		Mailer:      &fakeMailer{},
		Workers:     2,
		SendTimeout: 2 * time.Second,
	})
	t.Cleanup(func() { _ = service.Close() })
	notifiers := make(map[uuid.UUID]*fakeNotifier, len(channels))
	for _, channel := range channels {
		notifiers[channel.ID] = newFakeNotifier()
	}
	service.factory = func(channel Channel) (Notifier, error) {
		notifier, ok := notifiers[channel.ID]
		if !ok {
			return nil, ErrNotFound
		}
		return notifier, nil
	}
	return service, notifiers
}

func countRows(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var count int
	if err := st.DB.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	return count
}

func TestCertificateEventsWithSameTimestampKeepDistinctIdentities(t *testing.T) {
	st := scratchNotificationStore(t)
	channel := integrationChannel(t, st)
	service, notifiers := newIntegrationService(t, st, channel)

	at := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	first := Event{
		Kind: EventCertificate, Outcome: OutcomeFailure, Name: "a.example.com", Host: "a.example.com",
		Error: "Certificate expires at 2026-11-01T00:00:00Z", At: at,
	}
	second := Event{
		Kind: EventCertificate, Outcome: OutcomeFailure, Name: "b.example.com", Host: "b.example.com",
		Error: "Certificate expires at 2026-11-01T00:00:00Z", At: at,
	}
	for _, event := range []Event{first, second, first} {
		if err := service.Enqueue(event); err != nil {
			t.Fatalf("enqueue %s: %v", event.Name, err)
		}
	}

	if got := countRows(t, st, `SELECT count(*) FROM notifications WHERE event_key = 'certificate_expiring'`); got != 2 {
		t.Fatalf("inbox rows = %d, want two distinct certificate alerts despite the duplicate producer", got)
	}
	if got := countRows(t, st, `SELECT count(*) FROM notification_deliveries`); got != 2 {
		t.Fatalf("deliveries = %d, want two distinct certificate deliveries", got)
	}
	waitFor(t, "both certificate deliveries", func() bool {
		return len(notifiers[channel.ID].delivered()) == 2
	})
}

func TestEnqueueWritesInboxAndDeliveriesInOneTransaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st := scratchNotificationStore(t)
	repo := newStoreRepository(st)
	now := time.Now().UTC()
	key := "tx:" + uuid.NewString()
	notification := Notification{
		ID: uuid.New(), EventKey: EventDeployFailure, Name: "shop-web",
		CreatedAt: now, DedupeKey: key,
	}
	bad := Delivery{
		ID: uuid.New(), ChannelID: uuid.New(), EventType: string(EventDeployFailure),
		Status: DeliveryPending, CreatedAt: now, DedupeKey: key, Payload: "{}",
		NextAttemptAt: now, ExpiresAt: now.Add(maxDeliveryAge),
	}
	if _, err := repo.PersistEvent(ctx, notification, []Delivery{bad}); err == nil {
		t.Fatal("PersistEvent with a missing channel = nil error, want the FK failure")
	}
	if got := countRows(t, st, `SELECT count(*) FROM notifications WHERE dedupe_key = $1`, key); got != 0 {
		t.Fatalf("inbox rows after the failed transaction = %d, want none", got)
	}

	channel := integrationChannel(t, st)
	good := bad
	good.ChannelID = channel.ID
	inserted, err := repo.PersistEvent(ctx, notification, []Delivery{good})
	if err != nil || !inserted {
		t.Fatalf("retried PersistEvent = %v, %v; want both rows", inserted, err)
	}
	if got := countRows(t, st, `SELECT count(*) FROM notifications WHERE dedupe_key = $1`, key); got != 1 {
		t.Fatalf("inbox rows after retry = %d, want one", got)
	}
	if got := countRows(t, st, `SELECT count(*) FROM notification_deliveries WHERE dedupe_key = $1`, key); got != 1 {
		t.Fatalf("deliveries after retry = %d, want one", got)
	}
}

func TestOwnerRetrySendsDeliveryOlderThan24Hours(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st := scratchNotificationStore(t)
	channel := integrationChannel(t, st)
	owner, err := st.CreateFirstUser(ctx, "retry-owner@example.com", nil)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	service, notifiers := newIntegrationService(t, st, channel)

	now := time.Now().UTC()
	event := Event{
		Kind: EventDeploy, Outcome: OutcomeFailure, ResourceType: ResourceApplication,
		ResourceID: uuid.New(), Name: "shop-web", Error: "build failed", At: now.Add(-26 * time.Hour),
	}
	deliveryID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries
		(id, channel_id, event_type, status, error_summary, created_at, dedupe_key, payload, next_attempt_at, expires_at, completed_at)
		VALUES ($1,$2,$3,'terminal_failed','answered status 401',$4,$5,$6,$4,$7,$4)`,
		deliveryID, channel.ID, string(event.Key()), now.Add(-26*time.Hour), event.Identity(), event.encode(), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("seed old terminal delivery: %v", err)
	}
	if err := service.RetryDelivery(ctx, uuidFromPG(owner.ID), deliveryID); err != nil {
		t.Fatalf("RetryDelivery: %v", err)
	}
	waitFor(t, "the retried delivery", func() bool {
		return len(notifiers[channel.ID].delivered()) == 1
	})
	row, err := service.repo.GetDelivery(ctx, deliveryID)
	if err != nil {
		t.Fatalf("get retried delivery: %v", err)
	}
	if row.Status != DeliverySucceeded {
		t.Fatalf("retried delivery status = %q, want delivered", row.Status)
	}
	if !row.ExpiresAt.After(now.Add(23 * time.Hour)) {
		t.Fatalf("expires_at = %s, want a fresh 24-hour window from the retry", row.ExpiresAt)
	}
}

func TestRetryOfOpeningAfterRecoveryIsCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st := scratchNotificationStore(t)
	channel := integrationChannel(t, st)
	owner, err := st.CreateFirstUser(ctx, "opening-owner@example.com", nil)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	service, notifiers := newIntegrationService(t, st, channel)

	now := time.Now().UTC()
	incidentID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO incidents (id, identity_key, detector_key, scope_kind, severity, lifecycle, first_observed_at, last_observed_at, created_at, updated_at)
		VALUES ($1,$2,'detector','global','critical','resolved',$3,$3,$3,$3)`, incidentID, "identity-"+incidentID.String(), now); err != nil {
		t.Fatalf("seed incident: %v", err)
	}
	openingEvent := Event{
		Kind: EventIncident, Action: "opened", IncidentID: incidentID, Severity: "critical",
		Name: "shop-web", At: now.Add(-time.Hour),
	}
	recoveryEvent := Event{
		Kind: EventIncident, Action: "recovered", IncidentID: incidentID,
		Name: "shop-web", At: now.Add(-30 * time.Minute),
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries
		(id, channel_id, event_type, status, created_at, dedupe_key, payload, incident_id, next_attempt_at, expires_at, completed_at)
		VALUES ($1,$2,'incident_recovered','delivered',$3,$4,$5,$6,$3,$7,$3)`,
		uuid.New(), channel.ID, now.Add(-30*time.Minute), recoveryEvent.Identity(), recoveryEvent.encode(), incidentID, now.Add(-6*time.Hour)); err != nil {
		t.Fatalf("seed delivered recovery: %v", err)
	}
	openingID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries
		(id, channel_id, event_type, status, error_summary, created_at, dedupe_key, payload, incident_id, next_attempt_at, expires_at, completed_at)
		VALUES ($1,$2,'incident_opened','terminal_failed','answered status 401',$3,$4,$5,$6,$3,$7,$3)`,
		openingID, channel.ID, now.Add(-time.Hour), openingEvent.Identity(), openingEvent.encode(), incidentID, now.Add(-5*time.Hour)); err != nil {
		t.Fatalf("seed terminal opening: %v", err)
	}
	if err := service.RetryDelivery(ctx, uuidFromPG(owner.ID), openingID); err != nil {
		t.Fatalf("RetryDelivery: %v", err)
	}
	waitFor(t, "the stale opening cancellation", func() bool {
		row, err := service.repo.GetDelivery(ctx, openingID)
		return err == nil && row.Status == DeliveryCancelled
	})
	row, err := service.repo.GetDelivery(ctx, openingID)
	if err != nil || !strings.Contains(row.ErrorSummary, "progressed") {
		t.Fatalf("cancelled opening = %+v, %v; want the incident-progressed reason", row, err)
	}
	if len(notifiers[channel.ID].delivered()) != 0 {
		t.Fatal("a stale opening was sent after its recovery had completed")
	}
}

func TestFinishDeliveryClearsPayloadOnRealTable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st := scratchNotificationStore(t)
	channel := integrationChannel(t, st)
	repo := newStoreRepository(st)
	now := time.Now().UTC()
	delivery := Delivery{
		ID: uuid.New(), ChannelID: channel.ID, EventType: string(EventDeployFailure),
		Status: DeliveryPending, CreatedAt: now, DedupeKey: "payload-" + uuid.NewString(),
		Payload: `{"Kind":"deploy"}`, NextAttemptAt: now, ExpiresAt: now.Add(maxDeliveryAge),
	}
	if err := repo.PersistDeliveries(ctx, []Delivery{delivery}); err != nil {
		t.Fatalf("persist: %v", err)
	}
	delivery.Status = DeliverySucceeded
	completed := now
	delivery.CompletedAt = &completed
	delivery.UpdatedAt = now
	if err := repo.FinishDelivery(ctx, delivery); err != nil {
		t.Fatalf("finish: %v", err)
	}
	row, err := repo.GetDelivery(ctx, delivery.ID)
	if err != nil || row.Payload != "" {
		t.Fatalf("payload = %q, %v; want it cleared on completion", row.Payload, err)
	}
}
