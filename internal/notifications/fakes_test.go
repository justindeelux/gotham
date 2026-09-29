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

	"github.com/justindeelux/gotham/internal/teams"
)

// testSecret is the deployment key every test service seals channel configs
// with.
const testSecret = "notifications-test-secret"

// TestMain clears the feature flag so the suite runs with notifications
// enabled; individual tests override it with t.Setenv.
func TestMain(m *testing.M) {
	_ = os.Unsetenv(FeatureEnv)
	os.Exit(m.Run())
}

// discardLogger keeps the service's operational logging out of test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// syncBuffer is a goroutine-safe log sink: workers log while the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write implements io.Writer.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns what was logged so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fakeRepository is an in-memory Repository that mimics the SQL semantics
// tests care about: team scoping, enabled-only dispatcher reads and resource
// override matching.
type fakeRepository struct {
	mu       sync.Mutex
	channels map[uuid.UUID]Channel
	order    []uuid.UUID

	createErr       error
	getErr          error
	listErr         error
	listForEventErr error
	updateErr       error
	deleteErr       error

	// listForEventCalls and the last resource arguments let a test prove the
	// dispatcher asked for exactly the event's resource.
	listForEventCalls int
	lastResourceType  string
	lastResourceID    uuid.UUID
}

// Compile-time guarantee.
var _ Repository = (*fakeRepository)(nil)

func newFakeRepository() *fakeRepository {
	return &fakeRepository{channels: make(map[uuid.UUID]Channel)}
}

// seed stores a channel directly, filling the fields the service would.
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

// get is the test-side accessor.
func (r *fakeRepository) get(id uuid.UUID) (Channel, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	channel, ok := r.channels[id]
	return channel, ok
}

// CreateChannel implements Repository.
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

// GetChannel implements Repository.
func (r *fakeRepository) GetChannel(_ context.Context, teamID, channelID uuid.UUID) (Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return Channel{}, r.getErr
	}
	channel, ok := r.channels[channelID]
	if !ok || channel.TeamID != teamID {
		return Channel{}, ErrNotFound
	}
	return channel, nil
}

// ListChannels implements Repository.
func (r *fakeRepository) ListChannels(_ context.Context, teamID uuid.UUID) ([]Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.byTeam(teamID), nil
}

// ListChannelsForEvent implements Repository with the dispatcher query's
// semantics: enabled channels of the team that are team-wide or scoped to the
// exact resource.
func (r *fakeRepository) ListChannelsForEvent(_ context.Context, teamID uuid.UUID, resourceType string, resourceID uuid.UUID) ([]Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listForEventCalls++
	r.lastResourceType, r.lastResourceID = resourceType, resourceID
	if r.listForEventErr != nil {
		return nil, r.listForEventErr
	}
	var matched []Channel
	for _, channel := range r.byTeam(teamID) {
		if !channel.Enabled {
			continue
		}
		if channel.ResourceType == "" || (channel.ResourceType == resourceType && channel.ResourceID == resourceID) {
			matched = append(matched, channel)
		}
	}
	return matched, nil
}

// UpdateChannel implements Repository.
func (r *fakeRepository) UpdateChannel(_ context.Context, channel Channel) (Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return Channel{}, r.updateErr
	}
	stored, ok := r.channels[channel.ID]
	if !ok || stored.TeamID != channel.TeamID {
		return Channel{}, ErrNotFound
	}
	r.channels[channel.ID] = channel
	return channel, nil
}

// DeleteChannel implements Repository.
func (r *fakeRepository) DeleteChannel(_ context.Context, teamID, channelID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deleteErr != nil {
		return r.deleteErr
	}
	stored, ok := r.channels[channelID]
	if !ok || stored.TeamID != teamID {
		return ErrNotFound
	}
	delete(r.channels, channelID)
	return nil
}

// byTeam returns a team's channels oldest first. Callers hold the lock.
func (r *fakeRepository) byTeam(teamID uuid.UUID) []Channel {
	channels := make([]Channel, 0, len(r.channels))
	for _, channel := range r.channels {
		if channel.TeamID == teamID {
			channels = append(channels, channel)
		}
	}
	sort.Slice(channels, func(i, j int) bool {
		if !channels[i].CreatedAt.Equal(channels[j].CreatedAt) {
			return channels[i].CreatedAt.Before(channels[j].CreatedAt)
		}
		return channels[i].ID.String() < channels[j].ID.String()
	})
	return channels
}

// fakeNotifier records the events delivered to it, optionally failing or
// blocking so the dispatcher's isolation and non-blocking properties are
// testable.
type fakeNotifier struct {
	mu      sync.Mutex
	events  []Event
	err     error
	block   chan struct{}
	entered chan struct{}
}

// Compile-time guarantee.
var _ Notifier = (*fakeNotifier)(nil)

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{}
}

// Notify implements Notifier.
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

// delivered returns the recorded events.
func (n *fakeNotifier) delivered() []Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]Event(nil), n.events...)
}

// fakeMailer records the mails handed to it.
type fakeMailer struct {
	mu      sync.Mutex
	mails   []Mail
	err     error
	entered chan struct{}
}

// Compile-time guarantee.
var _ Mailer = (*fakeMailer)(nil)

// Send implements Mailer.
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

// delivered returns the recorded mails.
func (m *fakeMailer) delivered() []Mail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Mail(nil), m.mails...)
}

// newTestService builds a Service over fakes with a fast timeout and a
// deterministic clock. The worker pool is stopped at cleanup.
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

// scopedCtx puts a team scope on the context, as RequireTeam does.
func scopedCtx(userID, teamID uuid.UUID, role teams.Role) context.Context {
	return teams.WithScope(context.Background(), teams.Scope{UserID: userID, TeamID: teamID, Role: role})
}

// sampleChannel builds a stored channel with a sealed Discord config.
func sampleChannel(t *testing.T, teamID uuid.UUID) Channel {
	t.Helper()
	sealed, err := sealConfig(testSecret, ChannelConfig{WebhookURL: "https://discord.com/api/webhooks/1184/8f2c-secret"})
	if err != nil {
		t.Fatalf("seal config: %v", err)
	}
	now := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	return Channel{
		ID:           uuid.New(),
		TeamID:       teamID,
		Name:         "team alerts",
		Kind:         KindDiscord,
		Enabled:      true,
		Events:       append([]EventKey(nil), AllEvents...),
		SealedConfig: sealed,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// sampleEvent builds a deploy-failure event.
func sampleEvent(teamID uuid.UUID) Event {
	return Event{
		Kind:         EventDeploy,
		Outcome:      OutcomeFailure,
		TeamID:       teamID,
		ResourceType: ResourceApplication,
		ResourceID:   uuid.New(),
		Name:         "shop-web",
		Host:         "shop.example.com",
		Error:        "build step failed: exit status 1",
		At:           time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	}
}

// waitFor blocks until cond holds, failing the test when it never does.
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
