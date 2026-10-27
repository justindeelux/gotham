package server

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/files"
	"github.com/justindeelux/gotham/internal/servers"
)

func TestAuthorizeFileTransferChecksBothNodes(t *testing.T) {
	s, _ := newScopeTestServer(t)
	registry := s.servers.(*fakeServerService)
	sourceID, destinationID := uuid.New(), uuid.New()
	registry.items[sourceID] = servers.Server{ID: sourceID, Name: "source"}
	registry.items[destinationID] = servers.Server{ID: destinationID, Name: "destination"}

	ownerCtx := context.WithValue(context.Background(), userIDKey, testOwnerID)
	memberCtx := context.WithValue(context.Background(), userIDKey, testUserID)
	anonymousCtx := context.Background()

	if err := s.authorizeFileTransfer(ownerCtx, sourceID, destinationID); err != nil {
		t.Fatalf("owner transfer = %v, want nil", err)
	}
	if err := s.authorizeFileTransfer(memberCtx, sourceID, destinationID); !errors.Is(err, files.ErrForbidden) {
		t.Fatalf("member transfer = %v, want ErrForbidden", err)
	}
	if err := s.authorizeFileTransfer(anonymousCtx, sourceID, destinationID); !errors.Is(err, files.ErrForbidden) {
		t.Fatalf("anonymous transfer = %v, want ErrForbidden", err)
	}
	if err := s.authorizeFileTransfer(ownerCtx, sourceID, sourceID); !errors.Is(err, files.ErrValidation) {
		t.Fatalf("same-node transfer = %v, want ErrValidation", err)
	}
	if err := s.authorizeFileTransfer(ownerCtx, sourceID, uuid.New()); !errors.Is(err, files.ErrNotFound) {
		t.Fatalf("forged destination = %v, want ErrNotFound", err)
	}

	// A role revoked after the source URL was authorized must not keep the
	// destination reachable.
	owner := s.auth.(*fakeAuthService).users[testOwnerID]
	owner.Role = auth.RoleMember
	if err := s.authorizeFileTransfer(ownerCtx, sourceID, destinationID); !errors.Is(err, files.ErrForbidden) {
		t.Fatalf("revoked owner transfer = %v, want ErrForbidden", err)
	}
}
