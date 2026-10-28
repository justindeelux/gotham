package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testSecret = "notifications-test-secret"

func TestMain(m *testing.M) {
	_ = os.Unsetenv(FeatureEnv)
	os.Exit(m.Run())
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fakeRepository struct {
	mu       sync.Mutex
	channels map[uuid.UUID]Channel
	order    []uuid.UUID

	deliveries map[uuid.UUID][]Delivery

	createErr       error
	getErr          error
	listErr         error
	listForEventErr error
	updateErr       error
	deleteErr       error

	createDeliveryErr error
	listDeliveriesErr error

	notifications          []Notification
	resourceProjects       map[uuid.UUID]uuid.UUID
	seenAt                 map[uuid.UUID]time.Time
	createNotificationErr  error
	listNotificationsErr   error
	notificationRecipients map[uuid.UUID][]uuid.UUID
	recipientsErr          error

	listForEventCalls int
	lastResourceType  string
	lastResourceID    uuid.UUID

	nonOwners map[uuid.UUID]bool
}

var _ Repository = (*fakeRepository)(nil)

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		channels:               make(map[uuid.UUID]Channel),
		deliveries:             make(map[uuid.UUID][]Delivery),
		resourceProjects:       make(map[uuid.UUID]uuid.UUID),
		seenAt:                 make(map[uuid.UUID]time.Time),
		notificationRecipients: make(map[uuid.UUID][]uuid.UUID),
		nonOwners:              make(map[uuid.UUID]bool),
	}
}

func (r *fakeRepository) seed(channel Channel) Channel {
	r.mu.Lock()
	defer r.mu.Unlock()
	if channel.ID == uuid.Nil {
		channel.ID = uuid.New()
	}
	if channel.CreatedAt.IsZero() {
		channel.CreatedAt = time.Now().UTC()
	}
	if channel.UpdatedAt.IsZero() {
		channel.UpdatedAt = channel.CreatedAt
	}
	r.channels[channel.ID] = channel
	r.order = append(r.order, channel.ID)
	return channel
}

func (r *fakeRepository) get(id uuid.UUID) (Channel, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	channel, ok := r.channels[id]
	return channel, ok
}

func (r *fakeRepository) CreateChannel(_ context.Context, channel Channel) (Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return Channel{}, r.createErr
	}
	r.channels[channel.ID] = channel
	r.order = append(r.order, channel.ID)
	return channel, nil
}

func (r *fakeRepository) GetChannel(_ context.Context, channelID uuid.UUID) (Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return Channel{}, r.getErr
	}
	channel, ok := r.channels[channelID]
	if !ok {
		return Channel{}, ErrNotFound
	}
	return channel, nil
}

func (r *fakeRepository) ListChannels(_ context.Context) ([]Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.allChannels(), nil
}

func (r *fakeRepository) ListChannelsForEvent(_ context.Context, resourceType string, resourceID uuid.UUID) ([]Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listForEventCalls++
	r.lastResourceType, r.lastResourceID = resourceType, resourceID
	if r.listForEventErr != nil {
		return nil, r.listForEventErr
	}
	var matched []Channel
	for _, channel := range r.allChannels() {
		if !channel.Enabled {
			continue
		}
		if channel.ResourceType == "" || (channel.ResourceType == resourceType && channel.ResourceID == resourceID) {
			matched = append(matched, channel)
		}
	}
	return matched, nil
}

func (r *fakeRepository) ResourceProject(_ context.Context, _ string, resourceID uuid.UUID) (uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if projectID, ok := r.resourceProjects[resourceID]; ok {
		return projectID, nil
	}
	return uuid.Nil, errors.New("resource has no project")
}

func (r *fakeRepository) CreateNotification(_ context.Context, notification Notification) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createNotificationErr != nil {
		return false, r.createNotificationErr
	}
	if notification.DedupeKey != "" {
		for _, existing := range r.notifications {
			if existing.DedupeKey == notification.DedupeKey {
				return false, nil
			}
		}
	}
	if notification.ID == uuid.Nil {
		notification.ID = uuid.New()
	}
	r.notifications = append(r.notifications, notification)
	return true, nil
}

func (r *fakeRepository) IsOwner(_ context.Context, userID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.nonOwners[userID], nil
}

func (r *fakeRepository) PersistDeliveries(_ context.Context, deliveries []Delivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createDeliveryErr != nil {
		return r.createDeliveryErr
	}
	for _, delivery := range deliveries {
		if delivery.ID == uuid.Nil {
			delivery.ID = uuid.New()
		}
		if delivery.Status == "" {
			delivery.Status = DeliveryPending
		}
		duplicate := false
		for _, existing := range r.deliveries[delivery.ChannelID] {
			if delivery.DedupeKey != "" && existing.DedupeKey == delivery.DedupeKey {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		history := append(r.deliveries[delivery.ChannelID], delivery)
		sortDeliveries(history)
		r.deliveries[delivery.ChannelID] = history
	}
	return nil
}

func (r *fakeRepository) ClaimDeliveries(_ context.Context, now, leaseUntil time.Time, limit int) ([]Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var claimed []Delivery
	for _, channelID := range r.order {
		history := r.deliveries[channelID]
		for i := range history {
			delivery := &history[i]
			if delivery.Status != DeliveryPending && delivery.Status != DeliveryRetrying {
				continue
			}
			if delivery.NextAttemptAt.After(now) {
				continue
			}
			if delivery.IncidentID != uuid.Nil && r.hasEarlierOpen(history, i) {
				continue
			}
			if len(claimed) >= limit {
				r.deliveries[channelID] = history
				return claimed, nil
			}
			delivery.Status = DeliverySending
			delivery.Attempts++
			lease := leaseUntil
			delivery.LeaseUntil = &lease
			delivery.UpdatedAt = now
			claimed = append(claimed, *delivery)
		}
		r.deliveries[channelID] = history
	}
	return claimed, nil
}

func (r *fakeRepository) hasEarlierOpen(history []Delivery, index int) bool {
	delivery := history[index]
	for _, earlier := range history {
		if earlier.ID == delivery.ID || earlier.IncidentID != delivery.IncidentID || earlier.ChannelID != delivery.ChannelID {
			continue
		}
		if earlier.CreatedAt.After(delivery.CreatedAt) ||
			(earlier.CreatedAt.Equal(delivery.CreatedAt) && earlier.ID.String() > delivery.ID.String()) {
			continue
		}
		if earlier.Status == DeliveryPending || earlier.Status == DeliveryRetrying || earlier.Status == DeliverySending {
			return true
		}
	}
	return false
}

func (r *fakeRepository) ReclaimDeliveries(_ context.Context, now time.Time, maxAttempts int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for _, channelID := range r.order {
		history := r.deliveries[channelID]
		for i := range history {
			delivery := &history[i]
			if delivery.Status != DeliverySending || delivery.LeaseUntil == nil || !delivery.LeaseUntil.Before(now) {
				continue
			}
			delivery.LeaseUntil = nil
			delivery.NextAttemptAt = now
			delivery.UpdatedAt = now
			if delivery.Attempts >= maxAttempts {
				delivery.Status = DeliveryFailed
				delivery.CompletedAt = &now
				delivery.ErrorSummary = "delivery lease expired"
			} else {
				delivery.Status = DeliveryRetrying
			}
			count++
		}
		r.deliveries[channelID] = history
	}
	return count, nil
}

func (r *fakeRepository) ExpireDeliveries(_ context.Context, now, cutoff time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for _, channelID := range r.order {
		history := r.deliveries[channelID]
		for i := range history {
			delivery := &history[i]
			if (delivery.Status != DeliveryPending && delivery.Status != DeliveryRetrying) || !delivery.CreatedAt.Before(cutoff) {
				continue
			}
			delivery.Status = DeliveryFailed
			delivery.ErrorSummary = "delivery expired after 24 hours"
			delivery.CompletedAt = &now
			delivery.UpdatedAt = now
			count++
		}
		r.deliveries[channelID] = history
	}
	return count, nil
}

func (r *fakeRepository) CleanupDeliveries(_ context.Context, cutoff time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for _, channelID := range r.order {
		kept := make([]Delivery, 0, len(r.deliveries[channelID]))
		for _, delivery := range r.deliveries[channelID] {
			terminal := delivery.Status == DeliveryFailed || delivery.Status == DeliveryCancelled
			if terminal && delivery.CompletedAt != nil && delivery.CompletedAt.Before(cutoff) {
				count++
				continue
			}
			kept = append(kept, delivery)
		}
		r.deliveries[channelID] = kept
	}
	return count, nil
}

func (r *fakeRepository) FinishDelivery(_ context.Context, delivery Delivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	history := r.deliveries[delivery.ChannelID]
	for i := range history {
		if history[i].ID == delivery.ID {
			history[i] = delivery
			return nil
		}
	}
	return ErrNotFound
}

func (r *fakeRepository) CancelDelivery(_ context.Context, deliveryID uuid.UUID, reason string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, channelID := range r.order {
		history := r.deliveries[channelID]
		for i := range history {
			if history[i].ID != deliveryID {
				continue
			}
			history[i].Status = DeliveryCancelled
			history[i].ErrorSummary = reason
			history[i].CompletedAt = &at
			history[i].UpdatedAt = at
			history[i].LeaseUntil = nil
			return nil
		}
	}
	return ErrNotFound
}

func (r *fakeRepository) GetDelivery(_ context.Context, deliveryID uuid.UUID) (Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, channelID := range r.order {
		for _, delivery := range r.deliveries[channelID] {
			if delivery.ID == deliveryID {
				return delivery, nil
			}
		}
	}
	return Delivery{}, ErrNotFound
}

func (r *fakeRepository) RetryDelivery(_ context.Context, deliveryID uuid.UUID, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, channelID := range r.order {
		history := r.deliveries[channelID]
		for i := range history {
			if history[i].ID != deliveryID || history[i].Status != DeliveryFailed {
				continue
			}
			history[i].Status = DeliveryPending
			history[i].Attempts = 0
			history[i].NextAttemptAt = at
			history[i].CompletedAt = nil
			history[i].LeaseUntil = nil
			history[i].UpdatedAt = at
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepository) HasDeliveredOpening(_ context.Context, incidentID, channelID uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, delivery := range r.deliveries[channelID] {
		if delivery.IncidentID == incidentID && delivery.Status == DeliverySucceeded {
			switch delivery.EventType {
			case string(EventIncidentOpened), string(EventIncidentSeverity), string(EventRuntimeFailure):
				return true, nil
			}
		}
	}
	return false, nil
}

func (r *fakeRepository) ListTerminalFailures(_ context.Context, limit int) ([]DeliveryFailure, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var failures []DeliveryFailure
	for _, channelID := range r.order {
		for _, delivery := range r.deliveries[channelID] {
			if delivery.Status != DeliveryFailed {
				continue
			}
			channel := r.channels[channelID]
			failedAt := time.Time{}
			if delivery.CompletedAt != nil {
				failedAt = *delivery.CompletedAt
			}
			failures = append(failures, DeliveryFailure{
				ID: delivery.ID, ChannelID: channelID, ChannelName: channel.Name,
				ChannelKind: channel.Kind, EventType: delivery.EventType,
				ErrorSummary: delivery.ErrorSummary, Attempts: delivery.Attempts, FailedAt: failedAt,
			})
		}
	}
	sort.Slice(failures, func(i, j int) bool { return failures[i].FailedAt.After(failures[j].FailedAt) })
	if limit > 0 && len(failures) > limit {
		failures = failures[:limit]
	}
	return failures, nil
}

func (r *fakeRepository) ChannelHealth(_ context.Context) (map[uuid.UUID]ChannelHealth, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	health := make(map[uuid.UUID]ChannelHealth)
	for _, channelID := range r.order {
		item := ChannelHealth{Status: "ok"}
		for _, delivery := range r.deliveries[channelID] {
			switch delivery.Status {
			case DeliverySucceeded:
				if delivery.CompletedAt != nil {
					item.LastDeliveredAt = delivery.CompletedAt
				}
			case DeliveryFailed:
				if delivery.CompletedAt != nil {
					item.LastFailedAt = delivery.CompletedAt
					item.LastError = delivery.ErrorSummary
				}
			case DeliveryPending, DeliveryRetrying, DeliverySending:
				item.Pending++
			}
		}
		if item.LastFailedAt != nil && (item.LastDeliveredAt == nil || item.LastFailedAt.After(*item.LastDeliveredAt)) {
			item.Status = "failing"
		} else if item.Pending > 0 {
			item.Status = "pending"
		}
		health[channelID] = item
	}
	return health, nil
}

func (r *fakeRepository) ListNotificationRecipients(_ context.Context, projectID uuid.UUID) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recipientsErr != nil {
		return nil, r.recipientsErr
	}
	return append([]uuid.UUID(nil), r.notificationRecipients[projectID]...), nil
}

func (r *fakeRepository) ListNotifications(_ context.Context, userID uuid.UUID, limit int) ([]Notification, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listNotificationsErr != nil {
		return nil, 0, r.listNotificationsErr
	}
	notifications := append([]Notification(nil), r.notifications...)
	sort.Slice(notifications, func(i, j int) bool {
		if !notifications[i].CreatedAt.Equal(notifications[j].CreatedAt) {
			return notifications[i].CreatedAt.After(notifications[j].CreatedAt)
		}
		return notifications[i].ID.String() > notifications[j].ID.String()
	})
	var unread int64
	for _, notification := range notifications {
		if notification.CreatedAt.After(r.seenAt[userID]) {
			unread++
		}
	}
	if limit > 0 && len(notifications) > limit {
		notifications = notifications[:limit]
	}
	return notifications, unread, nil
}

func (r *fakeRepository) MarkNotificationsSeen(_ context.Context, userID uuid.UUID, seenAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seenAt[userID] = seenAt
	return nil
}

func (r *fakeRepository) notificationsSnapshot() []Notification {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Notification(nil), r.notifications...)
}

func (r *fakeRepository) UpdateChannel(_ context.Context, channel Channel) (Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return Channel{}, r.updateErr
	}
	if _, ok := r.channels[channel.ID]; !ok {
		return Channel{}, ErrNotFound
	}
	r.channels[channel.ID] = channel
	return channel, nil
}

func (r *fakeRepository) DeleteChannel(_ context.Context, channelID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deleteErr != nil {
		return r.deleteErr
	}
	if _, ok := r.channels[channelID]; !ok {
		return ErrNotFound
	}
	delete(r.channels, channelID)
	return nil
}

func (r *fakeRepository) CreateDelivery(_ context.Context, delivery Delivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createDeliveryErr != nil {
		return r.createDeliveryErr
	}
	if delivery.ID == uuid.Nil {
		delivery.ID = uuid.New()
	}
	history := append(r.deliveries[delivery.ChannelID], delivery)
	sortDeliveries(history)
	if len(history) > deliveryRetention {
		history = history[:deliveryRetention]
	}
	r.deliveries[delivery.ChannelID] = history
	return nil
}

func (r *fakeRepository) ListDeliveries(_ context.Context, channelID uuid.UUID, limit int) ([]Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listDeliveriesErr != nil {
		return nil, r.listDeliveriesErr
	}
	if _, ok := r.channels[channelID]; !ok {
		return []Delivery{}, nil
	}
	history := append([]Delivery(nil), r.deliveries[channelID]...)
	sortDeliveries(history)
	if limit > 0 && len(history) > limit {
		history = history[:limit]
	}
	return history, nil
}

func (r *fakeRepository) deliveriesFor(channelID uuid.UUID) []Delivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Delivery(nil), r.deliveries[channelID]...)
}

func sortDeliveries(deliveries []Delivery) {
	sort.Slice(deliveries, func(i, j int) bool {
		if !deliveries[i].CreatedAt.Equal(deliveries[j].CreatedAt) {
			return deliveries[i].CreatedAt.After(deliveries[j].CreatedAt)
		}
		return deliveries[i].ID.String() > deliveries[j].ID.String()
	})
}

func (r *fakeRepository) allChannels() []Channel {
	channels := make([]Channel, 0, len(r.channels))
	for _, channel := range r.channels {
		channels = append(channels, channel)
	}
	sort.Slice(channels, func(i, j int) bool {
		if !channels[i].CreatedAt.Equal(channels[j].CreatedAt) {
			return channels[i].CreatedAt.Before(channels[j].CreatedAt)
		}
		return channels[i].ID.String() < channels[j].ID.String()
	})
	return channels
}

type fakeNotifier struct {
	mu      sync.Mutex
	events  []Event
	err     error
	block   chan struct{}
	entered chan struct{}
}

var _ Notifier = (*fakeNotifier)(nil)

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{}
}

func (n *fakeNotifier) Notify(_ context.Context, event Event) error {
	if n.entered != nil {
		select {
		case n.entered <- struct{}{}:
		default:
		}
	}
	if n.block != nil {
		<-n.block
	}
	n.mu.Lock()
	n.events = append(n.events, event)
	n.mu.Unlock()
	return n.err
}

func (n *fakeNotifier) delivered() []Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]Event(nil), n.events...)
}

type fakeMailer struct {
	mu      sync.Mutex
	mails   []Mail
	err     error
	entered chan struct{}
}

var _ Mailer = (*fakeMailer)(nil)

func (m *fakeMailer) Send(_ context.Context, mail Mail) error {
	if m.entered != nil {
		select {
		case m.entered <- struct{}{}:
		default:
		}
	}
	m.mu.Lock()
	m.mails = append(m.mails, mail)
	m.mu.Unlock()
	return m.err
}

func (m *fakeMailer) delivered() []Mail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Mail(nil), m.mails...)
}

func newTestService(t *testing.T, repo Repository, publishers ...Publisher) *Service {
	t.Helper()
	var publisher Publisher
	if len(publishers) > 0 {
		publisher = publishers[0]
	}
	service := NewService(Config{
		Repository:  repo,
		Secret:      testSecret,
		Logger:      discardLogger(),
		Mailer:      &fakeMailer{},
		HTTPClient:  nil,
		Publisher:   publisher,
		Workers:     2,
		QueueSize:   16,
		SendTimeout: 2 * time.Second,
		Now:         func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) },
	})
	t.Cleanup(func() { _ = service.Close() })
	return service
}

type fakePublisher struct {
	mu     sync.Mutex
	frames map[string][]Frame
	err    error
}

var _ Publisher = (*fakePublisher)(nil)

func newFakePublisher() *fakePublisher {
	return &fakePublisher{frames: make(map[string][]Frame)}
}

func (p *fakePublisher) Publish(_ context.Context, channel, payload string) error {
	if p.err != nil {
		return p.err
	}
	var frame Frame
	if err := json.Unmarshal([]byte(payload), &frame); err != nil {
		return err
	}
	p.mu.Lock()
	p.frames[channel] = append(p.frames[channel], frame)
	p.mu.Unlock()
	return nil
}

func (p *fakePublisher) channels() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	channels := make([]string, 0, len(p.frames))
	for channel := range p.frames {
		channels = append(channels, channel)
	}
	sort.Strings(channels)
	return channels
}

func (p *fakePublisher) framesFor(channel string) []Frame {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Frame(nil), p.frames[channel]...)
}

func scopedCtx() context.Context {
	return context.Background()
}

func sampleChannel(t *testing.T) Channel {
	t.Helper()
	sealed, err := sealConfig(testSecret, ChannelConfig{WebhookURL: "https://discord.com/api/webhooks/1184/8f2c-secret"})
	if err != nil {
		t.Fatalf("seal config: %v", err)
	}
	now := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	return Channel{
		ID:           uuid.New(),
		Name:         "alerts",
		Kind:         KindDiscord,
		Enabled:      true,
		Events:       append([]EventKey(nil), AllEvents...),
		SealedConfig: sealed,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func sampleEvent() Event {
	return Event{
		Kind:         EventDeploy,
		Outcome:      OutcomeFailure,
		ResourceType: ResourceApplication,
		ResourceID:   uuid.New(),
		Name:         "shop-web",
		Host:         "shop.example.com",
		Error:        "build step failed: exit status 1",
		At:           time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
