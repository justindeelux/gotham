package files

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type FolderSize struct {
	Bytes     int64  `json:"bytes"`
	Entries   int64  `json:"entries"`
	Skipped   uint64 `json:"skipped"`
	Truncated bool   `json:"truncated"`
}

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

func (h *handler) size(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	size, err := h.svc.DirectorySize(r.Context(), serverID, query.Get("root"), query.Get("path"))
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, size)
}

func newFolderSize(info *agentv1.DirectorySizeResponse) FolderSize {
	return FolderSize{
		Bytes:     info.GetBytes(),
		Entries:   info.GetEntries(),
		Skipped:   info.GetSkipped(),
		Truncated: info.GetTruncated(),
	}
}
