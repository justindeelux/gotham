package notifications

import (
	"bytes"
	"context"
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

	listForEventCalls int
	lastResourceType  string
	lastResourceID    uuid.UUID
}

var _ Repository = (*fakeRepository)(nil)

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		channels:   make(map[uuid.UUID]Channel),
		deliveries: make(map[uuid.UUID][]Delivery),
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

func newTestService(t *testing.T, repo Repository) *Service {
	t.Helper()
	service := NewService(Config{
		Repository:  repo,
		Secret:      testSecret,
		Logger:      discardLogger(),
		Mailer:      &fakeMailer{},
		HTTPClient:  nil,
		Workers:     2,
		QueueSize:   16,
		SendTimeout: 2 * time.Second,
		Now:         func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) },
	})
	t.Cleanup(func() { _ = service.Close() })
	return service
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
