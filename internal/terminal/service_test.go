package terminal

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeRegistry struct {
	server *servers.Server
	err    error
}

func (f *fakeRegistry) Get(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.server == nil || f.server.ID != id {
		return nil, errors.New("not found")
	}
	return f.server, nil
}

func newTestService(t *testing.T, cfg Config) (*Service, uuid.UUID, uuid.UUID) {
	t.Helper()
	userID := uuid.New()
	serverID := uuid.New()
	if cfg.Registry == nil {
		cfg.Registry = &fakeRegistry{server: &servers.Server{ID: serverID}}
	}
	if cfg.Logger == nil {
		cfg.Logger = discardLogger()
	}
	return NewService(cfg), userID, serverID
}

func TestCreateValidatesRequest(t *testing.T) {
	t.Parallel()
	svc, userID, serverID := newTestService(t, Config{})
	valid := CreateRequest{Target: "host", Root: "volumes", Cwd: "app", Cols: 80, Rows: 24}
	if _, err := svc.Create(context.Background(), userID, serverID, valid); err != nil {
		t.Fatalf("valid create: %v", err)
	}
	cases := []struct {
		name string
		req  CreateRequest
	}{
		{"unsupported target", CreateRequest{Target: "container", Root: "volumes", Cols: 80, Rows: 24}},
		{"cwd without root", CreateRequest{Target: "host", Cwd: "app", Cols: 80, Rows: 24}},
		{"bad root", CreateRequest{Target: "host", Root: "../etc", Cols: 80, Rows: 24}},
		{"absolute cwd", CreateRequest{Target: "host", Root: "volumes", Cwd: "/etc", Cols: 80, Rows: 24}},
		{"parent cwd", CreateRequest{Target: "host", Root: "volumes", Cwd: "a/../b", Cols: 80, Rows: 24}},
		{"nul cwd", CreateRequest{Target: "host", Root: "volumes", Cwd: "a\x00b", Cols: 80, Rows: 24}},
		{"zero cols", CreateRequest{Target: "host", Root: "volumes", Cols: 0, Rows: 24}},
		{"huge rows", CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 1001}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := svc.Create(context.Background(), userID, serverID, testCase.req); err == nil {
				t.Fatalf("Create(%+v) = nil error, want validation failure", testCase.req)
			}
		})
	}
}

func TestCreateHostWithoutRoot(t *testing.T) {
	t.Parallel()
	svc, userID, serverID := newTestService(t, Config{})
	for _, target := range []string{"host", ""} {
		session, err := svc.Create(context.Background(), userID, serverID, CreateRequest{
			Target: target, Cols: 120, Rows: 32,
		})
		if err != nil {
			t.Fatalf("Create(target=%q, no root): %v", target, err)
		}
		if session.Target != TargetHost || session.Root != "" || session.Cwd != "" {
			t.Fatalf("session = %+v, want a host session with no root/cwd", session)
		}
	}
}

func TestCreateRejectsCwdWithoutRoot(t *testing.T) {
	t.Parallel()
	svc, userID, serverID := newTestService(t, Config{})
	_, err := svc.Create(context.Background(), userID, serverID, CreateRequest{
		Target: "host", Cwd: "app", Cols: 80, Rows: 24,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Create(cwd without root) = %v, want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "requires a file root") {
		t.Fatalf("error = %q, want a clear missing-root message", err)
	}
}

func TestCreateUnknownNode(t *testing.T) {
	t.Parallel()
	svc, userID, _ := newTestService(t, Config{})
	_, err := svc.Create(context.Background(), userID, uuid.New(), CreateRequest{
		Target: "host", Root: "volumes", Cols: 80, Rows: 24,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Create on unknown node = %v, want ErrNotFound", err)
	}
}

func TestAttachOwnershipAndSingleUse(t *testing.T) {
	t.Parallel()
	svc, userID, serverID := newTestService(t, Config{})
	otherUser := uuid.New()
	otherServer := uuid.New()
	session, err := svc.Create(context.Background(), userID, serverID, CreateRequest{
		Target: "host", Root: "volumes", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := svc.Attach(context.Background(), otherUser, serverID, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign user attach = %v, want ErrNotFound", err)
	}
	if _, err := svc.Attach(context.Background(), userID, otherServer, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign server attach = %v, want ErrNotFound", err)
	}
	if _, err := svc.Attach(context.Background(), userID, serverID, session.ID); err != nil {
		t.Fatalf("owner attach: %v", err)
	}
	if _, err := svc.Attach(context.Background(), userID, serverID, session.ID); !errors.Is(err, ErrAlreadyAttached) {
		t.Fatalf("second attach = %v, want ErrAlreadyAttached", err)
	}

	svc.Release(session.ID)
	if _, err := svc.Attach(context.Background(), userID, serverID, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("attach after release = %v, want ErrNotFound", err)
	}
	select {
	case <-session.Done():
	default:
		t.Fatal("Done is not closed after Release")
	}
}

func TestPendingSessionExpires(t *testing.T) {
	t.Parallel()
	svc, userID, serverID := newTestService(t, Config{PendingTTL: 20 * time.Millisecond})
	session, err := svc.Create(context.Background(), userID, serverID, CreateRequest{
		Target: "host", Root: "volumes", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	select {
	case <-session.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("pending session did not expire")
	}
	if _, err := svc.Attach(context.Background(), userID, serverID, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("attach after expiry = %v, want ErrNotFound", err)
	}
}

func TestConfirmExtendsLifetime(t *testing.T) {
	t.Parallel()
	svc, userID, serverID := newTestService(t, Config{
		PendingTTL:  20 * time.Millisecond,
		MaxDuration: time.Hour,
	})
	session, err := svc.Create(context.Background(), userID, serverID, CreateRequest{
		Target: "host", Root: "volumes", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Attach(context.Background(), userID, serverID, session.ID); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	svc.Confirm(session.ID)
	time.Sleep(60 * time.Millisecond)
	select {
	case <-session.Done():
		t.Fatal("confirmed session expired at the pending TTL")
	default:
	}
	svc.CloseAll()
	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("CloseAll did not close the session")
	}
}

func TestQuotaPerUser(t *testing.T) {
	t.Parallel()
	svc, userID, serverID := newTestService(t, Config{MaxSessionsPerUser: 1})
	if _, err := svc.Create(context.Background(), userID, serverID, CreateRequest{
		Target: "host", Root: "volumes", Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := svc.Create(context.Background(), userID, serverID, CreateRequest{
		Target: "host", Root: "volumes", Cols: 80, Rows: 24,
	}); !errors.Is(err, ErrTooManySessions) {
		t.Fatalf("second Create = %v, want ErrTooManySessions", err)
	}
}

func TestCloseChecksOwnership(t *testing.T) {
	t.Parallel()
	svc, userID, serverID := newTestService(t, Config{})
	session, err := svc.Create(context.Background(), userID, serverID, CreateRequest{
		Target: "host", Root: "volumes", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Close(context.Background(), uuid.New(), serverID, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign close = %v, want ErrNotFound", err)
	}
	if err := svc.Close(context.Background(), userID, serverID, session.ID); err != nil {
		t.Fatalf("owner close: %v", err)
	}
	if err := svc.Close(context.Background(), userID, serverID, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double close = %v, want ErrNotFound", err)
	}
}
