package files

import (
	"net/http"
	"strings"
)

func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	recursive := strings.EqualFold(query.Get("recursive"), "true")
	listing, err := h.svc.Search(
		r.Context(),
		serverID,
		query.Get("root"),
		query.Get("path"),
		query.Get("query"),
		recursive,
	)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if listing.Results == nil {
		listing.Results = []SearchHit{}
	}
	writeJSON(w, http.StatusOK, listing)
}
