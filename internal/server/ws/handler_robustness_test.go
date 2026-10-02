package ws

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

// TestPumpRequestsStopsWhenBlocked is the B1-6 regression: the request reader
// must exit when the connection tears down a stop channel, even while it is
// blocked sending on a full incoming queue. Without the stop select it would
// block forever and leak the goroutine.
func TestPumpRequestsStopsWhenBlocked(t *testing.T) {
	incoming := make(chan clientRequest, 1)
	stop := make(chan struct{})

	var calls atomic.Int32
	errCh := make(chan error)
	receive := func(req *clientRequest) error {
		calls.Add(1)
		req.Subscribe = "logs:srv:ctr"
		time.Sleep(time.Millisecond)
		select {
		case err := <-errCh:
			return err
		default:
			return nil
		}
	}

	done := make(chan struct{})
	go func() {
		pumpRequests(receive, incoming, stop)
		close(done)
	}()

	waitForCondition(t, 2*time.Second, "reader blocked on a full queue", func() bool {
		return len(incoming) == 1 && calls.Load() >= 2
	})

	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pumpRequests did not exit after stop (B1-6 reader leak)")
	}
}

// TestSubscriptionCapPerConnection is the B1-10 regression: a connection may
// hold at most maxSubscriptionsPerClient rooms; the overflow is denied and
// never joins the hub.
func TestSubscriptionCapPerConnection(t *testing.T) {
	hub, server := newTestServer(t)
	conn := dial(t, wsURL(server, "/?token="+testToken))

	for i := 0; i < maxSubscriptionsPerClient; i++ {
		channel := LogChannel("srv-cap", fmt.Sprintf("ctr-%d", i))
		if err := websocket.JSON.Send(conn, clientRequest{Subscribe: channel}); err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
		if msg := receive(t, conn); msg.Type != TypeSubscribed {
			t.Fatalf("subscription %d = %+v, want subscribed", i, msg)
		}
	}

	overflow := LogChannel("srv-cap", "overflow")
	if err := websocket.JSON.Send(conn, clientRequest{Subscribe: overflow}); err != nil {
		t.Fatalf("overflow subscribe: %v", err)
	}
	if msg := receive(t, conn); msg.Type != TypeDenied {
		t.Fatalf("overflow subscription = %+v, want denied", msg)
	}
	if got := hub.Subscribers(overflow); got != 0 {
		t.Errorf("overflow channel subscribers = %d, want 0", got)
	}
	if got := hub.Subscribers(LogChannel("srv-cap", "ctr-0")); got != 1 {
		t.Errorf("accepted channel subscribers = %d, want 1", got)
	}
}

// TestAuthorizationCachedPerConnection is the second half of B1-10: repeated
// log channels for the same node must not re-query the authorizer (and thus
// the database) on every subscribe.
func TestAuthorizationCachedPerConnection(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Close)

	nodeID := uuid.New()
	userID := uuid.New()
	var calls atomic.Int32
	authorize := func(_ context.Context, serverID, user uuid.UUID) error {
		calls.Add(1)
		if serverID != nodeID || user != userID {
			return errors.New("not a member of this team")
		}
		return nil
	}

	handler := NewHandler(hub, subjectVerifier{token: testToken, userID: userID}, nil, authorize)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	conn := dial(t, wsURL(server, "/?token="+testToken+"&channel="+LogChannel(nodeID.String(), "a")))
	if msg := receive(t, conn); msg.Type != TypeSubscribed {
		t.Fatalf("first subscription = %+v, want subscribed", msg)
	}

	if err := websocket.JSON.Send(conn, clientRequest{Subscribe: LogChannel(nodeID.String(), "b")}); err != nil {
		t.Fatalf("second subscribe: %v", err)
	}
	if msg := receive(t, conn); msg.Type != TypeSubscribed {
		t.Fatalf("second subscription = %+v, want subscribed", msg)
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("authorize calls = %d, want 1 (cached per node)", got)
	}
}
