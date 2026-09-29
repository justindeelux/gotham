package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// webhookCall captures one request a mock webhook received. The mutex keeps
// the asynchronous-delivery tests race-free.
type webhookCall struct {
	mu          sync.Mutex
	method      string
	path        string
	contentType string
	body        []byte
}

// snapshot reads the recorded request under the lock.
func (c *webhookCall) snapshot() (method, path, contentType string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.method, c.path, c.contentType, append([]byte(nil), c.body...)
}

// newWebhookServer starts a mock endpoint answering status and recording the
// request.
func newWebhookServer(t *testing.T, status int) (*httptest.Server, *webhookCall) {
	t.Helper()
	call := &webhookCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		call.mu.Lock()
		call.method = r.Method
		call.path = r.URL.Path
		call.contentType = r.Header.Get("Content-Type")
		call.body = body
		call.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
		}
	}))
	t.Cleanup(server.Close)
	return server, call
}

// fixedEvent is the event every notifier test formats.
func fixedEvent() Event {
	return Event{
		Kind:         EventDeploy,
		Outcome:      OutcomeFailure,
		ResourceType: ResourceApplication,
		Name:         "shop-web",
		Host:         "shop.example.com",
		Error:        "build step failed: exit status 1",
		At:           time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	}
}

func TestDiscordNotifierPostsContent(t *testing.T) {
	server, call := newWebhookServer(t, http.StatusOK)
	notifier := newDiscordNotifier(server.URL+"/webhook", server.Client())

	if err := notifier.Notify(context.Background(), fixedEvent()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	method, path, contentType, body := call.snapshot()
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
	if path != "/webhook" {
		t.Errorf("path = %q, want /webhook", path)
	}
	want := `{"content":` + mustJSONString(t, fixedEvent().Message()) + `}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestSlackNotifierPostsText(t *testing.T) {
	server, call := newWebhookServer(t, http.StatusOK)
	notifier := newSlackNotifier(server.URL+"/services/T04/B07", server.Client())

	if err := notifier.Notify(context.Background(), fixedEvent()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	method, _, _, body := call.snapshot()
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	want := `{"text":` + mustJSONString(t, fixedEvent().Message()) + `}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestTelegramNotifierPostsMessage(t *testing.T) {
	server, call := newWebhookServer(t, http.StatusOK)
	notifier := newTelegramNotifier("7184:AAH-test-token", "-1002184", server.URL, server.Client())

	if err := notifier.Notify(context.Background(), fixedEvent()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	method, path, _, body := call.snapshot()
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	if want := "/bot7184:AAH-test-token/sendMessage"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	want := `{"chat_id":"-1002184","text":` + mustJSONString(t, fixedEvent().Message()) + `}`
	if string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

// TestTelegramNotifierHidesTokenOnFailure proves a failed delivery never puts
// the bot token (which lives in the URL path) into the error text.
func TestTelegramNotifierHidesTokenOnFailure(t *testing.T) {
	server, _ := newWebhookServer(t, http.StatusInternalServerError)
	const token = "7184:AAH-super-secret-token"
	notifier := newTelegramNotifier(token, "-1002184", server.URL, server.Client())

	err := notifier.Notify(context.Background(), fixedEvent())
	if err == nil {
		t.Fatal("Notify = nil error, want a failure for a 500 answer")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error %q leaks the bot token", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want the status", err)
	}
}

func TestEmailNotifierBuildsMail(t *testing.T) {
	mailer := &fakeMailer{}
	notifier := newEmailNotifier(mailer, ChannelConfig{
		Host:     "smtp.gotham.dev",
		Port:     587,
		Username: "ops@gotham.dev",
		Password: "smtp-pass",
		From:     "ops@gotham.dev",
		To:       []string{"oncall@gotham.dev", "team@gotham.dev"},
	})

	if err := notifier.Notify(context.Background(), fixedEvent()); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	mails := mailer.delivered()
	if len(mails) != 1 {
		t.Fatalf("mails = %d, want 1", len(mails))
	}
	mail := mails[0]
	if mail.Host != "smtp.gotham.dev" || mail.Port != 587 {
		t.Errorf("transport = %s:%d, want smtp.gotham.dev:587", mail.Host, mail.Port)
	}
	if mail.Username != "ops@gotham.dev" || mail.Password != "smtp-pass" {
		t.Errorf("credentials = %q/%q, want the SMTP login", mail.Username, mail.Password)
	}
	if mail.From != "ops@gotham.dev" {
		t.Errorf("from = %q", mail.From)
	}
	if strings.Join(mail.To, ",") != "oncall@gotham.dev,team@gotham.dev" {
		t.Errorf("to = %v", mail.To)
	}
	if want := "[Gotham] Deploy failed: shop-web"; mail.Subject != want {
		t.Errorf("subject = %q, want %q", mail.Subject, want)
	}
	if !strings.Contains(mail.Body, "Error: build step failed: exit status 1") {
		t.Errorf("body = %q, want the failure text", mail.Body)
	}
}

// TestNotifierFailureIsReported proves a non-2xx answer is an error carrying
// the status but not the URL.
func TestNotifierFailureIsReported(t *testing.T) {
	server, _ := newWebhookServer(t, http.StatusBadRequest)
	notifier := newDiscordNotifier(server.URL+"/webhook/secret-token", server.Client())

	err := notifier.Notify(context.Background(), fixedEvent())
	if err == nil {
		t.Fatal("Notify = nil error, want a failure")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %q, want the status", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Errorf("error %q leaks the webhook URL", err)
	}
}

// TestRedactedConfigMasksSecrets pins the read DTO rule: secret fields are
// masked to their last four characters and can never round-trip.
func TestRedactedConfigMasksSecrets(t *testing.T) {
	config := ChannelConfig{
		WebhookURL: "https://discord.com/api/webhooks/1184/8f2c-secret-value",
		BotToken:   "7184:AAH-token-value",
		ChatID:     "-1002184",
		Host:       "smtp.gotham.dev",
		Port:       587,
		Username:   "ops@gotham.dev",
		Password:   "smtp-password",
		From:       "ops@gotham.dev",
		To:         []string{"oncall@gotham.dev"},
	}

	redacted := config.redacted()
	if redacted.WebhookURL == config.WebhookURL || redacted.BotToken == config.BotToken || redacted.Password == config.Password {
		t.Fatal("a secret was returned unchanged")
	}
	if !strings.HasSuffix(redacted.WebhookURL, "alue") || !strings.HasSuffix(redacted.BotToken, "alue") {
		t.Errorf("masked secrets = %q / %q, want the last four characters", redacted.WebhookURL, redacted.BotToken)
	}
	if redacted.ChatID != config.ChatID || redacted.Host != config.Host || redacted.Port != config.Port {
		t.Error("non-secret fields must pass through the redaction")
	}
	if !config.hasSecrets() {
		t.Error("hasSecrets = false, want true")
	}
	if (ChannelConfig{Host: "smtp.gotham.dev"}).hasSecrets() {
		t.Error("hasSecrets = true without any secret field")
	}
}

// TestRenderMessageFramesHeaders proves the SMTP document is CRLF-framed and
// header injection through the subject is neutralized.
func TestRenderMessageFramesHeaders(t *testing.T) {
	raw := string(renderMessage(Mail{
		From:    "ops@gotham.dev",
		To:      []string{"oncall@gotham.dev"},
		Subject: "hello\r\nX-Injected: yes",
		Body:    "line one\nline two",
	}))
	if !strings.HasPrefix(raw, "From: ops@gotham.dev\r\n") {
		t.Errorf("document = %q, want the From header first", raw)
	}
	if strings.Contains(raw, "\r\nX-Injected:") {
		t.Errorf("document = %q, the subject injected a header", raw)
	}
	if !strings.Contains(raw, "\r\n\r\nline one\r\nline two") {
		t.Errorf("document = %q, want a CRLF body", raw)
	}
}

// mustJSONString encodes s as a JSON string, including the quotes.
func mustJSONString(t *testing.T, s string) string {
	t.Helper()
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal %q: %v", s, err)
	}
	return string(encoded)
}
