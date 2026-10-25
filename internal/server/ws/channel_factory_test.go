package ws

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestChannelAuthorizerIsPerConnection(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Close)
	handler := NewHandler(hub, subjectVerifier{token: testToken, userID: uuid.New()}, nil, nil)
	var connections atomic.Int32
	handler.channelAuthorizeFactory = func() func(context.Context, string, uuid.UUID) error {
		connections.Add(1)
		return func(context.Context, string, uuid.UUID) error { return nil }
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	for range 2 {
		conn := dial(t, wsURL(server, "?token="+testToken+"&channel=custom:project"))
		if message := receive(t, conn); message.Type != TypeSubscribed {
			t.Fatalf("subscribe=%+v", message)
		}
	}
	if connections.Load() != 2 {
		t.Fatalf("authorizers=%d want one per connection", connections.Load())
	}
}
