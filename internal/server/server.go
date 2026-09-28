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
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/server/ws"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/templates"
	"github.com/justindeelux/gotham/internal/webhooks"
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
	deploy      deploy.DeployService
	proxy       proxy.ProxyService
	backups     databases.BackupService
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
	}
	s.closer = func() {
		// The deploy service owns its worker pool and realtime publisher;
		// shutting it down first stops in-flight deployments before the
		// shared Redis pinger goes away. The backup service stops its cron
		// scheduler for the same reason.
		if s.backups != nil {
			_ = s.backups.Close()
		}
		if closer, ok := s.deploy.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		_ = redisClient.Close()
		limiter.Close()
		if oauthService != nil {
			oauthService.Close()
		}
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
		// registry) mounts nothing. The mTLS agent dialer is plugged in here
		// (the P3-CONN seam) so the container routes and the databases
		// provisioning path both reach a node agent.
		containerService := s.containerService()
		containers.Mount(api, s.RequireAuth, containerService)

		// Traefik proxy synchronization (BE-6.1) and the SSL surface
		// (BE-6.2): the shared container service provisions the
		// gotham-traefik container and the mTLS agent dialer pushes the
		// generated configuration. A nil service (no database, no container
		// service, or FEATURE_PROXY=false) mounts nothing and leaves the
		// deploy lifecycle without a proxy hook. Every endpoint under this
		// group mutates node state or holds DNS credentials, so it requires
		// the admin scope on top of authentication (JWT sessions already
		// hold every scope).
		s.proxy = s.proxyService(containerService)
		adminOnly := func(next http.Handler) http.Handler {
			return s.RequireAuth(RequireScopes(auth.ScopeAdmin)(next))
		}
		sslConfig := proxy.SSLConfig{
			Secret: s.cfg.Snapshot().SecretKey,
			Logger: s.logger,
			Resync: s.resyncProxyNodes,
		}
		if s.persistence != nil {
			sslConfig.Store = proxy.NewStoreSSL(s.persistence)
		}
		// Redirect rules (BE-6.3) and the certificate status read path share
		// the same store, the same admin scope and the same best-effort
		// resync as the SSL surface; the status service dials the node agent
		// lazily per read.
		redirectConfig := proxy.RedirectConfig{Logger: s.logger, Resync: s.resyncProxyNodes}
		statusConfig := proxy.CertificateStatusConfig{Logger: s.logger}
		if s.persistence != nil {
			redirectConfig.Store = proxy.NewStoreRedirect(s.persistence)
			statusConfig.Store = proxy.NewStoreCertificateStatus(s.persistence)
		}
		if dialer, ok := s.servers.(proxyDialer); ok {
			statusConfig.Dial = func(ctx context.Context, serverID uuid.UUID) (proxy.ACMEReader, error) {
				return dialer.DialProxyClient(ctx, serverID)
			}
		}
		proxy.Mount(api, adminOnly, s.proxy,
			proxy.NewDefaultProviderService(sslConfig),
			proxy.NewDefaultCertificateService(sslConfig),
			proxy.NewDefaultRedirectService(redirectConfig),
			proxy.NewDefaultCertificateStatusService(statusConfig))

		// Shared realtime channel (WS + Redis pub/sub); auth via query token.
		ws.Mount(api, s.auth, s.cfg.Snapshot().Redis.Addr, s.logger)

		// Source providers (GitHub/GitLab/Gitea): list connections and repos.
		providerSvc := providers.NewDefaultService(s.persistence, s.cfg.Snapshot().SecretKey, s.logger)
		providers.Mount(api, s.RequireAuth, UserIDFromContext, providerSvc)

		// Application deploy orchestration (BE-4.3): a nil service (no
		// database) or FEATURE_APPLICATIONS=false mounts nothing, so Phases
		// 0–3 stay unaffected. The service is kept on the server so the
		// closer can stop its worker pool and publisher on shutdown. The
		// provider service is passed in for deploy keys (BE-4.4b): registering
		// a key on the Git host needs the same stored connection the webhook
		// lifecycle uses. The proxy service (BE-6.1) receives a best-effort
		// resync after application mutations and successful deployments.
		s.deploy = s.deployService(providerSvc, s.proxy)
		deploy.Mount(api, s.RequireAuth, UserIDFromContext, s.deploy)

		// Push webhooks (BE-4.4): the public, signature-verified delivery
		// endpoint plus authenticated hook management. It reuses the deploy
		// service instance above so both share one worker pool.
		webhooks.Mount(api, s.RequireAuth, UserIDFromContext, s.webhookService(providerSvc))

		// Managed databases (BE-5.1): same container service as above, so a
		// database container is created through the shared container service
		// rather than a second agent path.
		databases.Mount(api, s.RequireAuth, UserIDFromContext, s.databaseService(containerService))

		// Backup and restore surface (BE-5.2), same container service and
		// same feature flag as the databases routes above: a nil service
		// (no database) or FEATURE_DATABASES=false mounts nothing. The
		// service owns the internal cron scheduler, started here and stopped
		// by the closer above.
		s.backups = s.backupService(containerService)
		databases.MountBackups(api, s.RequireAuth, UserIDFromContext, s.backups)

		// Compose services (BE-7.1): one docker-compose project per service,
		// run by the node agent's compose CLI. Like the proxy group, the
		// whole surface mutates node state with user-supplied compose, so it
		// requires the admin scope on top of authentication (JWT sessions
		// already hold every scope). A nil service (no database, no agent
		// dialer, or FEATURE_SERVICES=false) mounts nothing.
		services.Mount(api, adminOnly, UserIDFromContext, s.composeService())

		// One-click templates (BE-7.2): the built-in catalog (embedded in
		// the binary) and the render engine. The surface is read-only and
		// stateless; a rendered document is created and deployed through
		// the services routes above, so it shares their admin scope and
		// rides the same FEATURE_SERVICES kill switch (a nil catalog
		// mounts nothing).
		templates.Mount(api, adminOnly, templates.NewDefaultService(s.logger))
	})

	spa, err := newSPAHandler()
	if err != nil {
		return nil, err
	}
	r.NotFound(spa.ServeHTTP)

	return r, nil
}

// deployService builds the deploy domain service for the HTTP wiring: the
// database, the key that opens sealed application secrets and the realtime
// publisher, plus the mTLS agent dialer when the concrete node registry is
// available. providerSvc contributes deploy-key registration on the Git host
// (nil, or a provider service that cannot register keys, leaves the registrar
// unwired and deploy-key creation answers a clear error). proxySvc receives
// the best-effort resync after domain changes and successful deployments.
// Tests pass a fake registry that cannot dial agents, which leaves the dialer
// unwired instead of forcing a wider interface change. It returns nil (no
// database, or FEATURE_APPLICATIONS=false) so deploy.Mount is a no-op.
func (s *Server) deployService(providerSvc providers.ProviderService, proxySvc proxy.ProxyService) deploy.DeployService {
	if s.persistence == nil {
		return nil
	}
	cfg := deploy.Config{
		Store:     s.persistence,
		Secret:    s.cfg.Snapshot().SecretKey,
		RedisAddr: s.cfg.Snapshot().Redis.Addr,
		Logger:    s.logger,
	}
	if providerSvc != nil {
		if registrar, ok := providerSvc.(deploy.KeyRegistrar); ok {
			cfg.KeyRegistrar = registrar
		}
	}
	if dialer, ok := s.servers.(deploy.AgentDialer); ok {
		cfg.Dial = deploy.AgentDial(dialer)
	}
	if proxySvc != nil {
		cfg.Proxy = proxySvc
	}
	return deploy.NewDefaultService(cfg)
}

// webhookService builds the webhook domain service for the HTTP wiring from
// the deploy and provider services routes() already built: a delivery needs a
// deploy service to queue with (nil when there is no database or
// FEATURE_APPLICATIONS=false) and hook management needs a provider service
// that can reach the Git host. Either missing, it returns nil so
// webhooks.Mount registers nothing.
func (s *Server) webhookService(providerSvc providers.ProviderService) *webhooks.Service {
	if s.deploy == nil || providerSvc == nil {
		return nil
	}
	deployer, ok := s.deploy.(webhooks.Deployer)
	if !ok {
		return nil
	}
	installer, ok := providerSvc.(webhooks.Installer)
	if !ok {
		return nil
	}
	return webhooks.NewDefaultService(webhooks.Config{
		Store:     s.persistence,
		Installer: installer,
		Deployer:  deployer,
		Secret:    s.cfg.Snapshot().SecretKey,
		Logger:    s.logger,
	})
}

// containerDialer is the mTLS dial implemented by *servers.ServerService. The
// HTTP layer type-asserts its server registry to this interface, so tests that
// pass a fake registry simply leave the dialer unwired instead of forcing a
// wider interface change (deploy.AgentDialer declares the same contract).
type containerDialer interface {
	DialDockerClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.DockerClient, error)
}

// containerService builds the shared container service for the HTTP wiring and
// plugs the agent dialer, which is what makes container operations (and
// database provisioning through them) reach a node. It returns a nil
// interface when there is no server registry, so Mount stays a no-op.
func (s *Server) containerService() containers.ContainerService {
	service := containers.NewDefaultService(s.servers, s.cfg.Snapshot().Redis.Addr)
	if service == nil {
		return nil
	}
	if dialer, ok := s.servers.(containerDialer); ok {
		service.SetDial(func(ctx context.Context, server *servers.Server) (containers.DockerClient, error) {
			return dialer.DialDockerClient(ctx, server.ID)
		})
	}
	return service
}

// proxyDialer is the mTLS ProxyService dial implemented by
// *servers.ServerService. The HTTP layer type-asserts its server registry to
// this interface, so tests that pass a fake registry simply leave the dialer
// unwired instead of forcing a wider interface change (containerDialer and
// deploy.AgentDialer declare the same pattern).
type proxyDialer interface {
	DialProxyClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.ProxyClient, error)
}

// proxyService builds the proxy domain service for the HTTP wiring: the
// routing input from the database, the shared container service that
// provisions the gotham-traefik container, the mTLS agent dialer that pushes
// the generated configuration and the key that opens sealed DNS provider
// credentials. It returns nil (no database, no container service, or
// FEATURE_PROXY=false) so proxy.Mount is a no-op and the deploy lifecycle
// receives no resync hook.
func (s *Server) proxyService(containerService containers.ContainerService) proxy.ProxyService {
	if s.persistence == nil || containerService == nil {
		return nil
	}
	cfg := proxy.Config{
		Store:      s.persistence,
		Containers: containerService,
		Logger:     s.logger,
		Secret:     s.cfg.Snapshot().SecretKey,
		// Compose service hosts (BE-7.1) join the generated routing model
		// through the same generator; the adapter renders each stored
		// document, which this package cannot do without a cycle.
		Services: services.NewProxySource(s.persistence),
	}
	if dialer, ok := s.servers.(proxyDialer); ok {
		cfg.Dial = func(ctx context.Context, serverID uuid.UUID) (proxy.AgentClient, error) {
			return dialer.DialProxyClient(ctx, serverID)
		}
	}
	return proxy.NewDefaultService(cfg)
}

// resyncProxyNodes pushes the current desired proxy state to every node after
// an SSL mutation. It is best effort by contract: the mutation is already
// durable, and a failed node is reported as an error only so the mutation's
// caller can log it — the next sync (manual or deploy-driven) converges the
// node. Per-node failures are summarized without touching credentials.
func (s *Server) resyncProxyNodes(ctx context.Context) error {
	if s.proxy == nil {
		return nil
	}
	results, err := s.proxy.SyncAll(ctx)
	if err != nil {
		return err
	}
	failed := 0
	for _, result := range results {
		if result.Error != "" {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("proxy: SSL resync failed on %d of %d node(s)", failed, len(results))
	}
	return nil
}

// databaseService builds the databases domain service for the HTTP wiring: the
// database, the shared container service and the key that opens sealed
// credentials. It returns nil (no database, no container service, or
// FEATURE_DATABASES=false) so databases.Mount is a no-op.
func (s *Server) databaseService(containerService containers.ContainerService) databases.DatabaseService {
	if s.persistence == nil || containerService == nil {
		return nil
	}
	return databases.NewDefaultService(databases.Config{
		Store:      s.persistence,
		Containers: containerService,
		Secret:     s.cfg.Snapshot().SecretKey,
		Logger:     s.logger,
	})
}

// backupService builds the backup domain service for the HTTP wiring: the
// database, the shared container service, the key that opens sealed
// credentials and (through the service itself) the local backup directory
// and the cron scheduler. It returns nil (no database, no container service,
// or FEATURE_DATABASES=false) so databases.MountBackups stays a no-op.
func (s *Server) backupService(containerService containers.ContainerService) databases.BackupService {
	if s.persistence == nil || containerService == nil {
		return nil
	}
	return databases.NewDefaultBackupService(databases.BackupConfig{
		Store:      s.persistence,
		Containers: containerService,
		Secret:     s.cfg.Snapshot().SecretKey,
		Logger:     s.logger,
	})
}

// composeDialer is the mTLS ComposeService dial implemented by
// *servers.ServerService. The HTTP layer type-asserts its server registry to
// this interface, so tests that pass a fake registry simply leave the dialer
// unwired instead of forcing a wider interface change (containerDialer and
// proxyDialer declare the same pattern).
type composeDialer interface {
	DialComposeClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.ComposeClient, error)
}

// composeService builds the compose-service domain service for the HTTP
// wiring: the database plus the mTLS dialer that reaches each node's compose
// service. It returns nil (no database, no dialer, or FEATURE_SERVICES=false)
// so services.Mount is a no-op.
func (s *Server) composeService() services.ServiceService {
	if s.persistence == nil {
		return nil
	}
	cfg := services.Config{
		Store:  s.persistence,
		Logger: s.logger,
	}
	if dialer, ok := s.servers.(composeDialer); ok {
		cfg.Dial = func(ctx context.Context, serverID uuid.UUID) (services.ComposeAgent, error) {
			client, err := dialer.DialComposeClient(ctx, serverID)
			if err != nil {
				return nil, err
			}
			return services.NewGRPCComposeAgent(client), nil
		}
	}
	// Lifecycle changes resync the node's Phase 6 routing configuration, so a
	// deploy/stop/restart/delete is reflected in Traefik through the same
	// generator applications use.
	if s.proxy != nil {
		cfg.Proxy = s.proxy
	}
	return services.NewDefaultService(cfg)
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
