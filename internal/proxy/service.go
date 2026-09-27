package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	// control-plane state, bootstraps the gotham-traefik container when it is
	// missing and confirms the reload through the agent.
	SyncServer(ctx context.Context, serverID uuid.UUID) error
	// SyncAll syncs every node that hosts at least one application with a
	// base domain, reporting each node's outcome.
	SyncAll(ctx context.Context) ([]SyncResult, error)
}

// SyncResult is one node's outcome of a SyncAll run.
type SyncResult struct {
	ServerID uuid.UUID `json:"server_id"`
	Error    string    `json:"error,omitempty"`
}

// Config wires a Service. Store (or an explicit ApplicationSource) and
// Containers are required inputs; Dial may be nil until the mTLS dialer is
// plugged in, and every tunable falls back to a documented default.
type Config struct {
	// Store is the PostgreSQL-backed routing input. Ignored when
	// Applications is set.
	Store *store.Store
	// Applications overrides Store (tests).
	Applications ApplicationSource
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

// Service generates the Traefik configuration from control-plane state and
// pushes it to the node agents. Generation is a pure function of the
// database, so every sync is idempotent: an unchanged state yields
// byte-identical files and Traefik's file provider reloads nothing.
type SyncService struct {
	source      ApplicationSource
	containers  containers.ContainerService
	dial        DialFunc
	logger      *slog.Logger
	format      Format
	backendHost string
	timeout     time.Duration
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
	return &SyncService{
		source:      cfg.source(),
		containers:  cfg.Containers,
		dial:        cfg.Dial,
		logger:      logger,
		format:      cfg.Format,
		backendHost: backendHost,
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

// SyncServer regenerates the node's routing configuration, ensures the
// gotham-traefik container is running and confirms the reload. When the
// container does not exist yet the files are written before it is created,
// because Traefik loads its static configuration at container start.
func (s *SyncService) SyncServer(ctx context.Context, serverID uuid.UUID) error {
	if s == nil || s.source == nil {
		return errors.New("proxy: application source is not configured")
	}
	if serverID == uuid.Nil {
		return fmt.Errorf("%w: server id is required", ErrValidation)
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	apps, err := s.source.ListProxiedApplications(ctx)
	if err != nil {
		return fmt.Errorf("proxy: list proxied applications: %w", err)
	}
	routes, err := routesForServer(apps, serverID, s.backendHost)
	if err != nil {
		return err
	}
	files, err := Generate(BuildConfig(routes), s.format)
	if err != nil {
		return err
	}

	client, err := s.dialAgent(ctx, serverID)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			s.logger.Debug("proxy: close agent connection", "server_id", serverID, "error", closeErr)
		}
	}()

	state, err := s.inspectContainer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("proxy: bootstrap traefik: %w", mapNodeError(err))
	}
	if !state.running {
		// The static configuration must exist before Traefik starts: it is
		// read once at boot. The write is unverified because the proxy is not
		// up yet; the verified write below confirms the reload.
		if _, err := client.WriteProxyConfig(ctx, &agentv1.WriteProxyConfigRequest{Files: configFiles(files)}); err != nil {
			return mapAgentError("write proxy config", err)
		}
		if err := s.bootstrapContainer(ctx, serverID, state); err != nil {
			return fmt.Errorf("proxy: bootstrap traefik: %w", mapNodeError(err))
		}
	}

	response, err := client.WriteProxyConfig(ctx, &agentv1.WriteProxyConfigRequest{
		Files:  configFiles(files),
		Verify: true,
	})
	if err != nil {
		return mapAgentError("write proxy config", err)
	}
	if !response.GetReloaded() {
		detail := strings.TrimSpace(response.GetPingError())
		if detail == "" {
			detail = "traefik did not answer its ping"
		}
		return fmt.Errorf("%w: %s", ErrReload, detail)
	}
	s.logger.Info("proxy: configuration synced", "server_id", serverID.String(), "routes", len(routes))
	return nil
}

// SyncAll regenerates the configuration of every node that hosts a proxied
// application. A lookup failure is returned; per-node failures are reported in
// the results so one unreachable node does not hide the others.
func (s *SyncService) SyncAll(ctx context.Context) ([]SyncResult, error) {
	if s == nil || s.source == nil {
		return nil, errors.New("proxy: application source is not configured")
	}
	apps, err := s.source.ListProxiedApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("proxy: list proxied applications: %w", err)
	}
	seen := make(map[uuid.UUID]bool, len(apps))
	results := make([]SyncResult, 0, len(apps))
	for _, app := range apps {
		if app.ServerID == uuid.Nil || seen[app.ServerID] {
			continue
		}
		seen[app.ServerID] = true
		result := SyncResult{ServerID: app.ServerID}
		if err := s.SyncServer(ctx, app.ServerID); err != nil {
			result.Error = err.Error()
		}
		results = append(results, result)
	}
	return results, nil
}

// routesForServer filters the proxied applications down to one node and turns
// them into routing input. Rows on other nodes are ignored, so one node's
// configuration never references another node's containers. A row hosted on
// this node that cannot be routed (invalid domain, no pinned host and
// container port) is an error, not a silent omission: the operator asked for a
// domain and must see why it is not served.
func routesForServer(apps []ProxiedApplication, serverID uuid.UUID, backendHost string) ([]Route, error) {
	routes := make([]Route, 0, len(apps))
	for _, app := range apps {
		if app.ServerID != serverID {
			continue
		}
		domain := NormalizeDomain(app.BaseDomain)
		if err := ValidateDomain(domain); err != nil {
			return nil, fmt.Errorf("application %s: %w", app.ID, err)
		}
		if app.Port <= 0 || app.HostPort <= 0 {
			return nil, fmt.Errorf("%w: application %s has domain %q but no pinned host port (set port and host_port)",
				ErrValidation, app.ID, domain)
		}
		routes = append(routes, Route{
			AppID:  app.ID,
			Domain: domain,
			Target: fmt.Sprintf("http://%s:%d", backendHost, app.HostPort),
		})
	}
	return routes, nil
}

// containerState is what one node's proxy container currently looks like.
type containerState struct {
	exists  bool
	running bool
	id      string
}

// inspectContainer finds the gotham-traefik container on the node.
func (s *SyncService) inspectContainer(ctx context.Context, serverID uuid.UUID) (containerState, error) {
	if s.containers == nil {
		return containerState{}, errors.New("container service is not configured")
	}
	existing, err := s.containers.List(ctx, serverID)
	if err != nil {
		return containerState{}, err
	}
	for _, container := range existing {
		if container.Name != TraefikContainerName {
			continue
		}
		return containerState{
			exists:  true,
			running: strings.EqualFold(container.State, "running"),
			id:      container.ID,
		}, nil
	}
	return containerState{}, nil
}

// bootstrapContainer brings the proxy container up: it starts an existing
// stopped container, or pulls the image and creates the container when it is
// missing. Creation is safe only after the static configuration exists (the
// caller writes it first).
func (s *SyncService) bootstrapContainer(ctx context.Context, serverID uuid.UUID, state containerState) error {
	if state.exists {
		return s.containers.Start(ctx, serverID, state.id)
	}
	if err := s.containers.Pull(ctx, serverID, TraefikImage); err != nil {
		return err
	}
	_, err := s.containers.Run(ctx, serverID, containers.RunOptions{
		Image:   TraefikImage,
		Name:    TraefikContainerName,
		Labels:  TraefikLabels,
		Ports:   TraefikPorts,
		Volumes: TraefikVolumes,
	})
	return err
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
