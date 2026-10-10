package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

// TestTaskSubscriptionReplaysSnapshot is the JUS-91 reconnect contract: a
// tasks:{teamID} join first replays the team's running tasks (closed by a
// task_snapshot marker) and then acknowledges the subscription, so the
// progress card restores on mount, navigation and reconnect.
func TestTaskSubscriptionReplaysSnapshot(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Close)

	userID := uuid.New()
	handler := NewHandler(hub, subjectVerifier{token: testToken, userID: userID}, nil,
		func(context.Context, uuid.UUID, uuid.UUID) error { return nil })
	running := `{"channel":"tasks:t1","type":"task","data":"{}"}`
	end := `{"channel":"tasks:t1","type":"task_snapshot"}`
	handler.SetTaskFeed(
		func(_ context.Context, teamID string, userID uuid.UUID) error { return nil },
		func(teamID string) []string {
			if teamID != "t1" {
				t.Errorf("snapshot team = %q, want t1", teamID)
			}
			return []string{running, end}
		},
	)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	conn := dial(t, wsURL(server, "?token="+testToken))
	_ = websocket.JSON.Send(conn, clientRequest{Subscribe: "tasks:t1"})

	first := receive(t, conn)
	if first.Type != "task" {
		t.Fatalf("first frame = %+v, want the running-task replay", first)
	}
	second := receive(t, conn)
	if second.Type != "task_snapshot" {
		t.Fatalf("second frame = %+v, want the snapshot end marker", second)
	}
	third := receive(t, conn)
	if third.Type != TypeSubscribed || third.Channel != "tasks:t1" {
		t.Fatalf("third frame = %+v, want the subscribed ack", third)
	}
}

// TestTaskSubscriptionRequiresTeamMembership keeps one team's deploy progress
// from leaking to another: a non-member gets a denied frame and never joins
// the room.
func TestTaskSubscriptionRequiresTeamMembership(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Close)

	member, outsider := uuid.New(), uuid.New()
	authorize := func(_ context.Context, teamID string, userID uuid.UUID) error {
		if teamID != "t1" || userID != member {
			return errors.New("not a member of this team")
		}
		return nil
	}
	newServer := func(t *testing.T, userID uuid.UUID) *httptest.Server {
		t.Helper()
		handler := NewHandler(hub, subjectVerifier{token: testToken, userID: userID}, nil,
			func(context.Context, uuid.UUID, uuid.UUID) error { return nil })
		handler.SetTaskFeed(authorize, func(teamID string) []string {
			end, _ := json.Marshal(Message{Channel: "tasks:" + teamID, Type: "task_snapshot"})
			return []string{string(end)}
		})
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		return server
	}

	deniedConn := dial(t, wsURL(newServer(t, outsider), "?token="+testToken))
	_ = websocket.JSON.Send(deniedConn, clientRequest{Subscribe: "tasks:t1"})
	if msg := receive(t, deniedConn); msg.Type != TypeDenied {
		t.Fatalf("outsider subscription = %+v, want a denied frame", msg)
	}

	memberConn := dial(t, wsURL(newServer(t, member), "?token="+testToken))
	_ = websocket.JSON.Send(memberConn, clientRequest{Subscribe: "tasks:t1"})
	if msg := receive(t, memberConn); msg.Type != "task_snapshot" {
		t.Fatalf("member replay = %+v, want the snapshot end marker", msg)
	}
	if msg := receive(t, memberConn); msg.Type != TypeSubscribed {
		t.Fatalf("member subscription = %+v, want a subscribed frame", msg)
	}
}
