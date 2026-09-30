package agent

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// HealthServer serves the loopback liveness endpoint the update wrapper probes
// after a restart. It reports process liveness only (200 once serving); it
// deliberately does not check the control-plane connection, so a node whose CP
// is temporarily unreachable is never rolled back by an update.
type HealthServer struct {
	server   *http.Server
	listener net.Listener
}

// StartHealthServer binds addr and serves /healthz until Close.
func StartHealthServer(addr string, log *slog.Logger) (*HealthServer, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Warn("agent: health server stopped", "error", err)
		}
	}()
	return &HealthServer{server: server, listener: listener}, nil
}

// Close shuts the health server down.
func (h *HealthServer) Close(ctx context.Context) error {
	return h.server.Shutdown(ctx)
}
