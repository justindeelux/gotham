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
	"github.com/justindeelux/gotham/updatecore"
)

type ServerService interface {
	Add(ctx context.Context, userID uuid.UUID, name, ip string, port int, sshUser string, sshKeyID uuid.UUID, password string) (*servers.Server, error)
	List(ctx context.Context) ([]servers.Server, error)
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
	Update(ctx context.Context, id uuid.UUID, params servers.UpdateParams) (*servers.Server, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Validate(ctx context.Context, id uuid.UUID, auth servers.ValidateAuth) (*servers.ValidationResult, error)
	InstallKey(ctx context.Context, id uuid.UUID, password string) (*servers.ValidationResult, error)
	InstallDocker(ctx context.Context, id uuid.UUID, auth servers.ValidateAuth, out io.Writer) error
	ResetHostKey(ctx context.Context, id uuid.UUID) (*servers.Server, error)
	AddPrivateKey(ctx context.Context, name, privateKeyPEM, description, publicKey string) (*servers.PrivateKey, error)
	GeneratePrivateKey(ctx context.Context, name, keyType, description string, rsaBits int) (*servers.PrivateKey, error)
	ListPrivateKeys(ctx context.Context) ([]servers.PrivateKey, error)
	GetPrivateKey(ctx context.Context, id uuid.UUID) (*servers.PrivateKey, error)
	UpdatePrivateKey(ctx context.Context, id uuid.UUID, params servers.PrivateKeyUpdateParams) (*servers.PrivateKey, error)
	DeletePrivateKey(ctx context.Context, id uuid.UUID) error
	RevealPrivateKey(ctx context.Context, id uuid.UUID) (string, error)
	Metrics(ctx context.Context, id uuid.UUID, from, to time.Time, step string) ([]servers.MetricPoint, error)
}

type createServerRequest struct {
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	SSHUser  string `json:"ssh_user"`
	SSHKeyID string `json:"ssh_key_id"`
	Password string `json:"password"`
}

type updateServerRequest struct {
	Name     *string `json:"name"`
	IP       *string `json:"ip"`
	Port     *int    `json:"port"`
	SSHUser  *string `json:"ssh_user"`
	SSHKeyID *string `json:"ssh_key_id"`
	Password *string `json:"password"`
}

type validateServerRequest struct {
	SSHKeyID     string `json:"ssh_key_id"`
	Password     string `json:"password"`
	Passphrase   string `json:"passphrase"`
	TrustHostKey bool   `json:"trust_host_key"`
}

type installKeyRequest struct {
	Password string `json:"password"`
}

type createPrivateKeyRequest struct {
	Name        string `json:"name"`
	PrivateKey  string `json:"private_key"`
	Description string `json:"description"`
	PublicKey   string `json:"public_key"`
}

type generatePrivateKeyRequest struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Bits        int    `json:"bits"`
}

type updatePrivateKeyRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type serverDTO struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	IP             string     `json:"ip"`
	Port           int        `json:"port"`
	SSHUser        string     `json:"ssh_user"`
	SSHKeyID       *string    `json:"ssh_key_id,omitempty"`
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

	AgentVersion           *string    `json:"agent_version"`
	AgentVersionReportedAt *time.Time `json:"agent_version_reported_at"`

	HostKeyFingerprint *string   `json:"host_key_fingerprint,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type serverEnvelope struct {
	Server serverDTO `json:"server"`
}

type serverListEnvelope struct {
	Servers []serverDTO `json:"servers"`
}

type validateResponse struct {
	Message    string                `json:"message,omitempty"`
	Checks     []servers.CheckResult `json:"checks"`
	Server     *serverDTO            `json:"server,omitempty"`
	AuthFailed bool                  `json:"auth_failed,omitempty"`
}

type privateKeyDTO struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Description *string   `json:"description"`
	PublicKey   string    `json:"public_key"`
	CreatedAt   time.Time `json:"created_at"`
}

type privateKeyListEnvelope struct {
	PrivateKeys []privateKeyDTO `json:"private_keys"`
}

type privateKeyRevealResponse struct {
	PrivateKey string `json:"private_key"`
}

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

type metricsEnvelope struct {
	Step   string           `json:"step"`
	Points []metricPointDTO `json:"points"`
}

type agentVersionDTO struct {
	NodeID  string    `json:"node_id"`
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

type agentVersionsEnvelope struct {
	Agents         []agentVersionDTO `json:"agents"`
	RolloutVersion string            `json:"rollout_version,omitempty"`
	LatestVersion  string            `json:"latest_version,omitempty"`
}

type updateAllAgentsResponse struct {
	TargetVersion string `json:"target_version,omitempty"`
	Agents        int    `json:"agents"`
	Pending       int    `json:"pending"`
	Message       string `json:"message,omitempty"`
}

type agentUpdateController interface {
	KnownAgentVersions() []servers.AgentVersion
	AgentRolloutVersion() string
	StartAgentRollout(version string)
	AgentUpdateTarget(ctx context.Context) (string, error)
}

func (s *Server) mountServerRoutes(api chi.Router) {
	api.Group(func(protected chi.Router) {
		protected.Use(s.RequireAuth, requireResourceScopes, s.requireOwnerAccount)
		protected.Get("/v1/servers", s.handleListServers)
		protected.Post("/v1/servers", s.handleCreateServer)
		protected.Get("/v1/servers/{id}", s.handleGetServer)
		protected.Patch("/v1/servers/{id}", s.handleUpdateServer)
		protected.Delete("/v1/servers/{id}", s.handleDeleteServer)
		protected.With(s.sshRateLimit).Post("/v1/servers/{id}/validate", s.handleValidateServer)
		protected.With(s.sshRateLimit).Post("/v1/servers/{id}/install-key", s.handleInstallKey)
		protected.Post("/v1/servers/{id}/docker-install", s.handleInstallDocker)
		protected.Delete("/v1/servers/{id}/host-key", s.handleResetHostKey)
		protected.Post("/v1/private-keys", s.handleCreatePrivateKey)
		protected.Post("/v1/private-keys/generate", s.handleGeneratePrivateKey)
		protected.Get("/v1/private-keys", s.handleListPrivateKeys)
		protected.Get("/v1/private-keys/{id}", s.handleGetPrivateKey)
		protected.Patch("/v1/private-keys/{id}", s.handleUpdatePrivateKey)
		protected.Delete("/v1/private-keys/{id}", s.handleDeletePrivateKey)
		protected.Post("/v1/private-keys/{id}/reveal", s.handleRevealPrivateKey)
		if servers.MetricsEnabled() {
			protected.Get("/v1/servers/{id}/metrics", s.handleServerMetrics)
		}
	})

	if _, ok := s.servers.(agentUpdateController); ok {
		api.Group(func(platform chi.Router) {
			platform.Use(s.RequireAuth, s.RequireOwner)
			platform.Get("/v1/servers/agents", s.handleListAgentVersions)
			platform.Post("/v1/servers/agents/update-all", s.handleUpdateAllAgents)
		})
	}
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	items, err := s.servers.List(r.Context())
	if err != nil {
		s.writeServerError(w, "list", err)
		return
	}

	response := serverListEnvelope{Servers: make([]serverDTO, 0, len(items))}
	for i := range items {
		response.Servers = append(response.Servers, s.observedServerDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, response)
}

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
	writeJSON(w, http.StatusOK, serverEnvelope{Server: s.observedServerDTO(found)})
}

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
		if result != nil && errors.Is(err, servers.ErrValidation) {
			writeJSON(w, http.StatusUnprocessableEntity, validateResponse{
				Message:    err.Error(),
				Checks:     result.Checks,
				Server:     serverDTOPtr(result.Server),
				AuthFailed: result.AuthFailed,
			})
			return
		}
		s.writeServerError(w, "validate", err)
		return
	}

	if s.proxy != nil {
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

func (s *Server) handleInstallKey(w http.ResponseWriter, r *http.Request) {
	id, ok := serverIDParam(w, r)
	if !ok {
		return
	}

	var req installKeyRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	result, err := s.servers.InstallKey(r.Context(), id, req.Password)
	if err != nil {
		if errors.Is(err, servers.ErrValidation) {
			response := validateResponse{Message: err.Error(), Checks: []servers.CheckResult{}}
			if result != nil {
				response.Checks = result.Checks
				response.Server = serverDTOPtr(result.Server)
				response.AuthFailed = result.AuthFailed
			}
			writeJSON(w, http.StatusUnprocessableEntity, response)
			return
		}
		s.writeServerError(w, "install key", err)
		return
	}

	if s.proxy != nil {
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

const (
	installMarkerPrefix = "\x1e[gotham-install] "
	installNonceHeader  = "X-Install-Nonce"
	installFramingByte  = 0x1e
)

type installMarker struct {
	Status   string `json:"status"`
	Nonce    string `json:"nonce"`
	ExitCode int    `json:"exit_code,omitempty"`
	Message  string `json:"message,omitempty"`
}

func newInstallNonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}

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

type streamWriter struct {
	w        http.ResponseWriter
	flusher  http.Flusher
	nonce    string
	started  bool
	lastByte byte
}

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

func flusherOf(w http.ResponseWriter) http.Flusher {
	flusher, _ := w.(http.Flusher)
	return flusher
}

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

func (s *Server) handleCreatePrivateKey(w http.ResponseWriter, r *http.Request) {
	var req createPrivateKeyRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	created, err := s.servers.AddPrivateKey(r.Context(), req.Name, req.PrivateKey, req.Description, req.PublicKey)
	if err != nil {
		s.writeServerError(w, "create private key", err)
		return
	}

	s.logger.Debug("servers: private key created", "private_key_id", created.ID.String())
	writeJSON(w, http.StatusCreated, newPrivateKeyDTO(created))
}

func (s *Server) handleGeneratePrivateKey(w http.ResponseWriter, r *http.Request) {
	var req generatePrivateKeyRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	created, err := s.servers.GeneratePrivateKey(r.Context(), req.Name, req.Type, req.Description, req.Bits)
	if err != nil {
		s.writeServerError(w, "generate private key", err)
		return
	}

	s.logger.Debug("servers: private key generated", "private_key_id", created.ID.String(), "type", created.Type)
	writeJSON(w, http.StatusCreated, newPrivateKeyDTO(created))
}

func (s *Server) handleListPrivateKeys(w http.ResponseWriter, r *http.Request) {
	items, err := s.servers.ListPrivateKeys(r.Context())
	if err != nil {
		s.writeServerError(w, "list private keys", err)
		return
	}

	response := privateKeyListEnvelope{PrivateKeys: make([]privateKeyDTO, 0, len(items))}
	for i := range items {
		response.PrivateKeys = append(response.PrivateKeys, newPrivateKeyDTO(&items[i]))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetPrivateKey(w http.ResponseWriter, r *http.Request) {
	id, ok := privateKeyIDParam(w, r)
	if !ok {
		return
	}

	key, err := s.servers.GetPrivateKey(r.Context(), id)
	if err != nil {
		s.writeServerError(w, "get private key", err)
		return
	}
	writeJSON(w, http.StatusOK, newPrivateKeyDTO(key))
}

func (s *Server) handleUpdatePrivateKey(w http.ResponseWriter, r *http.Request) {
	id, ok := privateKeyIDParam(w, r)
	if !ok {
		return
	}

	var req updatePrivateKeyRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	updated, err := s.servers.UpdatePrivateKey(r.Context(), id, servers.PrivateKeyUpdateParams{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		s.writeServerError(w, "update private key", err)
		return
	}
	writeJSON(w, http.StatusOK, newPrivateKeyDTO(updated))
}

func (s *Server) handleDeletePrivateKey(w http.ResponseWriter, r *http.Request) {
	id, ok := privateKeyIDParam(w, r)
	if !ok {
		return
	}

	if err := s.servers.DeletePrivateKey(r.Context(), id); err != nil {
		s.writeServerError(w, "delete private key", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRevealPrivateKey(w http.ResponseWriter, r *http.Request) {
	id, ok := privateKeyIDParam(w, r)
	if !ok {
		return
	}

	privateKey, err := s.servers.RevealPrivateKey(r.Context(), id)
	if err != nil {
		s.writeServerError(w, "reveal private key", err)
		return
	}

	actorID, _ := UserIDFromContext(r.Context())
	s.logger.Info("servers: audit",
		"actor_id", actorID.String(),
		"action", "private_key.reveal",
		"target_id", id.String())
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, privateKeyRevealResponse{PrivateKey: privateKey})
}

func newPrivateKeyDTO(key *servers.PrivateKey) privateKeyDTO {
	return privateKeyDTO{
		ID:          key.ID.String(),
		Name:        key.Name,
		Type:        key.Type,
		Description: key.Description,
		PublicKey:   key.PublicKey,
		CreatedAt:   key.CreatedAt,
	}
}

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
	if target, err := controller.AgentUpdateTarget(r.Context()); err != nil {
		s.logger.Warn("servers: agent target lookup failed", "error", err)
	} else {
		response.LatestVersion = target
	}
	writeJSON(w, http.StatusOK, response)
}

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

func versionsEqual(a, b string) bool {
	av, aErr := updatecore.ParseVersion(a)
	bv, bErr := updatecore.ParseVersion(b)
	if aErr != nil || bErr != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return av.Compare(bv) == 0
}

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

func (s *Server) writeServerError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, servers.ErrNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	case errors.Is(err, servers.ErrValidation), errors.Is(err, servers.ErrNoCredentials):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	case errors.Is(err, servers.ErrConflict):
		writeJSON(w, http.StatusConflict, apiError{Message: err.Error()})
	default:
		s.logger.Error("servers: "+op, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

func serverIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid server id"})
		return uuid.Nil, false
	}
	return id, true
}

func privateKeyIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid private key id"})
		return uuid.Nil, false
	}
	return id, true
}

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

func serverDTOPtr(server *servers.Server) *serverDTO {
	if server == nil {
		return nil
	}
	dto := newServerDTO(server)
	return &dto
}
