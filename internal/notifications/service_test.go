package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/teams"
)

// setFactory routes every channel to the fake notifier of its ID.
func setFactory(t *testing.T, service *Service, notifiers map[uuid.UUID]*fakeNotifier) {
	t.Helper()
	service.factory = func(channel Channel) (Notifier, error) {
		notifier, ok := notifiers[channel.ID]
		if !ok {
			return nil, errors.New("no fake notifier for channel " + channel.ID.String())
		}
		return notifier, nil
	}
}

// TestDeployFailureDeliversFormattedDiscordMessage runs the whole path the
// phase's verification names: a failed deploy result reaches a mock Discord
// webhook with the formatted message.
func TestDeployFailureDeliversFormattedDiscordMessage(t *testing.T) {
	teamID, appID := uuid.New(), uuid.New()
	server, call := newWebhookServer(t, http.StatusOK)
	repo := newFakeRepository()
	sealed, err := sealConfig(testSecret, ChannelConfig{WebhookURL: server.URL + "/webhook"})
	if err != nil {
		t.Fatalf("seal config: %v", err)
	}
	repo.seed(Channel{
		ID: uuid.New(), TeamID: teamID, Name: "discord", Kind: KindDiscord,
		Enabled: true, Events: AllEvents, SealedConfig: sealed,
	})
	service := newTestService(t, repo)

	service.DeployFinished(context.Background(), deploy.DeployResult{
		ApplicationID: appID,
		Application:   "shop-web",
		TeamID:        teamID,
		State:         deploy.StateFailed,
		Error:         "build step failed: exit status 1",
		Host:          "shop.example.com",
		FinishedAt:    time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	})

	waitFor(t, "the Discord delivery", func() bool {
		_, _, _, body := call.snapshot()
		return len(body) > 0
	})
	method, path, _, body := call.snapshot()
	if method != http.MethodPost || path != "/webhook" {
		t.Fatalf("request = %s %s, want POST /webhook", method, path)
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode payload %s: %v", body, err)
	}
	want := "Deploy failed: shop-web\n" +
		"Host: shop.example.com\n" +
		"Error: build step failed: exit status 1\n" +
		"Time: 2026-09-29T12:00:00Z"
	if payload.Content != want {
		t.Errorf("content = %q, want %q", payload.Content, want)
	}
}

// TestDispatchSelectsTeamWideAndResourceOverride proves the dispatcher delivers
// to the team-wide channels plus the override scoped to the event's resource,
// and skips disabled channels, other resources and unsubscribed events.
func TestDispatchSelectsTeamWideAndResourceOverride(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	appID := uuid.New()
	repo := newFakeRepository()

	teamWide := repo.seed(sampleChannel(t, teamID))
	override := repo.seed(Channel{
		ID: uuid.New(), TeamID: teamID, Name: "app override", Kind: KindSlack,
		Enabled: true, ResourceType: ResourceApplication, ResourceID: appID,
		Events: AllEvents, SealedConfig: teamWide.SealedConfig,
	})
	otherResource := repo.seed(Channel{
		ID: uuid.New(), TeamID: teamID, Name: "other app", Kind: KindSlack,
		Enabled: true, ResourceType: ResourceApplication, ResourceID: uuid.New(),
		Events: AllEvents, SealedConfig: teamWide.SealedConfig,
	})
	disabled := repo.seed(Channel{
		ID: uuid.New(), TeamID: teamID, Name: "disabled", Kind: KindSlack,
		Enabled: false, Events: AllEvents, SealedConfig: teamWide.SealedConfig,
	})
	unsubscribed := repo.seed(Channel{
		ID: uuid.New(), TeamID: teamID, Name: "backup only", Kind: KindSlack,
		Enabled: true, Events: []EventKey{EventBackupFailure},
		SealedConfig: teamWide.SealedConfig,
	})

	service := newTestService(t, repo)
	notifiers := map[uuid.UUID]*fakeNotifier{}
	for _, channel := range []Channel{teamWide, override, otherResource, disabled, unsubscribed} {
		notifiers[channel.ID] = newFakeNotifier()
	}
	setFactory(t, service, notifiers)

	event := sampleEvent(teamID)
	event.ResourceID = appID
	service.Dispatch(event)

	waitFor(t, "the team-wide and override deliveries", func() bool {
		return len(notifiers[teamWide.ID].delivered()) == 1 && len(notifiers[override.ID].delivered()) == 1
	})
	for id, notifier := range notifiers {
		want := 0
		if id == teamWide.ID || id == override.ID {
			want = 1
		}
		if got := len(notifier.delivered()); got != want {
			t.Errorf("channel %s delivered %d events, want %d", id, got, want)
		}
	}
	if repo.listForEventCalls != 1 {
		t.Errorf("dispatcher lookups = %d, want 1", repo.listForEventCalls)
	}
	if repo.lastResourceType != ResourceApplication || repo.lastResourceID != appID {
		t.Errorf("dispatcher looked up %s/%s, want application/%s", repo.lastResourceType, repo.lastResourceID, appID)
	}
	_ = userID
}

// duplicateListRepository returns the first channel twice, which a well-formed
// SQL read can never do; the dispatcher must still deliver once.
type duplicateListRepository struct {
	*fakeRepository
}

// ListChannelsForEvent implements Repository by duplicating the page.
func (r *duplicateListRepository) ListChannelsForEvent(ctx context.Context, teamID uuid.UUID, resourceType string, resourceID uuid.UUID) ([]Channel, error) {
	channels, err := r.fakeRepository.ListChannelsForEvent(ctx, teamID, resourceType, resourceID)
	if err != nil || len(channels) == 0 {
		return channels, err
	}
	return append(channels, channels[0]), nil
}

// TestDispatchDeduplicatesChannels proves a channel returned twice is delivered
// once.
func TestDispatchDeduplicatesChannels(t *testing.T) {
	teamID := uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t, teamID))
	service := newTestService(t, &duplicateListRepository{fakeRepository: repo})
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	service.Dispatch(sampleEvent(teamID))

	waitFor(t, "the deduplicated delivery", func() bool { return len(notifier.delivered()) == 1 })
	time.Sleep(20 * time.Millisecond)
	if got := len(notifier.delivered()); got != 1 {
		t.Errorf("deliveries = %d, want exactly 1", got)
	}
}

// TestDispatchIsNonBlocking proves Dispatch returns while a notifier is stuck
// and while the queue is saturated: the deploy path can never be held up.
func TestDispatchIsNonBlocking(t *testing.T) {
	teamID := uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t, teamID))
	service := NewService(Config{
		Repository:  repo,
		Secret:      testSecret,
		Logger:      discardLogger(),
		Mailer:      &fakeMailer{},
		Workers:     1,
		QueueSize:   1,
		SendTimeout: time.Second,
	})
	t.Cleanup(func() { _ = service.Close() })

	blocked := newFakeNotifier()
	blocked.block = make(chan struct{})
	blocked.entered = make(chan struct{}, 1)
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: blocked})

	// The first event occupies the single worker.
	service.Dispatch(sampleEvent(teamID))
	select {
	case <-blocked.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the notifier was never entered")
	}

	// The queue is bounded: the second event fills it and the third is
	// dropped, and none of the calls may block.
	done := make(chan struct{})
	go func() {
		service.Dispatch(sampleEvent(teamID))
		service.Dispatch(sampleEvent(teamID))
		service.Dispatch(sampleEvent(teamID))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Dispatch blocked on a saturated queue")
	}
	close(blocked.block)
}

// TestFailingChannelDoesNotStopOthers proves one broken endpoint does not
// affect the deliveries of the other channels.
func TestFailingChannelDoesNotStopOthers(t *testing.T) {
	teamID := uuid.New()
	repo := newFakeRepository()
	first := repo.seed(sampleChannel(t, teamID))
	second := repo.seed(sampleChannel(t, teamID))
	service := newTestService(t, repo)
	failing := newFakeNotifier()
	failing.err = errors.New("endpoint is down")
	healthy := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{first.ID: failing, second.ID: healthy})

	service.Dispatch(sampleEvent(teamID))

	waitFor(t, "both deliveries", func() bool {
		return len(failing.delivered()) == 1 && len(healthy.delivered()) == 1
	})
}

// TestDeployFinishedMapsTerminalOutcome proves the deploy hook derives the
// event from the terminal state.
func TestDeployFinishedMapsTerminalOutcome(t *testing.T) {
	teamID, appID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t, teamID))
	service := newTestService(t, repo)
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	service.DeployFinished(context.Background(), deploy.DeployResult{
		ApplicationID: appID,
		Application:   "shop-web",
		TeamID:        teamID,
		State:         deploy.StateFailed,
		Error:         "build failed",
		FinishedAt:    time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	})
	waitFor(t, "the failure event", func() bool { return len(notifier.delivered()) == 1 })
	failure := notifier.delivered()[0]
	if failure.Key() != EventDeployFailure || failure.ResourceID != appID || failure.Name != "shop-web" {
		t.Errorf("failure event = %+v, want deploy_failure for shop-web", failure)
	}
	if failure.Error != "build failed" {
		t.Errorf("error = %q", failure.Error)
	}

	service.DeployFinished(context.Background(), deploy.DeployResult{
		ApplicationID: appID,
		Application:   "shop-web",
		TeamID:        teamID,
		State:         deploy.StateRunning,
		FinishedAt:    time.Date(2026, 9, 29, 12, 1, 0, 0, time.UTC),
	})
	waitFor(t, "the success event", func() bool { return len(notifier.delivered()) == 2 })
	success := notifier.delivered()[1]
	if success.Key() != EventDeploySuccess || success.Error != "" {
		t.Errorf("success event = %+v, want a clean deploy_success", success)
	}
}

// TestBackupFinishedMapsTerminalOutcome proves the backup hook derives the
// event from the backup status.
func TestBackupFinishedMapsTerminalOutcome(t *testing.T) {
	teamID, databaseID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t, teamID))
	service := newTestService(t, repo)
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	service.BackupFinished(context.Background(), databases.BackupResult{
		DatabaseID: databaseID,
		Database:   "orders-db",
		TeamID:     teamID,
		Status:     databases.BackupFailed,
		Error:      "dump exited 1",
		FinishedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	})
	service.BackupFinished(context.Background(), databases.BackupResult{
		DatabaseID: databaseID,
		Database:   "orders-db",
		TeamID:     teamID,
		Status:     databases.BackupCompleted,
		FinishedAt: time.Date(2026, 9, 29, 12, 1, 0, 0, time.UTC),
	})

	waitFor(t, "both backup events", func() bool { return len(notifier.delivered()) == 2 })
	events := notifier.delivered()
	if events[0].Key() != EventBackupFailure || events[1].Key() != EventBackupSuccess {
		t.Errorf("keys = %s/%s, want backup_failure/backup_success", events[0].Key(), events[1].Key())
	}
	if events[0].ResourceType != ResourceDatabase || events[0].ResourceID != databaseID {
		t.Errorf("resource = %s/%s, want database/%s", events[0].ResourceType, events[0].ResourceID, databaseID)
	}
}

// TestDeployFinishedWithoutNotifierIsSafe pins the flag-off hook path: an
// unwired or disabled service delivers nothing and never panics.
func TestDeployFinishedWithoutNotifierIsSafe(t *testing.T) {
	var nilService *Service
	nilService.DeployFinished(context.Background(), deploy.DeployResult{State: deploy.StateFailed})
	nilService.BackupFinished(context.Background(), databases.BackupResult{Status: databases.BackupFailed})
	nilService.Dispatch(sampleEvent(uuid.New()))

	t.Setenv(FeatureEnv, "false")
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t, uuid.New()))
	service := newTestService(t, repo)
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	service.Dispatch(sampleEvent(channel.TeamID))
	time.Sleep(30 * time.Millisecond)
	if got := len(notifier.delivered()); got != 0 {
		t.Errorf("deliveries = %d with the flag off, want 0", got)
	}
	if NewDefaultService(Config{Repository: repo, Secret: testSecret}) != nil {
		t.Error("NewDefaultService with the flag off = a service, want nil")
	}
}

// TestEnabledFlag pins the kill-switch semantics: only an explicit false
// disables the feature.
func TestEnabledFlag(t *testing.T) {
	tests := map[string]bool{"": true, "true": true, "false": false, "FALSE": false, "0": true}
	for value, want := range tests {
		t.Run(value, func(t *testing.T) {
			t.Setenv(FeatureEnv, value)
			if got := Enabled(); got != want {
				t.Errorf("Enabled() with %q = %v, want %v", value, got, want)
			}
		})
	}
}

// TestCreateChannelSealsAndRedacts proves the stored config is sealed and the
// read view never carries the secret.
func TestCreateChannelSealsAndRedacts(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	service := newTestService(t, repo)

	const webhook = "https://discord.com/api/webhooks/1184/8f2c-secret-value"
	view, err := service.CreateChannel(scopedCtx(userID, teamID, teams.RoleOwner), userID, ChannelRequest{
		Name: "team alerts",
		Kind: KindDiscord,
		Config: ChannelConfig{
			WebhookURL: webhook,
		},
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if view.SecretsConfigured != true {
		t.Error("secrets_configured = false, want true")
	}
	if view.Config.WebhookURL == webhook || strings.Contains(view.Config.WebhookURL, "8f2c-secret") {
		t.Errorf("view leaks the webhook: %q", view.Config.WebhookURL)
	}
	if len(view.Events) != len(AllEvents) {
		t.Errorf("events = %v, want the default subscription", view.Events)
	}
	if !view.Enabled {
		t.Error("enabled = false, want the default true")
	}

	stored, ok := repo.get(view.ID)
	if !ok {
		t.Fatal("the channel was not stored")
	}
	if stored.TeamID != teamID {
		t.Errorf("team = %s, want %s", stored.TeamID, teamID)
	}
	if stored.SealedConfig == webhook || strings.Contains(stored.SealedConfig, "8f2c-secret") {
		t.Errorf("stored config is not sealed: %q", stored.SealedConfig)
	}
	opened, err := openConfig(testSecret, stored.SealedConfig)
	if err != nil {
		t.Fatalf("openConfig: %v", err)
	}
	if opened.WebhookURL != webhook {
		t.Errorf("stored webhook = %q, want the original", opened.WebhookURL)
	}
}

// TestCreateChannelValidation rejects unknown kinds, malformed config and
// inconsistent resource overrides.
func TestCreateChannelValidation(t *testing.T) {
	validDiscord := ChannelConfig{WebhookURL: "https://discord.com/api/webhooks/1/abc"}
	cases := map[string]ChannelRequest{
		"missing name":           {Kind: KindDiscord, Config: validDiscord},
		"unknown kind":           {Name: "x", Kind: "mattermost", Config: validDiscord},
		"discord without url":    {Name: "x", Kind: KindDiscord},
		"telegram without token": {Name: "x", Kind: KindTelegram, Config: ChannelConfig{ChatID: "-1"}},
		"email without recipients": {Name: "x", Kind: KindEmail, Config: ChannelConfig{
			Host: "smtp.example.com", From: "ops@example.com"}},
		"unknown resource type": {Name: "x", Kind: KindDiscord, Config: validDiscord,
			ResourceType: ptr("service"), ResourceID: ptr(uuid.New().String())},
		"resource id alone": {Name: "x", Kind: KindDiscord, Config: validDiscord,
			ResourceID: ptr(uuid.New().String())},
		"empty events": {Name: "x", Kind: KindDiscord, Config: validDiscord, Events: []EventKey{}},
		"unknown event": {Name: "x", Kind: KindDiscord, Config: validDiscord,
			Events: []EventKey{EventDeploySuccess, "deploy_exploded"}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			service := newTestService(t, newFakeRepository())
			_, err := service.CreateChannel(scopedCtx(uuid.New(), uuid.New(), teams.RoleOwner), uuid.New(), req)
			if !errors.Is(err, ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}

// ptr returns a pointer to value.
func ptr[T any](value T) *T { return &value }

// TestChannelMutationsRequireWriteRole proves a read_only member reads but
// every mutation is refused.
func TestChannelMutationsRequireWriteRole(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t, teamID))
	service := newTestService(t, repo)
	ctx := scopedCtx(userID, teamID, teams.RoleReadOnly)

	if _, err := service.ListChannels(ctx, userID); err != nil {
		t.Errorf("ListChannels err = %v, want a read to pass", err)
	}
	if _, err := service.CreateChannel(ctx, userID, ChannelRequest{
		Name: "x", Kind: KindDiscord, Config: ChannelConfig{WebhookURL: "https://discord.com/api/webhooks/1/abc"},
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("CreateChannel err = %v, want ErrForbidden", err)
	}
	if _, err := service.UpdateChannel(ctx, userID, channel.ID, ChannelRequest{Name: "renamed"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("UpdateChannel err = %v, want ErrForbidden", err)
	}
	if err := service.DeleteChannel(ctx, userID, channel.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("DeleteChannel err = %v, want ErrForbidden", err)
	}
	if _, err := service.TestChannel(ctx, userID, channel.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("TestChannel err = %v, want ErrForbidden", err)
	}
}

// TestChannelTeamIsolation proves a channel of another team is indistinguishable
// from a missing one and lists never cross teams.
func TestChannelTeamIsolation(t *testing.T) {
	ownerID, teamA := uuid.New(), uuid.New()
	strangerID, teamB := uuid.New(), uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t, teamA))
	service := newTestService(t, repo)
	ctx := scopedCtx(strangerID, teamB, teams.RoleOwner)

	if _, err := service.GetChannel(ctx, strangerID, channel.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetChannel err = %v, want ErrNotFound", err)
	}
	channels, err := service.ListChannels(ctx, strangerID)
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("channels = %d, want none from another team", len(channels))
	}
	if _, err := service.UpdateChannel(ctx, strangerID, channel.ID, ChannelRequest{Name: "stolen"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateChannel err = %v, want ErrNotFound", err)
	}
	if err := service.DeleteChannel(ctx, strangerID, channel.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteChannel err = %v, want ErrNotFound", err)
	}

	// The owner still sees it.
	ownerCtx := scopedCtx(ownerID, teamA, teams.RoleOwner)
	ownerChannels, err := service.ListChannels(ownerCtx, ownerID)
	if err != nil || len(ownerChannels) != 1 {
		t.Fatalf("owner list = %v (%v), want one channel", ownerChannels, err)
	}
}

// TestUpdateChannelPartialAndKindImmutable proves zero fields keep stored
// values, secrets merge field-wise and the kind cannot change.
func TestUpdateChannelPartialAndKindImmutable(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	sealed, err := sealConfig(testSecret, ChannelConfig{BotToken: "7184:AAH-old-token", ChatID: "-1001"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	channel := repo.seed(Channel{
		ID: uuid.New(), TeamID: teamID, Name: "telegram", Kind: KindTelegram,
		Enabled: true, Events: []EventKey{EventDeployFailure}, SealedConfig: sealed,
	})
	service := newTestService(t, repo)
	ctx := scopedCtx(userID, teamID, teams.RoleAdmin)

	view, err := service.UpdateChannel(ctx, userID, channel.ID, ChannelRequest{
		Name:    "renamed",
		Enabled: ptr(false),
		Config:  ChannelConfig{ChatID: "-2002"},
	})
	if err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}
	if view.Name != "renamed" || view.Enabled {
		t.Errorf("view = %q/%v, want renamed and disabled", view.Name, view.Enabled)
	}
	if strings.Join(eventsToStrings(view.Events), ",") != string(EventDeployFailure) {
		t.Errorf("events = %v, want the stored subscription", view.Events)
	}
	stored, _ := repo.get(channel.ID)
	opened, err := openConfig(testSecret, stored.SealedConfig)
	if err != nil {
		t.Fatalf("openConfig: %v", err)
	}
	if opened.BotToken != "7184:AAH-old-token" {
		t.Errorf("bot token = %q, want the stored one (a masked resend must not wipe it)", opened.BotToken)
	}
	if opened.ChatID != "-2002" {
		t.Errorf("chat id = %q, want the updated one", opened.ChatID)
	}
	if view.Config.BotToken == "7184:AAH-old-token" {
		t.Error("the view leaks the bot token")
	}

	if _, err := service.UpdateChannel(ctx, userID, channel.ID, ChannelRequest{Kind: KindSlack}); !errors.Is(err, ErrValidation) {
		t.Errorf("kind change err = %v, want ErrValidation", err)
	}
}

// eventsToStrings renders event keys for comparisons.
func eventsToStrings(events []EventKey) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, string(event))
	}
	return out
}

// TestTestChannelDelivers proves the send-test action delivers through the
// injected mailer and reports the result without secrets.
func TestTestChannelDelivers(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	mailer := &fakeMailer{}
	sealed, err := sealConfig(testSecret, ChannelConfig{
		Host: "smtp.gotham.dev", Port: 587, From: "ops@gotham.dev", To: []string{"oncall@gotham.dev"},
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	channel := repo.seed(Channel{
		ID: uuid.New(), TeamID: teamID, Name: "email", Kind: KindEmail,
		Enabled: true, Events: AllEvents, SealedConfig: sealed,
	})
	service := NewService(Config{
		Repository: repo, Secret: testSecret, Logger: discardLogger(), Mailer: mailer, SendTimeout: time.Second,
	})
	t.Cleanup(func() { _ = service.Close() })

	result, err := service.TestChannel(scopedCtx(userID, teamID, teams.RoleOwner), userID, channel.ID)
	if err != nil {
		t.Fatalf("TestChannel: %v", err)
	}
	if !result.OK {
		t.Fatalf("result = %+v, want OK", result)
	}
	mails := mailer.delivered()
	if len(mails) != 1 || !strings.Contains(mails[0].Subject, "Deploy succeeded") {
		t.Fatalf("mails = %+v, want one test mail", mails)
	}
	if strings.Contains(result.Message, "smtp") {
		t.Errorf("message = %q, want no transport detail", result.Message)
	}

	// A failing mailer is reported, not returned as an error.
	mailer.err = errors.New("connection refused")
	result, err = service.TestChannel(scopedCtx(userID, teamID, teams.RoleOwner), userID, channel.ID)
	if err != nil {
		t.Fatalf("TestChannel (failing mailer): %v", err)
	}
	if result.OK {
		t.Error("result.OK = true for a failing mailer")
	}
}

// TestServiceWithoutRepositoryFailsClearly pins the not-ready path.
func TestServiceWithoutRepositoryFailsClearly(t *testing.T) {
	service := NewService(Config{Secret: testSecret, Logger: discardLogger()})
	t.Cleanup(func() { _ = service.Close() })
	if _, err := service.ListChannels(context.Background(), uuid.New()); err == nil {
		t.Error("ListChannels = nil error without a repository")
	}
	if _, err := service.CreateChannel(context.Background(), uuid.New(), ChannelRequest{}); err == nil {
		t.Error("CreateChannel = nil error without a repository")
	}
}
