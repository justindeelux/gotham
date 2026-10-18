package files

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// versionsEnvelope wraps one file's snapshots (newest first).
type versionsEnvelope struct {
	Versions []Version `json:"versions"`
}

// versionBodyEnvelope wraps one snapshot body.
type versionBodyEnvelope struct {
	Version Version `json:"version"`
	Content string  `json:"content"`
}

// deletedEnvelope reports a deleted snapshot.
type deletedEnvelope struct {
	Deleted bool `json:"deleted"`
}

// history serves GET .../files/history: one file's snapshots, newest first.
func (h *handler) history(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	versions, err := h.svc.History(r.Context(), serverID, query.Get("root"), query.Get("path"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	if versions == nil {
		versions = []Version{}
	}
	writeJSON(w, http.StatusOK, versionsEnvelope{Versions: versions})
}

// historyVersion serves GET .../files/history/{version}: one read-only body.
func (h *handler) historyVersion(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	versionID, ok := versionParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	body, err := h.svc.HistoryVersion(r.Context(), serverID, query.Get("root"), query.Get("path"), versionID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, versionBodyEnvelope(body))
}

// restoreVersion serves POST .../files/history/{version}/restore.
func (h *handler) restoreVersion(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	versionID, ok := versionParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	result, err := h.svc.RestoreVersion(r.Context(), serverID, query.Get("root"), query.Get("path"), versionID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, writeEnvelope(result))
}

// deleteVersion serves DELETE .../files/history/{version}.
func (h *handler) deleteVersion(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	versionID, ok := versionParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	if err := h.svc.DeleteVersion(r.Context(), serverID, query.Get("root"), query.Get("path"), versionID); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deletedEnvelope{Deleted: true})
}

// versionParam parses the {version} path parameter, answering 400 on a bad
// UUID. An opaque UUID (never a path or timestamp) is what keeps a crafted
// version from reaching the store's file key.
func versionParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "version"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid version id", Code: "invalid"})
		return uuid.Nil, false
	}
	return id, true
}
