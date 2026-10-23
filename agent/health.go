package agent

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type HealthServer struct {
	server   *http.Server
	listener net.Listener
}

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

func (h *HealthServer) Close(ctx context.Context) error {
	return h.server.Shutdown(ctx)
}
