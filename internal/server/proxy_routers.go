package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/proxy"
)

// MountProxyRouters registers the read-only router list under /api. A nil
// service mounts nothing. Like the proxy sync and DNS-provider surface it is
// platform-global (every node's generated routers), so it runs behind the
// platform admin chain: the server passes its RequirePlatformAdmin.
func MountProxyRouters(r chi.Router, platformAuth func(http.Handler) http.Handler, svc proxy.RouterService) {
	if svc == nil {
		return
	}
	r.Group(func(global chi.Router) {
		global.Use(platformAuth)
		global.Get("/v1/proxy/routers", func(w http.ResponseWriter, req *http.Request) {
			raw := strings.TrimSpace(req.URL.Query().Get("server_id"))
			serverID := uuid.Nil
			if raw != "" {
				id, err := uuid.Parse(raw)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid server_id"})
					return
				}
				serverID = id
			}
			response, err := svc.ListRouters(req.Context(), serverID)
			if err != nil {
				switch {
				case errors.Is(err, proxy.ErrServerNotFound):
					writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
				case errors.Is(err, proxy.ErrAgentUnavailable), errors.Is(err, proxy.ErrReload), errors.Is(err, proxy.ErrHistory):
					writeJSON(w, http.StatusBadGateway, apiError{Message: err.Error()})
				case errors.Is(err, proxy.ErrValidation):
					writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
				default:
					writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
				}
				return
			}
			if response.Routers == nil {
				response.Routers = []proxy.RouterInfo{}
			}
			if response.Nodes == nil {
				response.Nodes = []proxy.RouterNodeState{}
			}
			writeJSON(w, http.StatusOK, response)
		})
	})
}
