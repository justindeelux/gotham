package files

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const transferWriteTimeout = 60 * time.Second

func (h *handler) planTransfer(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req TransferRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.authorizeTransfer(r, serverID, req.Destination.ServerID); err != nil {
		h.writeError(w, err)
		return
	}
	plan, err := h.svc.PlanTransfer(r.Context(), serverID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (h *handler) executeTransfer(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req TransferRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.authorizeTransfer(r, serverID, req.Destination.ServerID); err != nil {
		h.writeError(w, err)
		return
	}
	run, err := h.svc.Transfer(r.Context(), serverID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(w)
	encoder := json.NewEncoder(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-run.Events:
			if !open {
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(transferWriteTimeout))
			if err := encoder.Encode(event); err != nil {
				return
			}
			_ = controller.Flush()
		}
	}
}

func (h *handler) authorizeTransfer(r *http.Request, sourceID, destinationID uuid.UUID) error {
	if destinationID == uuid.Nil {
		return ErrValidation
	}
	if h.transferAuth == nil {
		return ErrForbidden
	}
	return h.transferAuth.AuthorizeTransfer(r.Context(), sourceID, destinationID)
}
