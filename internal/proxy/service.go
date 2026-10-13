package proxy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// FeatureEnv is the kill switch for proxy synchronization: FEATURE_PROXY=false
// leaves the generated Traefik configuration untouched (the deploy hooks are
// not wired and POST /v1/proxy/sync answers 404), so a failing proxy rollout
// cannot affect running applications.
const FeatureEnv = "FEATURE_PROXY"

// Enabled reports whether proxy synchronization is on. Only an explicit false
// disables it — unset (or any other value) keeps it enabled, matching
// deploy.Enabled and containers' feature handling.
func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

// defaultSyncTimeout bounds one complete sync: generation, an optional image
// pull and the reload ping.
const defaultSyncTimeout = 5 * time.Minute

// startupReadiness bounds how long a freshly started proxy container gets to
// answer its ping before the sync gives up; startupPoll is the retry interval.
const (
	startupReadiness = 20 * time.Second
	startupPoll      = 500 * time.Millisecond
)

// AgentClient is the node agent's ProxyService as the control plane uses it.
// *servers.ProxyClient satisfies it; tests substitute a fake.
type AgentClient interface {
	WriteProxyConfig(ctx context.Context, in *agentv1.WriteProxyConfigRequest, opts ...grpc.CallOption) (*agentv1.WriteProxyConfigResponse, error)
	Close() error
}

// DialFunc opens the agent ProxyService of one node. It is satisfied by
// *servers.ServerService through the internal/server wiring.
type DialFunc func(ctx context.Context, serverID uuid.UUID) (AgentClient, error)

// ProxyService is the control-plane surface the HTTP layer and the deploy
// lifecycle depend on. It is implemented by SyncService and by fakes in tests.
type ProxyService interface {
	// SyncServer regenerates the Traefik configuration of one node from
	// control-plane state, bootstraps (or repairs) the gotham-traefik
	// container when needed and confirms the write through the agent. A
	// partial result returns an error matching ErrPartialSync with the
	// per-application diagnostics.
	SyncServer(ctx context.Context, serverID uuid.UUID) error
	// SyncAll syncs every node that hosts a proxied application plus every
	// registered node, reporting each node's outcome.
	SyncAll(ctx context.Context) ([]SyncResult, error)
	// RevertServer re-pushes a node's previous stored configuration version.
	RevertServer(ctx context.Context, serverID uuid.UUID) error
}

// SyncResult is one node's outcome of a SyncAll run.
type SyncResult struct {
	ServerID    uuid.UUID    `json:"server_id"`
	Error       string       `json:"error,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// Diagnostic explains why one application or compose service row was not
// routed. The sync still pushes the healthy routes, so a single pending or
// malformed row cannot freeze a node's configuration (BE-6.1 F3).
type Diagnostic struct {
	ApplicationID uuid.UUID `json:"application_id"`
	// Kind names the owning row: "application" (the default, empty) or
	// "service". It only changes how the diagnostic is phrased.
	Kind   string `json:"kind,omitempty"`
	Domain string `json:"domain,omitempty"`
	Reason string `json:"reason"`
}

// PartialError reports a sync that pushed the healthy routes but skipped some
// application or service rows. Errors.Is(err, ErrPartialSync) is true and
// Diagnostics carries the per-row reasons.
type PartialError struct {
	Diagnostics []Diagnostic
}

// Error lists the skipped rows, bounded to keep one runaway node from
// producing an unbounded message.
func (e *PartialError) Error() string {
	const maxReasons = 10
	parts := make([]string, 0, len(e.Diagnostics))
	for i, diagnostic := range e.Diagnostics {
		if i == maxReasons {
			parts = append(parts, fmt.Sprintf("and %d more", len(e.Diagnostics)-maxReasons))
			break
		}
		reason := diagnostic.Reason
		if diagnostic.Domain != "" {
			reason = diagnostic.Domain + ": " + reason
		}
		owner := "application"
		if diagnostic.Kind == "service" {
			owner = "service"
		}
		parts = append(parts, fmt.Sprintf("%s %s (%s)", owner, diagnostic.ApplicationID, reason))
	}
	return ErrPartialSync.Error() + ": " + strings.Join(parts, "; ")
}

// Is makes errors.Is(err, ErrPartialSync) true.
func (e *PartialError) Is(target error) bool { return target == ErrPartialSync }

// Config wires a SyncService. Store (or an explicit ApplicationSource) and
// Containers are required inputs; Dial may be nil until the mTLS dialer is
// plugged in, and every tunable falls back to a documented default.
type Config struct {
	// Store is the PostgreSQL-backed routing input and server registry.
	// Ignored when Applications/Nodes/History are set explicitly.
	Store *store.Store
	// Applications overrides the routing input (tests).
	Applications ApplicationSource
	// Services supplies compose service domains (BE-7.1). The production
	// implementation is internal/services.ProxySource, passed by the HTTP
	// wiring; nil (and no explicit source) leaves service routing off, so
	// application-only nodes are unaffected. There is deliberately no store
	// fallback: rendering a compose document belongs to internal/services,
	// which this package cannot import without a cycle.
	Services ServiceSource
	// Redirects overrides the redirect input (tests). nil (and no Store)
	// disables redirect generation.
	Redirects RedirectSource
	// Nodes overrides the registered-node list (tests).
	Nodes NodeSource
	// History overrides the configuration-version store (tests). nil (and no
	// Store) disables history and revert.
	History HistoryStore
	// DNSProviders overrides the DNS provider source (tests). nil (and no
	// Store) disables DNS-01 resolvers and credentials.
	DNSProviders DNSProviderSource
	// Containers provisions the gotham-traefik container on the node.
	Containers containers.ContainerService
	// Dial opens the node agent's ProxyService; nil fails a sync with
	// ErrAgentUnavailable.
	Dial DialFunc
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// Format selects the generated document syntax; default FormatYAML.
	Format Format
	// BackendHost is the address Traefik uses to reach published container
	// ports; default DefaultBackendHost (the docker0 bridge gateway).
	BackendHost string
	// Secret opens the sealed DNS provider credentials. An empty (or
	// whitespace-only) key is refused: no credential is placed in a node's
	// environment, and the affected routes stay HTTP-only with a diagnostic.
	Secret string
	// ACMEEmail is the optional ACME contact address rendered into every
	// generated certificate resolver; empty omits it.
	ACMEEmail string
	// CAServer is the optional ACME directory endpoint rendered into every
	// generated certificate resolver (e.g. the Let's Encrypt staging URL);
	// empty keeps the production default.
	CAServer string
	// ConfigDir and AcmeDir are the node directories mounted into the
	// Traefik container; they default to the agent's production layout
	// (TraefikDir / TraefikAcmeDir) and only need overriding for a relocated
	// agent root.
	ConfigDir string
	AcmeDir   string
	// Timeout bounds one sync; default 5 minutes.
	Timeout time.Duration
}

// source resolves the configured routing input.
func (c Config) source() ApplicationSource {
	if c.Applications != nil {
		return c.Applications
	}
	if c.Store != nil {
		return storeSource{store: c.Store}
	}
	return nil
}

// services resolves the configured compose service source. It has no store
// fallback on purpose (see Config.Services).
func (c Config) services() ServiceSource {
	return c.Services
}

// nodes resolves the configured node list.
func (c Config) nodes() NodeSource {
	if c.Nodes != nil {
		return c.Nodes
	}
	if c.Store != nil {
		return storeSource{store: c.Store}
	}
	return nil
}

// providers resolves the configured DNS provider list.
func (c Config) providers() DNSProviderSource {
	if c.DNSProviders != nil {
		return c.DNSProviders
	}
	if c.Store != nil {
		return storeSource{store: c.Store}
	}
	return nil
}

// redirects resolves the configured redirect input.
func (c Config) redirects() RedirectSource {
	if c.Redirects != nil {
		return c.Redirects
	}
	if c.Store != nil {
		return storeSource{store: c.Store}
	}
	return nil
}

// history resolves the configured configuration history.
func (c Config) history() HistoryStore {
	if c.History != nil {
		return c.History
	}
	if c.Store != nil {
		return storeHistory{store: c.Store}
	}
	return nil
}

// SyncService generates the Traefik configuration from control-plane state and
// pushes it to the node agents. Generation is a pure function of the
// database, so every sync is idempotent: an unchanged state yields
// byte-identical files and the agent's atomic writes leave the running
// configuration in place.
type SyncService struct {
	source      ApplicationSource
	services    ServiceSource
	redirects   RedirectSource
	nodes       NodeSource
	history     HistoryStore
	providers   DNSProviderSource
	containers  containers.ContainerService
	dial        DialFunc
	logger      *slog.Logger
	format      Format
	backendHost string
	configDir   string
	acmeDir     string
	secret      string
	acmeEmail   string
	caServer    string
	timeout     time.Duration

	// mu serializes one node's snapshot read through bootstrap and write, so
	// a delayed older snapshot can never overwrite a newer one and two first
	// syncs cannot race container creation. A single control plane process
	// makes one service-wide lock sufficient.
	// ponytail: service-wide lock; per-node locks if sync throughput matters.
	mu sync.Mutex
}

// Compile-time guarantee that SyncService satisfies the route/deploy contract.
var _ ProxyService = (*SyncService)(nil)

// NewService builds a SyncService from cfg. The returned service has no
// application source or container service only when cfg carries none; methods
// then fail with a clear error instead of panicking.
func NewService(cfg Config) *SyncService {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultSyncTimeout
	}
	backendHost := strings.TrimSpace(cfg.BackendHost)
	if backendHost == "" {
		backendHost = DefaultBackendHost
	}
	configDir := strings.TrimSpace(cfg.ConfigDir)
	if configDir == "" {
		configDir = TraefikDir
	}
	acmeDir := strings.TrimSpace(cfg.AcmeDir)
	if acmeDir == "" {
		acmeDir = TraefikAcmeDir
	}
	return &SyncService{
		source:      cfg.source(),
		services:    cfg.services(),
		redirects:   cfg.redirects(),
		nodes:       cfg.nodes(),
		history:     cfg.history(),
		providers:   cfg.providers(),
		containers:  cfg.Containers,
		dial:        cfg.Dial,
		logger:      logger,
		format:      cfg.Format,
		backendHost: backendHost,
		configDir:   configDir,
		acmeDir:     acmeDir,
		secret:      cfg.Secret,
		acmeEmail:   cfg.ACMEEmail,
		caServer:    cfg.CAServer,
		timeout:     timeout,
	}
}

// NewDefaultService builds the production service for the HTTP wiring. It
// returns nil (a nil ProxyService) when there is no routing input, no
// container service or FEATURE_PROXY=false, so callers can pass its result to
// Mount and the deploy wiring unconditionally.
func NewDefaultService(cfg Config) ProxyService {
	if cfg.source() == nil || cfg.Containers == nil {
		return nil
	}
	if !Enabled() {
		return nil
	}
	return NewService(cfg)
}

// SyncServer regenerates the node's routing configuration from a state
// snapshot taken under the service lock, ensures the gotham-traefik container
// is running (repairing a container whose published ports or credential
// environment do not match the production bindings), writes the files and
// confirms them through the agent. Rows that cannot be routed become
// diagnostics instead of failing the whole node.
//
// The container environment carries the DNS-01 credentials of the providers
// referenced by an active certificate on this node, and only those: a
// credential never enters the generated documents and is identified in the
// convergence labels by a non-reversible HMAC fingerprint.
func (s *SyncService) SyncServer(ctx context.Context, serverID uuid.UUID) error {
	if s == nil || s.source == nil {
		return errors.New("proxy: application source is not configured")
	}
	if serverID == uuid.Nil {
		return fmt.Errorf("%w: server id is required", ErrValidation)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	apps, err := s.source.ListProxiedApplications(ctx)
	if err != nil {
		return fmt.Errorf("proxy: list proxied applications: %w", err)
	}
	proxiedServices, err := s.listServices(ctx)
	if err != nil {
		return fmt.Errorf("proxy: list proxied services: %w", err)
	}
	rules, err := s.listRedirectRules(ctx)
	if err != nil {
		return fmt.Errorf("proxy: list redirect rules: %w", err)
	}
	nodeList, err := s.listNodeContainers(ctx, serverID)
	if err != nil {
		return fmt.Errorf("proxy: list node containers: %w", mapNodeError(err))
	}
	providers, err := s.listProviders(ctx)
	if err != nil {
		return fmt.Errorf("proxy: list dns providers: %w", err)
	}
	access := s.openProviders(providers)
	routes, diagnostics := routesForServer(apps, serverID, s.backendHost, nodeList, access)
	serviceRoutes, serviceDiagnostics, serviceHosts := serviceRoutesForServer(
		proxiedServices, serverID, s.backendHost, nodeList, applicationHosts(apps, serverID))
	routes = append(routes, serviceRoutes...)
	diagnostics = append(diagnostics, serviceDiagnostics...)
	// Redirect sources must never shadow a compose service host either, so
	// the declared service hosts join the application domains the redirect
	// generator already protects.
	redirects, redirectDiagnostics := redirectsForServer(apps, rules, serverID, serviceHosts...)
	diagnostics = append(diagnostics, redirectDiagnostics...)
	files, err := Generate(BuildConfig(routes, redirects, providers, s.acmeEmail, s.caServer), s.format)
	if err != nil {
		return err
	}
	desired := desiredState{
		files: files,
		env:   traefikEnv(routes, access),
	}
	desired.envHash = envFingerprint(s.secret, desired.env)

	// The configuration intent is durable before the node is touched (R2): a
	// failed record fails the sync without a replacement, so a protected push
	// is never advertised without its predecessor snapshot.
	version, changed, err := s.prepareHistory(ctx, serverID, files)
	if err != nil {
		return err
	}
	if err := s.pushAndPromote(ctx, serverID, nodeList, desired, version, changed); err != nil {
		return err
	}
	s.logger.Info("proxy: configuration synced",
		"server_id", serverID.String(), "routes", len(routes), "redirects", len(redirects), "skipped", len(diagnostics), "dns_credentials", len(desired.env))
	if len(diagnostics) > 0 {
		return &PartialError{Diagnostics: diagnostics}
	}
	return nil
}

// prepareHistory records the configuration intent before the node write. It
// returns the version to promote and whether the stored active snapshot
// differs from files.
func (s *SyncService) prepareHistory(ctx context.Context, serverID uuid.UUID, files []File) (ConfigVersion, bool, error) {
	if s.history == nil {
		return ConfigVersion{}, false, nil
	}
	version, changed, err := s.history.PrepareConfigVersion(ctx, serverID, files, configHash(files))
	if err != nil {
		return ConfigVersion{}, false, fmt.Errorf("%w: record configuration intent: %v", ErrHistory, err)
	}
	return version, changed, nil
}

// pushAndPromote pushes the desired state and promotes the prepared version.
// EVERY push failure retains the pending record: a failure can land after a
// partial write, a timeout or a ping, and even a dial failure leaves the
// durable intent harmless — revert targets the active snapshot while a
// pending record exists and a later same-content sync reuses the record and
// retries. Only a successful promotion (or the explicit restoration path)
// clears it.
func (s *SyncService) pushAndPromote(ctx context.Context, serverID uuid.UUID, nodeList []containers.Container, desired desiredState, version ConfigVersion, changed bool) error {
	if pushErr := s.push(ctx, serverID, nodeList, desired); pushErr != nil {
		if s.history != nil && changed {
			// Both sentinels stay matchable: the caller sees the underlying
			// transport/conflict error and the degraded history outcome.
			return fmt.Errorf("%w: node push failed, pending record retained: %w", ErrHistory, pushErr)
		}
		return pushErr
	}
	if s.history != nil && changed {
		if err := s.history.PromoteConfigVersion(ctx, serverID, version.ID); err != nil {
			return fmt.Errorf("%w: promote configuration version: %v", ErrHistory, err)
		}
	}
	return nil
}

// RevertServer re-pushes the node's previous stored configuration version and
// records the reverted configuration as the newest version, so the timeline
// stays append-only. It fails with ErrVersionNotFound when there is no second
// version to fall back to. Only the documents are reverted: the container
// environment is converged to the current DNS provider state, because
// credentials are deliberately not part of the stored history.
func (s *SyncService) RevertServer(ctx context.Context, serverID uuid.UUID) error {
	if s == nil || s.history == nil {
		return errHistoryNotConfigured
	}
	if serverID == uuid.Nil {
		return fmt.Errorf("%w: server id is required", ErrValidation)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	previous, err := s.history.PreviousConfigVersion(ctx, serverID)
	if err != nil {
		return err
	}
	nodeList, err := s.listNodeContainers(ctx, serverID)
	if err != nil {
		return fmt.Errorf("proxy: list node containers: %w", mapNodeError(err))
	}
	desired := desiredState{files: previous.Files}
	if s.source != nil {
		apps, err := s.source.ListProxiedApplications(ctx)
		if err != nil {
			return fmt.Errorf("proxy: list proxied applications: %w", err)
		}
		providers, err := s.listProviders(ctx)
		if err != nil {
			return fmt.Errorf("proxy: list dns providers: %w", err)
		}
		access := s.openProviders(providers)
		routes, _ := routesForServer(apps, serverID, s.backendHost, nodeList, access)
		desired.env = traefikEnv(routes, access)
	}
	desired.envHash = envFingerprint(s.secret, desired.env)
	hash := previous.ContentHash
	if hash == "" {
		hash = configHash(previous.Files)
	}
	version, changed, err := s.history.PrepareConfigVersion(ctx, serverID, previous.Files, hash)
	if err != nil {
		return fmt.Errorf("%w: record revert intent: %v", ErrHistory, err)
	}
	if err := s.pushAndPromote(ctx, serverID, nodeList, desired, version, changed); err != nil {
		return err
	}
	s.logger.Info("proxy: configuration reverted", "server_id", serverID.String())
	return nil
}

// SyncAll regenerates every registered node plus every node hosting a proxied
// application, so removing the last domain is repaired by a global sync too
// (BE-6.1 F9). Per-node failures are reported in the results so one
// unreachable node does not hide the others.
func (s *SyncService) SyncAll(ctx context.Context) ([]SyncResult, error) {
	if s == nil || s.source == nil {
		return nil, errors.New("proxy: application source is not configured")
	}
	apps, err := s.source.ListProxiedApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("proxy: list proxied applications: %w", err)
	}
	nodeSet := make(map[uuid.UUID]bool, len(apps))
	for _, app := range apps {
		if app.ServerID != uuid.Nil {
			nodeSet[app.ServerID] = true
		}
	}
	if s.nodes != nil {
		registered, err := s.nodes.ListNodes(ctx)
		if err != nil {
			return nil, fmt.Errorf("proxy: list nodes: %w", err)
		}
		for _, id := range registered {
			if id != uuid.Nil {
				nodeSet[id] = true
			}
		}
	}
	ids := make([]uuid.UUID, 0, len(nodeSet))
	for id := range nodeSet {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })

	results := make([]SyncResult, 0, len(ids))
	for _, id := range ids {
		result := SyncResult{ServerID: id}
		if err := s.SyncServer(ctx, id); err != nil {
			result.Error = err.Error()
			var partial *PartialError
			if errors.As(err, &partial) {
				result.Diagnostics = partial.Diagnostics
			}
		}
		results = append(results, result)
	}
	return results, nil
}

// listServices lists the compose service routing input. A nil source leaves
// service routing off without failing the sync.
func (s *SyncService) listServices(ctx context.Context) ([]ProxiedService, error) {
	if s.services == nil {
		return nil, nil
	}
	services, err := s.services.ListProxiedServices(ctx)
	if err != nil {
		return nil, err
	}
	return services, nil
}

// applicationHosts returns every application domain claimed on the node,
// normalized, mirroring the redirect generator's node-domain set: the wider
// claim (not only routable rows) is what keeps two routers from ever matching
// the same host. Since JUS-89 this is every domain row, not just the primary.
func applicationHosts(apps []ProxiedApplication, serverID uuid.UUID) map[string]bool {
	hosts := make(map[string]bool, len(apps))
	for _, app := range apps {
		if app.ServerID != serverID {
			continue
		}
		for _, domain := range appDomains(app) {
			if domain != "" {
				hosts[domain] = true
			}
		}
	}
	return hosts
}

// appDomains returns the normalized domains an application claims: every
// domain row when present (JUS-89), otherwise the single legacy BaseDomain so
// unit-test construction sites keep routing exactly one host.
func appDomains(app ProxiedApplication) []string {
	if len(app.Domains) == 0 {
		return []string{NormalizeDomain(app.BaseDomain)}
	}
	domains := make([]string, 0, len(app.Domains))
	for _, domain := range app.Domains {
		domains = append(domains, NormalizeDomain(domain))
	}
	return domains
}

// serviceRouteName is the deterministic Traefik name of one compose service
// host: "svc-<service id>-<host index>".
func serviceRouteName(id uuid.UUID, index int) string {
	return "svc-" + id.String() + "-" + strconv.Itoa(index)
}

// serviceRoutesForServer filters the proxied compose services down to one
// node, resolves each declared host's live backend from the project's
// containers and classifies rows that cannot be routed as diagnostics.
//
// Conflict rule: an application on the node wins a shared host (the service
// route is held back), and a host declared by two compose services holds both
// back — row order never proves ownership, exactly like duplicate application
// domains. The returned hosts are every declared service host (routable or
// not), so the redirect generator can never emit a source that shadows one.
func serviceRoutesForServer(proxied []ProxiedService, serverID uuid.UUID, backendHost string, nodeContainers []containers.Container, claimed map[string]bool) ([]Route, []Diagnostic, []string) {
	routes := make([]Route, 0, len(proxied))
	diagnostics := make([]Diagnostic, 0)
	hosts := []string{}

	// Count normalized duplicates across the node's services before routing
	// anything.
	hostCounts := make(map[string]int)
	for _, service := range proxied {
		if service.ServerID != serverID {
			continue
		}
		for _, domain := range service.Domains {
			hostCounts[NormalizeDomain(domain.Host)]++
		}
	}

	for _, service := range proxied {
		if service.ServerID != serverID {
			continue
		}
		diagnostic := Diagnostic{ApplicationID: service.ID, Kind: "service"}
		if service.Unroutable != "" {
			diagnostic.Reason = service.Unroutable
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		for index, domain := range service.Domains {
			host := NormalizeDomain(domain.Host)
			hosts = append(hosts, host)
			diagnostic.Domain = host
			switch {
			case ValidateDomain(host) != nil:
				diagnostic.Reason = "invalid domain"
				diagnostics = append(diagnostics, diagnostic)
				continue
			case claimed[host]:
				diagnostic.Reason = "domain is already routed by an application on this node"
				diagnostics = append(diagnostics, diagnostic)
				continue
			}
			if hostCounts[host] > 1 {
				diagnostic.Reason = "duplicate domain across compose services on this node; all conflicting bindings are held back"
				diagnostics = append(diagnostics, diagnostic)
				continue
			}
			if domain.Port <= 0 {
				diagnostic.Reason = "the routing label declares no container port"
				diagnostics = append(diagnostics, diagnostic)
				continue
			}
			container := findComposeContainer(nodeContainers, service.Project, domain.Service)
			hostPort, reason := resolveServiceEndpoint(container, domain.Port)
			if reason != "" {
				diagnostic.Reason = reason
				diagnostics = append(diagnostics, diagnostic)
				continue
			}
			routes = append(routes, Route{
				Name:   serviceRouteName(service.ID, index),
				AppID:  service.ID,
				Domain: host,
				Target: fmt.Sprintf("http://%s:%d", backendHost, hostPort),
			})
		}
	}
	return routes, diagnostics, hosts
}

// findComposeContainer returns the project's running container for one
// compose service, matched through Docker Compose's own labels.
func findComposeContainer(list []containers.Container, project, composeService string) *containers.Container {
	for i := range list {
		labels := list[i].Labels
		if labels[ComposeProjectLabel] != project || labels[ComposeServiceLabel] != composeService {
			continue
		}
		if !strings.EqualFold(list[i].State, "running") {
			continue
		}
		return &list[i]
	}
	return nil
}

// resolveServiceEndpoint picks the host port Traefik must reach for one
// compose service host. Only a positively matched, running container with an
// engine-reported publication is routable: the document must publish the
// container port for the node's proxy to reach it.
func resolveServiceEndpoint(container *containers.Container, port int32) (int32, string) {
	if container == nil {
		return 0, "no running container for the compose service found on the node"
	}
	if !container.PortsReported {
		return 0, "the container has no engine-reported published ports (or the node agent is too old to report them)"
	}
	if hostPort := publishedHostPort(container, port); hostPort > 0 {
		return hostPort, ""
	}
	return 0, fmt.Sprintf("container port %d is not published; add it to the compose service ports", port)
}

// routesForServer filters the proxied applications down to one node, resolves
// each one's live endpoint and classifies rows that cannot be routed as
// per-application diagnostics. Healthy rows are routed even when a sibling is
// pending or invalid, so no single row can freeze the node's configuration
// (BE-6.1 F3). Every binding of a duplicate normalized domain on this node is
// held back: row order does not prove ownership, and the unique index already
// prevents new active duplicates (R1).
//
// Since JUS-89 one application emits one route per domain: the primary keeps
// the legacy "app-<id>" router and service names (single-domain nodes render
// byte-identical configuration), while every alias gets its own
// "app-<id>-<domain>" routers sharing the application's service — one router
// per domain, because each domain may need its own TLS resolver. Removing a
// domain therefore removes only its routers; the survivors keep their names.
// Aliases of a domain-disabled application are held back with it.
//
// Certificate activation (BE-6.2) is resolved per route: a route whose
// certificate configuration is active gets an HTTPS route and the redirect,
// while a configured-but-inactive one stays plain HTTP exactly as in BE-6.1
// and explains itself through a diagnostic (an entry can therefore accompany
// a routed row, not only a skipped one).
func routesForServer(apps []ProxiedApplication, serverID uuid.UUID, backendHost string, nodeContainers []containers.Container, providers map[uuid.UUID]providerAccess) ([]Route, []Diagnostic) {
	routes := make([]Route, 0, len(apps))
	diagnostics := make([]Diagnostic, 0)
	byID := indexNodeContainers(nodeContainers)

	// Count active normalized duplicates per node before routing anything.
	// Invalid hosts are excluded: they are reported as invalid, never as
	// duplicates.
	domainCounts := make(map[string]int, len(apps))
	for _, app := range apps {
		if app.ServerID != serverID || app.Disabled {
			continue
		}
		for _, domain := range appDomains(app) {
			if ValidateDomain(domain) == nil {
				domainCounts[domain]++
			}
		}
	}

	for _, app := range apps {
		if app.ServerID != serverID {
			continue
		}
		if app.Disabled {
			diagnostic := Diagnostic{ApplicationID: app.ID, Domain: NormalizeDomain(app.BaseDomain)}
			diagnostic.Reason = "domain disabled by the uniqueness migration; set a new domain to re-enable it"
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		// Invalid hosts are reported before any endpoint state, exactly like
		// the single-domain path: a malformed domain is never a pending
		// deployment.
		domains := make([]string, 0, len(appDomains(app)))
		for _, domain := range appDomains(app) {
			if ValidateDomain(domain) != nil {
				diagnostics = append(diagnostics, Diagnostic{
					ApplicationID: app.ID,
					Domain:        domain,
					Reason:        "invalid domain",
				})
				continue
			}
			domains = append(domains, domain)
		}
		if len(domains) == 0 {
			continue
		}
		if app.Port <= 0 {
			diagnostic := Diagnostic{ApplicationID: app.ID, Domain: domains[0]}
			diagnostic.Reason = "application declares no container port"
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		if app.ContainerID == "" {
			diagnostic := Diagnostic{ApplicationID: app.ID, Domain: domains[0]}
			diagnostic.Reason = "no running deployment yet"
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		hostPort, reason := resolveEndpoint(app, findContainer(byID, app.ContainerID))
		if reason != "" {
			diagnostic := Diagnostic{ApplicationID: app.ID, Domain: domains[0]}
			diagnostic.Reason = reason
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		target := fmt.Sprintf("http://%s:%d", backendHost, hostPort)
		primary := NormalizeDomain(app.BaseDomain)
		for _, domain := range domains {
			diagnostic := Diagnostic{ApplicationID: app.ID, Domain: domain}
			if domainCounts[domain] > 1 {
				diagnostic.Reason = "duplicate domain on this node; all conflicting bindings are held back"
				diagnostics = append(diagnostics, diagnostic)
				continue
			}
			route := Route{
				AppID:  app.ID,
				Domain: domain,
				Target: target,
			}
			if domain != primary {
				route.Name = AliasRouteName(app.ID, domain)
				route.Service = serviceName(app.ID)
			}
			certificate, certReason := resolveRouteCertificate(app, domain, providers)
			route.Certificate = certificate
			if certReason != "" {
				diagnostic.Reason = certReason
				diagnostics = append(diagnostics, diagnostic)
			}
			routes = append(routes, route)
		}
	}
	return routes, diagnostics
}

// redirectsForServer filters the redirect rules down to one node and holds
// back every rule that cannot be generated safely, reporting it as a
// per-application diagnostic instead of freezing the node (BE-6.1 F3). The
// rules that survive are emitted as web-entrypoint redirect routers:
//
//   - a rule of a domain-disabled application is held back (its route is
//     already excluded by the uniqueness conflict; the redirect is held back
//     with it so an excluded application cannot claim another host),
//   - invalid or self-referential hosts are held back,
//   - a source that shadows an application base domain on this node is held
//     back (two routers must never match the same host),
//   - duplicate sources hold every conflicting rule back (row order does not
//     prove ownership, and the unique index only prevents new duplicates),
//   - a rule that forms a chain with an earlier enabled rule is held back
//     (see the no-chain net below), so the generated per-snapshot selection
//     is chain-free.
//
// extraDomains are hosts claimed by compose services on the node (BE-7.1);
// they join the application domains a redirect source must never shadow, so a
// redirect can never claim a host a service router answers for.
func redirectsForServer(apps []ProxiedApplication, rules []RedirectRule, serverID uuid.UUID, extraDomains ...string) ([]Redirect, []Diagnostic) {
	if len(rules) == 0 {
		return nil, nil
	}
	nodeDomains := make(map[string]bool, len(apps)+len(extraDomains))
	for _, app := range apps {
		if app.ServerID != serverID {
			continue
		}
		for _, domain := range appDomains(app) {
			if domain != "" {
				nodeDomains[domain] = true
			}
		}
	}
	for _, domain := range extraDomains {
		if domain = NormalizeDomain(domain); domain != "" {
			nodeDomains[domain] = true
		}
	}

	type candidate struct {
		rule   RedirectRule
		rank   int
		source string
		target string
		reason string
	}
	candidates := make([]candidate, 0, len(rules))
	for rank, rule := range rules {
		if !rule.Enabled || rule.ServerID != serverID {
			continue
		}
		entry := candidate{
			rule:   rule,
			rank:   rank,
			source: NormalizeDomain(rule.SourceDomain),
			target: NormalizeDomain(rule.TargetDomain),
		}
		switch {
		case rule.DomainDisabled:
			entry.reason = "the application's domain is disabled by the uniqueness migration; its redirects are held back"
		case rule.Code != RedirectCodePermanent && rule.Code != RedirectCodeTemporary:
			entry.reason = "the redirect code is not 301 or 302"
		case ValidateDomain(entry.source) != nil:
			entry.reason = "invalid redirect source"
		case ValidateDomain(entry.target) != nil:
			entry.reason = "invalid redirect target"
		case entry.source == entry.target:
			entry.reason = "the redirect source and target are the same host"
		case nodeDomains[entry.source]:
			entry.reason = "the redirect source is an application's base domain on this node"
		}
		candidates = append(candidates, entry)
	}

	sourceCounts := make(map[string]int, len(candidates))
	for _, entry := range candidates {
		sourceCounts[entry.source]++
	}
	for i := range candidates {
		if candidates[i].reason == "" && sourceCounts[candidates[i].source] > 1 {
			candidates[i].reason = "duplicate redirect source on this node; all conflicting rules are held back"
		}
	}

	// No-chain net. The service rejects a sequential chain at write time in
	// both directions; this net catches racing writes (the guard is a
	// check-then-write, not a database constraint). It runs against every
	// enabled rule, not just this node's candidates, because a chain can span
	// nodes: for a single committed snapshot it holds back a chain's later-
	// *created* rule (the order of ListRedirectRules: created_at, id — not the
	// rule whose update committed last), while an earlier rule that was valid
	// on its own keeps serving. This is a per-snapshot selection, not an
	// atomic fleet replacement: SyncServer writes one node and SyncAll applies
	// nodes separately, so a node that has not converged yet can still serve a
	// chain until its own sync runs (an unreachable or failing node prolongs
	// the window).
	for i := range candidates {
		entry := &candidates[i]
		if entry.reason != "" {
			continue
		}
		for j := 0; j < entry.rank; j++ {
			earlier := rules[j]
			if !earlier.Enabled {
				continue
			}
			earlierSource := NormalizeDomain(earlier.SourceDomain)
			earlierTarget := NormalizeDomain(earlier.TargetDomain)
			if earlierTarget == entry.source || earlierSource == entry.target {
				entry.reason = "the redirect chains with an earlier enabled redirect rule; redirect chains are not generated"
				break
			}
		}
	}

	redirects := make([]Redirect, 0, len(candidates))
	diagnostics := make([]Diagnostic, 0)
	for _, entry := range candidates {
		if entry.reason != "" {
			diagnostics = append(diagnostics, Diagnostic{
				ApplicationID: entry.rule.ApplicationID,
				Domain:        entry.source,
				Reason:        entry.reason,
			})
			continue
		}
		redirects = append(redirects, Redirect{
			ID:           entry.rule.ID,
			Source:       entry.source,
			Target:       entry.target,
			Code:         entry.rule.Code,
			PreservePath: entry.rule.PreservePath,
		})
	}
	return redirects, diagnostics
}

// resolveRouteCertificate decides whether one application's certificate
// configuration activates the HTTPS route. It returns the resolved TLS
// section, or nil plus a reason when the configuration is configured but
// cannot be activated (the route then stays plain HTTP). An explicitly
// disabled configuration activates nothing and is not a diagnostic: it is
// operator intent, not a failure.
//
// Since JUS-89 the intent is matched per domain: an intent recorded for the
// routed host activates it directly, and an enabled wildcard intent whose SAN
// covers the routed host is reused instead of demanding a second order for
// the sibling (the generated request still names the routed host as main,
// with the shared wildcard base as SAN).
func resolveRouteCertificate(app ProxiedApplication, domain string, providers map[uuid.UUID]providerAccess) (*RouteCertificate, string) {
	intents := app.Certificates
	if intents == nil && app.Certificate != nil {
		intents = []*CertificateIntent{app.Certificate}
	}
	var exact, wildcard *CertificateIntent
	for _, intent := range intents {
		if intent == nil || !intent.Enabled {
			continue
		}
		if intent.Domain == domain {
			exact = intent
			break
		}
		if intent.Wildcard && wildcard == nil && wildcardCovers(intent, domain, providers) {
			wildcard = intent
		}
	}
	switch {
	case exact != nil:
		return activateCertificate(exact, domain, providers)
	case wildcard != nil:
		return activateCertificate(wildcard, domain, providers)
	}
	for _, intent := range intents {
		if intent == nil || !intent.Enabled {
			continue
		}
		// The recorded host guards against issuing for a stale domain: changing
		// the application's domains never silently re-targets the
		// certificate; the configuration must be updated explicitly.
		return nil, fmt.Sprintf("certificate configuration is recorded for %q; update it to certify %q", intent.Domain, domain)
	}
	return nil, ""
}

// wildcardCovers reports whether an enabled wildcard intent's SAN covers the
// routed host: the generated request for the intent is main=intent host plus
// `*.base`, so a sibling one label under the same base reuses the resolver
// without its own order. The routed host must additionally sit under one of
// the provider's zones, like any DNS-01 activation.
func wildcardCovers(intent *CertificateIntent, domain string, providers map[uuid.UUID]providerAccess) bool {
	if domain == intent.Domain {
		return true
	}
	access, ok := providers[intent.DNSProviderID]
	if !ok || !access.provider.Enabled || access.err != nil {
		return false
	}
	base, ok := WildcardBase(intent.Domain, access.provider.Zones)
	if !ok {
		return false
	}
	if MatchZone(access.provider.Zones, domain) == "" {
		return false
	}
	labels := strings.Split(domain, ".")
	baseLabels := strings.Split(base, ".")
	return len(labels) == len(baseLabels)+1 && strings.HasSuffix(domain, "."+base)
}

// activateCertificate resolves one matched intent into the route's TLS
// section, or nil plus a diagnostic reason when the intent cannot be
// activated (the route then stays plain HTTP).
func activateCertificate(intent *CertificateIntent, domain string, providers map[uuid.UUID]providerAccess) (*RouteCertificate, string) {
	switch intent.Challenge {
	case ChallengeHTTP01:
		if intent.Wildcard {
			return nil, "wildcard certificates require the dns-01 challenge"
		}
		return &RouteCertificate{Resolver: DefaultResolverName}, ""
	case ChallengeDNS01:
		access, ok := providers[intent.DNSProviderID]
		if !ok || !access.provider.Enabled {
			return nil, "the configured DNS provider is disabled"
		}
		if access.err != nil {
			if errors.Is(access.err, ErrSecret) {
				return nil, "the deployment secret is not configured, so DNS provider credentials cannot be used"
			}
			return nil, "the DNS provider credentials could not be opened"
		}
		zone := MatchZone(access.provider.Zones, domain)
		if zone == "" {
			return nil, "the domain is not under any zone served by the DNS provider"
		}
		resolver := DNSResolverName(access.provider.Provider)
		if resolver == "" {
			return nil, "the configured DNS provider type is not supported"
		}
		certificate := &RouteCertificate{
			Resolver:        resolver,
			DNSProviderID:   access.provider.ID,
			DNSProviderType: access.provider.Provider,
		}
		if intent.Wildcard {
			// The wildcard must cover the routed host: keep the host as the
			// main name and request `*.base` for a base inside the provider's
			// zones.
			base, ok := WildcardBase(domain, access.provider.Zones)
			if !ok {
				return nil, "the wildcard base is not inside any zone served by the DNS provider"
			}
			certificate.WildcardBase = base
		}
		return certificate, ""
	default:
		return nil, "the certificate configuration has an unknown challenge"
	}
}

// providerAccess is the in-memory view of one DNS provider for a sync: the
// sealed row plus its opened credential. A credential that cannot be opened
// stays as an error on the entry so generation can keep the affected routes
// HTTP-only with a diagnostic instead of failing the whole node.
type providerAccess struct {
	provider   DNSProvider
	credential string
	err        error
}

// desiredState is one node's fully resolved desired proxy state: the rendered
// documents plus the DNS credential environment delivered to the container.
// The environment never enters the history (credentials are not part of the
// generated configuration); envHash identifies it for convergence.
type desiredState struct {
	files   []File
	env     []string
	envHash string
}

// listProviders resolves the configured DNS providers, nil when the service
// has no provider source.
func (s *SyncService) listProviders(ctx context.Context) ([]DNSProvider, error) {
	if s.providers == nil {
		return nil, nil
	}
	return s.providers.ListDNSProviders(ctx)
}

// listRedirectRules resolves the configured redirect rules, nil when the
// service has no redirect source.
func (s *SyncService) listRedirectRules(ctx context.Context) ([]RedirectRule, error) {
	if s.redirects == nil {
		return nil, nil
	}
	return s.redirects.ListRedirectRules(ctx)
}

// openProviders opens the sealed credentials of the configured providers.
func (s *SyncService) openProviders(providers []DNSProvider) map[uuid.UUID]providerAccess {
	access := make(map[uuid.UUID]providerAccess, len(providers))
	for _, provider := range providers {
		entry := providerAccess{provider: provider}
		if provider.Enabled {
			credential, err := openCredential(s.secret, provider.SealedCredential)
			if err != nil {
				entry.err = err
			} else {
				entry.credential = credential
			}
		}
		access[provider.ID] = entry
	}
	return access
}

// traefikEnv renders the credential environment of the DNS providers actually
// referenced by an active certificate on this node, sorted by variable name.
// Values are passed to the container runtime only and are never logged,
// rendered into configuration or returned through the API.
func traefikEnv(routes []Route, providers map[uuid.UUID]providerAccess) []string {
	env := make([]string, 0)
	seen := make(map[string]bool)
	for _, route := range routes {
		if route.Certificate == nil || route.Certificate.DNSProviderID == uuid.Nil {
			continue
		}
		access, ok := providers[route.Certificate.DNSProviderID]
		if !ok || access.err != nil {
			continue
		}
		name := DNSProviderEnvVar(access.provider.Provider)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		env = append(env, name+"="+access.credential)
	}
	sort.Strings(env)
	return env
}

// envFingerprint is the container label value used to detect environment
// drift. It is an HMAC over the sorted KEY=VALUE pairs with a
// deployment-derived key, so the value is non-reversible: reading the label
// cannot reveal or cheaply brute-force a credential, while rotating one still
// changes the fingerprint (which is what recreates the container).
//
// An unusable deployment secret never keys an HMAC: no credential can be
// placed in the environment without one (openCredential refuses it), so the
// environment is necessarily empty and gets a fixed marker instead of a
// publicly derivable digest.
//
// Limitation, documented deliberately: the engine does not report a running
// container's environment to the agent, so drift can only be detected against
// this recorded fingerprint — an environment changed out-of-band under Gotham
// is invisible until the next recorded change.
func envFingerprint(secret string, env []string) string {
	if !secretUsable(secret) {
		return "unconfigured"
	}
	key := sha256.Sum256([]byte("gotham:proxy:env:" + secret))
	mac := hmac.New(sha256.New, key[:])
	pairs := append([]string{}, env...)
	sort.Strings(pairs)
	for _, pair := range pairs {
		mac.Write([]byte(pair))
		mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil))
}

// resolveEndpoint picks the host port Traefik must reach for an application.
// Only a positively matched, running container with an engine-reported
// publication is routable: a missing, stopped or unusable container is
// isolated instead of being pointed at its declared port, which another
// workload may own by now (R1).
func resolveEndpoint(app ProxiedApplication, container *containers.Container) (int32, string) {
	if container == nil {
		return 0, "no running deployment container found on the node"
	}
	if !strings.EqualFold(container.State, "running") {
		return 0, "the deployment container is not running"
	}
	if !container.PortsReported {
		return 0, "the container has no engine-reported published ports (or the node agent is too old to report them)"
	}
	if hostPort := publishedHostPort(container, app.Port); hostPort > 0 {
		return hostPort, ""
	}
	return 0, fmt.Sprintf("container port %d has no reachable published binding", app.Port)
}

// publishedHostPort returns the host port a container publishes for
// privatePort that another container can reach. A loopback-only binding is
// not reachable from the Traefik container (its 127.0.0.1 is the Traefik
// container itself), so it is ignored.
func publishedHostPort(container *containers.Container, privatePort int32) int32 {
	for _, spec := range container.Ports {
		hostIP, hostPort, containerPort, ok := parseContainerPortSpec(spec)
		if !ok || containerPort != privatePort {
			continue
		}
		switch hostIP {
		case "", "0.0.0.0", "::":
			return hostPort
		}
	}
	return 0
}

// parseContainerPortSpec parses the agent's rendered bindings
// ("host:container" or "ip:host:container").
func parseContainerPortSpec(spec string) (hostIP string, hostPort, containerPort int32, ok bool) {
	parts := strings.Split(strings.TrimSpace(spec), ":")
	switch len(parts) {
	case 2:
		host, container := parts[0], parts[1]
		h, errHost := strconv.Atoi(host)
		c, errContainer := strconv.Atoi(container)
		if errHost != nil || errContainer != nil || h <= 0 || c <= 0 {
			return "", 0, 0, false
		}
		return "", int32(h), int32(c), true
	case 3:
		h, errHost := strconv.Atoi(parts[1])
		c, errContainer := strconv.Atoi(parts[2])
		if errHost != nil || errContainer != nil || h <= 0 || c <= 0 {
			return "", 0, 0, false
		}
		return parts[0], int32(h), int32(c), true
	default:
		return "", 0, 0, false
	}
}

// indexNodeContainers maps container ids to their node DTO.
func indexNodeContainers(list []containers.Container) map[string]*containers.Container {
	byID := make(map[string]*containers.Container, len(list))
	for i := range list {
		if list[i].ID != "" {
			byID[list[i].ID] = &list[i]
		}
	}
	return byID
}

// findContainer resolves a stored container id against the node list,
// tolerating the truncated ids some engines report.
func findContainer(byID map[string]*containers.Container, containerID string) *containers.Container {
	if containerID == "" {
		return nil
	}
	if container, ok := byID[containerID]; ok {
		return container
	}
	for id, container := range byID {
		if strings.HasPrefix(id, containerID) || strings.HasPrefix(containerID, id) {
			return container
		}
	}
	return nil
}

// nodeContainers was folded into SyncServer: one List per sync serves both
// endpoint resolution and the proxy-container inspection.

// push writes the documents to one node. It bootstraps Traefik when it is not
// running and replaces a container whose published bindings or credential
// environment do not match the production specifications (repairing
// containers created before the host-IP binding fix, F1, and containers whose
// DNS-01 credentials changed). The verified write only reports whether the
// proxy answered its ping after the write; it is not proof that Traefik
// accepted the document (BE-6.1 A1).
//
// A failure never invalidates the caller's pending history record: the caller
// retains it for every failure, including a dial error (R2).
//
// Credential redaction boundary: this is the one place where the node's
// credential environment is known, so every error and every log line it
// produces is scrubbed while the env is available — container lifecycle
// failures (Start/Pull/Remove/Run), agent write failures, reload verification
// failures, the dial error, the convergence warning's node-reported drift
// reason and the deferred Close log alike. Callers (SyncServer, RevertServer)
// can therefore propagate push errors to the API and the logs without a
// second scrub.
func (s *SyncService) push(ctx context.Context, serverID uuid.UUID, nodeList []containers.Container, desired desiredState) (err error) {
	// The single redaction boundary of the push path: every return below is
	// scrubbed, including any added later.
	defer func() {
		err = redactEnvValues(err, desired.env)
	}()

	client, err := s.dialAgent(ctx, serverID)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			s.logger.Debug("proxy: close agent connection", "server_id", serverID,
				"error", redactEnvValues(closeErr, desired.env))
		}
	}()

	state := findTraefik(nodeList)
	if state.exists {
		owned, matches, reason := s.traefikMatches(state.container, desired.envHash)
		switch {
		case !owned:
			// Never remove a same-name container this service cannot prove it
			// owns (R5); the operator resolves the conflict.
			return fmt.Errorf("%w: container %s exists on the node but is not a Gotham-managed proxy (%s)",
				ErrConflict, TraefikContainerName, reason)
		case !matches:
			// The drift reason is built from node-reported values (image,
			// ports, policy), so it is scrubbed before logging even though a
			// successful repair returns nil.
			redactedReason, _ := redactEnvText(reason, desired.env)
			s.logger.Warn("proxy: recreating gotham-traefik to converge managed state",
				"server_id", serverID.String(), "reason", redactedReason)
			if err := s.containers.Remove(ctx, serverID, state.container.ID); err != nil {
				return fmt.Errorf("proxy: remove drifted gotham-traefik: %w", mapNodeError(err))
			}
			state = containerState{}
		}
	}

	if !state.running {
		// The static configuration must exist before Traefik starts: it is
		// read once at boot. The write is unverified because the proxy is not
		// up yet; the verified write below confirms the ping.
		if _, err := client.WriteProxyConfig(ctx, &agentv1.WriteProxyConfigRequest{Files: configFiles(desired.files)}); err != nil {
			return mapAgentError("write proxy config", err)
		}
		if err := s.bootstrapContainer(ctx, serverID, state, desired.env); err != nil {
			return fmt.Errorf("proxy: bootstrap traefik: %w", mapNodeError(err))
		}
		return s.writeVerified(ctx, client, desired.files, time.Now().Add(startupReadiness))
	}
	return s.writeVerified(ctx, client, desired.files, time.Time{})
}

// writeVerified writes the documents with ping verification. A freshly
// started container gets until readyAt to bind its entrypoints (a fresh
// Traefik needs a moment before 8080 answers); an already running container
// is checked once. Only the ping is claimed, never configuration acceptance.
func (s *SyncService) writeVerified(ctx context.Context, client AgentClient, files []File, readyAt time.Time) error {
	for {
		response, err := client.WriteProxyConfig(ctx, &agentv1.WriteProxyConfigRequest{
			Files:  configFiles(files),
			Verify: true,
		})
		if err != nil {
			return mapAgentError("write proxy config", err)
		}
		if response.GetReloaded() {
			return nil
		}
		if readyAt.IsZero() || time.Now().After(readyAt) {
			detail := strings.TrimSpace(response.GetPingError())
			if detail == "" {
				detail = "the proxy did not answer its ping"
			}
			return fmt.Errorf("%w: %s", ErrReload, detail)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("proxy: waiting for traefik readiness: %w", ctx.Err())
		case <-time.After(startupPoll):
		}
	}
}

// configHash fingerprints a rendered configuration (order-independent file
// names, deterministic content).
func configHash(files []File) string {
	sum := sha256.New()
	for _, file := range files {
		sum.Write([]byte(file.Name))
		sum.Write([]byte{0})
		sum.Write(file.Content)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// containerState is what one node's proxy container currently looks like.
type containerState struct {
	exists    bool
	running   bool
	container containers.Container
}

// listNodeContainers resolves the node's live container list, preferring the
// uncached read: a container started by the deploy orchestrator never touches
// the containers UI cache, and a stale list would hide an ephemeral port.
func (s *SyncService) listNodeContainers(ctx context.Context, serverID uuid.UUID) ([]containers.Container, error) {
	type freshLister interface {
		ListFresh(ctx context.Context, serverID uuid.UUID) ([]containers.Container, error)
	}
	if lister, ok := s.containers.(freshLister); ok {
		return lister.ListFresh(ctx, serverID)
	}
	return s.containers.List(ctx, serverID)
}

// findTraefik locates the gotham-traefik container in a node's container
// list.
func findTraefik(nodeList []containers.Container) containerState {
	for _, container := range nodeList {
		if container.Name != TraefikContainerName {
			continue
		}
		return containerState{
			exists:    true,
			running:   strings.EqualFold(container.State, "running"),
			container: container,
		}
	}
	return containerState{}
}

// bootstrapContainer brings the proxy container up with its native restart
// policy: it starts an existing stopped container, or pulls the image and
// creates the container when it is missing. Creation is safe only after the
// static configuration exists (the caller writes it first). The DNS-01
// credentials travel in env and are recorded on the container only as the
// non-reversible fingerprint inside traefikLabels; errors are returned raw
// and scrubbed at the caller's push boundary.
func (s *SyncService) bootstrapContainer(ctx context.Context, serverID uuid.UUID, state containerState, env []string) error {
	if state.exists {
		return s.containers.Start(ctx, serverID, state.container.ID)
	}
	if err := s.containers.Pull(ctx, serverID, TraefikImage); err != nil {
		return err
	}
	_, err := s.containers.Run(ctx, serverID, containers.RunOptions{
		Image:         TraefikImage,
		Name:          TraefikContainerName,
		Env:           env,
		Labels:        traefikLabels(s.configDir, s.acmeDir, envFingerprint(s.secret, env)),
		Ports:         TraefikPorts,
		Volumes:       TraefikVolumesFor(s.configDir, s.acmeDir),
		RestartPolicy: TraefikRestartPolicy,
	})
	// The caller (push) owns the credential-redaction boundary: the error is
	// returned raw so the original classification survives, and the push
	// defer scrubs every env value before the error reaches the API or the
	// logs.
	return err
}

// redactedError carries sanitized error text while preserving the original
// error chain, so errors.Is/errors.As classification (mapNodeError and the
// HTTP status mapping) survives redaction.
type redactedError struct {
	message string
	err     error
}

// Error returns the sanitized message.
func (e *redactedError) Error() string { return e.message }

// Unwrap exposes the original error to errors.Is/errors.As.
func (e *redactedError) Unwrap() error { return e.err }

// redactEnvText replaces every credential value from env in text with
// "<redacted>" and reports whether anything was replaced. It is the string
// form shared by redactEnvValues and the convergence warning, so node-reported
// values logged while the credential environment is known can never carry a
// credential.
func redactEnvText(text string, env []string) (string, bool) {
	redacted := false
	for _, pair := range env {
		_, value, ok := strings.Cut(pair, "=")
		if !ok || value == "" {
			continue
		}
		if strings.Contains(text, value) {
			text = strings.ReplaceAll(text, value, "<redacted>")
			redacted = true
		}
	}
	return text, redacted
}

// redactEnvValues replaces every credential value from env in the error text
// with "<redacted>", so a backend error that echoes its request cannot leak a
// DNS-01 token into logs or API error text. Variable names and the rest of the
// message stay readable for diagnosis. The returned error keeps the original
// error chain (errors.Is/errors.As), and an error that does not mention any
// value is returned unchanged.
func redactEnvValues(err error, env []string) error {
	if err == nil || len(env) == 0 {
		return err
	}
	message, redacted := redactEnvText(err.Error(), env)
	if !redacted {
		return err
	}
	return &redactedError{message: message, err: err}
}

// traefikMatches verifies an existing container against the managed proxy's
// desired state: ownership labels, image, published ports, recorded proxy
// directories, the recorded credential environment fingerprint, engine-
// reported mounts and the real Docker restart policy. Unmanaged containers
// are reported as not owned so the caller can refuse to remove them (R5).
func (s *SyncService) traefikMatches(container containers.Container, envHash string) (owned, matches bool, reason string) {
	if container.Labels["gotham.managed"] != "true" || container.Labels["gotham.component"] != "proxy" {
		return false, false, "missing gotham.managed/gotham.component labels"
	}
	if container.Image != TraefikImage {
		return true, false, fmt.Sprintf("image %q, want %q", container.Image, TraefikImage)
	}
	if !portsMatch(container.Ports, TraefikPorts) {
		return true, false, fmt.Sprintf("ports %v, want %v", container.Ports, TraefikPorts)
	}
	if container.Labels["gotham.proxy.config_dir"] != s.configDir || container.Labels["gotham.proxy.acme_dir"] != s.acmeDir {
		return true, false, "the recorded proxy directories differ"
	}
	if container.Labels["gotham.proxy.restart_policy"] != TraefikRestartPolicy {
		return true, false, "the recorded restart policy differs"
	}
	if container.Labels[TraefikEnvHashLabel] != envHash {
		return true, false, "the recorded credential environment fingerprint differs"
	}
	if !mountMatches(container.Mounts, s.configDir, TraefikContainerConfigDir, true) {
		return true, false, "the config mount is missing, wrong-sourced or writable"
	}
	if !mountMatches(container.Mounts, s.acmeDir, TraefikAcmeMount, false) {
		return true, false, "the ACME mount is missing or wrong-sourced"
	}
	if container.RestartPolicy != TraefikRestartPolicy {
		return true, false, fmt.Sprintf("restart policy %q, want %q (empty means unknown)", container.RestartPolicy, TraefikRestartPolicy)
	}
	return true, true, ""
}

// mountMatches reports whether the engine reports a bind mount from source to
// destination with the expected read-only flag. Docker Desktop reports host
// bind sources under its /host_mnt prefix, so the source is normalized before
// comparison (no-op on a Linux node).
func mountMatches(mounts []containers.ContainerMount, source, destination string, readOnly bool) bool {
	want := strings.TrimPrefix(filepath.Clean(source), "/host_mnt")
	for _, mount := range mounts {
		got := strings.TrimPrefix(filepath.Clean(mount.Source), "/host_mnt")
		if got == want && mount.Destination == destination && mount.ReadOnly == readOnly {
			return true
		}
	}
	return false
}

// portsMatch reports whether the container publishes exactly the expected
// bindings (so a container created before the host-IP fix is detected).
func portsMatch(actual, expected []string) bool {
	actualSet := make(map[string]bool, len(actual))
	for _, spec := range actual {
		if normalized, ok := normalizePortSpec(spec); ok {
			actualSet[normalized] = true
		}
	}
	expectedSet := make(map[string]bool, len(expected))
	for _, spec := range expected {
		if normalized, ok := normalizePortSpec(spec); ok {
			expectedSet[normalized] = true
		}
	}
	if len(actualSet) != len(expectedSet) {
		return false
	}
	for spec := range expectedSet {
		if !actualSet[spec] {
			return false
		}
	}
	return true
}

// normalizePortSpec canonicalizes "host:container" and
// "ip:host:container" bindings for comparison.
func normalizePortSpec(spec string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(spec), ":")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return "", false
		}
	}
	switch len(parts) {
	case 2, 3:
		return strings.Join(parts, ":"), true
	default:
		return "", false
	}
}

// dialAgent opens the node's ProxyService through the configured dialer.
func (s *SyncService) dialAgent(ctx context.Context, serverID uuid.UUID) (AgentClient, error) {
	if s.dial == nil {
		return nil, fmt.Errorf("%w: agent dialer is not configured", ErrAgentUnavailable)
	}
	client, err := s.dial(ctx, serverID)
	if err != nil {
		return nil, mapNodeError(err)
	}
	if client == nil {
		return nil, fmt.Errorf("%w: dial returned no client", ErrAgentUnavailable)
	}
	return client, nil
}

// configFiles maps generated documents onto the agent contract.
func configFiles(files []File) []*agentv1.ProxyConfigFile {
	out := make([]*agentv1.ProxyConfigFile, 0, len(files))
	for _, file := range files {
		out = append(out, &agentv1.ProxyConfigFile{Path: file.Name, Content: file.Content})
	}
	return out
}

// mapAgentError translates a ProxyService RPC failure: an unreachable agent is
// retryable (ErrAgentUnavailable), a rejected path or file is a validation
// failure, everything else propagates unchanged.
func mapAgentError(action string, err error) error {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return fmt.Errorf("%w: %s: %v", ErrAgentUnavailable, action, err)
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s: %v", ErrValidation, action, err)
	default:
		return fmt.Errorf("proxy: %s: %w", action, err)
	}
}

// mapNodeError translates the shared node-service sentinels (servers,
// containers) into this package's sentinels so the routes layer maps one
// vocabulary. Unrecognised errors pass through unchanged.
func mapNodeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, servers.ErrNotFound), errors.Is(err, containers.ErrServerNotFound):
		return fmt.Errorf("%w: %v", ErrServerNotFound, err)
	case errors.Is(err, containers.ErrAgentUnavailable):
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	case errors.Is(err, containers.ErrValidation):
		return fmt.Errorf("%w: %v", ErrValidation, err)
	default:
		return err
	}
}
