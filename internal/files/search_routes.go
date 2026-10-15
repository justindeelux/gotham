package files

import (
	"net/http"
	"strings"
)

// search serves GET .../files/search?root=&path=&query=&recursive=.
//
// It is a bounded, name-only search under one directory: recursive=false
// matches the directory's immediate entries, recursive=true descends into
// subdirectories (the agent bounds the walk). Any team member may read it, like
// list and stat. A capped result set is reported with `truncated: true`, never
// as a silently short list.
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
