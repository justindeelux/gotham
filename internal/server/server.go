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
	"github.com/justindeelux/gotham/internal/backups"
	"github.com/justindeelux/gotham/internal/backupwatch"
	"github.com/justindeelux/gotham/internal/cleanup"
	"github.com/justindeelux/gotham/internal/clientip"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/databases"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/domainwatch"
	"github.com/justindeelux/gotham/internal/files"
	"github.com/justindeelux/gotham/internal/githubapp"
	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/instance"
	"github.com/justindeelux/gotham/internal/monitoring"
	"github.com/justindeelux/gotham/internal/notifications"
	"github.com/justindeelux/gotham/internal/projects"
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/runtimewatch"
	"github.com/justindeelux/gotham/internal/server/ws"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/taskevents"
	"github.com/justindeelux/gotham/internal/templates"
	"github.com/justindeelux/gotham/internal/terminal"
	"github.com/justindeelux/gotham/internal/toolchain"
	"github.com/justindeelux/gotham/internal/updates"
	"github.com/justindeelux/gotham/internal/volumes"
	"github.com/justindeelux/gotham/internal/webhooks"
	"github.com/justindeelux/gotham/updatecore"
)

const shutdownTimeout = 5 * time.Second

const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
)

const (
	statusOK   = "ok"
	statusDown = "down"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	cfg         *config.Config
	logger      *slog.Logger
	secretKey   string
	db          Pinger
	redis       Pinger
	auth        AuthService
	oauth       OAuthService
	tokens      TokenService
	servers     ServerService
	persistence *store.Store

	ephemeralSecret bool

	instanceSvc       *instance.Service
	deploy            deploy.DeployService
	proxy             proxy.ProxyService
	backups           backups.BackupService
	databaseSvc       databases.DatabaseService
	composeSvc        services.ServiceService
	toolchain         toolchain.ToolchainService
	jobLeases         *databases.JobLeases
	notify            notifications.NotificationService
	incidents         *incidents.Service
	monitoring        *monitoring.Service
	runtime           *runtimewatch.Service
	domainwatch       *domainwatch.Service
	backupwatch       *backupwatch.Service
	certificateExpiry *proxy.CertificateExpiryObserver
	webhooks          *webhooks.Service
	metrics           *servers.MetricsSweeper
	resourceHistory   *servers.ResourceHistorySweeper
	sessionSweeper    *auth.SessionSweeper
	databasesRetainer *databases.RetentionSweeper
	updates           updates.Service
	terminal          *terminal.Service
	realtime          *ws.Realtime
	authLimiter       *ipRateLimiter
	refreshLimiter    *ipRateLimiter
	mcpBaseURL        string
	router            http.Handler
	closer            func()

	trustedProxies []netip.Prefix

	oauthCodes *oauthCodeStore
}

func New(cfg *config.Config, logger *slog.Logger, authService AuthService, oauthService OAuthService, tokenService TokenService, serverService ServerService, st *store.Store) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("server: config is nil")
	}
	if logger == nil {
		return nil, errors.New("server: logger is nil")
	}

	snap := cfg.Snapshot()
	secretKey, generatedSecret, persistedSecret := ResolveSecretKey(snap.SecretKey, snap.CA.Dir)
	if persistedSecret {
		logger.Warn("GOTHAM_SECRET_KEY is empty; using the credential-encryption key stored in the CA directory. Set GOTHAM_SECRET_KEY in production.")
	}
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
	logDisabledFeatures(logger)

	var db Pinger
	if st != nil && st.DB != nil {
		db = st.DB
	} else {
		db = newPostgresPinger(snap.Database.DSN)
	}

	s := &Server{
		cfg:             cfg,
		logger:          logger,
		secretKey:       secretKey,
		ephemeralSecret: generatedSecret,
		db:              db,
		redis:           redisClient,
		auth:            authService,
		oauth:           oauthService,
		oauthCodes:      newOAuthCodeStore(),
		tokens:          tokenService,
		servers:         serverService,
		persistence:     st,
		authLimiter:     limiter,
		refreshLimiter:  refreshLimiter,
		trustedProxies:  trustedProxies,
		jobLeases:       databases.NewJobLeases(),
	}
	if snap.Server.Port > 0 {
		s.mcpBaseURL = "http://127.0.0.1:" + strconv.Itoa(snap.Server.Port)
	}
	s.closer = func() {
		if s.realtime != nil {
			s.realtime.Close()
		}
		if s.terminal != nil {
			s.terminal.CloseAll()
		}
		if s.backups != nil {
			_ = s.backups.Close()
		}
		if closer, ok := s.databaseSvc.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		if closer, ok := s.composeSvc.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		if s.metrics != nil {
			s.metrics.Close()
		}
		if s.resourceHistory != nil {
			s.resourceHistory.Close()
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
		if closer, ok := s.toolchain.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		if closer, ok := s.servers.(interface{ Close() }); ok {
			closer.Close()
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

func logDisabledFeatures(logger *slog.Logger) {
	for _, feature := range []struct {
		module  string
		flag    string
		enabled bool
	}{
		{"Applications", deploy.FeatureEnv, deploy.Enabled()},
		{"Previews", webhooks.FeatureEnv, webhooks.Enabled()},
		{"Databases", databases.FeatureEnv, databases.Enabled()},
		{"Services", services.FeatureEnv, services.Enabled()},
		{"Files", files.FeatureEnv, files.Enabled()},
		{"Notifications", notifications.FeatureEnv, notifications.Enabled()},
		{"Proxy", proxy.FeatureEnv, proxy.Enabled()},
		{"Metrics", servers.FeatureEnv, servers.MetricsEnabled()},
		{"Updates", updates.FeatureEnv, updates.Enabled()},
	} {
		if !feature.enabled {
			logger.Warn("feature disabled on this control plane",
				"module", feature.module, "flag", feature.flag+"=false")
		}
	}
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) routes() (http.Handler, error) {
	r := chi.NewRouter()
	s.baseMiddleware(r)

	r.Get("/healthz", s.handleHealthz)

	if s.tokens != nil {
		s.mountMCPHTTP(r)
	}

	r.Route("/api", func(api chi.Router) {
		api.NotFound(s.handleAPINotFound)

		if s.auth != nil {
			s.mountMCPRoutes(api)
			s.mountAuthRoutes(api)
		}
		if s.oauth != nil {
			s.mountOAuthRoutes(api)
		}

		if s.auth != nil && s.tokens != nil {
			s.mountTokenRoutes(api)
		}

		if s.servers != nil {
			s.mountServerRoutes(api)
			s.mountResourceHistoryRoutes(api)
		}

		if s.toolchain == nil {
			if svc := s.toolchainService(); svc != nil {
				s.toolchain = svc
				svc.Start()
			}
		}
		s.mountToolchainRoutes(api)

		if s.auth != nil {
			s.mountVersionRoutes(api)
		}

		s.metrics = s.metricsRetention()
		if s.metrics != nil {
			s.metrics.Start()
		}
		s.resourceHistory = s.resourceHistoryRetention()
		if s.resourceHistory != nil {
			s.resourceHistory.Start()
		}

		s.sessionSweeper = s.sessionRetention()
		if s.sessionSweeper != nil {
			s.sessionSweeper.Start()
		}

		s.notify = s.notificationService()
		if nodes, ok := s.servers.(interface{ SetStateNotifier(servers.StateNotifier) }); ok {
			notifier, _ := s.notify.(servers.StateNotifier)
			nodes.SetStateNotifier(notifier)
		}
		notifications.Mount(api, s.resourceAuth(), UserIDFromContext, s.notify)

		s.incidents = s.incidentService()
		if s.incidents != nil {
			incidents.Mount(api, s.incidentAuth(), UserIDFromContext, s.incidents)
			s.runtime = runtimewatch.NewService(runtimewatch.Config{
				Store:     s.persistence,
				Incidents: s.incidents,
				Logger:    s.logger,
			})
			if nodes, ok := s.servers.(interface {
				SetIncidentRecorder(servers.IncidentRecorder)
			}); ok {
				nodes.SetIncidentRecorder(incidentConnectivity{svc: s.incidents, runtime: s.runtime, logger: s.logger})
			}
			if nodes, ok := s.servers.(interface {
				SetRuntimeObserver(servers.RuntimeObserver)
			}); ok && s.runtime != nil {
				nodes.SetRuntimeObserver(s.runtime)
			}
		}
		s.domainwatch = s.domainwatchService()
		domainwatch.Mount(api, s.resourceAuth(), s.domainwatch)

		platformOnly := func(next http.Handler) http.Handler {
			return s.RequireAuth(s.RequireOwner(next))
		}
		containerService := s.containerService()
		containers.Mount(api, s.resourceAuth(), containerService)
		volumeService, _ := containerService.(containers.VolumeService)
		if volumeService != nil {
			containers.MountVolumes(api, platformOnly, volumeService)
		}
		cleanup.Mount(api, platformOnly, UserIDFromContext, s.cleanupService(volumeService))

		filesAuth := s.resourceAuth()
		files.Mount(api, filesAuth, files.TransferAuthorizerFunc(s.authorizeFileTransfer), s.fileService(), s.logger)

		s.databasesRetainer = s.databasesRetention(containerService)
		s.databasesRetainer.Start()

		s.proxy = s.proxyService(containerService)
		sslConfig := proxy.SSLConfig{
			Secret: s.secretKey,
			Logger: s.logger,
			Resync: s.resyncProxyNodes,
		}
		if s.persistence != nil {
			sslConfig.Store = proxy.NewStoreSSL(s.persistence)
		}
		redirectConfig := proxy.RedirectConfig{Logger: s.logger, Resync: s.resyncProxyNodes, GuardDomains: s.guardDomains}
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
		s.monitoring = s.monitoringService()
		if s.monitoring != nil {
			if nodes, ok := s.servers.(interface {
				SetPressureRecorder(servers.PressureRecorder)
			}); ok {
				nodes.SetPressureRecorder(s.monitoring)
			}
			monitoring.Mount(api, platformOnly, UserIDFromContext, s.monitoring)
		}
		if s.auth != nil && s.servers != nil {
			s.terminal = s.terminalService()
			terminal.Mount(api, platformOnly, s.terminal, s.auth, s.terminalAuthorizer(), UserIDFromContext, s.logger)
		}
		if s.persistence != nil {
			instanceSvc := instance.NewService(instance.NewStoreRepository(s.persistence), s.logger)
			if panel, ok := s.proxy.(interface {
				ReconcilePanelCertificates(context.Context, []string) error
			}); ok {
				instanceSvc.SyncPanelCertificates = panel.ReconcilePanelCertificates
			}
			instanceSvc.ResyncProxy = func(ctx context.Context) error {
				if syncer, ok := s.proxy.(interface {
					SyncControlPlane(context.Context) error
				}); ok {
					return syncer.SyncControlPlane(ctx)
				}
				return instance.ErrDomainNotSynced
			}
			if checker, ok := s.proxy.(interface {
				CheckControlPlaneHost(context.Context, string) error
			}); ok {
				instanceSvc.CheckControlPlaneHost = checker.CheckControlPlaneHost
			} else {
				checker := proxy.NewService(proxy.Config{Store: s.persistence, Services: services.NewProxySource(s.persistence)})
				instanceSvc.CheckControlPlaneHost = checker.CheckControlPlaneHost
			}
			s.instanceSvc = instanceSvc
			if err := instanceSvc.Reconcile(context.Background()); err != nil {
				s.logger.Warn("instance settings reconcile failed", "error", err)
			}
			instance.Mount(api, platformOnly, UserIDFromContext, instanceSvc)
			if dialer, ok := s.servers.(hostDialer); ok {
				nodeHosts := instance.NewNodeService(
					instance.NewStoreNodeRepository(s.persistence),
					nodeHostResolver{nodes: s.servers, dialer: dialer},
					instanceSvc, s.logger)
				instance.MountNode(api, platformOnly, UserIDFromContext, nodeHosts)
			}
			if setter, ok := s.oauth.(interface{ SetGitHubControlPlaneURLSource(func() string) }); ok {
				setter.SetGitHubControlPlaneURLSource(s.controlPlaneURL)
			}
		}
		certificateStatus := proxy.NewDefaultCertificateStatusService(statusConfig)
		expiryNotifier, _ := s.notify.(proxy.CertificateExpiryNotifier)
		s.certificateExpiry = proxy.NewCertificateExpiryObserver(proxy.CertificateExpiryConfig{
			Store: sslConfig.Store, Status: certificateStatus, Notifier: expiryNotifier, Logger: s.logger,
		})
		proxy.Mount(api, s.resourceAuth(), platformOnly, s.proxy,
			proxy.NewDefaultProviderService(sslConfig),
			proxy.NewDefaultCertificateService(sslConfig),
			proxy.NewDefaultRedirectService(redirectConfig),
			certificateStatus)
		MountProxyRouters(api, platformOnly, proxy.AsRouterService(s.proxy))

		s.updates = s.updatesService()
		updates.Mount(api, s.resourceAuth(), platformOnly, s.isOwner, s.updates)

		s.realtime = ws.Mount(api, realtimeTokenVerifier{server: s}, s.cfg.Snapshot().Redis.Addr, s.logger, s.authorizeLogSubscription)

		api.Group(func(protected chi.Router) {
			protected.Use(s.readScopeAuth)
			protected.Post("/v1/servers/{id}/containers/{containerID}/logs/stream", s.handleStartLogStream)
		})

		providerSvc := providers.NewDefaultService(s.persistence, s.secretKey, s.logger,
			s.providerConnectionApplications)
		providers.MountWithControlPlaneURL(api, s.resourceAuth(), UserIDFromContext, providerSvc, s.controlPlaneURLSource())

		githubAppSvc := githubapp.NewDefaultService(s.persistence, s.secretKey, s.logger)
		githubapp.MountWithControlPlaneURL(api, s.resourceAuth(), UserIDFromContext, githubAppSvc, s.controlPlaneURLSource())

		s.deploy = s.deployService(providerSvc, s.proxy, githubAppSvc)
		deploy.Mount(api, s.resourceAuth(), UserIDFromContext, s.deploy)
		if s.terminal != nil && s.deploy != nil {
			if resolver, ok := s.deploy.(deploy.TerminalResolver); ok {
				s.terminal.SetApplications(terminalApplicationResolver{deploy: resolver})
				terminal.MountApplications(api, s.applicationTerminalAuth(), s.terminal, s.auth,
					applicationTerminalAuthorizer{resolver: resolver, auth: s.auth},
					UserIDFromContext, s.logger)
			}
		}
		if s.terminal != nil && s.auth != nil {
			terminal.MountTargets(api, s.terminalTargetsAuth(), s.terminalTargetsSourceFor,
				UserIDFromContext, s.logger)
		}

		if s.realtime != nil {
			s.realtime.SetTaskFeed(s.authorizeTaskSubscription, s.taskSnapshot)
			s.realtime.SetChannelAuthorizer(s.newRealtimeChannelAuthorizer)
			s.realtime.SetFrameFilter(s.filterProjectFrame)
		}

		s.webhooks = s.webhookService(providerSvc, githubAppSvc)
		webhooks.Mount(api, s.resourceAuth(), UserIDFromContext, s.webhooks)
		s.webhooks.StartPreviews()

		databaseSvc := s.databaseService(containerService)
		s.databaseSvc = databaseSvc
		databases.Mount(api, s.resourceAuth(), RequireScopes(auth.ScopeDeploy), UserIDFromContext, databaseSvc)

		templateSvc := templates.NewDefaultService(s.logger)
		composeSvc := s.composeService(templateSvc)
		s.composeSvc = composeSvc

		s.backups = s.backupService(containerService, databaseSvc, s.deploy, composeSvc)
		backups.Mount(api, s.resourceAuth(), UserIDFromContext, s.backups)
		backups.MountLegacy(api, s.resourceAuth(), s.resourceAuth(), UserIDFromContext, s.backups)
		s.backupwatch = s.backupwatchService()
		backupwatch.Mount(api, s.resourceAuth(), s.backupwatch)

		services.Mount(api, s.resourceAuth(), UserIDFromContext, composeSvc)

		s.mountProjectMembers(api)
		s.mountMemberRoutes(api)
		projects.Mount(api, s.resourceAuth(), UserIDFromContext, s.projectService(s.deploy, composeSvc, databaseSvc))

		templates.Mount(api, s.resourceScopeAuth, templateSvc)
	})

	spa, err := newSPAHandler()
	if err != nil {
		return nil, err
	}
	r.NotFound(spa.ServeHTTP)

	return r, nil
}

func (s *Server) baseMiddleware(r chi.Router) {
	r.Use(s.securityHeaders)
	r.Use(middleware.RequestID)
	r.Use(s.requestLogger())
	r.Use(middleware.Recoverer)
	r.Use(acknowledgeMCPContract)
}

func (s *Server) defaultDomainTLS(ctx context.Context) bool {
	if s.instanceSvc == nil {
		return false
	}
	urls, err := s.instanceSvc.ControlPlaneURLs(ctx)
	if err != nil {
		s.logger.Warn("proxy: control-plane URLs unavailable for the default domain TLS gate", "error", err)
		return false
	}
	hosts, err := proxy.ControlPlaneHTTPSHosts(urls)
	if err != nil {
		s.logger.Warn("proxy: control-plane URL unparseable for the default domain TLS gate", "error", err)
		return false
	}
	return len(hosts) > 0
}

func (s *Server) deployService(providerSvc providers.ProviderService, proxySvc proxy.ProxyService, githubAppSvc *githubapp.Service) deploy.DeployService {
	if s.persistence == nil {
		return nil
	}
	cfg := deploy.Config{
		GuardDomains:         s.guardDomains,
		Store:                s.persistence,
		Secret:               s.secretKey,
		RedisAddr:            s.cfg.Snapshot().Redis.Addr,
		Logger:               s.logger,
		GitAllowPrivateHosts: s.cfg.Snapshot().Deploy.GitAllowPrivateHosts,
		RuntimeMonitor:       s.runtime,
		RuntimeHealth:        runtimewatch.NewReader(s.persistence),
		BackupActive:         s.backupJobActive,
	}
	cfg.TLSForDomains = s.defaultDomainTLS
	if githubAppSvc != nil {
		cfg.AppTokens = githubAppTokenAdapter{svc: githubAppSvc}
	}
	if providerSvc != nil {
		if registrar, ok := providerSvc.(deploy.KeyRegistrar); ok {
			cfg.KeyRegistrar = registrar
		}
	}
	if dialer, ok := s.servers.(deploy.AgentDialer); ok {
		cfg.Dial = deploy.AgentDial(dialer)
	}
	if ts, ok := s.toolchain.(deploy.ToolStatus); ok {
		cfg.ToolStatus = ts
	}
	if proxySvc != nil {
		cfg.Proxy = proxySvc
	}
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
	cfg.PreviewCleanup = func(ctx context.Context, appID uuid.UUID) error {
		if s.webhooks == nil {
			return nil
		}
		return s.webhooks.CleanupApplication(ctx, appID)
	}
	cfg.Hooks = func() deploy.HookLifecycle {
		if s.webhooks == nil {
			return nil
		}
		return s.webhooks
	}
	return deploy.NewDefaultService(cfg)
}

const previewNotifyTimeout = 15 * time.Second

type deployNotifier struct {
	primary deploy.Notifier
	preview func(result deploy.DeployResult)
}

func (n deployNotifier) DeployFinished(ctx context.Context, result deploy.DeployResult) {
	if n.primary != nil {
		n.primary.DeployFinished(ctx, result)
	}
	if n.preview != nil {
		n.preview(result)
	}
}

func (n deployNotifier) RedeployNeeded(ctx context.Context, result deploy.RedeployResult) {
	if n.primary != nil {
		n.primary.RedeployNeeded(ctx, result)
	}
}

func (s *Server) taskSnapshot(projectID string) []string {
	tracker, ok := s.deploy.(interface{ TaskTracker() *taskevents.Tracker })
	if !ok || tracker == nil {
		return taskevents.NewTracker().SnapshotFrames(projectID)
	}
	return tracker.TaskTracker().SnapshotFrames(projectID)
}

func (s *Server) providerConnectionApplications(ctx context.Context, userID uuid.UUID) ([]providers.ConnectionApplication, error) {
	if s.deploy == nil {
		return nil, nil
	}
	applications, err := s.deploy.ListApplications(ctx, userID, deploy.ApplicationFilter{})
	if err != nil {
		return nil, err
	}
	return connectionApplicationsOf(applications), nil
}

func connectionApplicationsOf(applications []deploy.Application) []providers.ConnectionApplication {
	out := make([]providers.ConnectionApplication, 0, len(applications))
	for _, app := range applications {
		out = append(out, providers.ConnectionApplication{
			Name:     app.Name,
			Provider: app.Provider,
			CloneURL: app.CloneURL,
		})
	}
	return out
}

func (s *Server) webhookService(providerSvc providers.ProviderService, githubAppSvc *githubapp.Service) *webhooks.Service {
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
		Store:           s.persistence,
		Installer:       installer,
		Deployer:        deployer,
		Secret:          s.secretKey,
		Logger:          s.logger,
		TrustedProxies:  s.trustedProxies,
		ControlPlaneURL: s.controlPlaneURLSource(),
	}
	if provisioner, ok := s.deploy.(webhooks.PreviewProvisioner); ok {
		cfg.Provisioner = provisioner
	}
	if commenter, ok := providerSvc.(webhooks.Commenter); ok {
		cfg.Commenter = commenter
	}
	if githubAppSvc != nil {
		cfg.AppEvents = githubAppSvc
		cfg.AppPush = githubAppPushAdapter{svc: githubAppSvc}
	}
	return webhooks.NewDefaultService(cfg)
}

type containerDialer interface {
	DialDockerClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.DockerClient, error)
}

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
	service.SetVolumeOwners(s.volumeOwners)
	return service
}

func (s *Server) cleanupService(volumes containers.VolumeService) cleanup.CleanupService {
	if s.servers == nil || s.persistence == nil {
		return nil
	}
	var dial cleanup.DialFunc
	if dialer, ok := s.servers.(containerDialer); ok {
		dial = func(ctx context.Context, server *servers.Server) (cleanup.DockerClient, error) {
			return dialer.DialDockerClient(ctx, server.ID)
		}
	}
	var host cleanup.HostCleaner
	if dialer, ok := s.servers.(hostDialer); ok {
		host = cleanupHost{nodes: nodeHostResolver{nodes: s.servers, dialer: dialer}}
	}
	return cleanup.NewService(cleanup.Config{
		Registry: s.servers,
		Dial:     dial,
		Volumes:  volumes,
		Host:     host,
		Leftovers: cleanup.NewLeftoversSource(cleanup.LeftoversConfig{
			Store:  s.persistence,
			Logger: s.logger,
		}),
		Store:  s.persistence,
		Logger: s.logger,
	})
}

func (s *Server) volumeOwners(ctx context.Context) (containers.VolumeOwners, error) {
	if s.persistence == nil {
		return containers.VolumeOwners{}, errors.New("server: persistence is unavailable")
	}
	recorded, err := s.persistence.ListRecordedVolumeOwners(ctx)
	if err != nil {
		return containers.VolumeOwners{}, err
	}
	return containers.NewVolumeOwners(recorded.Applications, recorded.Services, recorded.Databases), nil
}

type fileDialer interface {
	DialFileClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.FileClient, error)
}

func (s *Server) fileService() files.FileService {
	service := files.NewDefaultService(s.servers, s.logger)
	if service == nil {
		return nil
	}
	if s.persistence != nil {
		service.SetHistoryStore(files.NewStoreHistory(s.persistence))
	}
	if dialer, ok := s.servers.(fileDialer); ok {
		service.SetDial(func(ctx context.Context, server *servers.Server) (files.FileClient, error) {
			return dialer.DialFileClient(ctx, server.ID)
		})
	}
	return service
}

func (s *Server) reconcilePanelCertificates(ctx context.Context) error {
	if s.instanceSvc == nil {
		return errors.New("instance settings service is unavailable")
	}
	return s.instanceSvc.ReconcilePanelCertificates(ctx)
}

type proxyDialer interface {
	DialProxyClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.ProxyClient, error)
}

func (s *Server) proxyService(containerService containers.ContainerService) proxy.ProxyService {
	if s.persistence == nil || containerService == nil {
		return nil
	}
	cfg := proxy.Config{
		ReconcilePanelCertificates: s.reconcilePanelCertificates,
		ControlPlaneURLs: func(ctx context.Context) ([]string, error) {
			if s.instanceSvc == nil {
				return nil, nil
			}
			return s.instanceSvc.ControlPlaneURLs(ctx)
		},
		ControlPlaneURL: func(ctx context.Context) (string, error) {
			if s.instanceSvc == nil {
				return "", nil
			}
			return s.instanceSvc.ControlPlaneURL(ctx)
		},
		ServerPort: s.cfg.Snapshot().Server.Port,
		Store:      s.persistence,
		Containers: containerService,
		Logger:     s.logger,
		Secret:     s.secretKey,
		Services:   services.NewProxySource(s.persistence),
	}
	if dialer, ok := s.servers.(proxyDialer); ok {
		cfg.Dial = func(ctx context.Context, serverID uuid.UUID) (proxy.AgentClient, error) {
			return dialer.DialProxyClient(ctx, serverID)
		}
	}
	return proxy.NewDefaultService(cfg)
}

func (s *Server) resyncProxyNodes(ctx context.Context) error {
	if s.proxy == nil {
		return nil
	}
	results, err := s.proxy.SyncAll(ctx)
	failures := []error{err}
	failed := 0
	var diagnostics []proxy.Diagnostic
	for _, result := range results {
		if result.Error != "" && len(result.Diagnostics) == 0 {
			failed++
		}
		diagnostics = append(diagnostics, result.Diagnostics...)
	}
	if failed > 0 {
		failures = append(failures, fmt.Errorf("proxy: resync failed on %d of %d node(s)", failed, len(results)))
	}
	if len(diagnostics) > 0 {
		failures = append(failures, &proxy.PartialError{Diagnostics: diagnostics})
	}
	return errors.Join(failures...)
}

func (s *Server) databaseService(containerService containers.ContainerService) databases.DatabaseService {
	if s.persistence == nil || containerService == nil {
		return nil
	}
	return databases.NewDefaultService(databases.Config{
		Store:          s.persistence,
		Containers:     containerService,
		Secret:         s.secretKey,
		Logger:         s.logger,
		Leases:         s.jobLeases,
		RuntimeMonitor: s.runtime,
		RuntimeHealth:  runtimewatch.NewReader(s.persistence),
	})
}

func (s *Server) backupService(containerService containers.ContainerService, databaseSvc databases.DatabaseService, deploySvc deploy.DeployService, composeSvc services.ServiceService) backups.BackupService {
	if s.persistence == nil || containerService == nil || databaseSvc == nil {
		return nil
	}
	sources := map[backups.ResourceType]backups.Source{}
	if concrete, ok := databaseSvc.(*databases.Service); ok {
		sources[backups.ResourceDatabase] = databases.NewBackupSource(concrete)
	}
	if archiver, ok := containerService.(volumes.Archiver); ok && deploySvc != nil && composeSvc != nil {
		volumeSource := volumes.NewSource(deploySvc, composeSvc, archiver)
		sources[backups.ResourceApplication] = volumeSource
		sources[backups.ResourceService] = volumeSource
	}
	cfg := backups.BackupConfig{
		Store:      s.persistence,
		Sources:    sources,
		Containers: containerService,
		Secret:     s.secretKey,
		Logger:     s.logger,
		Leases:     s.jobLeases,
	}
	if notifier, ok := s.notify.(backups.BackupNotifier); ok {
		cfg.Notifier = notifier
	}
	return backups.NewDefaultBackupService(cfg)
}

// backupJobActive tells the deploy and service services whether a backup or
// restore currently owns the resource. The backup service is created after
// them, so the guard reads the field at request time.
func (s *Server) backupJobActive(resourceID uuid.UUID) bool {
	manager, ok := s.backups.(*backups.BackupManager)
	return ok && manager.JobActive(resourceID)
}

func (s *Server) incidentService() *incidents.Service {
	if s.persistence == nil {
		return nil
	}
	cfg := incidents.Config{Store: s.persistence, Logger: s.logger}
	if s.notify != nil {
		cfg.Notifier = incidentNotifier{notify: s.notify}
	}
	return incidents.NewService(cfg)
}

func (s *Server) domainwatchService() *domainwatch.Service {
	if s.persistence == nil || s.incidents == nil {
		return nil
	}
	return domainwatch.NewService(domainwatch.Config{
		Store:     s.persistence,
		Incidents: s.incidents,
		Logger:    s.logger,
	})
}

func (s *Server) backupwatchService() *backupwatch.Service {
	if s.persistence == nil || s.incidents == nil || s.backups == nil {
		return nil
	}
	cfg := backupwatch.Config{
		Store:     backupwatch.NewStoreSource(s.persistence),
		Incidents: s.incidents,
		Logger:    s.logger,
	}
	if leases, ok := s.backups.(interface {
		BackupRunActive(databaseID, backupID uuid.UUID) bool
	}); ok {
		cfg.Leases = leases
	}
	return backupwatch.NewService(cfg)
}

func (s *Server) monitoringService() *monitoring.Service {
	if s.persistence == nil || s.incidents == nil {
		return nil
	}
	return monitoring.NewService(monitoring.Config{
		Store:     s.persistence,
		Incidents: s.incidents,
		Logger:    s.logger,
	})
}

func (s *Server) notificationService() notifications.NotificationService {
	if s.persistence == nil {
		return nil
	}
	return notifications.NewDefaultService(notifications.Config{
		Store:     s.persistence,
		Secret:    s.secretKey,
		Logger:    s.logger,
		Publisher: realtimeNotificationPublisher{server: s},
	})
}

type realtimeNotificationPublisher struct{ server *Server }

func (p realtimeNotificationPublisher) Publish(ctx context.Context, channel, payload string) error {
	if p.server.realtime == nil {
		return nil
	}
	return p.server.realtime.Publish(ctx, channel, []byte(payload))
}

func (s *Server) metricsRetention() *servers.MetricsSweeper {
	if s.persistence == nil || !servers.MetricsEnabled() {
		return nil
	}
	return servers.NewMetricsSweeper(s.persistence, s.logger)
}

func (s *Server) resourceHistoryRetention() *servers.ResourceHistorySweeper {
	if s.persistence == nil {
		return nil
	}
	return servers.NewResourceHistorySweeper(s.persistence, s.logger)
}

func (s *Server) sessionRetention() *auth.SessionSweeper {
	if s.persistence == nil {
		return nil
	}
	return auth.NewSessionSweeper(s.persistence, s.logger)
}

func (s *Server) databasesRetention(containerService containers.ContainerService) *databases.RetentionSweeper {
	if s.persistence == nil || containerService == nil {
		return nil
	}
	return databases.NewRetentionSweeper(s.persistence, containerService, s.logger)
}

type versionReporter interface{ Version() string }

func (s *Server) updatesService() updates.Service {
	if !updates.Enabled() {
		return nil
	}
	current := s.currentVersion()
	if fromEnv := updates.CurrentFromEnv(); fromEnv != "" {
		if updatecore.HasEmbeddedKey() {
			s.logger.Warn("updates: ignoring GOTHAM_UPDATE_CURRENT on a build with an embedded release key",
				"version", fromEnv)
		} else {
			current = fromEnv
		}
	}
	config, err := updates.FromEnv(current, s.logger)
	config.Notifier, _ = s.notify.(updates.AvailabilityNotifier)
	if s.persistence != nil {
		config.Schedules = updates.NewStoreSchedule(s.persistence)
	}
	if s.instanceSvc != nil {
		config.Timezone = func() string {
			tz, err := s.instanceSvc.Timezone(context.Background())
			if err != nil {
				return ""
			}
			return tz
		}
	}
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

type composeDialer interface {
	DialComposeClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.ComposeClient, error)
}

func (s *Server) composeService(templateSvc templates.Service) services.ServiceService {
	if s.persistence == nil {
		return nil
	}
	cfg := services.Config{
		GuardDomains: s.guardDomains,
		Store:        s.persistence,
		Logger:       s.logger,
		BackupActive: s.backupJobActive,
	}
	if templateSvc != nil {
		cfg.TemplateExists = func(slug string) bool {
			_, ok := templateSvc.Get(slug)
			return ok
		}
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
	if s.proxy != nil {
		cfg.Proxy = s.proxy
	}
	return services.NewDefaultService(cfg)
}

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

type apiError struct {
	Message string `json:"message"`
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
}

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

type healthResponse struct {
	Status string `json:"status"`
	DB     string `json:"db"`
	Redis  string `json:"redis"`
}

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

func (s *Server) ping(ctx context.Context, name string, p Pinger) string {
	if err := p.Ping(ctx); err != nil {
		s.logger.Warn("health check failed", "dependency", name, "error", err)
		return statusDown
	}
	return statusOK
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) Run(ctx context.Context) error {
	if s.certificateExpiry != nil {
		expiryCtx, cancelExpiry := context.WithCancel(ctx)
		defer cancelExpiry()
		go s.certificateExpiry.Run(expiryCtx)
	}
	if nodes, ok := s.servers.(interface{ RunOfflineSweep(context.Context) }); ok {
		sweepCtx, cancelSweep := context.WithCancel(ctx)
		defer cancelSweep()
		go nodes.RunOfflineSweep(sweepCtx)
	}
	if s.incidents != nil {
		incidentCtx, cancelIncidents := context.WithCancel(ctx)
		defer cancelIncidents()
		go s.incidents.Run(incidentCtx)
	}
	if s.monitoring != nil {
		monitorCtx, cancelMonitor := context.WithCancel(ctx)
		defer cancelMonitor()
		go s.monitoring.Run(monitorCtx)
	}
	if s.domainwatch != nil {
		watchCtx, cancelWatch := context.WithCancel(ctx)
		defer cancelWatch()
		go s.domainwatch.Run(watchCtx)
	}
	if s.backupwatch != nil {
		backupCtx, cancelBackup := context.WithCancel(ctx)
		defer cancelBackup()
		go s.backupwatch.Run(backupCtx)
	}
	if panel, ok := s.proxy.(interface{ RunPanelCertificates(context.Context) }); ok {
		panelCtx, cancelPanel := context.WithCancel(ctx)
		defer cancelPanel()
		go panel.RunPanelCertificates(panelCtx)
	}
	if s.closer != nil {
		defer s.closer()
	}
	if s.instanceSvc != nil {
		defer s.instanceSvc.Close()
	}

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
