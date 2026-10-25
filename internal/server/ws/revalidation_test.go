package ws

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/auth"
)

type roleVerifier struct {
	subjectVerifier
	role string
}

func (v roleVerifier) VerifyConnection(_ context.Context, token string) (*auth.Claims, string, error) {
	claims, err := v.VerifyAccessToken(token)
	return claims, v.role, err
}

func TestFramesDoNotReauthorizeSubscriptions(t *testing.T) {
	for _, role := range []string{"owner", "member"} {
		t.Run(role, func(t *testing.T) {
			hub := NewHub()
			go hub.Run()
			t.Cleanup(hub.Close)
			handler := NewHandler(hub, roleVerifier{subjectVerifier: subjectVerifier{token: testToken, userID: uuid.New()}, role: role}, nil, nil)
			var checks atomic.Int32
			handler.channelAuthorizeFactory = func() func(context.Context, string, uuid.UUID) error {
				return func(context.Context, string, uuid.UUID) error { checks.Add(1); return nil }
			}
			handler.frameFilter = func(ctx context.Context, _ uuid.UUID, payload []byte) []byte {
				if actual, ok := ConnectionRole(ctx); !ok || actual != role {
					t.Errorf("cached role=%q", actual)
				}
				return payload
			}
			server := httptest.NewServer(handler)
			t.Cleanup(server.Close)
			conn := dial(t, wsURL(server, "?token="+testToken+"&channel=custom:project"))
			if message := receive(t, conn); message.Type != TypeSubscribed {
				t.Fatalf("subscribe=%+v", message)
			}
			before := checks.Load()
			for range 100 {
				hub.Broadcast("custom:project", []byte(`{"type":"log","data":"frame"}`))
				if message := receive(t, conn); message.Type != TypeLog {
					t.Fatalf("frame=%+v", message)
				}
			}
			// A periodic check may run on a busy race-test host; frames must not add checks.
			if delta := checks.Load() - before; delta > 1 {
				t.Fatalf("100 frames caused %d authorization calls", delta)
			}
		})
	}
}
