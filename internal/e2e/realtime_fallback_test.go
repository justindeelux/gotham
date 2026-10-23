package e2e

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/justindeelux/gotham/internal/server/ws"
)

func TestRealtimePollFallback(t *testing.T) {
	t.Setenv(ws.RealtimeEnv, "false")
	if ws.RealtimeEnabled() {
		t.Fatal("RealtimeEnabled() = true with REALTIME_ENABLED=false")
	}

	var logs syncBuffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))

	const unreachableRedis = "127.0.0.1:1"
	api := chi.NewRouter()
	rt := ws.Mount(api, acceptAnyToken{}, unreachableRedis, logger, nil)
	t.Cleanup(rt.Close)
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)

	waitForLog(t, &logs, "realtime bridge disabled")
	time.Sleep(200 * time.Millisecond)
	if got := logs.String(); strings.Contains(got, "redis bridge stopped") {
		t.Fatalf("realtime bridge ran with REALTIME_ENABLED=false: %s", got)
	}

	channel := ws.LogChannel("srv-fallback", "ctr-fallback")
	query := url.Values{"token": {"e2e-token"}, "channel": {channel}}
	conn := dialWS(t, wsURL(server.URL, "/v1/ws", query.Encode()))
	messages, errs := startWSReader(conn)
	nextWSMsg(t, messages, errs, 5*time.Second, ws.TypeSubscribed)

	payload, err := json.Marshal(ws.Message{Channel: channel, Type: ws.TypeLog, Data: "poll-fallback"})
	if err != nil {
		t.Fatalf("marshal hub payload: %v", err)
	}
	if err := (ws.HubPublisher{Hub: rt.Hub}).Publish(context.Background(), channel, payload); err != nil {
		t.Fatalf("hub publish: %v", err)
	}

	msg := nextWSMsg(t, messages, errs, 5*time.Second, ws.TypeLog, ws.TypeSubscribed)
	if msg.Channel != channel {
		t.Errorf("message channel = %q, want %q", msg.Channel, channel)
	}
	if msg.Data != "poll-fallback" {
		t.Errorf("message data = %q, want %q", msg.Data, "poll-fallback")
	}
}
