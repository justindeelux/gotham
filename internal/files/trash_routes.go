package files

import "net/http"

// emptyTrashEnvelope reports how many entries an empty call removed.
type emptyTrashEnvelope struct {
	Removed uint64 `json:"removed"`
}

// trash serves GET .../files/trash?root=: list a root's recycle bin.
func (h *handler) trash(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	listing, err := h.svc.Trash(r.Context(), serverID, r.URL.Query().Get("root"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	if listing.Entries == nil {
		listing.Entries = []TrashEntry{}
	}
	writeJSON(w, http.StatusOK, listing)
}

// restoreTrash serves POST .../files/trash/restore.
func (h *handler) restoreTrash(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req RestoreRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	entry, err := h.svc.Restore(r.Context(), serverID, req.Root, req.ID, req.Destination, req.Overwrite)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statEnvelope{Entry: entry})
}

// purgeTrash serves POST .../files/trash/purge.
func (h *handler) purgeTrash(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req PurgeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.Purge(r.Context(), serverID, req.Root, req.ID); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// emptyTrash serves POST .../files/trash/empty.
func (h *handler) emptyTrash(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req EmptyTrashRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	removed, err := h.svc.EmptyTrash(r.Context(), serverID, req.Root)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, emptyTrashEnvelope{Removed: removed})
}
