// Command verify-chain is a self-update verification fixture, not a shipped
// binary. deploy/verify-systemd.sh builds it with GOTHAM_GO and runs it as the
// scratch unit's service binary.
//
// It exercises the real control-plane startup ordering with the real
// internal/updates service: NewService (which runs startup Recover) -> Resume
// (which must not block on the wrapper's lock) -> listen on /healthz. A
// regression in that ordering (for example a blocking lock in Resume) makes the
// fixture never listen, so the wrapper's health check fails and the script
// reports a rollback instead of ok.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/justindeelux/gotham/internal/updates"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8000", "health listen address")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, keyErr := updates.FromEnv(os.Getenv("GOTHAM_UPDATE_CURRENT"), logger)
	if keyErr != nil {
		logger.Info("verify-chain: no release public key (expected for the fixture)", "reason", keyErr)
	}
	svc, err := updates.NewService(cfg)
	if err != nil {
		logger.Error("verify-chain: NewService failed", "error", err)
		os.Exit(1)
	}

	// The real ordering under test: Resume before the listener exists.
	if err := svc.Resume(context.Background()); err != nil {
		logger.Warn("verify-chain: Resume returned an error", "error", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	logger.Info("verify-chain: listening", "addr", *addr)
	if err := (&http.Server{Addr: *addr, Handler: mux}).ListenAndServe(); err != nil {
		logger.Error("verify-chain: listen failed", "error", err)
		os.Exit(1)
	}
}
