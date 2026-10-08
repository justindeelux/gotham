package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/clientip"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/notifications"
	"github.com/justindeelux/gotham/internal/projects"
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/server/ws"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/teams"
	"github.com/justindeelux/gotham/internal/templates"
	"github.com/justindeelux/gotham/internal/updates"
	"github.com/justindeelux/gotham/internal/webhooks"
	"github.com/justindeelux/gotham/updatecore"
)

// shutdownTimeout bounds graceful shutdown after the context is cancelled.
const shutdownTimeout = 5 * time.Second

// HTTP server timeouts. ReadHeaderTimeout bounds a slow request header;
// readTimeout bounds reading the whole request (headers plus body) so a slow
// body cannot pin a connection. idleTimeout bounds keep-alive connections
// between requests. WriteTimeout is deliberately unset: the services log
// stream and the WebSocket endpoint are long-lived, and a write deadline would
// cut a quiet but healthy stream (see Run).
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
)

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
	cfg    *config.Config
	logger *slog.Logger
	// secretKey is the credential-encryption key resolved once at startup:
	// the configured GOTHAM_SECRET_KEY, or a generated ephemeral one when it
	// is empty. Every subsystem that seals credentials is wired with this
	// value, so none can fall back to the publicly derivable SHA-256("").
	secretKey   string
	db          Pinger
	redis       Pinger
	auth        AuthService
	oauth       OAuthService
	tokens      TokenService
	servers     ServerService
	persistence *store.Store
	teamService teams.TeamService
	// invites is the teams-domain slice backing invite registration (P-A2);
	// it is the same object as teamService, typed for the auth seam.
	invites auth.InviteAcceptor
	// allowRegistration mirrors GOTHAM_AUTH_ALLOW_REGISTRATION (test/dev only).
	allowRegistration bool
	deploy            deploy.DeployService
	proxy             proxy.ProxyService
	backups           databases.BackupService
	// jobLeases is the exclusion registry shared by the database service and
	// the backup manager: a backup/restore owns the volume while a lifecycle
	// Start/Restart is refused, and vice versa.
	jobLeases         *databases.JobLeases
	notify            notifications.NotificationService
	webhooks          *webhooks.Service
	metrics           *servers.MetricsSweeper
	sessionSweeper    *auth.SessionSweeper
	databasesRetainer *databases.RetentionSweeper
	updates           updates.Service
	// realtime is the mounted WebSocket hub, Redis bridge and log-stream
	// manager; the closer owns its lifecycle.
	realtime       *ws.Realtime
	authLimiter    *ipRateLimiter
	refreshLimiter *ipRateLimiter
	router         http.Handler
	closer         func()

	// trustedProxies are the peers whose X-Forwarded-For / X-Forwarded-Proto
	// headers are honored for client-IP keying and scheme detection. Empty
	// trusts no peer.
	trustedProxies []netip.Prefix

	// oauthCodes holds one-time OAuth exchange codes and their browser binding.
	oauthCodes *oauthCodeStore
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
	secretKey, generatedSecret := ensureSecretKey(snap.SecretKey)
	if generatedSecret {
		logger.Warn("GOTHAM_SECRET_KEY is empty; generated an ephemeral credential-encryption key for this process. Stored credentials will not survive a restart — set GOTHAM_SECRET_KEY in production.")
	}
	redisClient := newRedisPinger(snap.Redis.Addr)
	limiter := newDefaultAuthLimiter()
	refreshLimiter := newDefaultRefreshLimiter()

	trustedProxies, err := clientip.Parse(snap.Server.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("server: trusted proxies: %w", err)
	}
	warnTrustedProxyConfig(logger, trustedProxies, snap, oauthService != nil)

	// Prefer the store's pool so the control plane does not open a second
	// PostgreSQL connection just for the health check.
	var db Pinger
	if st != nil && st.DB != nil {
		db = st.DB
	} else {
		db = newPostgresPinger(snap.Database.DSN)
	}

	s := &Server{
		cfg:               cfg,
		logger:            logger,
		secretKey:         secretKey,
		db:                db,
		allowRegistration: snap.Auth.AllowRegistration,
		redis:             redisClient,
		auth:              authService,
		oauth:             oauthService,
		oauthCodes:        newOAuthCodeStore(),
		tokens:            tokenService,
		servers:           serverService,
		persistence:       st,
		authLimiter:       limiter,
		refreshLimiter:    refreshLimiter,
		trustedProxies:    trustedProxies,
		jobLeases:         databases.NewJobLeases(),
	}
	s.closer = func() {
		// The realtime hub, bridge and log streams stop first: they hold their
		// own Redis client and publish to clients while the rest of the plane
		// shuts down. Closing them here releases Redis connections and lets
		// connection handlers finish instead of leaking past shutdown (B1-8).
		if s.realtime != nil {
			s.realtime.Close()
		}
		// The deploy service owns its worker pool and realtime publisher;
		// shutting it down first stops in-flight deployments before the
		// shared Redis pinger goes away. The backup service stops its cron
		// scheduler for the same reason, and the metrics retention stops its
		// sweep. The notification dispatcher stops its delivery pool.
		if s.backups != nil {
			_ = s.backups.Close()
		}
		if s.metrics != nil {
			s.metrics.Close()
		}
		if s.sessionSweeper != nil {
			s.sessionSweeper.Close()
		}
		if s.databasesRetainer != nil {
			s.databasesRetainer.Close()
		}
		if closer, ok := s.notify.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		if s.webhooks != nil {
			_ = s.webhooks.Close()
		}
		if closer, ok := s.deploy.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		_ = redisClient.Close()
		limiter.Close()
		refreshLimiter.Close()
		if oauthService != nil {
			oauthService.Close()
		}
		if s.oauthCodes != nil {
			s.oauthCodes.Close()
		}
	}

	router, err := s.routes()
	if err != nil {
		return nil, err
	}
	s.router = router

	return s, nil
}

// warnTrustedProxyConfig logs the two configuration footguns around
// GOTHAM_TRUSTED_PROXIES: prefixes broad enough that a host inside them can
// spoof forwarded client addresses, and a missing configuration on a
// deployment that likely sits behind a TLS-terminating proxy (OAuth enabled or
// an https redirect base), where X-Forwarded-Proto is then ignored and secure
// cookies are downgraded.
func warnTrustedProxyConfig(logger *slog.Logger, trusted []netip.Prefix, snap config.Values, oauth bool) {
	for _, prefix := range trusted {
		if prefix.Bits() <= 8 {
			logger.Warn("GOTHAM_TRUSTED_PROXIES contains an overly broad prefix; every host it covers can spoof the forwarded client address and scheme. List the proxy's exact IPs.",
				"prefix", prefix.String())
		}
	}
	if len(trusted) == 0 && (oauth || strings.HasPrefix(snap.OAuth.GitHub.RedirectURL, "https://")) {
		logger.Warn("no trusted proxies configured: X-Forwarded-For and X-Forwarded-Proto are ignored. Behind a TLS-terminating proxy this downgrades secure cookies and makes OAuth fail closed; set GOTHAM_TRUSTED_PROXIES to the proxy's address.")
	}
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
	s.baseMiddleware(r)

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

		// Running binary version (JUS-7): any authenticated caller, no team
		// scope. It reads through the versionReporter seam, so it mounts
		// whenever auth is wired even without a node registry.
		if s.auth != nil {
			s.mountVersionRoutes(api)
		}

		// Server time series (BE-8.4): the 30-day retention sweep runs beside
		// the routes. A nil retention (no database, or FEATURE_METRICS=false)
		// leaves the heartbeat path without persistence, and a nil store
		// never fails the heartbeat.
		s.metrics = s.metricsRetention()
		if s.metrics != nil {
			s.metrics.Start()
		}

		// Refresh/logout sessions are pruned periodically: expired rows and
		// revoked rows past the reuse-detection window are dead weight. The
		// sweep mirrors the metrics retention and is stopped by the closer.
		s.sessionSweeper = s.sessionRetention()
		if s.sessionSweeper != nil {
			s.sessionSweeper.Start()
		}

		// Teams (BE-8.2): the team service backs the teams/invites routes
		// (mounted by teams.Mount only when FEATURE_TEAMS is on) and the
		// active-team middleware behind every team-scoped resource route
		// below. It is built even when the feature flag is off, because the
		// middleware then always resolves the caller's personal team. A nil
		// service (no database: the handler tests) leaves resource routes on
		// their pre-teams, creator-scoped behavior.
		if s.persistence != nil {
			teamSvc := teams.NewService(teams.Config{Store: s.persistence, Logger: s.logger})
			s.teamService = teamSvc
			// The same service backs invite registration (P-A2): the auth
			// service consumes invites through the auth.InviteAcceptor seam,
			// so auth never imports teams.
			s.invites = teamSvc
		}
		teams.Mount(api, s.adminScopeAuth, UserIDFromContext, s.teamService)

		// Notifications (BE-8.3): the team-scoped channel CRUD plus the
		// dispatcher behind the deploy and backup hooks below. A nil service
		// (no database, or FEATURE_NOTIFICATIONS=false) mounts nothing and
		// leaves both hooks unwired, so the deploy/backup flows are
		// untouched.
		s.notify = s.notificationService()
		notifications.Mount(api, s.withTeam(), UserIDFromContext, s.notify)

		// Container management routes to the node agent; a nil service (no
		// registry) mounts nothing. The mTLS agent dialer is plugged in here
		// (the P3-CONN seam) so the container routes and the databases
		// provisioning path both reach a node agent. The routes run through
		// the team chain and the service authorizes the target node's team and
		// role before any agent or cache access, so a stranger cannot list or
		// mutate a team's containers (and a missing vs foreign node is
		// indistinguishable).
		containerService := s.containerService()
		containers.Mount(api, s.withTeam(), containerService)

		// Deleted database volumes are kept for the grace window and then
		// removed by the retention sweep. The loop starts here and is stopped
		// by the closer above; a nil retainer (no database or container
		// service) makes Start a no-op.
		s.databasesRetainer = s.databasesRetention(containerService)
		s.databasesRetainer.Start()

		// Traefik proxy synchronization (BE-6.1) and the SSL surface
		// (BE-6.2): the shared container service provisions the
		// gotham-traefik container and the mTLS agent dialer pushes the
		// generated configuration. A nil service (no database, no container
		// service, or FEATURE_PROXY=false) mounts nothing and leaves the
		// deploy lifecycle without a proxy hook. The surface is split by
		// blast radius: per-application certificates and redirects run
		// through the team chain (their handlers authorize the owning
		// application's team), while the node-wide sync and the DNS-provider
		// CRUD require a real platform operator (RequirePlatformAdmin: an
		// admin-scoped API token, an admin-role session, or an email listed
		// in PLATFORM_ADMINS).
		s.proxy = s.proxyService(containerService)
		sslConfig := proxy.SSLConfig{
			Secret: s.secretKey,
			Logger: s.logger,
			Resync: s.resyncProxyNodes,
		}
		if s.persistence != nil {
			sslConfig.Store = proxy.NewStoreSSL(s.persistence)
		}
		// Redirect rules (BE-6.3) and the certificate status read path share
		// the same store and the same best-effort resync as the SSL surface;
		// the status service dials the node agent lazily per read.
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
		platformOnly := func(next http.Handler) http.Handler {
			return s.RequireAuth(s.RequirePlatformAdmin(next))
		}
		proxy.Mount(api, s.withTeam(), platformOnly, s.proxy,
			proxy.NewDefaultProviderService(sslConfig),
			proxy.NewDefaultCertificateService(sslConfig),
			proxy.NewDefaultRedirectService(redirectConfig),
			proxy.NewDefaultCertificateStatusService(statusConfig))

		// Self-update (BE-9.1): the check route is available to any
		// authenticated caller, while apply requires a platform operator
		// because it replaces the whole runtime (a plain session must never
		// be able to swap the binary). A nil service (FEATURE_UPDATES=false,
		// or unusable configuration) mounts nothing.
		s.updates = s.updatesService()
		updates.Mount(api, s.readScopeAuth, platformOnly, s.isPlatformOperator, s.updates)

		// Shared realtime channel (WS + Redis pub/sub); auth via query token.
		// Log subscriptions are authorized against the node's team before the
		// client joins the room. The returned Realtime owns the lifecycle
		// (bridge, Redis, hub) and is closed by s.closer.
		s.realtime = ws.Mount(api, s.auth, s.cfg.Snapshot().Redis.Addr, s.logger, s.authorizeLogSubscription)

		// Starting a container log stream. The FE drawer calls this on mount;
		// the stream is idempotent per channel and reaped once no subscriber
		// remains. It is a read surface, so it takes the read scope rather than
		// the team write gate (log viewing is allowed for every team role).
		api.Group(func(protected chi.Router) {
			protected.Use(s.readScopeAuth)
			protected.Post("/v1/servers/{id}/containers/{containerID}/logs/stream", s.handleStartLogStream)
		})

		// Source providers (GitHub/GitLab/Gitea): list connections and repos,
		// plus create/connect. The method-based scope boundary keeps reads on
		// the read scope and the create/connect mutations on the deploy scope,
		// so a read-only API token cannot add a connection. The deploy service
		// feeds the disconnect in-use check, so a connection applications
		// still deploy through is refused instead of stranding their hooks.
		providerSvc := providers.NewDefaultService(s.persistence, s.secretKey, s.logger,
			func(ctx context.Context, userID uuid.UUID) ([]providers.ConnectionApplication, error) {
				if s.deploy == nil {
					return nil, nil
				}
				applications, err := s.deploy.ListApplications(ctx, userID, deploy.ApplicationFilter{})
				if err != nil {
					return nil, err
				}
				out := make([]providers.ConnectionApplication, 0, len(applications))
				for _, app := range applications {
					out = append(out, providers.ConnectionApplication{
						Name:     app.Name,
						Provider: app.Provider,
						CloneURL: app.CloneURL,
					})
				}
				return out, nil
			})
		providers.Mount(api, s.resourceScopeAuth, UserIDFromContext, providerSvc)

		// Application deploy orchestration (BE-4.3): a nil service (no
		// database) or FEATURE_APPLICATIONS=false mounts nothing, so Phases
		// 0–3 stay unaffected. The service is kept on the server so the
		// closer can stop its worker pool and publisher on shutdown. The
		// provider service is passed in for deploy keys (BE-4.4b): registering
		// a key on the Git host needs the same stored connection the webhook
		// lifecycle uses. The proxy service (BE-6.1) receives a best-effort
		// resync after application mutations and successful deployments.
		s.deploy = s.deployService(providerSvc, s.proxy)
		deploy.Mount(api, s.withTeam(), UserIDFromContext, s.deploy)

		// Push webhooks (BE-4.4) and preview deployments (BE-8.1): the public,
		// signature-verified delivery endpoint plus authenticated hook and
		// preview listing. It reuses the deploy service instance above so both
		// share one worker pool, and the same provider service posts the
		// preview badge comment. The management routes run through the team
		// chain, so a demoted or removed creator can no longer install or
		// remove a team application's hook. The preview surface (pull_request
		// handling, the orphan sweep, the listing route) is gated by
		// FEATURE_PREVIEWS; push deliveries are untouched by that flag.
		s.webhooks = s.webhookService(providerSvc)
		webhooks.Mount(api, s.withTeam(), UserIDFromContext, s.webhooks)
		s.webhooks.StartPreviews()

		// Managed databases (BE-5.1): same container service as above, so a
		// database container is created through the shared container service
		// rather than a second agent path.
		databaseSvc := s.databaseService(containerService)
		databases.Mount(api, s.withTeam(), RequireScopes(auth.ScopeDeploy), UserIDFromContext, databaseSvc)

		// Backup and restore surface (BE-5.2), same container service and
		// same feature flag as the databases routes above: a nil service
		// (no database) or FEATURE_DATABASES=false mounts nothing. The
		// service owns the internal cron scheduler, started here and stopped
		// by the closer above.
		s.backups = s.backupService(containerService)
		databases.MountBackups(api, s.withTeam(), s.resourceScopeAuth, UserIDFromContext, s.backups)

		// Compose services (BE-7.1): one docker-compose project per service,
		// run by the node agent's compose CLI. The whole surface mutates node
		// state with user-supplied compose, so it rides the team chain and the
		// API-token scope boundary (reads need read, mutations need deploy;
		// JWT sessions already hold every scope). A nil service (no database,
		// no agent dialer, or FEATURE_SERVICES=false) mounts nothing.
		composeSvc := s.composeService()
		services.Mount(api, s.withTeam(), UserIDFromContext, composeSvc)

		// Projects and environments (Phase 13, PE-1): the grouping layer for
		// every workload. It rides the team chain like the other resource
		// packages (reads need read, mutations need deploy plus
		// owner/admin). A nil service (no database) mounts nothing.
		// Resource counts come from the real counter over the resource
		// tables, and the resources surface lists through the domain
		// services above (nil services stay nil-safe: the endpoint answers a
		// clear error when its lister is missing).
		projects.Mount(api, s.withTeam(), UserIDFromContext, s.projectService(s.deploy, composeSvc, databaseSvc))

		// One-click templates (BE-7.2): the built-in catalog (embedded in
		// the binary) and the render engine. The surface is read-only and
		// stateless; a rendered document is created and deployed through
		// the services routes above, so it rides the same FEATURE_SERVICES
		// kill switch and the same resource scope boundary (a nil catalog
		// mounts nothing).
		templates.Mount(api, s.resourceScopeAuth, templates.NewDefaultService(s.logger))
	})

	spa, err := newSPAHandler()
	if err != nil {
		return nil, err
	}
	r.NotFound(spa.ServeHTTP)

	return r, nil
}

// baseMiddleware installs the cross-cutting middleware shared by every route.
// The order is load-bearing: the request logger wraps the recoverer, so a panic
// recovered below it still produces one structured request line (status 500)
// instead of the line vanishing with the panic.
func (s *Server) baseMiddleware(r chi.Router) {
	r.Use(s.securityHeaders)
	r.Use(middleware.RequestID)
	r.Use(s.requestLogger())
	r.Use(middleware.Recoverer)
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
		Secret:    s.secretKey,
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
	// Terminal deployment results fan out to the BE-8.3 notification
	// dispatcher and the previews terminal-comment hook. The preview hook
	// reads s.webhooks lazily (built after the deploy service, like
	// PreviewCleanup) and runs detached, so a Git host can never slow the
	// deploy worker. Its semantics do not change BE-8.3 delivery.
	var notifier deploy.Notifier
	if n, ok := s.notify.(deploy.Notifier); ok {
		notifier = n
	}
	cfg.Notifier = deployNotifier{
		primary: notifier,
		preview: func(result deploy.DeployResult) {
			if s.webhooks == nil {
				return
			}
			go func() {
				notifyCtx, cancel := context.WithTimeout(context.Background(), previewNotifyTimeout)
				defer cancel()
				s.webhooks.DeployFinished(notifyCtx, result)
			}()
		},
	}
	// Preview siblings hang off the base application outside the deploy
	// schema; they are torn down before the base row is deleted, and a
	// failure aborts the delete (the base FK cascades the bindings away, so
	// deleting it after a failed teardown would leave the siblings untracked)
	// — BE-8.1. The closure reads s.webhooks lazily: the webhook service is
	// built after the deploy service, and the hook only fires once the server
	// is serving.
	cfg.PreviewCleanup = func(ctx context.Context, appID uuid.UUID) error {
		if s.webhooks == nil {
			return nil
		}
		return s.webhooks.CleanupApplication(ctx, appID)
	}
	// The automatic hook lifecycle (BE-4.4) is resolved lazily for the same
	// reason: webhookService consumes the deploy service as its deployer, so
	// it can only be built after this one. Until then the closures answer nil
	// and application create/delete simply skip hook management.
	cfg.Hooks = func() deploy.HookLifecycle {
		if s.webhooks == nil {
			return nil
		}
		return s.webhooks
	}
	return deploy.NewDefaultService(cfg)
}

// previewNotifyTimeout bounds the detached preview terminal-comment hook: a
// slow lookup or Git host is cut off instead of accumulating goroutines.
const previewNotifyTimeout = 15 * time.Second

// deployNotifier fans one terminal deployment result out to the configured
// notifier (BE-8.3) and the previews terminal-comment hook (BE-8.1). The
// preview hook returns immediately (it dispatches its own goroutine), and a
// nil primary leaves BE-8.3 semantics untouched.
type deployNotifier struct {
	primary deploy.Notifier
	preview func(result deploy.DeployResult)
}

// DeployFinished implements deploy.Notifier.
func (n deployNotifier) DeployFinished(ctx context.Context, result deploy.DeployResult) {
	if n.primary != nil {
		n.primary.DeployFinished(ctx, result)
	}
	if n.preview != nil {
		n.preview(result)
	}
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
	cfg := webhooks.Config{
		Store:          s.persistence,
		Installer:      installer,
		Deployer:       deployer,
		Secret:         s.secretKey,
		Logger:         s.logger,
		TrustedProxies: s.trustedProxies,
	}
	// Preview siblings are provisioned through the deploy service (clone +
	// system delete) and the badge comment through the provider service. Both
	// are optional seams: without them the preview path answers ignored and
	// push deliveries keep working.
	if provisioner, ok := s.deploy.(webhooks.PreviewProvisioner); ok {
		cfg.Provisioner = provisioner
	}
	if commenter, ok := providerSvc.(webhooks.Commenter); ok {
		cfg.Commenter = commenter
	}
	return webhooks.NewDefaultService(cfg)
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
		Secret:     s.secretKey,
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
		Secret:     s.secretKey,
		Logger:     s.logger,
		Leases:     s.jobLeases,
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
	cfg := databases.BackupConfig{
		Store:      s.persistence,
		Containers: containerService,
		Secret:     s.secretKey,
		Logger:     s.logger,
		Leases:     s.jobLeases,
	}
	if notifier, ok := s.notify.(databases.BackupNotifier); ok {
		cfg.Notifier = notifier
	}
	return databases.NewDefaultBackupService(cfg)
}

// notificationService builds the notifications domain service for the HTTP
// wiring: the database and the key that seals channel configs. The dispatcher
// behind it receives the terminal deploy and backup events. It returns nil
// (no database, or FEATURE_NOTIFICATIONS=false) so notifications.Mount is a
// no-op and both hooks stay unwired.
func (s *Server) notificationService() notifications.NotificationService {
	if s.persistence == nil {
		return nil
	}
	return notifications.NewDefaultService(notifications.Config{
		Store:  s.persistence,
		Secret: s.secretKey,
		Logger: s.logger,
	})
}

// metricsRetention builds the retention sweep for the server time series. It
// returns nil (no database, or FEATURE_METRICS=false) so the sweep and the
// metrics route stay off without affecting the rest of the servers surface.
func (s *Server) metricsRetention() *servers.MetricsSweeper {
	if s.persistence == nil || !servers.MetricsEnabled() {
		return nil
	}
	return servers.NewMetricsSweeper(s.persistence, s.logger)
}

// sessionRetention builds the stale-session sweep. It returns nil without a
// database (the handler tests) so no goroutine is started.
func (s *Server) sessionRetention() *auth.SessionSweeper {
	if s.persistence == nil {
		return nil
	}
	return auth.NewSessionSweeper(s.persistence, s.logger)
}

// databasesRetention builds the deleted-database grace-window sweep. It
// returns nil without a database or container service (the handler tests) so
// no goroutine is started.
func (s *Server) databasesRetention(containerService containers.ContainerService) *databases.RetentionSweeper {
	if s.persistence == nil || containerService == nil {
		return nil
	}
	return databases.NewRetentionSweeper(s.persistence, containerService, s.logger)
}

// versionReporter exposes the control-plane build version without widening the
// ServerService interface (the concrete servers.ServerService implements it).
type versionReporter interface{ Version() string }

// updatesService builds the self-update domain service. It reads the running
// version from the node registry when available, resolves the release public
// key from the embedded value (or GOTHAM_UPDATE_PUBLIC_KEY for development) and
// wires the release API and platform from the environment. It returns nil when
// FEATURE_UPDATES=false or the configuration is unusable, so updates.Mount is a
// no-op; with no public key Check still works but Apply stays disabled.
func (s *Server) updatesService() updates.Service {
	if !updates.Enabled() {
		return nil
	}
	current := s.currentVersion()
	// GOTHAM_UPDATE_CURRENT is a development seam for wrappers and tests. On a
	// release build (an embedded release key) it is ignored so a stray
	// environment value cannot pin the reported version; the node-registry
	// version is used instead.
	if fromEnv := updates.CurrentFromEnv(); fromEnv != "" {
		if updatecore.HasEmbeddedKey() {
			s.logger.Warn("updates: ignoring GOTHAM_UPDATE_CURRENT on a build with an embedded release key",
				"version", fromEnv)
		} else {
			current = fromEnv
		}
	}
	config, err := updates.FromEnv(current, s.logger)
	if err != nil {
		s.logger.Info("updates: release public key not configured; apply disabled", "reason", err)
	}
	svc, err := updates.NewService(config)
	if err != nil {
		s.logger.Warn("updates: self-update disabled", "error", err)
		return nil
	}
	return svc
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

// projectService builds the projects domain service for the HTTP wiring. It
// returns nil (no database) so projects.Mount is a no-op. Counts come from
// the real resource counter; the resources surface lists through the domain
// services (a nil service leaves its lister unwired and the endpoint answers
// a clear error).
func (s *Server) projectService(deploySvc deploy.DeployService, composeSvc services.ServiceService, databaseSvc databases.DatabaseService) projects.ProjectService {
	if s.persistence == nil {
		return nil
	}
	cfg := projects.Config{
		Store:   s.persistence,
		Counter: projects.StoreCounter{Store: s.persistence},
		Secret:  s.secretKey,
		Logger:  s.logger,
	}
	if deploySvc != nil {
		if lister, ok := deploySvc.(projects.ApplicationLister); ok {
			cfg.Applications = lister
		}
	}
	if composeSvc != nil {
		if lister, ok := composeSvc.(projects.ServiceLister); ok {
			cfg.Services = lister
		}
	}
	if databaseSvc != nil {
		if lister, ok := databaseSvc.(projects.DatabaseLister); ok {
			cfg.Databases = lister
		}
	}
	return projects.NewDefaultService(cfg)
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

	// Resume a staged update left behind by a crash/reboot during the health
	// window, then run the self-update loop (AUTO_UPDATE=true); both are bound
	// to the server lifetime.
	if s.updates != nil {
		if err := s.updates.Resume(ctx); err != nil {
			s.logger.Warn("updates: could not resume a staged update", "error", err)
		}
		s.updates.StartAuto(ctx)
	}

	snap := s.cfg.Snapshot()
	addr := net.JoinHostPort(snap.Server.Addr, strconv.Itoa(snap.Server.Port))
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		// WriteTimeout is intentionally unset: the service log stream
		// (?follow=true) and the WebSocket route stay open indefinitely, and
		// a write deadline would close a quiet but healthy stream.
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
