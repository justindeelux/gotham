package files

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTicketStoreSingleUseAndExpiry(t *testing.T) {
	now := time.Now()
	store := newTicketStore(60*time.Second, func() time.Time { return now })
	want := ticket{UserID: uuid.New(), TeamID: uuid.New(), ServerID: uuid.New(), Root: "volumes", Path: "a"}

	token := store.issue(want)
	got, ok := store.consume(token)
	if !ok {
		t.Fatal("fresh ticket was not consumed")
	}
	if got.ServerID != want.ServerID || got.Root != want.Root || got.Path != want.Path || got.UserID != want.UserID {
		t.Fatalf("consumed ticket = %+v, want %+v", got, want)
	}
	if _, ok := store.consume(token); ok {
		t.Fatal("ticket was reusable")
	}

	expiring := store.issue(want)
	now = now.Add(61 * time.Second)
	if _, ok := store.consume(expiring); ok {
		t.Fatal("expired ticket was accepted")
	}

	if _, ok := store.consume(""); ok {
		t.Fatal("empty token was accepted")
	}
}
