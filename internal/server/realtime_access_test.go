package server

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/notifications"
	"github.com/justindeelux/gotham/internal/servers"
)

func TestRealtimeChannelAuthorizerNotificationChannels(t *testing.T) {
	userID := testUserID
	otherID := uuid.New()
	owner := &Server{auth: ownerAuthService()}
	member := &Server{auth: newFakeAuthService()}

	for _, tc := range []struct {
		name    string
		server  *Server
		user    uuid.UUID
		channel string
		allowed bool
	}{
		{"member own channel", member, userID, notifications.UserChannel(userID), true},
		{"member other user channel", member, userID, notifications.UserChannel(otherID), false},
		{"member malformed id", member, userID, "notifications:not-a-uuid", false},
		{"member missing id", member, userID, "notifications:", false},
		{"owner own channel", owner, userID, notifications.UserChannel(userID), true},
		{"owner other user channel", owner, userID, notifications.UserChannel(otherID), false},
		{"owner malformed id", owner, userID, "notifications:" + uuid.NewString() + ":extra", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authorize := tc.server.newRealtimeChannelAuthorizer()
			err := authorize(context.Background(), tc.channel, tc.user)
			if (err == nil) != tc.allowed {
				t.Fatalf("channel %q allowed = %v, want %v (err %v)", tc.channel, err == nil, tc.allowed, err)
			}
		})
	}
}

func TestRealtimeChannelAuthorizerKeepsTaskAndLogChannels(t *testing.T) {
	userID := testUserID
	nodeID := uuid.New()
	base := newFakeServerService()
	base.items[nodeID] = servers.Server{ID: nodeID, Name: "node", Status: servers.StatusReady}
	owner := &Server{auth: ownerAuthService(), servers: base}
	member := &Server{auth: newFakeAuthService()}

	for _, tc := range []struct {
		name    string
		server  *Server
		channel string
		allowed bool
	}{
		{"owner task channel", owner, "tasks:" + uuid.NewString(), true},
		{"owner log channel", owner, "logs:" + nodeID.String() + ":container", true},
		{"member task channel", member, "tasks:" + uuid.NewString(), false},
		{"member log channel", member, "logs:" + nodeID.String() + ":container", false},
		{"owner unknown channel", owner, "toolchain:guessed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authorize := tc.server.newRealtimeChannelAuthorizer()
			err := authorize(context.Background(), tc.channel, userID)
			if (err == nil) != tc.allowed {
				t.Fatalf("channel %q allowed = %v, want %v (err %v)", tc.channel, err == nil, tc.allowed, err)
			}
		})
	}
}
