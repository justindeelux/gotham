package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func alwaysUser(id uuid.UUID) UserIDFunc {
	return func(context.Context) (uuid.UUID, bool) { return id, true }
}

func newRouteServer(userID uuid.UUID, svc NotificationService) http.Handler {
	auth := func(next http.Handler) http.Handler {
		return next
	}
	router := chi.NewRouter()
	Mount(router, auth, alwaysUser(userID), svc)
	return router
}

func doRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func discordBody(secret string) string {
	return `{"name":"alerts","kind":"discord","config":{"webhook_url":"` + secret + `"}}`
}

func decodeChannel(t *testing.T, rec *httptest.ResponseRecorder) ChannelView {
	t.Helper()
	var envelope channelEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode channel %q: %v", rec.Body.String(), err)
	}
	return envelope.Channel
}

func TestChannelRoutesLifecycleAndRedaction(t *testing.T) {
	userID := uuid.New()
	repo := newFakeRepository()
	service := newTestService(t, repo)
	handler := newRouteServer(userID, service)
	const secret = "https://discord.com/api/webhooks/1184/8f2c-secret-value"

	rec := doRequest(handler, http.MethodPost, "/v1/notification-channels", discordBody(secret))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d (body %s), want 201", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "8f2c-secret-value") {
		t.Fatalf("create response leaks the webhook: %s", rec.Body.String())
	}
	created := decodeChannel(t, rec)
	if created.Kind != KindDiscord || !created.SecretsConfigured || !created.Enabled {
		t.Errorf("created = %+v, want an enabled discord channel with a stored secret", created)
	}
	if created.Config.WebhookURL == secret {
		t.Error("config.webhook_url was returned in full")
	}

	rec = doRequest(handler, http.MethodGet, "/v1/notification-channels/"+created.ID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "8f2c-secret-value") {
		t.Fatalf("get response leaks the webhook: %s", rec.Body.String())
	}

	rec = doRequest(handler, http.MethodGet, "/v1/notification-channels", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "8f2c-secret-value") {
		t.Fatalf("list response leaks the webhook: %s", rec.Body.String())
	}

	rec = doRequest(handler, http.MethodPatch, "/v1/notification-channels/"+created.ID.String(), `{"name":"renamed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d (body %s)", rec.Code, rec.Body.String())
	}
	if updated := decodeChannel(t, rec); updated.Name != "renamed" {
		t.Errorf("name = %q, want renamed", updated.Name)
	}

	rec = doRequest(handler, http.MethodDelete, "/v1/notification-channels/"+created.ID.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", rec.Code)
	}
	rec = doRequest(handler, http.MethodGet, "/v1/notification-channels/"+created.ID.String(), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete = %d, want 404", rec.Code)
	}
}

func TestChannelRouteValidation(t *testing.T) {
	userID := uuid.New()
	service := newTestService(t, newFakeRepository())
	handler := newRouteServer(userID, service)

	cases := map[string]string{
		"empty body":      "",
		"unknown kind":    `{"name":"x","kind":"mattermost","config":{"webhook_url":"https://discord.com/api/webhooks/1/a"}}`,
		"missing webhook": `{"name":"x","kind":"discord","config":{}}`,
		"unknown field":   `{"name":"x","kind":"discord","unexpected":true}`,
		"empty events":    `{"name":"x","kind":"discord","events":[],"config":{"webhook_url":"https://discord.com/api/webhooks/1/a"}}`,
		"unknown event":   `{"name":"x","kind":"discord","events":["deploy_exploded"],"config":{"webhook_url":"https://discord.com/api/webhooks/1/a"}}`,
		"bad resource id": `{"name":"x","kind":"discord","resource_type":"application","resource_id":"nope","config":{"webhook_url":"https://discord.com/api/webhooks/1/a"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := doRequest(handler, http.MethodPost, "/v1/notification-channels", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d (body %s), want 400", rec.Code, rec.Body.String())
			}
		})
	}

	rec := doRequest(handler, http.MethodGet, "/v1/notification-channels/not-a-uuid", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id = %d, want 400", rec.Code)
	}
}

func TestChannelRouteEmptyListIsArray(t *testing.T) {
	userID := uuid.New()
	service := newTestService(t, newFakeRepository())
	handler := newRouteServer(userID, service)

	rec := doRequest(handler, http.MethodGet, "/v1/notification-channels", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"channels":[]}` {
		t.Errorf("body = %s, want an empty array", body)
	}
}

func TestChannelRouteTestDeliversMail(t *testing.T) {
	userID := uuid.New()
	repo := newFakeRepository()
	mailer := &fakeMailer{}
	sealed, err := sealConfig(testSecret, ChannelConfig{
		Host: "smtp.gotham.dev", Port: 587, Username: "ops@gotham.dev", Password: "smtp-secret",
		From: "ops@gotham.dev", To: []string{"oncall@gotham.dev"}, AllowPrivate: true,
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	channel := repo.seed(Channel{
		ID: uuid.New(), Name: "email", Kind: KindEmail,
		Enabled: true, Events: AllEvents, SealedConfig: sealed,
	})
	service := NewService(Config{
		Repository: repo, Secret: testSecret, Logger: discardLogger(), Mailer: mailer, SendTimeout: 2 * time.Second,
	})
	t.Cleanup(func() { _ = service.Close() })
	handler := newRouteServer(userID, service)

	rec := doRequest(handler, http.MethodPost, "/v1/notification-channels/"+channel.ID.String()+"/test", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("test = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	var envelope testEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode test result: %v", err)
	}
	if !envelope.Check.OK {
		t.Errorf("check = %+v, want OK", envelope.Check)
	}
	if strings.Contains(rec.Body.String(), "smtp-secret") {
		t.Fatalf("test response leaks the SMTP password: %s", rec.Body.String())
	}
	if len(mailer.delivered()) != 1 {
		t.Errorf("mails = %d, want 1", len(mailer.delivered()))
	}
}

func TestChannelRouteTestHidesTransportSecret(t *testing.T) {
	userID := uuid.New()
	server, _ := newWebhookServer(t, http.StatusOK)
	endpoint := server.URL + "/webhook/route-secret-token"
	server.Close()

	repo := newFakeRepository()
	sealed, err := sealConfig(testSecret, ChannelConfig{WebhookURL: endpoint, AllowPrivate: true})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	channel := repo.seed(Channel{
		ID: uuid.New(), Name: "discord", Kind: KindDiscord,
		Enabled: true, Events: AllEvents, SealedConfig: sealed,
	})
	service := newTestService(t, repo)
	handler := newRouteServer(userID, service)

	rec := doRequest(handler, http.MethodPost, "/v1/notification-channels/"+channel.ID.String()+"/test", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("test = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "route-secret-token") {
		t.Fatalf("test response leaks the webhook: %s", rec.Body.String())
	}
	var envelope testEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode test result: %v", err)
	}
	if envelope.Check.OK {
		t.Errorf("check = %+v, want a failed delivery", envelope.Check)
	}
	if !strings.Contains(envelope.Check.Message, "connection failed") {
		t.Errorf("message = %q, want the transport category", envelope.Check.Message)
	}
}

func TestChannelRouteMaskedConfigPatch(t *testing.T) {
	userID := uuid.New()
	repo := newFakeRepository()
	service := newTestService(t, repo)
	handler := newRouteServer(userID, service)
	const webhook = "https://discord.com/api/webhooks/1184/8f2c-secret-value"

	rec := doRequest(handler, http.MethodPost, "/v1/notification-channels", discordBody(webhook))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d (body %s)", rec.Code, rec.Body.String())
	}
	created := decodeChannel(t, rec)

	patch, err := json.Marshal(map[string]any{"name": "renamed", "config": created.Config})
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	rec = doRequest(handler, http.MethodPatch, "/v1/notification-channels/"+created.ID.String(), string(patch))
	if rec.Code != http.StatusOK {
		t.Fatalf("masked patch = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	stored, ok := repo.get(created.ID)
	if !ok {
		t.Fatal("the channel disappeared")
	}
	opened, err := openConfig(testSecret, stored.SealedConfig)
	if err != nil {
		t.Fatalf("openConfig: %v", err)
	}
	if opened.WebhookURL != webhook {
		t.Errorf("webhook = %q, want the stored one after a masked resend", opened.WebhookURL)
	}
}

func TestChannelRouteTestHidesStatusReasonPhrase(t *testing.T) {
	userID := uuid.New()
	server := newReflectingStatusServer(t, http.StatusInternalServerError)
	repo := newFakeRepository()
	sealed, err := sealConfig(testSecret, ChannelConfig{WebhookURL: server.URL + "/webhook/route-reason-secret", AllowPrivate: true})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	channel := repo.seed(Channel{
		ID: uuid.New(), Name: "discord", Kind: KindDiscord,
		Enabled: true, Events: AllEvents, SealedConfig: sealed,
	})
	service := newTestService(t, repo)
	handler := newRouteServer(userID, service)

	rec := doRequest(handler, http.MethodPost, "/v1/notification-channels/"+channel.ID.String()+"/test", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("test = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "route-reason-secret") {
		t.Fatalf("test response leaks the webhook: %s", rec.Body.String())
	}
	var envelope testEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode test result: %v", err)
	}
	if envelope.Check.OK {
		t.Errorf("check = %+v, want a failed delivery", envelope.Check)
	}
	if !strings.Contains(envelope.Check.Message, "status 500") {
		t.Errorf("message = %q, want the numeric status", envelope.Check.Message)
	}
}

func TestChannelRoutesUnmountWhenDisabled(t *testing.T) {
	userID := uuid.New()
	t.Setenv(FeatureEnv, "false")
	service := newTestService(t, newFakeRepository())
	handler := newRouteServer(userID, service)

	if rec := doRequest(handler, http.MethodGet, "/v1/notification-channels", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("flag off list = %d, want 404", rec.Code)
	}

	t.Setenv(FeatureEnv, "true")
	nilHandler := chi.NewRouter()
	Mount(nilHandler, func(next http.Handler) http.Handler { return next }, alwaysUser(userID), nil)
	if rec := doRequest(nilHandler, http.MethodGet, "/v1/notification-channels", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("nil service list = %d, want 404", rec.Code)
	}
}

func TestChannelRouteDeliveriesHistory(t *testing.T) {
	userID := uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	empty := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	if err := repo.CreateDelivery(context.Background(), Delivery{
		ID:           uuid.New(),
		ChannelID:    channel.ID,
		EventType:    testEventType,
		Status:       DeliveryFailed,
		ErrorSummary: "connection failed",
		CreatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateDelivery: %v", err)
	}

	handler := newRouteServer(userID, service)
	rec := doRequest(handler, http.MethodGet, "/v1/notification-channels/"+channel.ID.String()+"/deliveries", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("deliveries = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "8f2c-secret") {
		t.Fatalf("history response leaks the webhook: %s", rec.Body.String())
	}
	var envelope deliveryListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode deliveries %q: %v", rec.Body.String(), err)
	}
	if len(envelope.Deliveries) != 1 || envelope.Deliveries[0].ErrorSummary != "connection failed" {
		t.Errorf("deliveries = %+v, want the recorded attempt", envelope.Deliveries)
	}

	emptyRec := doRequest(handler, http.MethodGet, "/v1/notification-channels/"+empty.ID.String()+"/deliveries", "")
	if body := strings.TrimSpace(emptyRec.Body.String()); body != `{"deliveries":[]}` {
		t.Errorf("empty history = %s, want an empty array", body)
	}
}

func TestNotificationInboxLimitsAndSeenMarker(t *testing.T) {
	userID := uuid.New()
	repo := newFakeRepository()
	service := newTestService(t, repo)
	handler := newRouteServer(userID, service)
	base := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	for i := 0; i < 40; i++ {
		if _, err := repo.CreateNotification(context.Background(), Notification{
			EventKey:  EventDeploySuccess,
			Name:      fmt.Sprintf("app-%d", i),
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("seed notification: %v", err)
		}
	}

	decodePage := func(rec *httptest.ResponseRecorder) NotificationPage {
		t.Helper()
		var page NotificationPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode page %q: %v", rec.Body.String(), err)
		}
		return page
	}

	rec := doRequest(handler, http.MethodGet, "/v1/notifications", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("inbox = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	page := decodePage(rec)
	if len(page.Notifications) != defaultNotificationLimit {
		t.Errorf("default page = %d rows, want %d", len(page.Notifications), defaultNotificationLimit)
	}
	if page.UnreadCount != 40 {
		t.Errorf("unread_count = %d, want 40", page.UnreadCount)
	}
	if got := page.Notifications[0].Name; got != "app-39" {
		t.Errorf("first row = %q, want the newest", got)
	}

	rec = doRequest(handler, http.MethodGet, "/v1/notifications?limit=2", "")
	if rec.Code != http.StatusOK || len(decodePage(rec).Notifications) != 2 {
		t.Fatalf("limit=2 = %d (body %s), want 2 rows", rec.Code, rec.Body.String())
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=abc"} {
		rec = doRequest(handler, http.MethodGet, "/v1/notifications?"+query, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", query, rec.Code)
		}
	}

	rec = doRequest(handler, http.MethodPost, "/v1/notifications/seen", `{"seen_at":"2026-09-29T11:30:00Z"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("seen = %d (body %s), want 204", rec.Code, rec.Body.String())
	}
	rec = doRequest(handler, http.MethodGet, "/v1/notifications", "")
	if got := decodePage(rec).UnreadCount; got != 9 {
		t.Errorf("unread_count after seen = %d, want 9", got)
	}

	rec = doRequest(handler, http.MethodPost, "/v1/notifications/seen", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("seen without body = %d (body %s), want 204", rec.Code, rec.Body.String())
	}
	rec = doRequest(handler, http.MethodGet, "/v1/notifications", "")
	if got := decodePage(rec).UnreadCount; got != 0 {
		t.Errorf("unread_count after an empty seen body = %d, want 0", got)
	}

	for _, body := range []string{`{"seen_at":"yesterday"}`, `{"seen_at":42}`, `{"seen_at":"2026-09-29T11:30:00Z","extra":true}`} {
		rec = doRequest(handler, http.MethodPost, "/v1/notifications/seen", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("seen %s = %d, want 400", body, rec.Code)
		}
	}
}

func TestDeliveryFailureRoutesAreOwnerOnly(t *testing.T) {
	userID := uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t))
	service := newTestService(t, repo)
	setFactory(t, service, map[uuid.UUID]*fakeNotifier{channel.ID: newFakeNotifier()})

	deliveryID := uuid.New()
	if err := repo.PersistDeliveries(context.Background(), []Delivery{{
		ID: deliveryID, ChannelID: channel.ID, EventType: string(EventDeployFailure),
		Status: DeliveryFailed, ErrorSummary: "answered status 500", Payload: sampleEvent().encode(),
	}}); err != nil {
		t.Fatalf("seed terminal delivery: %v", err)
	}
	handler := newRouteServer(userID, service)

	rec := doRequest(handler, http.MethodGet, "/v1/notification-channels/failures", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "answered status 500") {
		t.Fatalf("owner failures = %d (body %s), want the retained failure", rec.Code, rec.Body.String())
	}
	rec = doRequest(handler, http.MethodPost, "/v1/notification-channels/deliveries/"+deliveryID.String()+"/retry", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("owner retry = %d (body %s), want 204", rec.Code, rec.Body.String())
	}

	repo.nonOwners[userID] = true
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/v1/notification-channels/failures"},
		{http.MethodPost, "/v1/notification-channels/deliveries/" + deliveryID.String() + "/retry"},
	} {
		rec = doRequest(handler, tc.method, tc.path, "")
		if rec.Code != http.StatusForbidden {
			t.Errorf("member %s %s = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
}
