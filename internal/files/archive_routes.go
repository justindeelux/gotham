package files

import "net/http"

func (h *handler) compress(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req CompressRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	entry, err := h.svc.Compress(r.Context(), serverID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statEnvelope{Entry: entry})
}

func (h *handler) extract(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req ExtractRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.Extract(r.Context(), serverID, req); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
