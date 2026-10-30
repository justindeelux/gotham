package cleanup

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
)

const (
	maxBodyBytes = 1 << 20
	maxScanNodes = 50
	maxCleanIDs  = 1000
	maxAgeHours  = 8760
)

// UserIDFunc resolves the authenticated user of a protected request.
type UserIDFunc func(context.Context) (uuid.UUID, bool)

// CleanupService is the slice of the cleanup service the routes use.
type CleanupService interface {
	Scan(ctx context.Context, serverIDs []string, minAge time.Duration) []NodeScan
	Clean(
		ctx context.Context, actor uuid.UUID, trigger, serverID string, ids []string, minAge time.Duration,
	) CleanResult
}

type handler struct {
	svc    CleanupService
	userID UserIDFunc
}

// Mount registers the owner-only cleanup routes. Scanning is safe; cleaning
// removes what the operator selected and is never mounted for members.
func Mount(r chi.Router, admin func(http.Handler) http.Handler, userID UserIDFunc, svc CleanupService) {
	if svc == nil || admin == nil {
		return
	}
	h := &handler{svc: svc, userID: userID}
	r.Group(func(protected chi.Router) {
		protected.Use(admin)
		protected.Get("/v1/cleanup/categories", h.categories)
		protected.Post("/v1/cleanup/scan", h.scan)
		protected.Post("/v1/cleanup/clean", h.clean)
	})
}

type errorBody struct {
	Message string `json:"message"`
}

type categoryEnvelope struct {
	Categories []Category `json:"categories"`
}

type scanRequest struct {
	ServerIDs   []string `json:"server_ids"`
	MinAgeHours int      `json:"min_age_hours"`
}

type noticeResponse struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

type itemResponse struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	Tier      string `json:"tier"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt string `json:"created_at"`
	Reason    string `json:"reason"`
}

type nodeScanResponse struct {
	ServerID string           `json:"server_id"`
	Status   string           `json:"status"`
	Reason   string           `json:"reason"`
	Items    []itemResponse   `json:"items"`
	Notices  []noticeResponse `json:"notices"`
}

type scanEnvelope struct {
	Nodes []nodeScanResponse `json:"nodes"`
}

type cleanRequest struct {
	ServerID    string   `json:"server_id"`
	IDs         []string `json:"ids"`
	MinAgeHours int      `json:"min_age_hours"`
}

type cleanItemResponse struct {
	ID             string `json:"id"`
	Category       string `json:"category"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	ReclaimedBytes int64  `json:"reclaimed_bytes"`
}

type cleanEnvelope struct {
	ServerID       string              `json:"server_id"`
	Status         string              `json:"status"`
	Reason         string              `json:"reason"`
	RunID          string              `json:"run_id,omitempty"`
	ReclaimedBytes int64               `json:"reclaimed_bytes"`
	Items          []cleanItemResponse `json:"items"`
}

func (h *handler) categories(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, categoryEnvelope{Categories: Categories()})
}

func (h *handler) scan(w http.ResponseWriter, r *http.Request) {
	var req scanRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if len(req.ServerIDs) < 1 || len(req.ServerIDs) > maxScanNodes {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "server_ids must contain 1 to 50 ids"})
		return
	}
	for _, raw := range req.ServerIDs {
		if _, err := uuid.Parse(raw); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
			return
		}
	}
	if !validAgeHours(req.MinAgeHours) {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "min_age_hours must be between 0 and 8760"})
		return
	}

	scans := h.svc.Scan(r.Context(), req.ServerIDs, time.Duration(req.MinAgeHours)*time.Hour)
	nodes := make([]nodeScanResponse, 0, len(scans))
	for _, scan := range scans {
		nodes = append(nodes, scanNodeResponse(scan))
	}
	writeJSON(w, http.StatusOK, scanEnvelope{Nodes: nodes})
}

func (h *handler) clean(w http.ResponseWriter, r *http.Request) {
	var req cleanRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if _, err := uuid.Parse(req.ServerID); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
		return
	}
	if len(req.IDs) < 1 || len(req.IDs) > maxCleanIDs {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "ids must contain 1 to 1000 ids"})
		return
	}
	for _, id := range req.IDs {
		if id == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "ids must not be empty"})
			return
		}
	}
	if !validAgeHours(req.MinAgeHours) {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "min_age_hours must be between 0 and 8760"})
		return
	}
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}

	result := h.svc.Clean(
		r.Context(), actor, TriggerManual, req.ServerID, req.IDs,
		time.Duration(req.MinAgeHours)*time.Hour,
	)
	envelope := cleanEnvelope{
		ServerID: result.ServerID,
		Status:   result.Status,
		Reason:   nodeReason(result.Status, result.Reason),
		RunID:    result.RunID,
		Items:    make([]cleanItemResponse, 0, len(result.Items)),
	}
	for _, item := range result.Items {
		envelope.ReclaimedBytes += item.ReclaimedBytes
		envelope.Items = append(envelope.Items, cleanItemResponse(item))
	}
	writeJSON(w, http.StatusOK, envelope)
}

func (h *handler) actor(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	if h.userID == nil {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	actor, ok := h.userID(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	return actor, true
}

func scanNodeResponse(scan NodeScan) nodeScanResponse {
	response := nodeScanResponse{
		ServerID: scan.ServerID,
		Status:   scan.Status,
		Reason:   nodeReason(scan.Status, scan.Reason),
		Items:    make([]itemResponse, 0, len(scan.Items)),
		Notices:  []noticeResponse{},
	}
	for _, item := range scan.Items {
		createdAt := ""
		if item.CreatedUnix > 0 {
			createdAt = time.Unix(item.CreatedUnix, 0).UTC().Format(time.RFC3339)
		}
		response.Items = append(response.Items, itemResponse{
			ID:        item.ID,
			Category:  item.Category,
			Tier:      CategoryTier(item.Category),
			Name:      item.Name,
			SizeBytes: item.SizeBytes,
			CreatedAt: createdAt,
			Reason:    item.Reason,
		})
	}
	for _, notice := range scan.Notices {
		response.Notices = append(response.Notices, noticeResponse(notice))
	}
	return response
}

// nodeReason reports an unknown node as not_found instead of leaking the
// registry's internal error text.
func nodeReason(status, reason string) string {
	if status == StatusFailed && reason == servers.ErrNotFound.Error() {
		return ReasonNotFound
	}
	return reason
}

func validAgeHours(hours int) bool {
	return hours >= 0 && hours <= maxAgeHours
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
