package ws

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/net/websocket"

	"github.com/justindeelux/gotham/internal/auth"
)

type subjectVerifier struct {
	token  string
	userID uuid.UUID
}

func (s subjectVerifier) VerifyAccessToken(token string) (*auth.Claims, error) {
	if token != s.token {
		return nil, errors.New("unauthorized")
	}
	return &auth.Claims{
		Role:             "user",
		RegisteredClaims: jwt.RegisteredClaims{Subject: s.userID.String()},
	}, nil
}

func newAuthorizedTestServer(t *testing.T, hub *Hub, userID uuid.UUID, authorize SubscriptionAuthorizer) *httptest.Server {
	t.Helper()
	handler := NewHandler(hub, subjectVerifier{token: testToken, userID: userID}, nil, authorize)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func expectNoMessage(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	var msg Message
	if err := websocket.JSON.Receive(conn, &msg); err == nil {
		t.Fatalf("received %+v, want nothing", msg)
	}
}

func TestLogSubscriptionRequiresTeamMembership(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Close)

	nodeID := uuid.New()
	member, outsider := uuid.New(), uuid.New()
	authorize := func(_ context.Context, serverID, userID uuid.UUID) error {
		if serverID != nodeID {
			return errors.New("unknown server")
		}
		if userID != member {
			return errors.New("not a member of this team")
		}
		return nil
	}
	channel := LogChannel(nodeID.String(), "abc")

	memberServer := newAuthorizedTestServer(t, hub, member, authorize)
	memberConn := dial(t, wsURL(memberServer, "?token="+testToken+"&server="+nodeID.String()+"&container=abc"))
	if msg := receive(t, memberConn); msg.Type != TypeSubscribed || msg.Channel != channel {
		t.Fatalf("member subscription = %+v, want a subscribed frame", msg)
	}
	hub.Broadcast(channel, []byte(`{"type":"log","data":"hello"}`))
	if msg := receive(t, memberConn); msg.Type != TypeLog || msg.Data != "hello" {
		t.Fatalf("member broadcast = %+v, want the node's log frame", msg)
	}
	_ = websocket.JSON.Send(memberConn, clientRequest{Subscribe: LogChannel(nodeID.String(), "other")})
	if msg := receive(t, memberConn); msg.Type != TypeSubscribed {
		t.Fatalf("member frame subscription = %+v, want a subscribed frame", msg)
	}

	outsiderServer := newAuthorizedTestServer(t, hub, outsider, authorize)
	deniedConn := dial(t, wsURL(outsiderServer, "?token="+testToken+"&server="+nodeID.String()+"&container=abc"))
	if msg := receive(t, deniedConn); msg.Type != TypeDenied || msg.Channel != channel {
		t.Fatalf("non-member subscription = %+v, want a denied frame", msg)
	}
	hub.Broadcast(channel, []byte(`{"type":"log","data":"secret"}`))
	expectNoMessage(t, deniedConn)

	frameConn := dial(t, wsURL(outsiderServer, "?token="+testToken))
	_ = websocket.JSON.Send(frameConn, clientRequest{Subscribe: channel})
	if msg := receive(t, frameConn); msg.Type != TypeDenied {
		t.Fatalf("non-member frame subscription = %+v, want a denied frame", msg)
	}
}
