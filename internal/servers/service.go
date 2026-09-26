package servers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Server lifecycle statuses.
const (
	StatusPending    = "pending"
	StatusValidating = "validating"
	StatusReady      = "ready"
	StatusOffline    = "offline"
	StatusError      = "error"
)

// defaultSSHPort is used when a create request omits the port.
const defaultSSHPort = 22

// Server is a managed node. It is the domain representation, decoupled from the
// sqlc row so the HTTP and gRPC layers never see storage types.
type Server struct {
	ID             uuid.UUID
	Name           string
	IP             string
	Port           int
	SSHUser        string
	SSHKeyID       *uuid.UUID
	Status         string
	NodeID         *string
	OS             *string
	DockerVersion  *string
	Arch           *string
	TotalMem       *int64
	TotalDisk      *int64
	CPUUsage       *float64
	MemUsage       *float64
	DiskUsage      *float64
	ContainerCount *int64
	LastSeen       *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// PrivateKey is the metadata of a stored SSH private key. The encrypted
// material is never exposed.
type PrivateKey struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

// ValidationResult reports the outcome of an SSH validation run.
type ValidationResult struct {
	Checks []CheckResult
	Server *Server
}

// ValidateAuth selects the credentials for a validation run. An explicit key or
// password overrides the key attached to the server.
type ValidateAuth struct {
	KeyID    uuid.UUID
	Password string
}

// Config wires a ServerService.
type Config struct {
	Store     *store.Store
	Authority *Authority
	Secret    string
	Version   string
	Logger    *slog.Logger
}

// ServerService is the node-management domain service: server registry, SSH
// validation, private-key storage, agent registration, and heartbeat recording.
type ServerService struct {
	store     *store.Store
	authority *Authority
	secret    string
	version   string
	logger    *slog.Logger
}

// NewService builds a ServerService. When secret is empty an ephemeral
// encryption secret is generated and a warning is logged: encrypted private
// keys then do not survive a restart.
func NewService(cfg Config) *ServerService {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	secret := cfg.Secret
	if secret == "" {
		secret = randomSecret()
		logger.Warn("GOTHAM_SECRET_KEY is empty; generated an ephemeral key — stored SSH private keys will not survive a restart")
	}

	return &ServerService{
		store:     cfg.Store,
		authority: cfg.Authority,
		secret:    secret,
		version:   cfg.Version,
		logger:    logger,
	}
}

// Add registers a new server. sshKeyID may be the zero UUID when no key is
// attached. userID is recorded for future ownership scoping; the schema does
// not yet carry an owner column.
func (s *ServerService) Add(ctx context.Context, userID uuid.UUID, name, ip string, port int, sshUser string, sshKeyID uuid.UUID) (*Server, error) {
	name = strings.TrimSpace(name)
	ip = strings.TrimSpace(ip)
	sshUser = strings.TrimSpace(sshUser)

	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if ip == "" {
		return nil, fmt.Errorf("%w: ip is required", ErrValidation)
	}
	if sshUser == "" {
		return nil, fmt.Errorf("%w: ssh_user is required", ErrValidation)
	}
	if port == 0 {
		port = defaultSSHPort
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("%w: port must be between 1 and 65535", ErrValidation)
	}

	keyID := pgtype.UUID{}
	if sshKeyID != uuid.Nil {
		if s.store == nil {
			return nil, errors.New("servers: store is not configured")
		}
		if _, err := s.store.GetPrivateKeyByID(ctx, pgtype.UUID{Bytes: sshKeyID, Valid: true}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("%w: unknown ssh_key_id", ErrValidation)
			}
			return nil, fmt.Errorf("lookup ssh key: %w", err)
		}
		keyID = pgtype.UUID{Bytes: sshKeyID, Valid: true}
	}

	row, err := s.store.CreateServer(ctx, sqlc.CreateServerParams{
		Name:     name,
		Ip:       ip,
		Port:     int32(port),
		SshUser:  sshUser,
		SshKeyID: keyID,
	})
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}

	s.logger.Debug("servers: added", "server_id", row.ID.String(), "user_id", userID.String())
	return serverFromRow(row), nil
}

// List returns every managed server, newest first.
func (s *ServerService) List(ctx context.Context) ([]Server, error) {
	rows, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}

	servers := make([]Server, 0, len(rows))
	for _, row := range rows {
		servers = append(servers, *serverFromRow(row))
	}
	return servers, nil
}

// Get returns one server, or ErrNotFound.
func (s *ServerService) Get(ctx context.Context, id uuid.UUID) (*Server, error) {
	row, err := s.store.GetServerByID(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get server: %w", err)
	}
	return serverFromRow(row), nil
}

// Delete removes a server, or returns ErrNotFound.
func (s *ServerService) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.store.GetServerByID(ctx, pgUUID(id)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get server: %w", err)
	}
	if err := s.store.DeleteServer(ctx, pgUUID(id)); err != nil {
		return fmt.Errorf("delete server: %w", err)
	}
	return nil
}

// AddPrivateKey encrypts and stores an SSH private key.
func (s *ServerService) AddPrivateKey(ctx context.Context, name, privateKeyPEM string) (*PrivateKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if strings.TrimSpace(privateKeyPEM) == "" {
		return nil, fmt.Errorf("%w: private_key is required", ErrValidation)
	}

	encrypted, err := EncryptKey(privateKeyPEM, s.secret)
	if err != nil {
		return nil, err
	}

	row, err := s.store.CreatePrivateKey(ctx, sqlc.CreatePrivateKeyParams{
		Name:         name,
		EncryptedKey: encrypted,
	})
	if err != nil {
		return nil, fmt.Errorf("create private key: %w", err)
	}

	s.logger.Debug("servers: private key added", "private_key_id", row.ID.String())
	return &PrivateKey{
		ID:        uuidFromPG(row.ID),
		Name:      row.Name,
		CreatedAt: row.CreatedAt.Time,
	}, nil
}

// Validate runs the SSH probes against a server and, on success, records the
// gathered agent info and marks the server ready. auth selects the credentials;
// when it is empty the server's stored key is used.
//
// It returns the per-check results even when validation fails, together with an
// error wrapping ErrValidation (or ErrNotFound / ErrNoCredentials).
func (s *ServerService) Validate(ctx context.Context, id uuid.UUID, auth ValidateAuth) (*ValidationResult, error) {
	row, err := s.store.GetServerByID(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get server: %w", err)
	}
	server := serverFromRow(row)

	credentials, err := s.credentials(ctx, row, auth)
	if err != nil {
		return nil, err
	}

	s.setStatus(ctx, id, StatusValidating)

	checks, info, err := ValidateNode(ctx, server.IP, server.Port, server.SSHUser, credentials)
	result := &ValidationResult{Checks: checks, Server: server}

	if err != nil {
		if len(checks) == 0 {
			result.Checks = failedChecks(err.Error())
		}
		s.setStatus(ctx, id, StatusError)
		result.Server.Status = StatusError
		return result, fmt.Errorf("%w: %v", ErrValidation, err)
	}

	for _, check := range checks {
		if !check.OK {
			s.setStatus(ctx, id, StatusError)
			result.Server.Status = StatusError
			return result, fmt.Errorf("%w: %s check failed: %s", ErrValidation, check.Name, check.Detail)
		}
	}

	updated, err := s.store.UpdateServerAgentInfo(ctx, sqlc.UpdateServerAgentInfoParams{
		ID:            pgUUID(id),
		NodeID:        row.NodeID,
		Os:            strPtr(info.OS),
		DockerVersion: strPtr(info.DockerVersion),
		Arch:          strPtr(info.Arch),
		TotalMem:      ptrInt64(info.TotalMem),
		TotalDisk:     ptrInt64(info.TotalDisk),
	})
	if err != nil {
		return result, fmt.Errorf("record agent info: %w", err)
	}

	s.logger.Info("servers: validation succeeded", "server_id", id.String(), "docker_version", info.DockerVersion)
	return &ValidationResult{Checks: checks, Server: serverFromRow(updated)}, nil
}

// RegisterNode handles an agent Register call: it finds or creates the server
// for the node, records the reported capabilities, and issues an agent
// certificate from the CA (when one is configured).
func (s *ServerService) RegisterNode(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	if req == nil || strings.TrimSpace(req.GetNodeId()) == "" {
		return nil, fmt.Errorf("%w: node_id is required", ErrValidation)
	}
	nodeID := strings.TrimSpace(req.GetNodeId())

	row, err := s.store.GetServerByNodeID(ctx, &nodeID)
	switch {
	case err == nil:
		// Existing registration: refresh capabilities below.
	case errors.Is(err, pgx.ErrNoRows):
		row, err = s.createRegisteredServer(ctx, nodeID)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("lookup server by node id: %w", err)
	}

	updated, err := s.store.UpdateServerAgentInfo(ctx, sqlc.UpdateServerAgentInfoParams{
		ID:            row.ID,
		NodeID:        &nodeID,
		Os:            strPtr(req.GetOs()),
		DockerVersion: strPtr(req.GetDockerVersion()),
		Arch:          strPtr(req.GetArch()),
		TotalMem:      ptrInt64(req.GetTotalMem()),
		TotalDisk:     ptrInt64(req.GetTotalDisk()),
	})
	if err != nil {
		return nil, fmt.Errorf("record agent info: %w", err)
	}

	var cert []byte
	if s.authority != nil {
		cert, err = s.authority.IssueAgentCert(nodeID)
		if err != nil {
			return nil, fmt.Errorf("issue agent certificate: %w", err)
		}
	} else {
		s.logger.Warn("servers: no CA configured; agent registration returned no certificate", "node_id", nodeID)
	}

	s.logger.Info("servers: node registered", "node_id", nodeID, "server_id", updated.ID.String())
	return &agentv1.RegisterResponse{Cert: cert, CpVersion: s.version}, nil
}

// RecordHeartbeat records one heartbeat message from the node identified by
// nodeID. Redis publish lands with realtime in Phase 3; for now the event is
// logged.
func (s *ServerService) RecordHeartbeat(ctx context.Context, nodeID string, req *agentv1.HeartbeatRequest) error {
	if nodeID == "" {
		return fmt.Errorf("%w: node id is required", ErrValidation)
	}

	row, err := s.store.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lookup server by node id: %w", err)
	}

	cpu, mem, disk := req.GetCpuUsage(), req.GetMemUsage(), req.GetDiskUsage()
	count := req.GetContainerCount()

	updated, err := s.store.UpdateServerMetrics(ctx, sqlc.UpdateServerMetricsParams{
		ID:             row.ID,
		CpuUsage:       &cpu,
		MemUsage:       &mem,
		DiskUsage:      &disk,
		ContainerCount: &count,
	})
	if err != nil {
		return fmt.Errorf("record heartbeat: %w", err)
	}

	s.logger.Debug("servers: heartbeat",
		"node_id", nodeID,
		"server_id", updated.ID.String(),
		"cpu_usage", cpu,
		"mem_usage", mem,
		"container_count", count,
	)
	return nil
}

// createRegisteredServer creates the registry row for a node that registered
// before it was added by an operator.
func (s *ServerService) createRegisteredServer(ctx context.Context, nodeID string) (sqlc.Server, error) {
	ip := ""
	if net.ParseIP(nodeID) != nil {
		ip = nodeID
	}
	row, err := s.store.CreateServer(ctx, sqlc.CreateServerParams{
		Name:     nodeID,
		Ip:       ip,
		Port:     defaultSSHPort,
		SshUser:  "",
		SshKeyID: pgtype.UUID{},
	})
	if err != nil {
		return sqlc.Server{}, fmt.Errorf("create server for node %s: %w", nodeID, err)
	}
	s.logger.Info("servers: created server for unregistered node", "node_id", nodeID, "server_id", row.ID.String())
	return row, nil
}

// credentials resolves the SSH credentials for a validation run.
func (s *ServerService) credentials(ctx context.Context, row sqlc.Server, auth ValidateAuth) (SSHAuth, error) {
	switch {
	case auth.Password != "":
		return SSHAuth{Password: auth.Password}, nil
	case auth.KeyID != uuid.Nil:
		return s.loadKeyAuth(ctx, pgUUID(auth.KeyID), true)
	default:
		if !row.SshKeyID.Valid {
			return SSHAuth{}, fmt.Errorf("%w: server has no SSH key", ErrNoCredentials)
		}
		return s.loadKeyAuth(ctx, row.SshKeyID, false)
	}
}

// loadKeyAuth loads and decrypts a private key into an SSHAuth. explicit
// distinguishes an operator-supplied key ID from the server's attached key so
// the error text matches the cause.
func (s *ServerService) loadKeyAuth(ctx context.Context, id pgtype.UUID, explicit bool) (SSHAuth, error) {
	key, err := s.store.GetPrivateKeyByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if explicit {
				return SSHAuth{}, fmt.Errorf("%w: unknown ssh_key_id", ErrValidation)
			}
			return SSHAuth{}, fmt.Errorf("%w: attached SSH key no longer exists", ErrNoCredentials)
		}
		return SSHAuth{}, fmt.Errorf("load ssh key: %w", err)
	}

	plain, err := DecryptKey(key.EncryptedKey, s.secret)
	if err != nil {
		return SSHAuth{}, fmt.Errorf("decrypt ssh key: %w", err)
	}
	return SSHAuth{PrivateKeyPEM: []byte(plain)}, nil
}

// setStatus updates a server's status, logging but not failing on error.
func (s *ServerService) setStatus(ctx context.Context, id uuid.UUID, status string) {
	if _, err := s.store.SetServerStatus(ctx, sqlc.SetServerStatusParams{ID: pgUUID(id), Status: status}); err != nil {
		s.logger.Warn("servers: set status", "server_id", id.String(), "status", status, "error", err)
	}
}

// serverFromRow maps a sqlc row to the domain type.
func serverFromRow(row sqlc.Server) *Server {
	server := &Server{
		ID:             uuidFromPG(row.ID),
		Name:           row.Name,
		IP:             row.Ip,
		Port:           int(row.Port),
		SSHUser:        row.SshUser,
		Status:         row.Status,
		NodeID:         row.NodeID,
		OS:             row.Os,
		DockerVersion:  row.DockerVersion,
		Arch:           row.Arch,
		TotalMem:       row.TotalMem,
		TotalDisk:      row.TotalDisk,
		CPUUsage:       row.CpuUsage,
		MemUsage:       row.MemUsage,
		DiskUsage:      row.DiskUsage,
		ContainerCount: row.ContainerCount,
		LastSeen:       timePtr(row.LastSeen),
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
	if row.SshKeyID.Valid {
		keyID := uuidFromPG(row.SshKeyID)
		server.SSHKeyID = &keyID
	}
	return server
}

// failedChecks synthesises the full probe list for a connection-level failure.
func failedChecks(detail string) []CheckResult {
	return []CheckResult{
		{Name: checkDocker, OK: false, Detail: detail},
		{Name: checkCPU, OK: false, Detail: detail},
		{Name: checkRAM, OK: false, Detail: detail},
		{Name: checkDisk, OK: false, Detail: detail},
	}
}

// pgUUID converts a uuid.UUID to the pgx type (valid unless nil).
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// uuidFromPG converts a pgx UUID to uuid.UUID.
func uuidFromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

// timePtr converts a nullable timestamp to a pointer.
func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

// strPtr returns a pointer to s, or nil when s is empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ptrInt64 returns a pointer to a copy of v.
func ptrInt64(v int64) *int64 {
	return &v
}

// randomSecret returns a base64-encoded 32-byte random secret.
func randomSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand.Read only fails on a broken system RNG; there is no
		// safe fallback for key material.
		panic(fmt.Sprintf("servers: generate secret: %v", err))
	}
	return base64.StdEncoding.EncodeToString(buf)
}
