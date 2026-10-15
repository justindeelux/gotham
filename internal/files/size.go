package files

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// FolderSize is one folder's recursive size (JUS-162). It is metadata only:
// Bytes sums regular-file sizes, Skipped counts the symlinks and special files
// the walk never followed, and Truncated reports a partial result the agent
// stopped at its entry cap with.
type FolderSize struct {
	Bytes     int64  `json:"bytes"`
	Entries   int64  `json:"entries"`
	Skipped   uint64 `json:"skipped"`
	Truncated bool   `json:"truncated"`
}

// DirectorySize returns the recursive size of one folder under a root. It is a
// read: any team member of the node's team may call it, exactly like List and
// Stat (it exposes directory metadata, not file contents). The agent bounds the
// walk by an entry cap and reports a partial result rather than failing.
func (s *Service) DirectorySize(ctx context.Context, serverID uuid.UUID, root, path string) (FolderSize, error) {
	if !Enabled() {
		return FolderSize{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return FolderSize{}, err
	}
	if err := validatePathArg("path", path); err != nil {
		return FolderSize{}, err
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, false)
	if err != nil {
		return FolderSize{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.DirectorySize(ctx, &agentv1.DirectorySizeRequest{Root: root, Path: path})
	if err != nil {
		return FolderSize{}, mapRPCError(err)
	}
	return newFolderSize(response), nil
}

// size serves GET .../files/size.
func (h *handler) size(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	size, err := h.svc.DirectorySize(r.Context(), serverID, query.Get("root"), query.Get("path"))
	if err != nil {
		// A client abort is not a server error: the caller is already gone, so
		// there is nobody to write a status to and nothing to log.
		if r.Context().Err() != nil {
			return
		}
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, size)
}

// newFolderSize maps the agent's DirectorySizeResponse onto the API DTO.
func newFolderSize(info *agentv1.DirectorySizeResponse) FolderSize {
	return FolderSize{
		Bytes:     info.GetBytes(),
		Entries:   info.GetEntries(),
		Skipped:   info.GetSkipped(),
		Truncated: info.GetTruncated(),
	}
}
