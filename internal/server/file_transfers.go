package server

import (
	"context"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/files"
)

// authorizeFileTransfer is the both-node check for cross-node copies. File
// routes are operator-only, so the source URL's owner gate does not by itself
// grant the destination: the role is re-read from the database on every
// request and both node IDs must resolve as operator-owned servers.
func (s *Server) authorizeFileTransfer(ctx context.Context, sourceID, destinationID uuid.UUID) error {
	if sourceID == uuid.Nil || destinationID == uuid.Nil || sourceID == destinationID {
		return files.ErrValidation
	}
	userID, ok := UserIDFromContext(ctx)
	if !ok || s.auth == nil {
		return files.ErrForbidden
	}
	user, err := s.auth.Me(ctx, userID)
	if err != nil {
		if err != auth.ErrUnauthorized && s.logger != nil {
			s.logger.Error("server: file transfer authorization", "error", err)
		}
		return files.ErrForbidden
	}
	if user.Role != auth.RoleOwner {
		return files.ErrForbidden
	}
	if s.servers == nil {
		return files.ErrForbidden
	}
	if _, err := s.servers.Get(ctx, sourceID); err != nil {
		return files.ErrNotFound
	}
	if _, err := s.servers.Get(ctx, destinationID); err != nil {
		return files.ErrNotFound
	}
	return nil
}
