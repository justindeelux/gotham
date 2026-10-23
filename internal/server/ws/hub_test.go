package ws

import (
	"bytes"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

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
		hub.subscribe(clients[0], "logs:srv:ctr")
		hub.unsubscribe(clients[0], "logs:srv:ctr")
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("hub cleanup blocked after Close (B1-9 deadlock)")
	}
}

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

func TestBroadcastSaturationCounted(t *testing.T) {
	hub := NewHub()
	for i := 0; i < 300; i++ {
		hub.Broadcast("logs:srv:ctr", []byte("x"))
	}
	if hub.Drops() == 0 {
		t.Fatal("saturated broadcasts were not counted (U4)")
	}
}

func TestConnectionUpgradedAfterHubCloseIsDropped(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	handler := NewHandler(hub, stubVerifier{token: testToken}, nil, nil)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	hub.Close()

	conn, err := websocket.Dial(wsURL(server, "/?token="+testToken), "", "http://localhost/")
	if err != nil {
		return
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	var msg Message
	recvErr := websocket.JSON.Receive(conn, &msg)
	if recvErr == nil {
		t.Fatalf("received %+v after hub close, want the connection dropped", msg)
	}
	if ne, ok := recvErr.(net.Error); ok && ne.Timeout() {
		t.Fatal("connection was not dropped promptly after hub close (U9)")
	}
}
