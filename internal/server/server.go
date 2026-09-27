package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/server/ws"
	"github.com/justindeelux/gotham/internal/store"
)

// shutdownTimeout bounds graceful shutdown after the context is cancelled.
const shutdownTimeout = 5 * time.Second

// readHeaderTimeout bounds how long a client may take to send request headers.
const readHeaderTimeout = 10 * time.Second

// Health status values reported by /healthz.
const (
	statusOK   = "ok"
	statusDown = "down"
)

// Pinger is the minimal health-check surface shared by the PostgreSQL and Redis
// backends. Abstracting it keeps the health handler testable without live
// services.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Server is the control-plane HTTP server.
type Server struct {
	cfg         *config.Config
	logger      *slog.Logger
	db          Pinger
	redis       Pinger
	auth        AuthService
	oauth       OAuthService
	tokens      TokenService
	servers     ServerService
	persistence *store.Store
	authLimiter *ipRateLimiter
	router      http.Handler
	closer      func()
}

// New constructs a Server bound to cfg and logging through logger. The
// authService provides the authentication flows, oauthService the OAuth2 login
// flows, tokenService the scoped API tokens, and serverService the node
// registry and SSH validation (nil disables the corresponding routes); st
// supplies the shared database pool used by the health check, and when st is
// nil a short-lived pinger is used instead. Run owns the HTTP lifecycle.
func New(cfg *config.Config, logger *slog.Logger, authService AuthService, oauthService OAuthService, tokenService TokenService, serverService ServerService, st *store.Store) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("server: config is nil")
	}
	if logger == nil {
		return nil, errors.New("server: logger is nil")
	}

	snap := cfg.Snapshot()
	redisClient := newRedisPinger(snap.Redis.Addr)
	limiter := newDefaultAuthLimiter()

	// Prefer the store's pool so the control plane does not open a second
	// PostgreSQL connection just for the health check.
	var db Pinger
	if st != nil && st.DB != nil {
		db = st.DB
	} else {
		db = newPostgresPinger(snap.Database.DSN)
	}

	s := &Server{
		cfg:         cfg,
		logger:      logger,
		db:          db,
		redis:       redisClient,
		auth:        authService,
		oauth:       oauthService,
		tokens:      tokenService,
		servers:     serverService,
		persistence: st,
		authLimiter: limiter,
		closer: func() {
			_ = redisClient.Close()
			limiter.Close()
			if oauthService != nil {
				oauthService.Close()
			}
		},
	}

	router, err := s.routes()
	if err != nil {
		return nil, err
	}
	s.router = router

	return s, nil
}

// Handler returns the server's HTTP handler. It is exposed mainly for tests.
func (s *Server) Handler() http.Handler {
	return s.router
}

// routes assembles the Chi router and its middleware chain. The SPA handler is
// registered as the final fallback so unmatched paths render the single-page
// app, while /healthz and /api keep their own handling.
func (s *Server) routes() (http.Handler, error) {
	r := chi.NewRouter()
	r.Use(securityHeaders)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(s.requestLogger())

	r.Get("/healthz", s.handleHealthz)

	// Unmatched API routes return JSON rather than the SPA shell.
	r.Route("/api", func(api chi.Router) {
		api.NotFound(s.handleAPINotFound)

		if s.auth != nil {
			s.mountAuthRoutes(api)
		}
		if s.oauth != nil {
			s.mountOAuthRoutes(api)
		}

		// Token management requires both a JWT verifier and the token service.
		if s.auth != nil && s.tokens != nil {
			s.mountTokenRoutes(api)
		}

		// Node registry and SSH validation require an authenticated caller.
		if s.servers != nil {
			s.mountServerRoutes(api)
		}

		// Container management routes to the node agent; a nil service (no
		// registry) mounts nothing. The agent dialer stays unwired until the
		// P3-CONN mTLS dial lands (see containers.Service.SetDial).
		containers.Mount(api, s.RequireAuth, containers.NewDefaultService(s.servers, s.cfg.Snapshot().Redis.Addr))

		// Shared realtime channel (WS + Redis pub/sub); auth via query token.
		ws.Mount(api, s.auth, s.cfg.Snapshot().Redis.Addr, s.logger)

		// Source providers (GitHub/GitLab/Gitea): list connections and repos.
		providers.Mount(api, s.RequireAuth, UserIDFromContext, providers.NewDefaultService(s.persistence, s.cfg.Snapshot().SecretKey, s.logger))
	})

	spa, err := newSPAHandler()
	if err != nil {
		return nil, err
	}
	r.NotFound(spa.ServeHTTP)

	return r, nil
}

// apiError is the JSON body returned for API failures.
type apiError struct {
	Message string `json:"message"`
}

// handleAPINotFound answers unknown /api routes with a JSON 404.
func (s *Server) handleAPINotFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
}

// requestLogger logs one structured line per request, including the request ID
// produced by Chi's RequestID middleware.
func (s *Server) requestLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}

			s.logger.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", middleware.GetReqID(r.Context())),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

// healthResponse is the JSON body returned by /healthz.
type healthResponse struct {
	Status string `json:"status"`
	DB     string `json:"db"`
	Redis  string `json:"redis"`
}

// handleHealthz reports control-plane dependency health as JSON. It returns 200
// when PostgreSQL and Redis both answer, and 503 when either is down.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	dbStatus, redisStatus := s.checkDependencies(r.Context())

	response := healthResponse{Status: statusOK, DB: dbStatus, Redis: redisStatus}
	status := http.StatusOK
	if dbStatus != statusOK || redisStatus != statusOK {
		response.Status = "degraded"
		status = http.StatusServiceUnavailable
	}

	writeJSON(w, status, response)
}

// checkDependencies pings PostgreSQL and Redis concurrently.
func (s *Server) checkDependencies(ctx context.Context) (db string, redis string) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		db = s.ping(ctx, "postgres", s.db)
	}()
	go func() {
		defer wg.Done()
		redis = s.ping(ctx, "redis", s.redis)
	}()

	wg.Wait()
	return db, redis
}

// ping runs one health check and maps the result to a status string.
func (s *Server) ping(ctx context.Context, name string, p Pinger) string {
	if err := p.Ping(ctx); err != nil {
		s.logger.Warn("health check failed", "dependency", name, "error", err)
		return statusDown
	}
	return statusOK
}

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// Run starts the HTTP server and blocks until ctx is cancelled (typically by a
// signal handler) or the listener fails, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	if s.closer != nil {
		defer s.closer()
	}

	snap := s.cfg.Snapshot()
	addr := net.JoinHostPort(snap.Server.Addr, strconv.Itoa(snap.Server.Port))
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http server listening", "addr", addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return nil
}
