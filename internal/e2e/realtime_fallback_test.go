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

// TestRealtimePollFallback verifies the backend half of the
// REALTIME_ENABLED=false fallback without any infrastructure: Mount must not
// start the Redis bridge even when a Redis address is configured, and a
// subscribed WebSocket client must still receive messages published straight
// into the hub — the in-memory path a polling client (and any in-process
// publisher) relies on when Redis realtime is off.
//
// It always runs: no Docker, no Redis, no GOTHAM_E2E gate.
func TestRealtimePollFallback(t *testing.T) {
	t.Setenv(ws.RealtimeEnv, "false")
	if ws.RealtimeEnabled() {
		t.Fatal("RealtimeEnabled() = true with REALTIME_ENABLED=false")
	}

	var logs syncBuffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// An address nothing listens on: the disabled path must never dial it.
	const unreachableRedis = "127.0.0.1:1"
	api := chi.NewRouter()
	hub := ws.Mount(api, acceptAnyToken{}, unreachableRedis, logger)
	t.Cleanup(hub.Close)
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)

	// Mount announces the fallback instead of starting the bridge, and the
	// bridge therefore never reports a connection failure either.
	waitForLog(t, &logs, "realtime bridge disabled")
	time.Sleep(200 * time.Millisecond)
	if got := logs.String(); strings.Contains(got, "redis bridge stopped") {
		t.Fatalf("realtime bridge ran with REALTIME_ENABLED=false: %s", got)
	}

	// A subscribed client still receives hub-direct (in-memory) publishes.
	channel := ws.LogChannel("srv-fallback", "ctr-fallback")
	query := url.Values{"token": {"e2e-token"}, "channel": {channel}}
	conn := dialWS(t, wsURL(server.URL, "/v1/ws", query.Encode()))
	messages, errs := startWSReader(conn)
	nextWSMsg(t, messages, errs, 5*time.Second, ws.TypeSubscribed)

	payload, err := json.Marshal(ws.Message{Channel: channel, Type: ws.TypeLog, Data: "poll-fallback"})
	if err != nil {
		t.Fatalf("marshal hub payload: %v", err)
	}
	if err := (ws.HubPublisher{Hub: hub}).Publish(context.Background(), channel, payload); err != nil {
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
