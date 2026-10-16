package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// This file verifies each sender against the endpoint contract the real
// provider publishes (status codes, 429 + Retry-After, error bodies,
// per-transport content limits). It cannot reach Slack, Discord or Telegram;
// the fakes below mirror the documented shapes, and the PR states exactly what
// remains unverified against the live services.

// providerServer starts a server whose handler both records the request and
// answers with a fixed status/headers/body.
func providerServer(t *testing.T, status int, headers map[string]string, body string) (*httptest.Server, *webhookCall) {
	t.Helper()
	call := &webhookCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		call.mu.Lock()
		call.method = r.Method
		call.path = r.URL.Path
		call.contentType = r.Header.Get("Content-Type")
		call.body = payload
		call.mu.Unlock()
		for key, value := range headers {
			w.Header().Set(key, value)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, call
}

// TestDiscordAccepts204 proves Discord's real success code (204 No Content) is
// a success.
func TestDiscordAccepts204(t *testing.T) {
	server, _ := providerServer(t, http.StatusNoContent, nil, "")
	notifier := newDiscordNotifier(server.URL+"/api/webhooks/1/abc", server.Client())
	if err := notifier.Notify(context.Background(), fixedEvent()); err != nil {
		t.Fatalf("Notify(204) = %v, want nil", err)
	}
}

// TestSlackAcceptsOK proves Slack's plain-text "ok" 200 is a success and the
// payload keeps the documented {"text": ...} shape.
func TestSlackAcceptsOK(t *testing.T) {
	server, call := providerServer(t, http.StatusOK, nil, "ok")
	notifier := newSlackNotifier(server.URL+"/services/T04/B07/secret", server.Client())
	if err := notifier.Notify(context.Background(), fixedEvent()); err != nil {
		t.Fatalf("Notify(ok) = %v, want nil", err)
	}
	_, _, _, body := call.snapshot()
	if !strings.HasPrefix(string(body), `{"text":"`) {
		t.Errorf("body = %s, want the Slack text shape", body)
	}
}

// TestTelegramAcceptsOK proves the Bot API's {"ok":true} 200 is a success.
func TestTelegramAcceptsOK(t *testing.T) {
	server, _ := providerServer(t, http.StatusOK, nil, `{"ok":true,"result":{"message_id":1}}`)
	notifier := newTelegramNotifier("7184:AAH-token", "-100", server.URL, server.Client())
	if err := notifier.Notify(context.Background(), fixedEvent()); err != nil {
		t.Fatalf("Notify(ok:true) = %v, want nil", err)
	}
}

// TestTelegramErrorBodyIsNotEchoed proves a Bot API error ({ok:false,
// description:...}) is reported by status only: the token in the URL path and
// the remote description must never reach the error.
func TestTelegramErrorBodyIsNotEchoed(t *testing.T) {
	const token = "7184:AAH-contract-secret-token"
	server, _ := providerServer(t, http.StatusBadRequest, nil, `{"ok":false,"description":"chat 7184:AAH-contract-secret-token not found"}`)
	notifier := newTelegramNotifier(token, "-100", server.URL, server.Client())

	err := notifier.Notify(context.Background(), fixedEvent())
	if err == nil {
		t.Fatal("Notify(400) = nil, want a failure")
	}
	assertNoSecret(t, err, token, "not found", "description")
	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("error = %q, want the numeric status", err)
	}
}

// TestRateLimitReportsRetryAfter proves a 429 surfaces the parsed delta-seconds
// hint without echoing the header's raw form or the endpoint credential.
func TestRateLimitReportsRetryAfter(t *testing.T) {
	server, _ := providerServer(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "30"}, "slow down")
	notifier := newSlackNotifier(server.URL+"/services/T04/ratelimit-secret", server.Client())

	err := notifier.Notify(context.Background(), fixedEvent())
	if err == nil {
		t.Fatal("Notify(429) = nil, want a rate-limit failure")
	}
	assertNoSecret(t, err, "ratelimit-secret", "slow down")
	if !strings.Contains(err.Error(), "rate limited") || !strings.Contains(err.Error(), "retry after 30s") {
		t.Errorf("error = %q, want a rate-limit hint with the retry delay", err)
	}
}

// TestRateLimitWithoutRetryAfter proves the hint degrades cleanly when the
// header is absent or not a delta-seconds number (an HTTP-date is ignored).
func TestRateLimitWithoutRetryAfter(t *testing.T) {
	for name, header := range map[string]string{
		"absent":      "",
		"http date":   "Wed, 21 Oct 2026 07:28:00 GMT",
		"negative":    "-5",
		"non-numeric": "soon",
	} {
		t.Run(name, func(t *testing.T) {
			headers := map[string]string{}
			if header != "" {
				headers["Retry-After"] = header
			}
			server, _ := providerServer(t, http.StatusTooManyRequests, headers, "")
			notifier := newDiscordNotifier(server.URL+"/api/webhooks/rl-secret", server.Client())

			err := notifier.Notify(context.Background(), fixedEvent())
			if err == nil {
				t.Fatal("Notify(429) = nil, want a failure")
			}
			assertNoSecret(t, err, "rl-secret")
			if !strings.Contains(err.Error(), "rate limited") {
				t.Errorf("error = %q, want the rate-limit category", err)
			}
			if strings.Contains(err.Error(), "GMT") || strings.Contains(err.Error(), "soon") {
				t.Errorf("error = %q echoed the remote header", err)
			}
		})
	}
}

// TestTransportsTruncateAtTheirLimit proves an over-long message is cut to each
// transport's own limit, so a pathological resource name cannot make a provider
// reject the payload. The cap includes the ellipsis and is rune-safe: the input
// is astral (2 UTF-16 units per rune), so a byte or unit slip shows up.
func TestTransportsTruncateAtTheirLimit(t *testing.T) {
	event := fixedEvent()
	event.Name = strings.Repeat("🚀", 3000) // astral runes: pushes Message past every limit

	cases := map[string]struct {
		limit    int
		notifier func(url string) Notifier
	}{
		"discord": {discordContentLimit, func(url string) Notifier { return newDiscordNotifier(url, http.DefaultClient) }},
		"slack":   {slackTextLimit, func(url string) Notifier { return newSlackNotifier(url, http.DefaultClient) }},
		"telegram": {telegramTextLimit, func(url string) Notifier {
			return newTelegramNotifier("7184:AAH-token", "-100", url, http.DefaultClient)
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server, call := providerServer(t, http.StatusOK, nil, "")
			if err := tc.notifier(server.URL).Notify(context.Background(), event); err != nil {
				t.Fatalf("Notify: %v", err)
			}
			_, _, _, body := call.snapshot()
			var payload struct {
				Text    string `json:"text"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode payload %s: %v", body, err)
			}
			text := payload.Text
			if text == "" {
				text = payload.Content
			}
			if !utf8.ValidString(text) {
				t.Errorf("%s message is not valid UTF-8", name)
			}
			if units := utf16Length(text); units > tc.limit {
				t.Errorf("%s message = %d UTF-16 units, want <= %d (ellipsis included)", name, units, tc.limit)
			}
			if !strings.HasSuffix(text, "…") {
				t.Errorf("%s message does not end in the truncation marker", name)
			}
		})
	}
}

// TestTruncateTextCapsIncludingEllipsisRuneSafe pins the cap semantics: the
// ellipsis counts against the limit and truncation never splits a rune.
func TestTruncateTextCapsIncludingEllipsisRuneSafe(t *testing.T) {
	got := truncateText(strings.Repeat("🚀", 100), 10)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateText produced invalid UTF-8: %q", got)
	}
	if units := utf16Length(got); units > 10 {
		t.Errorf("units = %d, want <= the 10 cap including the ellipsis", units)
	}
	if !strings.HasSuffix(got, "…") || !strings.HasPrefix(got, "🚀") {
		t.Errorf("got = %q, want the head kept on a rune boundary plus an in-budget ellipsis", got)
	}
	ascii := truncateText(strings.Repeat("a", 4000), 2000)
	if units := utf16Length(ascii); units > 2000 {
		t.Errorf("ascii = %d UTF-16 units, want no more than the 2000 cap", units)
	}
	if runes := utf8.RuneCountInString(ascii); runes > 2000 {
		t.Errorf("ascii = %d runes, want no more than the 2000 cap", runes)
	}
	if got := truncateText("hello", 10); got != "hello" {
		t.Errorf("short text = %q, want it unchanged", got)
	}
	if exact := strings.Repeat("a", 10); truncateText(exact, 10) != exact {
		t.Error("text exactly at the cap must stay unchanged (no needless ellipsis)")
	}
}
