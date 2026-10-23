package files

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTicketStoreSingleUseAndExpiry(t *testing.T) {
	now := time.Now()
	store := newTicketStore(60*time.Second, func() time.Time { return now })
	serverID := uuid.New()
	want := ticket{UserID: uuid.New(), TeamID: uuid.New(), ServerID: serverID, Root: "volumes", Path: "a"}

	token := store.issue(want)
	got, ok := store.consumeFor(token, serverID)
	if !ok {
		t.Fatal("fresh ticket was not consumed")
	}
	if got.ServerID != want.ServerID || got.Root != want.Root || got.Path != want.Path || got.UserID != want.UserID {
		t.Fatalf("consumed ticket = %+v, want %+v", got, want)
	}
	if _, ok := store.consumeFor(token, serverID); ok {
		t.Fatal("ticket was reusable")
	}

	expiring := store.issue(want)
	now = now.Add(61 * time.Second)
	if _, ok := store.consumeFor(expiring, serverID); ok {
		t.Fatal("expired ticket was accepted")
	}

	if _, ok := store.consumeFor("", serverID); ok {
		t.Fatal("empty token was accepted")
	}
}

func TestTicketStoreWrongServerNotBurned(t *testing.T) {
	store := newTicketStore(60*time.Second, nil)
	serverID := uuid.New()
	token := store.issue(ticket{ServerID: serverID, Root: "volumes", Path: "a"})

	if _, ok := store.consumeFor(token, uuid.New()); ok {
		t.Fatal("a ticket was consumed for the wrong server")
	}
	if _, ok := store.consumeFor(token, serverID); !ok {
		t.Fatal("the ticket was burned by the wrong-server request")
	}
}
