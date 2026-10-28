package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
)

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

func TestDeployFailureDeliversFormattedDiscordMessage(t *testing.T) {
	appID := uuid.New()
	server, call := newWebhookServer(t, http.StatusOK)
	repo := newFakeRepository()
	sealed, err := sealConfig(testSecret, ChannelConfig{WebhookURL: server.URL + "/webhook"})
	if err != nil {
		t.Fatalf("seal config: %v", err)
	}
	repo.seed(Channel{
		ID: uuid.New(), Name: "discord", Kind: KindDiscord,
		Enabled: true, Events: AllEvents, SealedConfig: sealed,
	})
	service := newTestService(t, repo)

	service.DeployFinished(context.Background(), deploy.DeployResult{
		ApplicationID: appID,
		Application:   "shop-web",

		State:      deploy.StateFailed,
		Error:      "build step failed: exit status 1",
		Host:       "shop.example.com",
		FinishedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
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

func TestDispatchSelectsInstanceWideAndResourceOverride(t *testing.T) {
	userID := uuid.New()
	appID := uuid.New()
	repo := newFakeRepository()

	instanceWide := repo.seed(sampleChannel(t))
	override := repo.seed(Channel{
		ID: uuid.New(), Name: "app override", Kind: KindSlack,
		Enabled: true, ResourceType: ResourceApplication, ResourceID: appID,
		Events: AllEvents, SealedConfig: instanceWide.SealedConfig,
	})
	otherResource := repo.seed(Channel{
		ID: uuid.New(), Name: "other app", Kind: KindSlack,
		Enabled: true, ResourceType: ResourceApplication, ResourceID: uuid.New(),
		Events: AllEvents, SealedConfig: instanceWide.SealedConfig,
	})
	disabled := repo.seed(Channel{
		ID: uuid.New(), Name: "disabled", Kind: KindSlack,
		Enabled: false, Events: AllEvents, SealedConfig: instanceWide.SealedConfig,
	})
	unsubscribed := repo.seed(Channel{
		ID: uuid.New(), Name: "backup only", Kind: KindSlack,
		Enabled: true, Events: []EventKey{EventBackupFailure},
		SealedConfig: instanceWide.SealedConfig,
	})

	service := newTestService(t, repo)
	notifiers := map[uuid.UUID]*fakeNotifier{}
	for _, channel := range []Channel{instanceWide, override, otherResource, disabled, unsubscribed} {
		notifiers[channel.ID] = newFakeNotifier()
	}
	setFactory(t, service, notifiers)

	event := sampleEvent()
	event.ResourceID = appID
	service.Dispatch(event)

	waitFor(t, "the instance-wide and override deliveries", func() bool {
		return len(notifiers[instanceWide.ID].delivered()) == 1 && len(notifiers[override.ID].delivered()) == 1
	})
	for id, notifier := range notifiers {
		want := 0
		if id == instanceWide.ID || id == override.ID {
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

type duplicateListRepository struct {
	*fakeRepository
}

func (r *duplicateListRepository) ListChannelsForEvent(ctx context.Context, resourceType string, resourceID uuid.UUID) ([]Channel, error) {
	channels, err := r.fakeRepository.ListChannelsForEvent(ctx, resourceType, resourceID)
	if err != nil || len(channels) == 0 {
		return channels, err
	}
	return append(channels, channels[0]), nil
}

func TestDispatchDeduplicatesChannels(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, &duplicateListRepository{fakeRepository: repo})
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	service.Dispatch(sampleEvent())

	waitFor(t, "the deduplicated delivery", func() bool { return len(notifier.delivered()) == 1 })
	time.Sleep(20 * time.Millisecond)
	if got := len(notifier.delivered()); got != 1 {
		t.Errorf("deliveries = %d, want exactly 1", got)
	}
}

func TestDispatchIsNonBlocking(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
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

	service.Dispatch(sampleEvent())
	select {
	case <-blocked.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the notifier was never entered")
	}

	done := make(chan struct{})
	go func() {
		service.Dispatch(sampleEvent())
		service.Dispatch(sampleEvent())
		service.Dispatch(sampleEvent())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Dispatch blocked on a saturated queue")
	}
	close(blocked.block)
}

func TestFailingChannelDoesNotStopOthers(t *testing.T) {
	repo := newFakeRepository()
	first := repo.seed(sampleChannel(t))
	second := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	failing := newFakeNotifier()
	failing.err = errors.New("endpoint is down")
	healthy := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{first.ID: failing, second.ID: healthy})

	service.Dispatch(sampleEvent())

	waitFor(t, "both deliveries", func() bool {
		return len(failing.delivered()) == 1 && len(healthy.delivered()) == 1
	})
}

func TestRedeployNeededMapsEventAndHonoursChannelScope(t *testing.T) {
	appID := uuid.New()
	projectID := uuid.New()
	memberID := uuid.New()
	repo := newFakeRepository()
	repo.resourceProjects[appID] = projectID
	repo.notificationRecipients[projectID] = []uuid.UUID{memberID}

	instanceWide := repo.seed(sampleChannel(t))
	override := repo.seed(Channel{
		ID: uuid.New(), Name: "app override", Kind: KindSlack,
		Enabled: true, ResourceType: ResourceApplication, ResourceID: appID,
		Events: AllEvents, SealedConfig: instanceWide.SealedConfig,
	})
	otherResource := repo.seed(Channel{
		ID: uuid.New(), Name: "other app", Kind: KindSlack,
		Enabled: true, ResourceType: ResourceApplication, ResourceID: uuid.New(),
		Events: AllEvents, SealedConfig: instanceWide.SealedConfig,
	})
	database := repo.seed(Channel{
		ID: uuid.New(), Name: "database", Kind: KindSlack,
		Enabled: true, ResourceType: ResourceDatabase, ResourceID: uuid.New(),
		Events: AllEvents, SealedConfig: instanceWide.SealedConfig,
	})
	service := newTestService(t, repo)
	notifiers := map[uuid.UUID]*fakeNotifier{}
	for _, channel := range []Channel{instanceWide, override, otherResource, database} {
		notifiers[channel.ID] = newFakeNotifier()
	}
	setFactory(t, service, notifiers)

	service.RedeployNeeded(context.Background(), deploy.RedeployResult{
		ApplicationID: appID,
		Application:   "shop-web",
		Changes:       []string{"environment", "domains"},
		At:            time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	})

	waitFor(t, "the scoped deliveries and inbox row", func() bool {
		return len(notifiers[instanceWide.ID].delivered()) == 1 &&
			len(notifiers[override.ID].delivered()) == 1 &&
			len(repo.notificationsSnapshot()) == 1
	})
	for id, notifier := range notifiers {
		want := 0
		if id == instanceWide.ID || id == override.ID {
			want = 1
		}
		if got := len(notifier.delivered()); got != want {
			t.Errorf("channel %s delivered %d events, want %d", id, got, want)
		}
	}
	event := notifiers[instanceWide.ID].delivered()[0]
	if event.Key() != EventRedeployNeeded {
		t.Errorf("key = %q, want %q", event.Key(), EventRedeployNeeded)
	}
	if event.ResourceType != ResourceApplication || event.ResourceID != appID {
		t.Errorf("resource = %s/%s, want application/%s", event.ResourceType, event.ResourceID, appID)
	}
	if event.Detail != "domains,environment" {
		t.Errorf("detail = %q, want the canonical category codes", event.Detail)
	}
	if summary := event.Summary(); !strings.Contains(summary, "domains or ports") ||
		!strings.Contains(summary, "environment variables") {
		t.Errorf("summary = %q, want the pending categories", summary)
	}

	notification := repo.notificationsSnapshot()[0]
	if notification.EventKey != EventRedeployNeeded || notification.Detail != "domains,environment" {
		t.Errorf("notification = %+v, want a redeploy row carrying the categories", notification)
	}
	if notification.ResourceType != ResourceApplication || notification.ResourceID != appID {
		t.Errorf("notification resource = %s/%s, want application/%s",
			notification.ResourceType, notification.ResourceID, appID)
	}
}

func TestDeployFinishedMapsTerminalOutcome(t *testing.T) {
	appID := uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	service.DeployFinished(context.Background(), deploy.DeployResult{
		ApplicationID: appID,
		Application:   "shop-web",

		State:      deploy.StateFailed,
		Error:      "build failed",
		FinishedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
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

		State:      deploy.StateRunning,
		FinishedAt: time.Date(2026, 9, 29, 12, 1, 0, 0, time.UTC),
	})
	waitFor(t, "the success event", func() bool { return len(notifier.delivered()) == 2 })
	success := notifier.delivered()[1]
	if success.Key() != EventDeploySuccess || success.Error != "" {
		t.Errorf("success event = %+v, want a clean deploy_success", success)
	}
}

func TestBackupFinishedMapsTerminalOutcome(t *testing.T) {
	databaseID := uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := NewService(Config{
		Repository:  repo,
		Secret:      testSecret,
		Logger:      discardLogger(),
		Mailer:      &fakeMailer{},
		Workers:     1,
		QueueSize:   8,
		SendTimeout: time.Second,
	})
	t.Cleanup(func() { _ = service.Close() })
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	service.BackupFinished(context.Background(), databases.BackupResult{
		DatabaseID: databaseID,
		Database:   "orders-db",

		Status:     databases.BackupFailed,
		Error:      "dump exited 1",
		FinishedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	})
	service.BackupFinished(context.Background(), databases.BackupResult{
		DatabaseID: databaseID,
		Database:   "orders-db",

		Status:     databases.BackupCompleted,
		FinishedAt: time.Date(2026, 9, 29, 12, 1, 0, 0, time.UTC),
	})

	waitFor(t, "both backup events", func() bool { return len(notifier.delivered()) == 2 })
	byKey := map[EventKey]Event{}
	for _, event := range notifier.delivered() {
		byKey[event.Key()] = event
	}
	failure, ok := byKey[EventBackupFailure]
	if !ok {
		t.Fatalf("events = %v, want a backup_failure", byKey)
	}
	if _, ok := byKey[EventBackupSuccess]; !ok {
		t.Fatalf("events = %v, want a backup_success", byKey)
	}
	if failure.ResourceType != ResourceDatabase || failure.ResourceID != databaseID {
		t.Errorf("resource = %s/%s, want database/%s", failure.ResourceType, failure.ResourceID, databaseID)
	}
	if failure.Error != "dump exited 1" {
		t.Errorf("error = %q, want the failure text", failure.Error)
	}
}

func TestDeployFinishedWithoutNotifierIsSafe(t *testing.T) {
	var nilService *Service
	nilService.DeployFinished(context.Background(), deploy.DeployResult{State: deploy.StateFailed})
	nilService.BackupFinished(context.Background(), databases.BackupResult{Status: databases.BackupFailed})
	nilService.Dispatch(sampleEvent())

	t.Setenv(FeatureEnv, "false")
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	service.Dispatch(sampleEvent())
	time.Sleep(30 * time.Millisecond)
	if got := len(notifier.delivered()); got != 0 {
		t.Errorf("deliveries = %d with the flag off, want 0", got)
	}
	if NewDefaultService(Config{Repository: repo, Secret: testSecret}) != nil {
		t.Error("NewDefaultService with the flag off = a service, want nil")
	}
}

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

func TestCreateChannelSealsAndRedacts(t *testing.T) {
	userID := uuid.New()
	repo := newFakeRepository()
	service := newTestService(t, repo)

	const webhook = "https://discord.com/api/webhooks/1184/8f2c-secret-value"
	view, err := service.CreateChannel(scopedCtx(), userID, ChannelRequest{
		Name: "alerts",
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
	if len(view.Events) != len(defaultEvents) {
		t.Errorf("events = %v, want the default subscription", view.Events)
	}
	if !view.Enabled {
		t.Error("enabled = false, want the default true")
	}

	stored, ok := repo.get(view.ID)
	if !ok {
		t.Fatal("the channel was not stored")
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
			_, err := service.CreateChannel(scopedCtx(), uuid.New(), req)
			if !errors.Is(err, ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func ptr[T any](value T) *T { return &value }

func TestUpdateChannelPartialAndKindImmutable(t *testing.T) {
	userID := uuid.New()
	repo := newFakeRepository()
	sealed, err := sealConfig(testSecret, ChannelConfig{BotToken: "7184:AAH-old-token", ChatID: "-1001"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	channel := repo.seed(Channel{
		ID: uuid.New(), Name: "telegram", Kind: KindTelegram,
		Enabled: true, Events: []EventKey{EventDeployFailure}, SealedConfig: sealed,
	})
	service := newTestService(t, repo)
	ctx := scopedCtx()

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

func eventsToStrings(events []EventKey) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, string(event))
	}
	return out
}

func TestTestChannelDelivers(t *testing.T) {
	userID := uuid.New()
	repo := newFakeRepository()
	mailer := &fakeMailer{}
	sealed, err := sealConfig(testSecret, ChannelConfig{
		Host: "smtp.gotham.dev", Port: 587, From: "ops@gotham.dev", To: []string{"oncall@gotham.dev"},
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	channel := repo.seed(Channel{
		ID: uuid.New(), Name: "email", Kind: KindEmail,
		Enabled: true, Events: AllEvents, SealedConfig: sealed,
	})
	service := NewService(Config{
		Repository: repo, Secret: testSecret, Logger: discardLogger(), Mailer: mailer, SendTimeout: time.Second,
	})
	t.Cleanup(func() { _ = service.Close() })

	result, err := service.TestChannel(scopedCtx(), userID, channel.ID)
	if err != nil {
		t.Fatalf("TestChannel: %v", err)
	}
	if !result.OK {
		t.Fatalf("result = %+v, want OK", result)
	}
	mails := mailer.delivered()
	if len(mails) != 1 || !strings.Contains(mails[0].Subject, "Test notification") {
		t.Fatalf("mails = %+v, want one clearly-labelled test mail", mails)
	}
	if strings.Contains(result.Message, "smtp") {
		t.Errorf("message = %q, want no transport detail", result.Message)
	}

	mailer.err = errors.New("connection refused")
	result, err = service.TestChannel(scopedCtx(), userID, channel.ID)
	if err != nil {
		t.Fatalf("TestChannel (failing mailer): %v", err)
	}
	if result.OK {
		t.Error("result.OK = true for a failing mailer")
	}
}

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

func TestMaskedResendKeepsStoredSecrets(t *testing.T) {
	userID := uuid.New()
	ctx := scopedCtx()
	repo := newFakeRepository()
	service := newTestService(t, repo)

	const telegramToken = "7184:AAH-real-token"
	telegram, err := service.CreateChannel(ctx, userID, ChannelRequest{
		Name: "telegram", Kind: KindTelegram,
		Config: ChannelConfig{BotToken: telegramToken, ChatID: "-1001"},
	})
	if err != nil {
		t.Fatalf("create telegram: %v", err)
	}
	if telegram.Config.BotToken == telegramToken {
		t.Fatal("the create view leaks the token")
	}
	updated, err := service.UpdateChannel(ctx, userID, telegram.ID, ChannelRequest{
		Name: "telegram renamed", Config: telegram.Config,
	})
	if err != nil {
		t.Fatalf("masked telegram resend: %v", err)
	}
	stored, _ := repo.get(telegram.ID)
	opened, err := openConfig(testSecret, stored.SealedConfig)
	if err != nil {
		t.Fatalf("openConfig: %v", err)
	}
	if opened.BotToken != telegramToken {
		t.Errorf("token = %q, want the stored one after a masked resend", opened.BotToken)
	}
	if opened.ChatID != "-1001" {
		t.Errorf("chat id = %q, want the stored one", opened.ChatID)
	}
	if strings.Contains(updated.Config.BotToken, "AAH") {
		t.Errorf("view token = %q, want the mask", updated.Config.BotToken)
	}

	const webhook = "https://discord.com/api/webhooks/1184/8f2c-secret-value"
	discord, err := service.CreateChannel(ctx, userID, ChannelRequest{
		Name: "discord", Kind: KindDiscord, Config: ChannelConfig{WebhookURL: webhook},
	})
	if err != nil {
		t.Fatalf("create discord: %v", err)
	}
	if _, err := service.UpdateChannel(ctx, userID, discord.ID, ChannelRequest{
		Name: "discord renamed", Config: discord.Config,
	}); err != nil {
		t.Fatalf("masked discord resend: %v", err)
	}
	storedDiscord, _ := repo.get(discord.ID)
	openedDiscord, err := openConfig(testSecret, storedDiscord.SealedConfig)
	if err != nil {
		t.Fatalf("openConfig discord: %v", err)
	}
	if openedDiscord.WebhookURL != webhook {
		t.Errorf("webhook = %q, want the stored one", openedDiscord.WebhookURL)
	}

	const smtpPassword = "smtp-secret-password"
	email, err := service.CreateChannel(ctx, userID, ChannelRequest{
		Name: "email", Kind: KindEmail,
		Config: ChannelConfig{
			Host: "smtp.gotham.dev", Port: 587, Username: "ops@gotham.dev",
			Password: smtpPassword, From: "ops@gotham.dev", To: []string{"oncall@gotham.dev"},
		},
	})
	if err != nil {
		t.Fatalf("create email: %v", err)
	}
	if _, err := service.UpdateChannel(ctx, userID, email.ID, ChannelRequest{
		Config: ChannelConfig{Password: email.Config.Password},
	}); err != nil {
		t.Fatalf("masked password resend: %v", err)
	}
	storedEmail, _ := repo.get(email.ID)
	openedEmail, err := openConfig(testSecret, storedEmail.SealedConfig)
	if err != nil {
		t.Fatalf("openConfig email: %v", err)
	}
	if openedEmail.Password != smtpPassword {
		t.Errorf("password = %q, want the stored one", openedEmail.Password)
	}

	if _, err := service.UpdateChannel(ctx, userID, telegram.ID, ChannelRequest{
		Config: ChannelConfig{BotToken: "999:AAH-new-token", ChatID: "-2002"},
	}); err != nil {
		t.Fatalf("deliberate telegram replacement: %v", err)
	}
	stored, _ = repo.get(telegram.ID)
	opened, err = openConfig(testSecret, stored.SealedConfig)
	if err != nil {
		t.Fatalf("openConfig after replacement: %v", err)
	}
	if opened.BotToken != "999:AAH-new-token" || opened.ChatID != "-2002" {
		t.Errorf("token/chat = %q/%q, want the deliberate replacement", opened.BotToken, opened.ChatID)
	}
}

func TestDispatcherLogHidesTransportSecret(t *testing.T) {
	server, _ := newWebhookServer(t, http.StatusOK)
	endpoint := server.URL + "/webhook/log-secret-token"
	server.Close()

	repo := newFakeRepository()
	sealed, err := sealConfig(testSecret, ChannelConfig{WebhookURL: endpoint})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	repo.seed(Channel{
		ID: uuid.New(), Name: "discord", Kind: KindDiscord,
		Enabled: true, Events: AllEvents, SealedConfig: sealed,
	})

	logs := &syncBuffer{}
	service := NewService(Config{
		Repository: repo, Secret: testSecret,
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
		Mailer: &fakeMailer{}, Workers: 1, QueueSize: 4, SendTimeout: time.Second,
	})
	t.Cleanup(func() { _ = service.Close() })

	service.Dispatch(sampleEvent())
	waitFor(t, "the delivery failure log", func() bool { return strings.Contains(logs.String(), "delivery failed") })
	if strings.Contains(logs.String(), "log-secret-token") {
		t.Fatalf("log leaks the webhook URL: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "connection failed") {
		t.Errorf("log = %q, want the transport category", logs.String())
	}
}

func TestDispatcherLogHidesStatusReasonPhrase(t *testing.T) {
	server := newReflectingStatusServer(t, http.StatusInternalServerError)
	repo := newFakeRepository()
	sealed, err := sealConfig(testSecret, ChannelConfig{WebhookURL: server.URL + "/webhook/log-reason-secret"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	repo.seed(Channel{
		ID: uuid.New(), Name: "discord", Kind: KindDiscord,
		Enabled: true, Events: AllEvents, SealedConfig: sealed,
	})

	logs := &syncBuffer{}
	service := NewService(Config{
		Repository: repo, Secret: testSecret,
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
		Mailer: &fakeMailer{}, Workers: 1, QueueSize: 4, SendTimeout: time.Second,
	})
	t.Cleanup(func() { _ = service.Close() })

	service.Dispatch(sampleEvent())
	waitFor(t, "the delivery failure log", func() bool { return strings.Contains(logs.String(), "delivery failed") })
	if strings.Contains(logs.String(), "log-reason-secret") {
		t.Fatalf("log leaks the webhook URL: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "status 500") {
		t.Errorf("log = %q, want the numeric status", logs.String())
	}
}

func TestTelegramTokenNormalized(t *testing.T) {
	userID := uuid.New()
	ctx := scopedCtx()
	repo := newFakeRepository()
	service := newTestService(t, repo)

	view, err := service.CreateChannel(ctx, userID, ChannelRequest{
		Name: "telegram", Kind: KindTelegram,
		Config: ChannelConfig{BotToken: " 7184:AAH-padded-token ", ChatID: " -1001 "},
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	stored, ok := repo.get(view.ID)
	if !ok {
		t.Fatal("the channel was not stored")
	}
	opened, err := openConfig(testSecret, stored.SealedConfig)
	if err != nil {
		t.Fatalf("openConfig: %v", err)
	}
	if opened.BotToken != "7184:AAH-padded-token" {
		t.Errorf("token = %q, want the trimmed value", opened.BotToken)
	}
	if opened.ChatID != "-1001" {
		t.Errorf("chat id = %q, want the trimmed value", opened.ChatID)
	}
	notifier, err := service.notifierFor(stored)
	if err != nil {
		t.Fatalf("notifierFor: %v", err)
	}
	telegram, ok := notifier.(*telegramNotifier)
	if !ok {
		t.Fatalf("notifier = %T, want *telegramNotifier", notifier)
	}
	if telegram.token != "7184:AAH-padded-token" {
		t.Errorf("notifier token = %q, want the trimmed value", telegram.token)
	}

	if _, err := service.UpdateChannel(ctx, userID, view.ID, ChannelRequest{
		Config: ChannelConfig{BotToken: " 999:AAH-second "},
	}); err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}
	stored, _ = repo.get(view.ID)
	opened, err = openConfig(testSecret, stored.SealedConfig)
	if err != nil {
		t.Fatalf("openConfig after update: %v", err)
	}
	if opened.BotToken != "999:AAH-second" {
		t.Errorf("token after update = %q, want the trimmed replacement", opened.BotToken)
	}

	if _, err := service.CreateChannel(ctx, userID, ChannelRequest{
		Name: "bad", Kind: KindTelegram,
		Config: ChannelConfig{BotToken: "7184:AAH bad", ChatID: "-1"},
	}); !errors.Is(err, ErrValidation) {
		t.Errorf("malformed token err = %v, want ErrValidation", err)
	}
}

func TestEmptySecretKeyUsesEphemeralSecret(t *testing.T) {
	logs := &syncBuffer{}
	service := NewService(Config{
		Repository: newFakeRepository(),
		Logger:     slog.New(slog.NewTextHandler(logs, nil)),
		Mailer:     &fakeMailer{},
	})
	t.Cleanup(func() { _ = service.Close() })

	if service.secret == "" {
		t.Fatal("secret is empty, want an ephemeral key")
	}
	if !strings.Contains(logs.String(), "GOTHAM_SECRET_KEY is empty") {
		t.Errorf("log = %q, want the ephemeral-key warning", logs.String())
	}
	sealed, err := sealConfig(service.secret, ChannelConfig{WebhookURL: "https://discord.com/api/webhooks/1/abc"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := providers.OpenSecret("", sealed); err == nil {
		t.Error("the sealed config opens under an empty key")
	}
	if _, err := providers.OpenSecret(service.secret, sealed); err != nil {
		t.Errorf("OpenSecret(ephemeral) = %v, want a roundtrip", err)
	}
}

func TestDispatchRecordsDeliveryHistory(t *testing.T) {
	repo := newFakeRepository()
	healthyChannel := repo.seed(sampleChannel(t))
	brokenChannel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	healthy := newFakeNotifier()
	broken := newFakeNotifier()
	broken.err = errors.New("endpoint is down")
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{healthyChannel.ID: healthy, brokenChannel.ID: broken})

	service.Dispatch(sampleEvent())

	waitFor(t, "both history rows", func() bool {
		healthyRows := repo.deliveriesFor(healthyChannel.ID)
		brokenRows := repo.deliveriesFor(brokenChannel.ID)
		return len(healthyRows) == 1 && healthyRows[0].Status == DeliverySucceeded &&
			len(brokenRows) == 1 && brokenRows[0].Status == DeliveryRetrying
	})
	success := repo.deliveriesFor(healthyChannel.ID)[0]
	if success.Status != DeliverySucceeded || success.EventType != string(EventDeployFailure) || success.ErrorSummary != "" {
		t.Errorf("success row = %+v, want a successful deploy_failure with no summary", success)
	}
	failed := repo.deliveriesFor(brokenChannel.ID)[0]
	if failed.Status != DeliveryRetrying || failed.EventType != string(EventDeployFailure) || failed.ErrorSummary == "" {
		t.Errorf("failure row = %+v, want a retrying deploy_failure with a summary", failed)
	}
}

func TestTestChannelRecordsDelivery(t *testing.T) {
	userID := uuid.New()
	ctx := scopedCtx()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	if _, err := service.TestChannel(ctx, userID, channel.ID); err != nil {
		t.Fatalf("TestChannel: %v", err)
	}
	rows := repo.deliveriesFor(channel.ID)
	if len(rows) != 1 || rows[0].EventType != testEventType || rows[0].Status != DeliverySucceeded {
		t.Fatalf("history = %+v, want one successful test row", rows)
	}

	notifier.err = errors.New(strings.Repeat("x", maxDeliveryError*2))
	result, err := service.TestChannel(ctx, userID, channel.ID)
	if err != nil || result.OK {
		t.Fatalf("TestChannel(failing) = %+v, %v; want a failure result", result, err)
	}
	rows = repo.deliveriesFor(channel.ID)
	if len(rows) != 2 {
		t.Fatalf("history rows = %d, want 2", len(rows))
	}
	var failed *Delivery
	for i := range rows {
		if rows[i].Status == DeliveryFailed {
			failed = &rows[i]
		}
	}
	if failed == nil {
		t.Fatalf("history = %+v, want a failed row", rows)
	}
	if utf8.RuneCountInString(failed.ErrorSummary) > maxDeliveryError {
		t.Errorf("summary = %d runes, want it bounded at %d", utf8.RuneCountInString(failed.ErrorSummary), maxDeliveryError)
	}
}

func TestDeliveryHistoryIsBounded(t *testing.T) {
	repo := newFakeRepository()
	channelID := uuid.New()
	for i := 0; i < deliveryRetention+5; i++ {
		if err := repo.CreateDelivery(context.Background(), Delivery{
			ChannelID: channelID,
			EventType: testEventType,
			Status:    DeliverySucceeded,
			CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("CreateDelivery: %v", err)
		}
	}
	if got := len(repo.deliveriesFor(channelID)); got != deliveryRetention {
		t.Errorf("history = %d rows, want the %d cap", got, deliveryRetention)
	}
}

func TestTestEventIsClearlyLabelledAndNeverDispatched(t *testing.T) {
	event := Event{Kind: EventTest, Outcome: OutcomeSuccess, Name: "Deploy alerts", At: time.Now().UTC()}
	if got := event.Summary(); got != "Test notification: Deploy alerts" {
		t.Errorf("Summary = %q, want a test headline", got)
	}
	if key := event.Key(); key != "" {
		t.Errorf("Key = %q, want no subscription key", key)
	}
	if !strings.Contains(event.Message(), "Test notification: Deploy alerts") {
		t.Errorf("Message = %q, want the test headline", event.Message())
	}
}

func TestInboxRowIsWrittenWithoutChannels(t *testing.T) {
	repo := newFakeRepository()
	event := sampleEvent()
	projectID := uuid.New()
	repo.resourceProjects[event.ResourceID] = projectID
	service := newTestService(t, repo)

	service.Dispatch(event)

	waitFor(t, "the inbox row", func() bool {
		return len(repo.notificationsSnapshot()) == 1
	})
	row := repo.notificationsSnapshot()[0]
	if row.EventKey != EventDeployFailure {
		t.Errorf("event_key = %q, want %q", row.EventKey, EventDeployFailure)
	}
	if row.ProjectID != projectID {
		t.Errorf("project_id = %s, want %s", row.ProjectID, projectID)
	}
	if row.Name != event.Name || row.Host != event.Host || row.Error != event.Error {
		t.Errorf("row = %+v, want the event facts", row)
	}
	if !row.CreatedAt.Equal(event.At) {
		t.Errorf("created_at = %s, want %s", row.CreatedAt, event.At)
	}
}

func TestFailedInboxInsertStillDelivers(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	repo.createNotificationErr = errors.New("inbox insert failed")
	service := newTestService(t, repo)
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	service.Dispatch(sampleEvent())

	waitFor(t, "the channel delivery", func() bool {
		return len(notifier.delivered()) == 1
	})
	if rows := repo.notificationsSnapshot(); len(rows) != 0 {
		t.Fatalf("stored %d rows despite the failing insert", len(rows))
	}
}

func TestTestSendWritesNoInboxRow(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: newFakeNotifier()})

	if _, err := service.TestChannel(context.Background(), uuid.New(), channel.ID); err != nil {
		t.Fatalf("test channel: %v", err)
	}
	if rows := repo.notificationsSnapshot(); len(rows) != 0 {
		t.Fatalf("test send stored %d inbox rows, want none", len(rows))
	}
}

func persistPending(t *testing.T, repo *fakeRepository, event Event, channel Channel) Delivery {
	t.Helper()
	at := event.At
	if at.IsZero() {
		at = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	}
	delivery := Delivery{
		ID:            uuid.New(),
		ChannelID:     channel.ID,
		EventType:     string(event.Key()),
		Status:        DeliveryPending,
		CreatedAt:     at,
		DedupeKey:     event.Identity(),
		Payload:       event.encode(),
		IncidentID:    event.IncidentID,
		NextAttemptAt: at,
	}
	if err := repo.PersistDeliveries(context.Background(), []Delivery{delivery}); err != nil {
		t.Fatalf("persist delivery: %v", err)
	}
	return delivery
}

func TestTransientFailuresRetryThenTerminateBounded(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	service.retryBase = time.Millisecond
	notifier := newFakeNotifier()
	notifier.err = &sendFailure{message: "answered status 503"}
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	persistPending(t, repo, sampleEvent(), channel)
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for i := 0; i < maxDeliveryAttempts+2; i++ {
		service.processDeliveries()
		rows := repo.deliveriesFor(channel.ID)
		if len(rows) == 1 && rows[0].Status == DeliveryFailed {
			if rows[0].Attempts != maxDeliveryAttempts {
				t.Fatalf("attempts = %d, want %d", rows[0].Attempts, maxDeliveryAttempts)
			}
			return
		}
		// Advance the scheduled retry into the fixed test clock.
		for j := range repo.deliveries[channel.ID] {
			repo.deliveries[channel.ID][j].NextAttemptAt = at
		}
	}
	t.Fatalf("delivery did not terminate: %+v", repo.deliveriesFor(channel.ID))
}

func TestPermanentProviderFailureIsTerminalImmediately(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	notifier := newFakeNotifier()
	notifier.err = &sendFailure{message: "answered status 401", permanent: true}
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	persistPending(t, repo, sampleEvent(), channel)
	service.processDeliveries()
	rows := repo.deliveriesFor(channel.ID)
	if len(rows) != 1 || rows[0].Status != DeliveryFailed || rows[0].Attempts != 1 {
		t.Fatalf("rows = %+v, want one terminal attempt", rows)
	}
	if rows[0].CompletedAt == nil {
		t.Error("completed_at is not set on a terminal failure")
	}
}

func TestRetryScheduleHonoursBoundedRetryAfter(t *testing.T) {
	service := newTestService(t, newFakeRepository())
	if got := service.retryDelay(3, 2*time.Hour); got != maxRetryDelay {
		t.Errorf("retry delay = %s, want the %s cap", got, maxRetryDelay)
	}
	if got := service.retryDelay(3, 10*time.Second); got != 10*time.Second {
		t.Errorf("retry delay = %s, want the bounded Retry-After", got)
	}
	for attempts := 1; attempts <= maxDeliveryAttempts; attempts++ {
		got := service.retryDelay(attempts, 0)
		if got <= 0 || got > maxRetryDelay+maxRetryDelay/5 {
			t.Fatalf("attempt %d delay = %s, want jittered exponential backoff", attempts, got)
		}
	}
}

func TestDisabledChannelCancelsPendingDelivery(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	dispatched := sampleEvent()
	persistPending(t, repo, dispatched, channel)
	channel.Enabled = false
	repo.seed(channel)

	service.processDeliveries()
	if len(notifier.delivered()) != 0 {
		t.Fatal("a disabled channel was sent to")
	}
	rows := repo.deliveriesFor(channel.ID)
	if len(rows) != 1 || rows[0].Status != DeliveryCancelled || !strings.Contains(rows[0].ErrorSummary, "disabled") {
		t.Fatalf("rows = %+v, want a cancelled delivery with the reason", rows)
	}
}

func TestCancelledOpeningSuppressesStandaloneRecovery(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	notifier := newFakeNotifier()
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: notifier})

	incidentID := uuid.New()
	opening := Event{Kind: EventIncident, Action: "opened", IncidentID: incidentID, Severity: "critical", Name: "app", At: time.Date(2026, 9, 29, 11, 58, 0, 0, time.UTC)}
	recovery := Event{Kind: EventIncident, Action: "recovered", IncidentID: incidentID, Name: "app", At: time.Date(2026, 9, 29, 11, 59, 0, 0, time.UTC)}
	openingRow := persistPending(t, repo, opening, channel)
	if err := repo.CancelDelivery(context.Background(), openingRow.ID, "channel disabled or unsubscribed", time.Now().UTC()); err != nil {
		t.Fatalf("cancel opening: %v", err)
	}
	persistPending(t, repo, recovery, channel)

	service.processDeliveries()
	if len(notifier.delivered()) != 0 {
		t.Fatal("a recovery was sent although no opening reached the provider")
	}
	rows := repo.deliveriesFor(channel.ID)
	var recoveryRow *Delivery
	for i := range rows {
		if rows[i].EventType == string(EventIncidentRecovered) {
			recoveryRow = &rows[i]
		}
	}
	if recoveryRow == nil || recoveryRow.Status != DeliveryCancelled || !strings.Contains(recoveryRow.ErrorSummary, "opening") {
		t.Fatalf("recovery row = %+v, want a cancellation explaining the missing opening", recoveryRow)
	}
}

func TestDuplicateProducerDoesNotDuplicateInboxOrDelivery(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: newFakeNotifier()})

	event := sampleEvent()
	if err := service.Enqueue(event); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if err := service.Enqueue(event); err != nil {
		t.Fatalf("duplicate enqueue: %v", err)
	}
	if got := len(repo.notificationsSnapshot()); got != 1 {
		t.Errorf("inbox rows = %d, want one", got)
	}
	waitFor(t, "the delivery", func() bool {
		rows := repo.deliveriesFor(channel.ID)
		return len(rows) == 1 && rows[0].Status == DeliverySucceeded
	})
	if rows := repo.deliveriesFor(channel.ID); len(rows) != 1 {
		t.Errorf("delivery rows = %d, want one", len(rows))
	}
}

func TestIncidentTransitionOrderBlocksLaterRecoveryClaim(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	incidentID := uuid.New()
	opening := Event{Kind: EventIncident, Action: "opened", IncidentID: incidentID, Severity: "critical", Name: "app", At: time.Date(2026, 9, 29, 11, 58, 0, 0, time.UTC)}
	recovery := Event{Kind: EventIncident, Action: "recovered", IncidentID: incidentID, Name: "app", At: time.Date(2026, 9, 29, 11, 59, 0, 0, time.UTC)}
	openingRow := persistPending(t, repo, opening, channel)
	persistPending(t, repo, recovery, channel)
	repo.deliveriesFor(channel.ID)
	// Make the opening ineligible for this claim pass while it is still open work.
	for i := range repo.deliveries[channel.ID] {
		if repo.deliveries[channel.ID][i].ID == openingRow.ID {
			repo.deliveries[channel.ID][i].NextAttemptAt = time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC)
		}
	}

	at := time.Date(2026, 9, 29, 12, 5, 0, 0, time.UTC)
	claimed, err := repo.ClaimDeliveries(context.Background(), at, at.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	for _, delivery := range claimed {
		if delivery.EventType == string(EventIncidentRecovered) {
			t.Fatal("the recovery was claimed before its opening finished")
		}
	}
}

func TestDeliveryHealthAndFailuresAreOwnerOnly(t *testing.T) {
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	userID := uuid.New()
	if _, err := service.ListDeliveryFailures(scopedCtx(), userID); err != nil {
		t.Fatalf("owner ListDeliveryFailures: %v", err)
	}
	repo.nonOwners[userID] = true
	if _, err := service.ListDeliveryFailures(scopedCtx(), userID); !errors.Is(err, ErrForbidden) {
		t.Errorf("member ListDeliveryFailures err = %v, want ErrForbidden", err)
	}
	views, err := service.ListChannels(scopedCtx(), userID)
	if err != nil || len(views) != 1 {
		t.Fatalf("member ListChannels = %+v, %v", views, err)
	}
	if views[0].Health != nil {
		t.Errorf("health = %+v, want it hidden from a member", views[0].Health)
	}
	delete(repo.nonOwners, userID)
	views, err = service.ListChannels(scopedCtx(), userID)
	if err != nil || views[0].Health == nil {
		t.Fatalf("owner ListChannels health = %+v, %v", views[0].Health, err)
	}
	_ = channel
}
