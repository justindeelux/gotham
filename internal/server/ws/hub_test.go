package ws

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"
)

// waitForCondition polls cond until it is true or the deadline expires.
func waitForCondition(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestHubShutdownDoesNotDeadlockRemoval is the B1-9 regression: once the hub
// has stopped, remove/subscribe/unsubscribe from a connection finishing shutdown
// must not block on an unbuffered channel whose receiver is gone.
func TestHubShutdownDoesNotDeadlockRemoval(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	clients := make([]*Client, 16)
	for i := range clients {
		clients[i] = hub.newClient()
	}

	hub.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, c := range clients {
			hub.remove(c)
		}
		// A connection can also still try to change subscriptions as it exits.
		hub.subscribe(clients[0], "logs:srv:ctr")
		hub.unsubscribe(clients[0], "logs:srv:ctr")
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("hub cleanup blocked after Close (B1-9 deadlock)")
	}
}

// TestSlowClientDisconnected is the B1-7 regression: a connection that stops
// reading is disconnected under the write deadline and its room membership is
// released, and the discarded frames are observable via Drops.
func TestSlowClientDisconnected(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Close)

	handler := NewHandler(hub, stubVerifier{token: testToken}, nil, nil)
	handler.writeWait = 200 * time.Millisecond
	handler.pingPeriod = time.Hour
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	channel := LogChannel("srv-slow", "ctr")
	// The connection is opened and subscribed but never read from, so the
	// server's writes eventually block and time out.
	_ = dial(t, wsURL(server, "/?token="+testToken+"&channel="+channel))

	waitForCondition(t, 3*time.Second, "subscription", func() bool {
		return hub.Subscribers(channel) == 1
	})

	payload := bytes.Repeat([]byte("x"), 64<<10)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				hub.Broadcast(channel, payload)
			}
		}
	}()

	waitForCondition(t, 5*time.Second, "slow client disconnect", func() bool {
		return hub.Subscribers(channel) == 0
	})
	if hub.Drops() == 0 {
		t.Error("Drops() = 0, want the discarded frames to be observable")
	}
}
