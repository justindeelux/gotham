package proxy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
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

const FeatureEnv = "FEATURE_PROXY"

func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

const defaultSyncTimeout = 5 * time.Minute

const (
	startupReadiness = 20 * time.Second
	startupPoll      = 500 * time.Millisecond
)

type AgentClient interface {
	WriteProxyConfig(ctx context.Context, in *agentv1.WriteProxyConfigRequest, opts ...grpc.CallOption) (*agentv1.WriteProxyConfigResponse, error)
	Close() error
}

type DialFunc func(ctx context.Context, serverID uuid.UUID) (AgentClient, error)

type ProxyService interface {
	SyncServer(ctx context.Context, serverID uuid.UUID) error
	SyncAll(ctx context.Context) ([]SyncResult, error)
	RevertServer(ctx context.Context, serverID uuid.UUID) error
}

type SyncResult struct {
	ServerID    uuid.UUID    `json:"server_id"`
	Error       string       `json:"error,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type Diagnostic struct {
	ApplicationID uuid.UUID `json:"application_id"`
	Kind          string    `json:"kind,omitempty"`
	Domain        string    `json:"domain,omitempty"`
	Reason        string    `json:"reason"`
}

type PartialError struct {
	Diagnostics []Diagnostic
}

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
		if diagnostic.Kind == string(RouterKindControlPlane) {
			parts = append(parts, "control plane ("+reason+")")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s (%s)", owner, diagnostic.ApplicationID, reason))
	}
	return ErrPartialSync.Error() + ": " + strings.Join(parts, "; ")
}

func (e *PartialError) Is(target error) bool { return target == ErrPartialSync }

type Config struct {
	PanelCertificates          SSLStore
	ReconcilePanelCertificates func(context.Context) error

	CertificateStatus CertificateStatusService
	ControlPlaneURL   func(context.Context) (string, error)
	ControlPlaneURLs  func(context.Context) ([]string, error)
	ServerPort        int
	Store             *store.Store
	Applications      ApplicationSource
	Services          ServiceSource
	Redirects         RedirectSource
	Nodes             NodeSource
	History           HistoryStore
	DNSProviders      DNSProviderSource
	Containers        containers.ContainerService
	Dial              DialFunc
	Logger            *slog.Logger
	Format            Format
	BackendHost       string
	Secret            string
	ACMEEmail         string
	CAServer          string
	ConfigDir         string
	AcmeDir           string
	Timeout           time.Duration
}

func (c Config) source() ApplicationSource {
	if c.Applications != nil {
		return c.Applications
	}
	if c.Store != nil {
		return storeSource{store: c.Store}
	}
	return nil
}

func (c Config) services() ServiceSource {
	return c.Services
}

func (c Config) nodes() NodeSource {
	if c.Nodes != nil {
		return c.Nodes
	}
	if c.Store != nil {
		return storeSource{store: c.Store}
	}
	return nil
}

func (c Config) providers() DNSProviderSource {
	if c.DNSProviders != nil {
		return c.DNSProviders
	}
	if c.Store != nil {
		return storeSource{store: c.Store}
	}
	return nil
}

func (c Config) redirects() RedirectSource {
	if c.Redirects != nil {
		return c.Redirects
	}
	if c.Store != nil {
		return storeSource{store: c.Store}
	}
	return nil
}

func (c Config) history() HistoryStore {
	if c.History != nil {
		return c.History
	}
	if c.Store != nil {
		return storeHistory{store: c.Store}
	}
	return nil
}

type SyncService struct {
	panelCertificates SSLStore
	reconcilePanel    func(context.Context) error
	certificateStatus CertificateStatusService
	controlPlaneURL   func(context.Context) (string, error)
	controlPlaneURLs  func(context.Context) ([]string, error)
	serverPort        int
	source            ApplicationSource
	services          ServiceSource
	redirects         RedirectSource
	nodes             NodeSource
	history           HistoryStore
	providers         DNSProviderSource
	containers        containers.ContainerService
	dial              DialFunc
	logger            *slog.Logger
	format            Format
	backendHost       string
	configDir         string
	acmeDir           string
	secret            string
	acmeEmail         string
	caServer          string
	timeout           time.Duration

	mu sync.Mutex
}

var _ ProxyService = (*SyncService)(nil)

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
	if cfg.PanelCertificates == nil && cfg.Store != nil {
		cfg.PanelCertificates = NewStoreSSL(cfg.Store)
	}
	if cfg.CertificateStatus == nil && cfg.Store != nil {
		cfg.CertificateStatus = NewDefaultCertificateStatusService(CertificateStatusConfig{
			Store: NewStoreCertificateStatus(cfg.Store), Logger: logger,
			Dial: func(ctx context.Context, id uuid.UUID) (ACMEReader, error) {
				if cfg.Dial == nil {
					return nil, errStatusDialerUnconfigured
				}
				client, err := cfg.Dial(ctx, id)
				if err != nil {
					return nil, err
				}
				if reader, ok := client.(ACMEReader); ok {
					return reader, nil
				}
				_ = client.Close()
				return nil, errStatusDialerUnconfigured
			},
		})
	}
	return &SyncService{
		panelCertificates: cfg.PanelCertificates,
		reconcilePanel:    cfg.ReconcilePanelCertificates,
		certificateStatus: cfg.CertificateStatus,
		controlPlaneURL:   cfg.ControlPlaneURL,
		controlPlaneURLs:  cfg.ControlPlaneURLs,
		serverPort:        cfg.ServerPort,
		source:            cfg.source(),
		services:          cfg.services(),
		redirects:         cfg.redirects(),
		nodes:             cfg.nodes(),
		history:           cfg.history(),
		providers:         cfg.providers(),
		containers:        cfg.Containers,
		dial:              cfg.Dial,
		logger:            logger,
		format:            cfg.Format,
		backendHost:       backendHost,
		configDir:         configDir,
		acmeDir:           acmeDir,
		secret:            cfg.Secret,
		acmeEmail:         cfg.ACMEEmail,
		caServer:          cfg.CAServer,
		timeout:           timeout,
	}
}

func NewDefaultService(cfg Config) ProxyService {
	if cfg.source() == nil || cfg.Containers == nil {
		return nil
	}
	if !Enabled() {
		return nil
	}
	return NewService(cfg)
}

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
	routes, redirects, diagnostics, _ := s.gatherRoutes(ctx, apps, proxiedServices, rules, serverID, nodeList, access)
	files, err := Generate(BuildConfig(routes, redirects, providers, s.acmeEmail, s.caServer), s.format)
	if err != nil {
		return err
	}
	desired := desiredState{
		files: files,
		env:   traefikEnv(routes, access),
	}
	desired.envHash = envFingerprint(s.secret, desired.env)

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

func (s *SyncService) pushAndPromote(ctx context.Context, serverID uuid.UUID, nodeList []containers.Container, desired desiredState, version ConfigVersion, changed bool) error {
	if pushErr := s.push(ctx, serverID, nodeList, desired); pushErr != nil {
		if s.history != nil && changed {
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

func (s *SyncService) SyncControlPlane(ctx context.Context) error {
	named, ok := s.nodes.(namedNodeSource)
	if !ok {
		return errors.New("proxy: localhost node is not registered")
	}
	nodes, err := named.ListNamedNodes(ctx)
	if err != nil {
		return fmt.Errorf("proxy: find localhost node: %w", err)
	}
	for _, node := range nodes {
		if node.Name != "localhost" || node.ID == uuid.Nil {
			continue
		}
		err := s.SyncServer(ctx, node.ID)
		var partial *PartialError
		if errors.As(err, &partial) {
			for _, diagnostic := range partial.Diagnostics {
				if diagnostic.Kind == string(RouterKindControlPlane) {
					return err
				}
			}
			return nil
		}
		return err
	}
	return errors.New("proxy: localhost node is not registered")
}

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
	_, diagnostics := s.controlPlaneRoute(ctx, uuid.Nil, nil, nil)
	if len(diagnostics) > 0 {
		return results, &PartialError{Diagnostics: diagnostics}
	}
	return results, nil
}

func (s *SyncService) gatherRoutes(ctx context.Context, apps []ProxiedApplication, services []ProxiedService, rules []RedirectRule, serverID uuid.UUID, nodeList []containers.Container, access map[uuid.UUID]providerAccess) ([]Route, []Redirect, []Diagnostic, int) {
	routes, diagnostics := routesForServer(apps, serverID, s.backendHost, nodeList, access)
	appCount := len(routes)
	claimed := applicationHosts(apps, serverID)
	serviceRoutes, serviceDiagnostics, serviceHosts := serviceRoutesForServer(services, serverID, s.backendHost, nodeList, claimed)
	routes = append(routes, serviceRoutes...)
	diagnostics = append(diagnostics, serviceDiagnostics...)
	redirects, redirectDiagnostics := redirectsForServer(apps, rules, serverID, serviceHosts...)
	diagnostics = append(diagnostics, redirectDiagnostics...)
	for _, host := range serviceHosts {
		claimed[host] = true
	}
	for _, redirect := range redirects {
		claimed[redirect.Source] = true
	}
	panel, panelDiagnostics := s.controlPlaneRoute(ctx, serverID, claimed, access)
	routes = append(routes, panel...)
	diagnostics = append(diagnostics, panelDiagnostics...)
	return routes, redirects, diagnostics, appCount
}

func (s *SyncService) controlPlaneRoute(ctx context.Context, serverID uuid.UUID, claimed map[string]bool, access map[uuid.UUID]providerAccess) ([]Route, []Diagnostic) {
	if s.controlPlaneURLs == nil {
		return s.controlPlaneRouteForURL(ctx, serverID, claimed, access, s.controlPlaneURL)
	}
	urls, err := s.controlPlaneURLs(ctx)
	if err != nil {
		return s.controlPlaneRouteForURL(ctx, serverID, claimed, access, func(context.Context) (string, error) { return "", err })
	}
	var routes []Route
	var diagnostics []Diagnostic
	seen := map[string]bool{}
	for i, raw := range urls {
		entries, problems := s.controlPlaneRouteForURL(ctx, serverID, claimed, access, func(context.Context) (string, error) { return raw, nil })
		for _, route := range entries {
			if seen[route.Domain] {
				continue
			}
			seen[route.Domain] = true
			if i > 0 {
				route.Name = "panel-previous"
			}
			routes = append(routes, route)
		}
		diagnostics = append(diagnostics, problems...)
	}
	return routes, diagnostics
}

func (s *SyncService) controlPlaneRouteForURL(ctx context.Context, serverID uuid.UUID, claimed map[string]bool, access map[uuid.UUID]providerAccess, resolveURL func(context.Context) (string, error)) ([]Route, []Diagnostic) {
	if resolveURL == nil {
		return nil, nil
	}
	diagnostic := Diagnostic{Kind: string(RouterKindControlPlane)}
	fail := func(reason string) ([]Route, []Diagnostic) {
		diagnostic.Reason = reason
		s.logger.Warn("proxy: control-plane route skipped", "reason", reason, "domain", diagnostic.Domain)
		return nil, []Diagnostic{diagnostic}
	}
	localhost := uuid.Nil
	nodeReason := "localhost node is not registered"
	if named, ok := s.nodes.(namedNodeSource); ok {
		nodes, err := named.ListNamedNodes(ctx)
		if err != nil {
			nodeReason = "cannot find localhost node: " + err.Error()
		} else {
			for _, node := range nodes {
				if node.Name == "localhost" {
					localhost = node.ID
					break
				}
			}
		}
	}
	if serverID != localhost {
		return nil, nil
	}
	raw, err := resolveURL(ctx)
	if err != nil {
		return fail("cannot read control_plane_url: " + err.Error())
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if localhost == uuid.Nil {
		return fail(nodeReason)
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fail("control_plane_url must be an http(s) URL without credentials, query or fragment")
	}
	domain := NormalizeDomain(u.Hostname())
	diagnostic.Domain = domain
	if err := ValidateDomain(domain); err != nil {
		return fail("invalid control_plane_url host: " + err.Error())
	}
	if claimed[domain] {
		return fail("control_plane_url host is already claimed by an application, service or redirect on this node")
	}
	if s.serverPort < 1 || s.serverPort > 65535 {
		return fail("invalid control-plane HTTP listener port")
	}
	route := Route{Name: "panel", Domain: domain, Target: "http://" + net.JoinHostPort(s.backendHost, strconv.Itoa(s.serverPort))}
	if u.Scheme == "https" {
		state := PanelCertificateState{Status: "pending"}
		route.PanelCertificate = &state
		if s.panelCertificates != nil {
			certs, err := s.panelCertificates.ListCertificates(ctx)
			if err != nil {
				state.Status = "failed"
				s.logger.Warn("proxy: read panel certificates", "error", err)
			} else {
				for _, cert := range certs {
					if cert.ApplicationID != uuid.Nil || cert.Domain != domain {
						continue
					}
					intent := &CertificateIntent{Domain: cert.Domain, Enabled: cert.Enabled, Challenge: cert.Challenge, DNSProviderID: cert.DNSProviderID, Wildcard: cert.Wildcard}
					certificate, reason := resolveRouteCertificate(ProxiedApplication{Certificate: intent}, domain, access)
					observation := CertificateStatusObservation{}
					if s.certificateStatus != nil {
						observation = s.certificateStatus.CertificateStatuses(ctx, []DomainCertificate{cert})[cert.ID]
					}
					state = panelCertificateState(observation, certificate != nil && reason == "")
					route.Certificate = certificate
					route.PanelCertificate = &state
					break
				}
			}
		}
	}
	return []Route{route}, nil
}

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

func applicationHosts(apps []ProxiedApplication, serverID uuid.UUID) map[string]bool {
	hosts := make(map[string]bool, len(apps))
	for _, app := range apps {
		if app.ServerID != serverID {
			continue
		}
		for _, domain := range appDomains(app) {
			if domain.Disabled {
				continue
			}
			if host := NormalizeDomain(domain.Domain); host != "" {
				hosts[host] = true
			}
		}
	}
	return hosts
}

func appDomains(app ProxiedApplication) []ProxiedDomain {
	return app.Domains
}

func composeProjectName(id uuid.UUID) string {
	return "gotham-" + id.String()
}

func serviceRouteName(id uuid.UUID, index int) string {
	return "svc-" + id.String() + "-" + strconv.Itoa(index)
}

func serviceRoutesForServer(proxied []ProxiedService, serverID uuid.UUID, backendHost string, nodeContainers []containers.Container, claimed map[string]bool) ([]Route, []Diagnostic, []string) {
	routes := make([]Route, 0, len(proxied))
	diagnostics := make([]Diagnostic, 0)
	hosts := []string{}

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

func routesForServer(apps []ProxiedApplication, serverID uuid.UUID, backendHost string, nodeContainers []containers.Container, providers map[uuid.UUID]providerAccess) ([]Route, []Diagnostic) {
	routes := make([]Route, 0, len(apps))
	diagnostics := make([]Diagnostic, 0)
	byID := indexNodeContainers(nodeContainers)

	domainCounts := make(map[string]int, len(apps))
	for _, app := range apps {
		if app.ServerID != serverID {
			continue
		}
		for _, domain := range appDomains(app) {
			if domain.Disabled {
				continue
			}
			host := NormalizeDomain(domain.Domain)
			if ValidateDomain(host) == nil {
				domainCounts[host]++
			}
		}
	}

	for _, app := range apps {
		if app.ServerID != serverID {
			continue
		}
		for _, domain := range appDomains(app) {
			host := NormalizeDomain(domain.Domain)
			diagnostic := Diagnostic{ApplicationID: app.ID, Domain: host}
			switch {
			case domain.Disabled:
				diagnostic.Reason = "domain disabled by a domain-uniqueness conflict; remove it or set a new domain to re-enable it"
				diagnostics = append(diagnostics, diagnostic)
				continue
			case ValidateDomain(host) != nil:
				diagnostic.Reason = "invalid domain"
				diagnostics = append(diagnostics, diagnostic)
				continue
			case domainCounts[host] > 1:
				diagnostic.Reason = "duplicate domain on this node; all conflicting bindings are held back"
				diagnostics = append(diagnostics, diagnostic)
				continue
			}
			target, reason := resolveDomainTarget(app, domain, backendHost, nodeContainers, byID)
			if reason != "" {
				diagnostic.Reason = reason
				diagnostics = append(diagnostics, diagnostic)
				continue
			}
			protocol := NormalizeProtocol(domain.Protocol)
			if domain.Protocol == "" && app.Certificate != nil {
				protocol = ProtocolHTTPS
			}
			route := Route{
				Name:     AliasRouteName(app.ID, host),
				AppID:    app.ID,
				Domain:   host,
				Protocol: protocol,
				Target:   target,
			}
			route.Service = route.Name
			if protocol == ProtocolHTTPS {
				certificate, certReason := resolveRouteCertificate(app, host, providers)
				route.Certificate = certificate
				if certReason != "" {
					if certificate == nil {
						route.Protocol = ProtocolHTTP
					}
					diagnostics = append(diagnostics, Diagnostic{
						ApplicationID: app.ID,
						Domain:        host,
						Reason:        certReason,
					})
				}
			}
			routes = append(routes, route)
		}
	}
	return routes, diagnostics
}

func resolveDomainTarget(app ProxiedApplication, domain ProxiedDomain, backendHost string, nodeContainers []containers.Container, byID map[string]*containers.Container) (string, string) {
	if domain.Port <= 0 {
		return "", "no running container port, redeploy to apply"
	}
	service := strings.TrimSpace(domain.Service)
	var hostPort int32
	if service != "" {
		container := findComposeContainer(nodeContainers, composeProjectName(app.ID), service)
		if container == nil {
			return "", fmt.Sprintf("no running container for compose service %q found on the node", service)
		}
		hostPort = publishedHostPort(container, domain.Port)
		if hostPort <= 0 {
			return "", fmt.Sprintf("compose service %q does not publish container port %d", service, domain.Port)
		}
	} else {
		if app.ContainerID == "" {
			return "", "no running deployment yet"
		}
		var reason string
		hostPort, reason = resolveEndpoint(findContainer(byID, app.ContainerID), domain.Port)
		if reason != "" {
			return "", reason
		}
	}
	return fmt.Sprintf("http://%s:%d", backendHost, hostPort), ""
}

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
			host := NormalizeDomain(domain.Domain)
			if host != "" {
				nodeDomains[host] = true
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
		return nil, fmt.Sprintf("certificate configuration is recorded for %q; update it to certify %q", intent.Domain, domain)
	}
	return nil, ""
}

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

type providerAccess struct {
	provider   DNSProvider
	credential string
	err        error
}

type desiredState struct {
	files   []File
	env     []string
	envHash string
}

func (s *SyncService) listProviders(ctx context.Context) ([]DNSProvider, error) {
	if s.providers == nil {
		return nil, nil
	}
	return s.providers.ListDNSProviders(ctx)
}

func (s *SyncService) listRedirectRules(ctx context.Context) ([]RedirectRule, error) {
	if s.redirects == nil {
		return nil, nil
	}
	return s.redirects.ListRedirectRules(ctx)
}

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

func resolveEndpoint(container *containers.Container, port int32) (int32, string) {
	if container == nil {
		return 0, "no running deployment container found on the node"
	}
	if !strings.EqualFold(container.State, "running") {
		return 0, "the deployment container is not running"
	}
	if !container.PortsReported {
		return 0, "no running container port, redeploy to apply"
	}
	if hostPort := publishedHostPort(container, port); hostPort > 0 {
		return hostPort, ""
	}
	return 0, fmt.Sprintf("container port %d has no reachable published binding", port)
}

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

func indexNodeContainers(list []containers.Container) map[string]*containers.Container {
	byID := make(map[string]*containers.Container, len(list))
	for i := range list {
		if list[i].ID != "" {
			byID[list[i].ID] = &list[i]
		}
	}
	return byID
}

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

func (s *SyncService) push(ctx context.Context, serverID uuid.UUID, nodeList []containers.Container, desired desiredState) (err error) {
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
			return fmt.Errorf("%w: container %s exists on the node but is not a Gotham-managed proxy (%s)",
				ErrConflict, TraefikContainerName, reason)
		case !matches:
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

func configHash(files []File) string {
	sum := sha256.New()
	for _, file := range files {
		sum.Write([]byte(file.Name))
		sum.Write([]byte{0})
		sum.Write(file.Content)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

type containerState struct {
	exists    bool
	running   bool
	container containers.Container
}

func (s *SyncService) listNodeContainers(ctx context.Context, serverID uuid.UUID) ([]containers.Container, error) {
	type freshLister interface {
		ListFresh(ctx context.Context, serverID uuid.UUID) ([]containers.Container, error)
	}
	if lister, ok := s.containers.(freshLister); ok {
		return lister.ListFresh(ctx, serverID)
	}
	return s.containers.List(ctx, serverID)
}

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
	return err
}

type redactedError struct {
	message string
	err     error
}

func (e *redactedError) Error() string { return e.message }

func (e *redactedError) Unwrap() error { return e.err }

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

func configFiles(files []File) []*agentv1.ProxyConfigFile {
	out := make([]*agentv1.ProxyConfigFile, 0, len(files))
	for _, file := range files {
		out = append(out, &agentv1.ProxyConfigFile{Path: file.Name, Content: file.Content})
	}
	return out
}

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
