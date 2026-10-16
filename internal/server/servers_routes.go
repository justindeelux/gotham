package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/teams"
	"github.com/justindeelux/gotham/updatecore"
)

// ServerService is the subset of servers.ServerService the HTTP layer depends
// on. Keeping it an interface lets tests substitute a fake without a database.
type ServerService interface {
	Add(ctx context.Context, userID uuid.UUID, name, ip string, port int, sshUser string, sshKeyID uuid.UUID, password string) (*servers.Server, error)
	List(ctx context.Context) ([]servers.Server, error)
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
	Update(ctx context.Context, id uuid.UUID, params servers.UpdateParams) (*servers.Server, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Validate(ctx context.Context, id uuid.UUID, auth servers.ValidateAuth) (*servers.ValidationResult, error)
	InstallDocker(ctx context.Context, id uuid.UUID, auth servers.ValidateAuth, out io.Writer) error
	ResetHostKey(ctx context.Context, id uuid.UUID) (*servers.Server, error)
	AddPrivateKey(ctx context.Context, name, privateKeyPEM string) (*servers.PrivateKey, error)
	Metrics(ctx context.Context, id uuid.UUID, from, to time.Time, step string) ([]servers.MetricPoint, error)
}

// createServerRequest is the body of POST /api/v1/servers.
type createServerRequest struct {
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	SSHUser  string `json:"ssh_user"`
	SSHKeyID string `json:"ssh_key_id"`
	// Password is the optional node password for password auth. It is
	// write-only: encrypted on receipt, never returned, never logged.
	// Exactly one of ssh_key_id and password may be given.
	Password string `json:"password"`
}

// updateServerRequest is the body of PATCH /api/v1/servers/{id}. Every field
// is optional: a missing field leaves the column unchanged. SSHKeyID points to
// the new key, or to "" to detach the key; Password holds the new secret
// (which replaces any attached key), "" to forget the stored secret, and a
// missing password to leave it unchanged.
type updateServerRequest struct {
	Name     *string `json:"name"`
	IP       *string `json:"ip"`
	Port     *int    `json:"port"`
	SSHUser  *string `json:"ssh_user"`
	SSHKeyID *string `json:"ssh_key_id"`
	Password *string `json:"password"`
}

// validateServerRequest is the optional body of POST /api/v1/servers/{id}/validate.
type validateServerRequest struct {
	SSHKeyID string `json:"ssh_key_id"`
	Password string `json:"password"`
	// Passphrase decrypts a passphrase-protected SSH private key for this run.
	// It is transient: never stored, never logged.
	Passphrase string `json:"passphrase"`
	// TrustHostKey is the operator's explicit consent to trust an unpinned host
	// key, required for the first password validation of a node.
	TrustHostKey bool `json:"trust_host_key"`
}

// createPrivateKeyRequest is the body of POST /api/v1/private-keys.
type createPrivateKeyRequest struct {
	Name       string `json:"name"`
	PrivateKey string `json:"private_key"`
}

// serverDTO is the wire representation of a managed server.
type serverDTO struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	IP       string  `json:"ip"`
	Port     int     `json:"port"`
	SSHUser  string  `json:"ssh_user"`
	SSHKeyID *string `json:"ssh_key_id,omitempty"`
	// HasPassword reports whether a password secret is stored. The secret
	// itself never appears on the wire.
	HasPassword    bool       `json:"has_password"`
	Status         string     `json:"status"`
	NodeID         *string    `json:"node_id,omitempty"`
	OS             *string    `json:"os,omitempty"`
	DockerVersion  *string    `json:"docker_version,omitempty"`
	Arch           *string    `json:"arch,omitempty"`
	TotalMem       *int64     `json:"total_mem,omitempty"`
	TotalDisk      *int64     `json:"total_disk,omitempty"`
	CPUUsage       *float64   `json:"cpu_usage,omitempty"`
	MemUsage       *float64   `json:"mem_usage,omitempty"`
	DiskUsage      *float64   `json:"disk_usage,omitempty"`
	ContainerCount *int64     `json:"container_count,omitempty"`
	LastSeen       *time.Time `json:"last_seen,omitempty"`
	// HostKeyFingerprint is the TOFU-pinned SSH host key (SHA256), when known.
	HostKeyFingerprint *string   `json:"host_key_fingerprint,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// serverEnvelope wraps a single server.
type serverEnvelope struct {
	Server serverDTO `json:"server"`
}

// serverListEnvelope wraps a list of servers.
type serverListEnvelope struct {
	Servers []serverDTO `json:"servers"`
}

// validateResponse reports the per-check validation outcome.
type validateResponse struct {
	Message string                `json:"message,omitempty"`
	Checks  []servers.CheckResult `json:"checks"`
	Server  *serverDTO            `json:"server,omitempty"`
}

// privateKeyResponse is the body returned when a private key is created. The
// key material is never returned.
type privateKeyResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// metricPointDTO is one aggregated bucket of a server's time series on the
// wire. Buckets are UTC, epoch-aligned and carry the average of the samples
// that fell into them.
type metricPointDTO struct {
	Bucket         time.Time `json:"bucket"`
	CPUUsage       float64   `json:"cpu_usage"`
	MemUsage       float64   `json:"mem_usage"`
	DiskUsage      float64   `json:"disk_usage"`
	NetRxBps       float64   `json:"net_rx_bps"`
	NetTxBps       float64   `json:"net_tx_bps"`
	DiskReadBps    float64   `json:"disk_read_bps"`
	DiskWriteBps   float64   `json:"disk_write_bps"`
	ContainerCount float64   `json:"container_count"`
}

// metricsEnvelope wraps a metrics series. Buckets without samples are omitted
// rather than returned as zeros, so a gap in the series is a gap on the chart.
type metricsEnvelope struct {
	Step   string           `json:"step"`
	Points []metricPointDTO `json:"points"`
}

// agentVersionDTO is one node's reported agent version on the wire.
type agentVersionDTO struct {
	NodeID  string    `json:"node_id"`
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

// agentVersionsEnvelope is the GET /api/v1/servers/agents body: the known agent
// version map plus the active rollout target and the latest release the control
// plane can see.
type agentVersionsEnvelope struct {
	Agents         []agentVersionDTO `json:"agents"`
	RolloutVersion string            `json:"rollout_version,omitempty"`
	LatestVersion  string            `json:"latest_version,omitempty"`
}

// updateAllAgentsResponse is the POST /api/v1/servers/agents/update-all body.
type updateAllAgentsResponse struct {
	TargetVersion string `json:"target_version,omitempty"`
	Agents        int    `json:"agents"`
	// Pending is the number of known agents whose reported version differs from
	// the target.
	Pending int    `json:"pending"`
	Message string `json:"message,omitempty"`
}

// agentUpdateController is the optional agent-update surface of the server
// registry (BE-9.2). The HTTP layer type-asserts its ServerService to this
// interface, so tests with a plain fake registry simply do not mount these
// routes (versionReporter and the dialer interfaces declare the same pattern).
type agentUpdateController interface {
	KnownAgentVersions() []servers.AgentVersion
	AgentRolloutVersion() string
	StartAgentRollout(version string)
	// AgentUpdateTarget resolves the newest agent release version from the
	// agent release family (independent of the control plane's own version).
	AgentUpdateTarget(ctx context.Context) (string, error)
}

// mountServerRoutes registers the authenticated node-management endpoints under
// /api.
func (s *Server) mountServerRoutes(api chi.Router) {
	api.Group(func(protected chi.Router) {
		protected.Use(s.RequireAuth, requireResourceScopes, s.RequireTeam, s.teamWriteGate)
		protected.Get("/v1/servers", s.handleListServers)
		protected.Post("/v1/servers", s.handleCreateServer)
		protected.Get("/v1/servers/{id}", s.handleGetServer)
		protected.Patch("/v1/servers/{id}", s.handleUpdateServer)
		protected.Delete("/v1/servers/{id}", s.handleDeleteServer)
		protected.Post("/v1/servers/{id}/validate", s.handleValidateServer)
		protected.Post("/v1/servers/{id}/docker-install", s.handleInstallDocker)
		protected.Delete("/v1/servers/{id}/host-key", s.handleResetHostKey)
		protected.Post("/v1/private-keys", s.handleCreatePrivateKey)
		// The metrics range route is part of the servers surface but rides its
		// own kill switch: FEATURE_METRICS=false mounts nothing here.
		if servers.MetricsEnabled() {
			protected.Get("/v1/servers/{id}/metrics", s.handleServerMetrics)
		}
	})

	// Agent update surface (BE-9.2): platform-operator only, like the
	// control-plane self-update apply. The trigger only records a rollout
	// target; agents pick it up on their next RequestUpdate poll, so the
	// request never blocks on a node.
	if _, ok := s.servers.(agentUpdateController); ok {
		api.Group(func(platform chi.Router) {
			platform.Use(s.RequireAuth, s.RequirePlatformAdmin)
			platform.Get("/v1/servers/agents", s.handleListAgentVersions)
			platform.Post("/v1/servers/agents/update-all", s.handleUpdateAllAgents)
		})
	}
}

// handleListServers returns every managed server.
func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	items, err := s.servers.List(r.Context())
	if err != nil {
		s.writeServerError(w, "list", err)
		return
	}

	response := serverListEnvelope{Servers: make([]serverDTO, 0, len(items))}
	for i := range items {
		response.Servers = append(response.Servers, newServerDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, response)
}

// handleCreateServer registers a new server.
func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	var req createServerRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	keyID, ok := parseOptionalUUID(w, req.SSHKeyID, "ssh_key_id")
	if !ok {
		return
	}

	created, err := s.servers.Add(r.Context(), userID, req.Name, req.IP, req.Port, req.SSHUser, keyID, req.Password)
	if err != nil {
		s.writeServerError(w, "create", err)
		return
	}

	writeJSON(w, http.StatusCreated, serverEnvelope{Server: newServerDTO(created)})
}

// handleGetServer returns one server.
func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}

	found, err := s.servers.Get(r.Context(), id)
	if err != nil {
		s.writeServerError(w, "get", err)
		return
	}
	writeJSON(w, http.StatusOK, serverEnvelope{Server: newServerDTO(found)})
}

// handleUpdateServer applies a PATCH edit to one server.
func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}

	var req updateServerRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	params := servers.UpdateParams{
		Name:    req.Name,
		IP:      req.IP,
		Port:    req.Port,
		SSHUser: req.SSHUser,
	}
	if req.SSHKeyID != nil {
		keyID, ok := parseOptionalUUID(w, *req.SSHKeyID, "ssh_key_id")
		if !ok {
			return
		}
		params.SSHKeyID = &keyID
	}
	params.Password = req.Password

	updated, err := s.servers.Update(r.Context(), id, params)
	if err != nil {
		s.writeServerError(w, "update", err)
		return
	}
	writeJSON(w, http.StatusOK, serverEnvelope{Server: newServerDTO(updated)})
}

// handleDeleteServer removes one server.
func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}

	if err := s.servers.Delete(r.Context(), id); err != nil {
		s.writeServerError(w, "delete", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleValidateServer runs the SSH probes and reports each check.
func (s *Server) handleValidateServer(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}

	auth, ok := s.validateAuthFromBody(w, r)
	if !ok {
		return
	}

	result, err := s.servers.Validate(r.Context(), id, auth)
	if err != nil {
		// A failed check is a 422 with the structured check list; a missing
		// server or bad request maps to 404/400.
		if result != nil && errors.Is(err, servers.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, validateResponse{
				Message: err.Error(),
				Checks:  result.Checks,
				Server:  serverDTOPtr(result.Server),
			})
			return
		}
		s.writeServerError(w, "validate", err)
		return
	}

	// A validated node gets its proxy bootstrapped so the first domain attach
	// does not depend on a manual sync. The sync is idempotent (it only
	// creates the container when missing and rewrites byte-identical files
	// otherwise) and runs in the background: pulling the Traefik image can
	// take a minute, and the validation response must not wait for it. A
	// failure is logged and retried by the next domain change or a manual
	// POST /v1/proxy/sync.
	if s.proxy != nil {
		// The context is detached from the request before the goroutine
		// starts, because a Request's context is invalidated when the handler
		// returns.
		bootstrapCtx := context.WithoutCancel(r.Context())
		go func() {
			if err := s.proxy.SyncServer(bootstrapCtx, id); err != nil {
				s.logger.Warn("servers: traefik bootstrap failed", "server_id", id.String(), "error", err)
			}
		}()
	}

	writeJSON(w, http.StatusOK, validateResponse{
		Checks: result.Checks,
		Server: serverDTOPtr(result.Server),
	})
}

// installMarkerPrefix begins the final line of a docker-install stream. Once
// the stream is committed the HTTP status cannot change, so the terminal
// outcome travels in-band as a JSON object on this last line. Remote output can
// never carry the prefix: the stream writer strips the framing byte from every
// remote chunk, and the marker also carries a per-request nonce delivered in
// the nonce header, so a framing-strip bug alone still cannot forge it. Keep
// the prefix and the header name in sync with installMarkerPrefix and
// installNonceHeader in
// web/src/features/servers/composables/useDockerInstall.ts.
const (
	installMarkerPrefix = "\x1e[gotham-install] "
	// installNonceHeader carries the per-request marker nonce. It is set before
	// the first body byte, so the client always has it before the marker.
	installNonceHeader = "X-Install-Nonce"
	// installFramingByte is the record separator the marker prefix starts with.
	installFramingByte = 0x1e
)

// installMarker is the terminal status of one install stream.
type installMarker struct {
	Status   string `json:"status"` // "ok" | "fail"
	Nonce    string `json:"nonce"`
	ExitCode int    `json:"exit_code,omitempty"`
	Message  string `json:"message,omitempty"`
}

// newInstallNonce returns an unguessable per-request marker nonce. A failure to
// read randomness yields an empty nonce, which the client rejects (safe
// direction: an install with no valid nonce reads as a lost connection).
func newInstallNonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}

// handleInstallDocker installs Docker Engine on a node over SSH and streams the
// installer output as a plain-text chunked body (POST
// /v1/servers/{id}/docker-install). The 200 is deferred until the first output
// byte, so a failure before the install starts (SSH, privilege, unsupported
// distribution or architecture, or a concurrent install) is still answered as a
// real JSON error. Once the stream is committed the outcome is carried by the
// terminal marker line, because the status can no longer change.
func (s *Server) handleInstallDocker(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}

	auth, ok := s.validateAuthFromBody(w, r)
	if !ok {
		return
	}

	nonce := newInstallNonce()
	out := &streamWriter{w: w, flusher: flusherOf(w), nonce: nonce}
	err := s.servers.InstallDocker(r.Context(), id, auth, out)
	if !out.started {
		if err != nil {
			s.writeServerError(w, "install docker", err)
		}
		return
	}

	marker := installMarker{Status: "ok", Nonce: nonce}
	if err != nil {
		marker = installMarker{
			Status:   "fail",
			Nonce:    nonce,
			ExitCode: servers.DockerInstallExitCode(err),
			Message:  err.Error(),
		}
		s.logger.Warn("servers: docker install failed after the stream started",
			"server_id", id.String(), "exit_code", marker.ExitCode, "error", err)
	}
	out.writeMarker(marker)
}

// streamWriter defers the response status until the first byte, so a handler
// that can fail before producing output still answers a real error status. Once
// the first byte is written the response is a committed 200 text stream.
type streamWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	nonce   string
	started bool
	// lastByte is the last byte sent to the client, so the terminal marker can
	// be separated by a newline when the installer's output did not end in one.
	lastByte byte
}

// Write streams remote output. The framing byte is stripped, so remote output
// can never contain the terminal marker prefix; the whole slice is reported as
// written because every byte of it was consumed (only framing was dropped).
func (s *streamWriter) Write(p []byte) (int, error) {
	s.start()
	clean := stripInstallFraming(p)
	if len(clean) > 0 {
		if _, err := s.w.Write(clean); err != nil {
			return 0, err
		}
		s.lastByte = clean[len(clean)-1]
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return len(p), nil
}

// writeMarker writes the terminal status line on the frame-writer's own path
// (never stripped), separated from remote output by a newline so it can never
// be glued to an unterminated line.
func (s *streamWriter) writeMarker(marker installMarker) {
	s.start()
	if s.lastByte != '\n' {
		if _, err := io.WriteString(s.w, "\n"); err != nil {
			return
		}
	}
	payload, err := json.Marshal(marker)
	if err != nil {
		return
	}
	if _, err := io.WriteString(s.w, installMarkerPrefix+string(payload)+"\n"); err != nil {
		return
	}
	s.lastByte = '\n'
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// stripInstallFraming removes the marker framing byte from remote output. It
// returns the input unchanged when there is nothing to strip.
func stripInstallFraming(p []byte) []byte {
	if bytes.IndexByte(p, installFramingByte) < 0 {
		return p
	}
	clean := make([]byte, 0, len(p))
	for _, b := range p {
		if b != installFramingByte {
			clean = append(clean, b)
		}
	}
	return clean
}

// start commits the 200 and the streaming headers once.
func (s *streamWriter) start() {
	if s.started {
		return
	}
	s.started = true
	s.w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	s.w.Header().Set("Cache-Control", "no-store")
	if s.nonce != "" {
		s.w.Header().Set(installNonceHeader, s.nonce)
	}
	s.w.WriteHeader(http.StatusOK)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// flusherOf returns the response's Flusher when it has one.
func flusherOf(w http.ResponseWriter) http.Flusher {
	flusher, _ := w.(http.Flusher)
	return flusher
}

// handleResetHostKey forgets a server's pinned SSH host key, so the next
// validation re-pins it. It is the operator escape hatch after a legitimate
// host key rotation; a changed key otherwise fails validation closed.
func (s *Server) handleResetHostKey(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}

	updated, err := s.servers.ResetHostKey(r.Context(), id)
	if err != nil {
		s.writeServerError(w, "reset host key", err)
		return
	}
	writeJSON(w, http.StatusOK, serverEnvelope{Server: newServerDTO(updated)})
}

// handleCreatePrivateKey stores an encrypted SSH private key.
func (s *Server) handleCreatePrivateKey(w http.ResponseWriter, r *http.Request) {
	var req createPrivateKeyRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	created, err := s.servers.AddPrivateKey(r.Context(), req.Name, req.PrivateKey)
	if err != nil {
		s.writeServerError(w, "create private key", err)
		return
	}

	s.logger.Debug("servers: private key created", "private_key_id", created.ID.String())
	writeJSON(w, http.StatusCreated, privateKeyResponse{
		ID:        created.ID.String(),
		Name:      created.Name,
		CreatedAt: created.CreatedAt,
	})
}

// handleServerMetrics returns the aggregated time series of one server as JSON:
// GET /v1/servers/{id}/metrics?from&to&step. from and to are RFC 3339
// timestamps and step is one of 1m, 1h, 1d (default 1m).
func (s *Server) handleServerMetrics(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}

	query := r.URL.Query()
	from, ok := parseMetricTime(w, query.Get("from"), "from")
	if !ok {
		return
	}
	to, ok := parseMetricTime(w, query.Get("to"), "to")
	if !ok {
		return
	}

	step := query.Get("step")
	if step == "" {
		step = servers.DefaultMetricStep
	}
	points, err := s.servers.Metrics(r.Context(), id, from, to, step)
	if err != nil {
		s.writeServerError(w, "metrics", err)
		return
	}

	response := metricsEnvelope{Step: step, Points: make([]metricPointDTO, 0, len(points))}
	for _, point := range points {
		response.Points = append(response.Points, metricPointDTO{
			Bucket:         point.Bucket,
			CPUUsage:       point.CPUUsage,
			MemUsage:       point.MemUsage,
			DiskUsage:      point.DiskUsage,
			NetRxBps:       point.NetRxBps,
			NetTxBps:       point.NetTxBps,
			DiskReadBps:    point.DiskReadBps,
			DiskWriteBps:   point.DiskWriteBps,
			ContainerCount: point.ContainerCount,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

// handleListAgentVersions returns the known agent version map, the active
// rollout target and the latest release the control plane can resolve.
func (s *Server) handleListAgentVersions(w http.ResponseWriter, r *http.Request) {
	controller, ok := s.servers.(agentUpdateController)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, apiError{Message: "agent updates are not configured"})
		return
	}
	response := agentVersionsEnvelope{
		Agents:         make([]agentVersionDTO, 0),
		RolloutVersion: controller.AgentRolloutVersion(),
	}
	for _, agent := range controller.KnownAgentVersions() {
		response.Agents = append(response.Agents, agentVersionDTO{
			NodeID:  agent.NodeID,
			Version: agent.Version,
			At:      agent.At,
		})
	}
	// The latest version is the newest *agent* release, not the control
	// plane's own version: the CP is normally updated first, so the CP view
	// would report the fleet as current when it is not.
	if target, err := controller.AgentUpdateTarget(r.Context()); err != nil {
		s.logger.Warn("servers: agent target lookup failed", "error", err)
	} else {
		response.LatestVersion = target
	}
	writeJSON(w, http.StatusOK, response)
}

// handleUpdateAllAgents triggers a fleet-wide agent update rollout. It resolves
// the newest *agent* release, records it as the rollout target and returns
// immediately: agents converge on their next RequestUpdate poll, so the request
// never blocks on (or dials) any node. The control plane's own version is
// irrelevant — operators update the CP first, and the fleet is then behind it.
func (s *Server) handleUpdateAllAgents(w http.ResponseWriter, r *http.Request) {
	controller, ok := s.servers.(agentUpdateController)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, apiError{Message: "agent updates are not configured"})
		return
	}

	target, err := controller.AgentUpdateTarget(r.Context())
	if err != nil {
		s.logger.Error("servers: agent update target lookup failed", "error", err)
		writeJSON(w, http.StatusBadGateway, apiError{Message: "release server error"})
		return
	}
	if target == "" {
		writeJSON(w, http.StatusServiceUnavailable, apiError{Message: "no agent release is available (agent updates may be disabled)"})
		return
	}

	agents := controller.KnownAgentVersions()
	pending := 0
	for _, agent := range agents {
		if !versionsEqual(agent.Version, target) {
			pending++
		}
	}
	if len(agents) > 0 && pending == 0 {
		writeJSON(w, http.StatusOK, updateAllAgentsResponse{
			TargetVersion: target,
			Agents:        len(agents),
			Message:       "all known agents are already on the target version",
		})
		return
	}

	controller.StartAgentRollout(target)
	s.logger.Info("servers: agent update rollout started",
		"target_version", target, "agents", len(agents), "pending", pending)
	writeJSON(w, http.StatusOK, updateAllAgentsResponse{
		TargetVersion: target,
		Agents:        len(agents),
		Pending:       pending,
		Message:       "rollout queued; agents update on their next poll",
	})
}

// versionsEqual compares two version strings through the version parser, so a
// bare "1.2.0" and a canonical "v1.2.0" count as the same version (N4). An
// unparsable value on either side falls back to a literal comparison.
func versionsEqual(a, b string) bool {
	av, aErr := updatecore.ParseVersion(a)
	bv, bErr := updatecore.ParseVersion(b)
	if aErr != nil || bErr != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return av.Compare(bv) == 0
}

// parseMetricTime parses one required RFC 3339 query timestamp, answering 400
// when it is missing or malformed.
func parseMetricTime(w http.ResponseWriter, raw, field string) (time.Time, bool) {
	if raw == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Message: field + " is required (RFC 3339)"})
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid " + field + " timestamp (want RFC 3339)"})
		return time.Time{}, false
	}
	return parsed, true
}

// writeServerError maps a servers domain error to its HTTP response.
func (s *Server) writeServerError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, servers.ErrNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	case errors.Is(err, teams.ErrForbidden):
		writeJSON(w, http.StatusForbidden, apiError{Message: "insufficient team role"})
	case errors.Is(err, servers.ErrValidation), errors.Is(err, servers.ErrNoCredentials):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	case errors.Is(err, servers.ErrConflict):
		writeJSON(w, http.StatusConflict, apiError{Message: err.Error()})
	default:
		s.logger.Error("servers: "+op, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

// serverIDParam parses the {id} path parameter, answering 400 on a bad UUID.
func serverIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid server id"})
		return uuid.Nil, false
	}
	return id, true
}

// parseOptionalUUID parses an optional UUID string, answering 400 when it is
// neither empty nor a valid UUID.
func parseOptionalUUID(w http.ResponseWriter, raw, field string) (uuid.UUID, bool) {
	if raw == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid " + field})
		return uuid.Nil, false
	}
	return id, true
}

// validateAuthFromBody decodes the optional body shared by the validation and
// Docker-install routes and resolves the credentials selection. It answers the
// request itself (400) and reports false on a malformed body.
func (s *Server) validateAuthFromBody(w http.ResponseWriter, r *http.Request) (servers.ValidateAuth, bool) {
	var req validateServerRequest
	if !s.decodeOptionalJSON(w, r, &req) {
		return servers.ValidateAuth{}, false
	}
	keyID, ok := parseOptionalUUID(w, req.SSHKeyID, "ssh_key_id")
	if !ok {
		return servers.ValidateAuth{}, false
	}
	return servers.ValidateAuth{
		KeyID:        keyID,
		Password:     req.Password,
		Passphrase:   req.Passphrase,
		TrustHostKey: req.TrustHostKey,
	}, true
}

// decodeOptionalJSON decodes an optional body, treating an empty body as an
// empty struct.
func (s *Server) decodeOptionalJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		return true
	}
	err := decodeJSONBody(w, r, dst)
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid request body"})
	return false
}

// newServerDTO maps a domain server to its wire representation.
func newServerDTO(server *servers.Server) serverDTO {
	dto := serverDTO{
		ID:                 server.ID.String(),
		Name:               server.Name,
		IP:                 server.IP,
		Port:               server.Port,
		SSHUser:            server.SSHUser,
		HasPassword:        server.HasPassword,
		Status:             server.Status,
		NodeID:             server.NodeID,
		OS:                 server.OS,
		DockerVersion:      server.DockerVersion,
		Arch:               server.Arch,
		TotalMem:           server.TotalMem,
		TotalDisk:          server.TotalDisk,
		CPUUsage:           server.CPUUsage,
		MemUsage:           server.MemUsage,
		DiskUsage:          server.DiskUsage,
		ContainerCount:     server.ContainerCount,
		LastSeen:           server.LastSeen,
		HostKeyFingerprint: server.HostKeyFingerprint,
		CreatedAt:          server.CreatedAt,
		UpdatedAt:          server.UpdatedAt,
	}
	if server.SSHKeyID != nil {
		keyID := server.SSHKeyID.String()
		dto.SSHKeyID = &keyID
	}
	return dto
}

// serverDTOPtr maps an optional domain server.
func serverDTOPtr(server *servers.Server) *serverDTO {
	if server == nil {
		return nil
	}
	dto := newServerDTO(server)
	return &dto
}
