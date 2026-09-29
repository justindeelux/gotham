package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// alwaysUser returns a UserIDFunc for one user.
func alwaysUser(id uuid.UUID) UserIDFunc {
	return func(context.Context) (uuid.UUID, bool) { return id, true }
}

// newRouteServer mounts the channel routes with an auth middleware that
// injects the given team scope, standing in for the server's RequireAuth +
// RequireTeam chain.
func newRouteServer(userID, teamID uuid.UUID, role teams.Role, svc NotificationService) http.Handler {
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope := teams.Scope{UserID: userID, TeamID: teamID, Role: role}
			next.ServeHTTP(w, r.WithContext(teams.WithScope(r.Context(), scope)))
		})
	}
	router := chi.NewRouter()
	Mount(router, auth, alwaysUser(userID), svc)
	return router
}

// doRequest runs one request against the router and returns the recorder.
func doRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// discordBody is a valid create payload whose secret must never come back.
func discordBody(secret string) string {
	return `{"name":"team alerts","kind":"discord","config":{"webhook_url":"` + secret + `"}}`
}

// decodeChannel decodes the envelope of a single-channel response.
func decodeChannel(t *testing.T, rec *httptest.ResponseRecorder) ChannelView {
	t.Helper()
	var envelope channelEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode channel %q: %v", rec.Body.String(), err)
	}
	return envelope.Channel
}

// TestChannelRoutesLifecycleAndRedaction walks create, read, update and delete
// and asserts the secret never appears in a response.
func TestChannelRoutesLifecycleAndRedaction(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	service := newTestService(t, repo)
	handler := newRouteServer(userID, teamID, teams.RoleOwner, service)
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

// TestChannelRouteValidation pins the 400 mapping for malformed requests.
func TestChannelRouteValidation(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	service := newTestService(t, newFakeRepository())
	handler := newRouteServer(userID, teamID, teams.RoleOwner, service)

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

// TestChannelRouteReadOnlyCannotWrite proves the read_only role reads but
// every mutation is refused with 403.
func TestChannelRouteReadOnlyCannotWrite(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t, teamID))
	service := newTestService(t, repo)
	handler := newRouteServer(userID, teamID, teams.RoleReadOnly, service)

	if rec := doRequest(handler, http.MethodGet, "/v1/notification-channels", ""); rec.Code != http.StatusOK {
		t.Fatalf("read_only list = %d, want 200", rec.Code)
	}
	if rec := doRequest(handler, http.MethodPost, "/v1/notification-channels", discordBody("https://discord.com/api/webhooks/1/a")); rec.Code != http.StatusForbidden {
		t.Fatalf("read_only create = %d, want 403", rec.Code)
	}
	if rec := doRequest(handler, http.MethodPatch, "/v1/notification-channels/"+channel.ID.String(), `{"name":"x"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("read_only update = %d, want 403", rec.Code)
	}
	if rec := doRequest(handler, http.MethodDelete, "/v1/notification-channels/"+channel.ID.String(), ""); rec.Code != http.StatusForbidden {
		t.Fatalf("read_only delete = %d, want 403", rec.Code)
	}
	if rec := doRequest(handler, http.MethodPost, "/v1/notification-channels/"+channel.ID.String()+"/test", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("read_only test = %d, want 403", rec.Code)
	}
}

// TestChannelRouteForeignTeamIsNotFound proves a channel of another team is a
// 404 for every read and mutation.
func TestChannelRouteForeignTeamIsNotFound(t *testing.T) {
	ownerID, teamA := uuid.New(), uuid.New()
	strangerID, teamB := uuid.New(), uuid.New()
	repo := newFakeRepository()
	channel := repo.seed(sampleChannel(t, teamA))
	service := newTestService(t, repo)
	handler := newRouteServer(strangerID, teamB, teams.RoleOwner, service)
	path := "/v1/notification-channels/" + channel.ID.String()

	if rec := doRequest(handler, http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign get = %d, want 404", rec.Code)
	}
	if rec := doRequest(handler, http.MethodPatch, path, `{"name":"stolen"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign update = %d, want 404", rec.Code)
	}
	if rec := doRequest(handler, http.MethodDelete, path, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign delete = %d, want 404", rec.Code)
	}
	if rec := doRequest(handler, http.MethodPost, path+"/test", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign test = %d, want 404", rec.Code)
	}
	list := doRequest(handler, http.MethodGet, "/v1/notification-channels", "")
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), channel.ID.String()) {
		t.Fatalf("foreign list = %d %s, want an empty own-team list", list.Code, list.Body.String())
	}

	// The owner is unaffected.
	ownerHandler := newRouteServer(ownerID, teamA, teams.RoleOwner, service)
	if rec := doRequest(ownerHandler, http.MethodGet, path, ""); rec.Code != http.StatusOK {
		t.Fatalf("owner get = %d, want 200", rec.Code)
	}
}

// TestChannelRouteEmptyListIsArray proves an empty list serialises as [].
func TestChannelRouteEmptyListIsArray(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	service := newTestService(t, newFakeRepository())
	handler := newRouteServer(userID, teamID, teams.RoleOwner, service)

	rec := doRequest(handler, http.MethodGet, "/v1/notification-channels", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"channels":[]}` {
		t.Errorf("body = %s, want an empty array", body)
	}
}

// TestChannelRouteTestDeliversMail proves the test endpoint sends through the
// configured mailer and reports the result without secrets.
func TestChannelRouteTestDeliversMail(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	mailer := &fakeMailer{}
	sealed, err := sealConfig(testSecret, ChannelConfig{
		Host: "smtp.gotham.dev", Port: 587, Username: "ops@gotham.dev", Password: "smtp-secret",
		From: "ops@gotham.dev", To: []string{"oncall@gotham.dev"},
	})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	channel := repo.seed(Channel{
		ID: uuid.New(), TeamID: teamID, Name: "email", Kind: KindEmail,
		Enabled: true, Events: AllEvents, SealedConfig: sealed,
	})
	service := NewService(Config{
		Repository: repo, Secret: testSecret, Logger: discardLogger(), Mailer: mailer, SendTimeout: 2 * time.Second,
	})
	t.Cleanup(func() { _ = service.Close() })
	handler := newRouteServer(userID, teamID, teams.RoleOwner, service)

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

// TestChannelRouteTestHidesTransportSecret proves the authenticated send-test
// response never carries the endpoint credential.
func TestChannelRouteTestHidesTransportSecret(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	server, _ := newWebhookServer(t, http.StatusOK)
	endpoint := server.URL + "/webhook/route-secret-token"
	server.Close() // connections are refused

	repo := newFakeRepository()
	sealed, err := sealConfig(testSecret, ChannelConfig{WebhookURL: endpoint})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	channel := repo.seed(Channel{
		ID: uuid.New(), TeamID: teamID, Name: "discord", Kind: KindDiscord,
		Enabled: true, Events: AllEvents, SealedConfig: sealed,
	})
	service := newTestService(t, repo)
	handler := newRouteServer(userID, teamID, teams.RoleOwner, service)

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

// TestChannelRouteMaskedConfigPatch proves a UI round-trip (GET, then PATCH
// with the returned config) cannot wipe the stored secret.
func TestChannelRouteMaskedConfigPatch(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	service := newTestService(t, repo)
	handler := newRouteServer(userID, teamID, teams.RoleOwner, service)
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

// TestChannelRoutesUnmountWhenDisabled proves the feature flag and a nil
// service mount no routes.
func TestChannelRoutesUnmountWhenDisabled(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	t.Setenv(FeatureEnv, "false")
	service := newTestService(t, newFakeRepository())
	handler := newRouteServer(userID, teamID, teams.RoleOwner, service)

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
