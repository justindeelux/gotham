package files

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type versionsEnvelope struct {
	Versions []Version `json:"versions"`
}

type versionBodyEnvelope struct {
	Version Version `json:"version"`
	Content string  `json:"content"`
}

type deletedEnvelope struct {
	Deleted bool `json:"deleted"`
}

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
	writeJSON(w, http.StatusOK, versionBodyEnvelope{Version: body.Version, Content: body.Content})
}

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

func versionParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "version"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid version id", Code: "invalid"})
		return uuid.Nil, false
	}
	return id, true
}
