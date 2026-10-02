package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/server/ws"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// logStreamTail is how many historical lines a freshly started container log
// stream replays before following live output.
const logStreamTail = 200

// handleStartLogStream serves POST
// /v1/servers/{id}/containers/{containerID}/logs/stream. It authorizes the
// node's team, then asks the realtime manager to bridge the node agent's log
// stream into the hub (agent -> Redis -> bridge -> WS room). The stream is
// idempotent per channel, so repeated mounts and multiple viewers share one
// agent stream. It is the production caller of ws.PublishStream (B1-2/B2-1).
func (s *Server) handleStartLogStream(w http.ResponseWriter, r *http.Request) {
	serverID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid server id"})
		return
	}
	containerID := strings.TrimSpace(chi.URLParam(r, "containerID"))
	if containerID == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "container id is required"})
		return
	}
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}
	if err := s.authorizeLogSubscription(r.Context(), serverID, userID); err != nil {
		// Only a missing node and a denied membership answer 404, and they are
		// indistinguishable (server policy). A transient lookup/DB failure is a
		// real error, not "not found", so it is logged and answered 500 (U2).
		switch {
		case errors.Is(err, servers.ErrNotFound), errors.Is(err, teams.ErrNotFound):
			writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
		default:
			s.logger.Error("realtime: authorize log stream",
				"error", err, "server_id", serverID.String(), "user_id", userID.String())
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		}
		return
	}

	dialer, ok := s.servers.(containerDialer)
	if !ok {
		writeJSON(w, http.StatusBadGateway, apiError{Message: "agent unavailable"})
		return
	}
	if s.realtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, apiError{Message: "realtime unavailable"})
		return
	}

	opener := func(ctx context.Context) (ws.LogStreamer, io.Closer, error) {
		client, dialErr := dialer.DialDockerClient(ctx, serverID)
		if dialErr != nil {
			return nil, nil, dialErr
		}
		return client, client, nil
	}
	req := &agentv1.StreamLogsRequest{ContainerId: containerID, Follow: true, Tail: logStreamTail}
	if err := s.realtime.StartLogStream(opener, serverID.String(), containerID, req); err != nil {
		s.logger.Error("realtime: start log stream",
			"error", err, "server_id", serverID.String(), "container_id", containerID)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"channel": ws.LogChannel(serverID.String(), containerID),
	})
}
