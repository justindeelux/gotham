package proxy

import (
	"context"
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

// Diagnostic explains why one application row was not routed. The sync still
// pushes the healthy routes, so a single pending or malformed row cannot
// freeze a node's configuration (BE-6.1 F3).
type Diagnostic struct {
	ApplicationID uuid.UUID `json:"application_id"`
	Domain        string    `json:"domain,omitempty"`
	Reason        string    `json:"reason"`
}

// PartialError reports a sync that pushed the healthy routes but skipped some
// application rows. Errors.Is(err, ErrPartialSync) is true and Diagnostics
// carries the per-application reasons.
type PartialError struct {
	Diagnostics []Diagnostic
}

// Error lists the skipped applications, bounded to keep one runaway node from
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
		parts = append(parts, fmt.Sprintf("application %s (%s)", diagnostic.ApplicationID, reason))
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
	// Nodes overrides the registered-node list (tests).
	Nodes NodeSource
	// History overrides the configuration-version store (tests). nil (and no
	// Store) disables history and revert.
	History HistoryStore
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
	nodes       NodeSource
	history     HistoryStore
	containers  containers.ContainerService
	dial        DialFunc
	logger      *slog.Logger
	format      Format
	backendHost string
	configDir   string
	acmeDir     string
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
		nodes:       cfg.nodes(),
		history:     cfg.history(),
		containers:  cfg.Containers,
		dial:        cfg.Dial,
		logger:      logger,
		format:      cfg.Format,
		backendHost: backendHost,
		configDir:   configDir,
		acmeDir:     acmeDir,
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
// is running (repairing a container whose published ports do not match the
// production bindings), writes the files and confirms them through the agent.
// Rows that cannot be routed become diagnostics instead of failing the whole
// node.
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
	nodeList, err := s.listNodeContainers(ctx, serverID)
	if err != nil {
		return fmt.Errorf("proxy: list node containers: %w", mapNodeError(err))
	}
	routes, diagnostics := routesForServer(apps, serverID, s.backendHost, nodeList)
	files, err := Generate(BuildConfig(routes), s.format)
	if err != nil {
		return err
	}

	// The configuration intent is durable before the node is touched (R2): a
	// failed record fails the sync without a replacement, so a protected push
	// is never advertised without its predecessor snapshot.
	version, changed, err := s.prepareHistory(ctx, serverID, files)
	if err != nil {
		return err
	}
	if err := s.pushAndPromote(ctx, serverID, nodeList, files, version, changed); err != nil {
		return err
	}
	s.logger.Info("proxy: configuration synced",
		"server_id", serverID.String(), "routes", len(routes), "skipped", len(diagnostics))
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

// pushAndPromote pushes files and promotes the prepared version. EVERY push
// failure retains the pending record: a failure can land after a partial
// write, a timeout or a ping, and even a dial failure leaves the durable
// intent harmless — revert targets the active snapshot while a pending record
// exists and a later same-content sync reuses the record and retries. Only a
// successful promotion (or the explicit restoration path) clears it.
func (s *SyncService) pushAndPromote(ctx context.Context, serverID uuid.UUID, nodeList []containers.Container, files []File, version ConfigVersion, changed bool) error {
	if pushErr := s.push(ctx, serverID, nodeList, files); pushErr != nil {
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
// version to fall back to.
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
	hash := previous.ContentHash
	if hash == "" {
		hash = configHash(previous.Files)
	}
	version, changed, err := s.history.PrepareConfigVersion(ctx, serverID, previous.Files, hash)
	if err != nil {
		return fmt.Errorf("%w: record revert intent: %v", ErrHistory, err)
	}
	if err := s.pushAndPromote(ctx, serverID, nodeList, previous.Files, version, changed); err != nil {
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

// routesForServer filters the proxied applications down to one node, resolves
// each one's live endpoint and classifies rows that cannot be routed as
// per-application diagnostics. Healthy rows are routed even when a sibling is
// pending or invalid, so no single row can freeze the node's configuration
// (BE-6.1 F3). Every binding of a duplicate normalized domain on this node is
// held back: row order does not prove ownership, and the unique index already
// prevents new active duplicates (R1).
func routesForServer(apps []ProxiedApplication, serverID uuid.UUID, backendHost string, nodeContainers []containers.Container) ([]Route, []Diagnostic) {
	routes := make([]Route, 0, len(apps))
	diagnostics := make([]Diagnostic, 0)
	byID := indexNodeContainers(nodeContainers)

	// Count active normalized duplicates per node before routing anything.
	domainCounts := make(map[string]int, len(apps))
	for _, app := range apps {
		if app.ServerID != serverID || app.Disabled {
			continue
		}
		domainCounts[NormalizeDomain(app.BaseDomain)]++
	}

	for _, app := range apps {
		if app.ServerID != serverID {
			continue
		}
		domain := NormalizeDomain(app.BaseDomain)
		diagnostic := Diagnostic{ApplicationID: app.ID, Domain: domain}

		switch {
		case app.Disabled:
			diagnostic.Reason = "domain disabled by the uniqueness migration; set a new domain to re-enable it"
			diagnostics = append(diagnostics, diagnostic)
			continue
		case ValidateDomain(domain) != nil:
			diagnostic.Reason = "invalid domain"
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		if domainCounts[domain] > 1 {
			diagnostic.Reason = "duplicate domain on this node; all conflicting bindings are held back"
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		if app.Port <= 0 {
			diagnostic.Reason = "application declares no container port"
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		if app.ContainerID == "" {
			diagnostic.Reason = "no running deployment yet"
			diagnostics = append(diagnostics, diagnostic)
			continue
		}

		hostPort, reason := resolveEndpoint(app, findContainer(byID, app.ContainerID))
		if reason != "" {
			diagnostic.Reason = reason
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		routes = append(routes, Route{
			AppID:  app.ID,
			Domain: domain,
			Target: fmt.Sprintf("http://%s:%d", backendHost, hostPort),
		})
	}
	return routes, diagnostics
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
// running and replaces a container whose published bindings do not match the
// production specifications (repairing containers created before the
// host-IP binding fix, F1). The verified write only reports whether the proxy
// answered its ping after the write; it is not proof that Traefik accepted
// the document (BE-6.1 A1).
//
// A failure never invalidates the caller's pending history record: the caller
// retains it for every failure, including a dial error (R2).
func (s *SyncService) push(ctx context.Context, serverID uuid.UUID, nodeList []containers.Container, files []File) error {
	client, err := s.dialAgent(ctx, serverID)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			s.logger.Debug("proxy: close agent connection", "server_id", serverID, "error", closeErr)
		}
	}()

	state := findTraefik(nodeList)
	if state.exists {
		owned, matches, reason := s.traefikMatches(state.container)
		switch {
		case !owned:
			// Never remove a same-name container this service cannot prove it
			// owns (R5); the operator resolves the conflict.
			return fmt.Errorf("%w: container %s exists on the node but is not a Gotham-managed proxy (%s)",
				ErrConflict, TraefikContainerName, reason)
		case !matches:
			s.logger.Warn("proxy: recreating gotham-traefik to converge managed state",
				"server_id", serverID.String(), "reason", reason)
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
		if _, err := client.WriteProxyConfig(ctx, &agentv1.WriteProxyConfigRequest{Files: configFiles(files)}); err != nil {
			return mapAgentError("write proxy config", err)
		}
		if err := s.bootstrapContainer(ctx, serverID, state); err != nil {
			return fmt.Errorf("proxy: bootstrap traefik: %w", mapNodeError(err))
		}
		return s.writeVerified(ctx, client, files, time.Now().Add(startupReadiness))
	}
	return s.writeVerified(ctx, client, files, time.Time{})
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
// static configuration exists (the caller writes it first).
func (s *SyncService) bootstrapContainer(ctx context.Context, serverID uuid.UUID, state containerState) error {
	if state.exists {
		return s.containers.Start(ctx, serverID, state.container.ID)
	}
	if err := s.containers.Pull(ctx, serverID, TraefikImage); err != nil {
		return err
	}
	_, err := s.containers.Run(ctx, serverID, containers.RunOptions{
		Image:         TraefikImage,
		Name:          TraefikContainerName,
		Labels:        traefikLabels(s.configDir, s.acmeDir),
		Ports:         TraefikPorts,
		Volumes:       TraefikVolumesFor(s.configDir, s.acmeDir),
		RestartPolicy: TraefikRestartPolicy,
	})
	return err
}

// traefikMatches verifies an existing container against the managed proxy's
// desired state: ownership labels, image, published ports, recorded proxy
// directories, engine-reported mounts and the real Docker restart policy.
// Unmanaged containers are reported as not owned so the caller can refuse to
// remove them (R5).
func (s *SyncService) traefikMatches(container containers.Container) (owned, matches bool, reason string) {
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
