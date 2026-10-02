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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
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

// heartbeatOfflineAfter bounds how long a ready node may go without an agent
// heartbeat before it is reported offline. The agent heartbeats every 10s, so
// three missed cycles mark a node unreachable without waiting on the gateway's
// 2-minute idle-stream timeout.
const heartbeatOfflineAfter = 30 * time.Second

// Server is a managed node. It is the domain representation, decoupled from the
// sqlc row so the HTTP and gRPC layers never see storage types.
type Server struct {
	ID       uuid.UUID
	Name     string
	IP       string
	Port     int
	SSHUser  string
	SSHKeyID *uuid.UUID
	// TeamID is the owning team. The zero UUID marks a legacy node that
	// predates teams: it stays visible to every authenticated caller, which
	// is the documented Phase 8 residual for the shared node registry.
	TeamID         uuid.UUID
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
	// HostKeyFingerprint is the TOFU-pinned SSH host key of the node, in
	// OpenSSH SHA256 form. nil until the node has been validated once.
	HostKeyFingerprint *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
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
	// Passphrase decrypts a passphrase-protected private key for this run only.
	// It is never persisted; a key stored without its passphrase is unusable
	// until the caller supplies one at validation time.
	Passphrase string
	// TrustHostKey is the operator's explicit consent to trust an unpinned host
	// key on this run. It is required for password auth to a node that has
	// never been validated (there is no key to TOFU-pin against) and is
	// ignored once the node is pinned.
	TrustHostKey bool
}

// Config wires a ServerService.
type Config struct {
	Store     *store.Store
	Authority *Authority
	Secret    string
	Version   string
	Logger    *slog.Logger
	// Updater resolves verified agent update offers (BE-9.2). nil disables the
	// agent update surface on the gRPC gateway.
	Updater AgentUpdateOfferer
}

// ServerService is the node-management domain service: server registry, SSH
// validation, private-key storage, agent registration, and heartbeat recording.
type ServerService struct {
	store     *store.Store
	authority *Authority
	secret    string
	version   string
	logger    *slog.Logger
	updater   AgentUpdateOfferer
	// agentUpdate is the agent version map and rollout marker (BE-9.2).
	agentUpdate agentUpdateState

	// metricMu guards lastMetricAt, the per-node wall-clock timestamp of the
	// last persisted time-series sample. It bounds an unauthenticated heartbeat
	// flood's growth of server_metrics without touching the live snapshot.
	metricMu     sync.Mutex
	lastMetricAt map[string]time.Time
	// now is the clock, overridable in tests.
	now func() time.Time
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
	if strings.TrimSpace(secret) == "" {
		secret = randomSecret()
		logger.Warn("GOTHAM_SECRET_KEY is empty; generated an ephemeral key — stored SSH private keys will not survive a restart")
	}

	return &ServerService{
		store:     cfg.Store,
		authority: cfg.Authority,
		secret:    secret,
		version:   cfg.Version,
		logger:    logger,
		updater:   cfg.Updater,
		agentUpdate: agentUpdateState{
			agents: map[string]AgentVersion{},
		},
		lastMetricAt: map[string]time.Time{},
		now:          time.Now,
	}
}

// Version returns the control-plane build version this service reports to
// agents; the self-update surface uses it as the running version.
func (s *ServerService) Version() string { return s.version }

// Add registers a new server. sshKeyID may be the zero UUID when no key is
// attached. userID is recorded as the creator, and the node is stamped with the
// caller's active team; a request without a team scope leaves team_id NULL,
// which is the legacy shared-node behavior.
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
		TeamID:   pgUUID(teams.ScopeFor(ctx, userID).TeamID),
	})
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}

	s.logger.Debug("servers: added", "server_id", row.ID.String(), "user_id", userID.String())
	return serverFromRow(row), nil
}

// List returns the managed servers of the caller's active team plus every
// legacy node (team_id NULL). Without a team scope it returns every server,
// which is the pre-teams behavior.
func (s *ServerService) List(ctx context.Context) ([]Server, error) {
	s.sweepOffline(ctx)
	scope := teams.ScopeFor(ctx, uuid.Nil)
	var (
		rows []sqlc.Server
		err  error
	)
	if scope.Active() {
		rows, err = s.store.ListServersByTeam(ctx, pgUUID(scope.TeamID))
	} else {
		rows, err = s.store.ListServers(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}

	servers := make([]Server, 0, len(rows))
	for _, row := range rows {
		servers = append(servers, *serverFromRow(row))
	}
	return servers, nil
}

// Get returns one server, or ErrNotFound. A node outside the active team
// answers ErrNotFound so node IDs cannot be probed; a legacy node (team_id
// NULL) stays readable by every authenticated caller.
func (s *ServerService) Get(ctx context.Context, id uuid.UUID) (*Server, error) {
	s.sweepOffline(ctx)
	row, err := s.store.GetServerByID(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get server: %w", err)
	}
	server := serverFromRow(row)
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(server.TeamID, false); err != nil {
		return nil, ErrNotFound
	}
	return server, nil
}

// Delete removes a server, or returns ErrNotFound. A node outside the active
// team is not deletable; a legacy node stays deletable by any authenticated
// caller, matching pre-teams behavior.
func (s *ServerService) Delete(ctx context.Context, id uuid.UUID) error {
	row, err := s.store.GetServerByID(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get server: %w", err)
	}
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(uuidFromPG(row.TeamID), true); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return err
		}
		return ErrNotFound
	}
	if err := s.store.DeleteServer(ctx, pgUUID(id)); err != nil {
		return fmt.Errorf("delete server: %w", err)
	}
	return nil
}

// ResetHostKey forgets a node's pinned SSH host key, so the next validation
// re-pins it. It is the operator escape hatch after a legitimate host key
// rotation; a team member cannot reset a node outside their team.
func (s *ServerService) ResetHostKey(ctx context.Context, id uuid.UUID) (*Server, error) {
	row, err := s.store.GetServerByID(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get server: %w", err)
	}
	server := serverFromRow(row)
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(server.TeamID, true); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return nil, err
		}
		return nil, ErrNotFound
	}

	updated, err := s.store.ClearServerHostKey(ctx, pgUUID(id))
	if err != nil {
		return nil, fmt.Errorf("clear host key: %w", err)
	}
	scope := teams.ScopeFor(ctx, uuid.Nil)
	s.logger.Info("servers: host key pin reset",
		"server_id", id.String(),
		"actor_team_id", scopeID(scope.TeamID),
		"actor_user_id", scopeID(scope.UserID),
	)
	return serverFromRow(updated), nil
}

// scopeID renders a scope UUID for audit logs, leaving it empty when the caller
// had no team/user scope, so logs never show the zero UUID as an actor.
func scopeID(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// pinHostKey persists a first-use host key fingerprint with compare-and-set
// semantics. When the CAS affects 0 rows the node was pinned (or unpinned)
// concurrently: it re-reads and either accepts a stored match, fails closed on
// a mismatch, or retries once when the row was cleared in the window. A pin
// that never lands is an error, so a node is never reported ready un-pinned.
func (s *ServerService) pinHostKey(ctx context.Context, id uuid.UUID, fingerprint string) error {
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := s.store.PinServerHostKey(ctx, sqlc.PinServerHostKeyParams{
			ID:                 pgUUID(id),
			HostKeyFingerprint: &fingerprint,
		}); err == nil {
			// The fingerprint is public; log it so operators can audit the
			// first pin. A routine pin is Info; only resets/mismatches Warn.
			s.logger.Info("servers: host key pinned on first use",
				"server_id", id.String(),
				"host_key_fingerprint", fingerprint,
			)
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("persist host key fingerprint: %w", err)
		}

		// Lost the CAS: another writer touched the row first.
		row, readErr := s.store.GetServerByID(ctx, pgUUID(id))
		if readErr != nil {
			return fmt.Errorf("persist host key fingerprint: %w", readErr)
		}
		switch stored := fingerprintOf(row.HostKeyFingerprint); {
		case stored == fingerprint:
			return nil // someone else pinned the same key: benign
		case stored != "":
			return fmt.Errorf("host key changed while pinning: got %s, want %s", fingerprint, stored)
		default:
			// The row was cleared in the window (an operator reset landed):
			// loop and re-run the CAS so the pin is not silently skipped.
		}
	}
	return fmt.Errorf("persist host key fingerprint: node %s was cleared repeatedly while pinning", id)
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
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(server.TeamID, true); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return nil, err
		}
		return nil, ErrNotFound
	}

	credentials, err := s.credentials(ctx, row, auth)
	if err != nil {
		return nil, err
	}

	// A pinned node is always verified against its pin. An unpinned node is
	// trusted on first use for public-key auth, or when the operator explicitly
	// asked for it. TOFU is inherent: an on-path attacker present at the very
	// first validation can win the pin (the deploy key proves the caller holds
	// a credential, not that the peer is the intended host). Password auth is
	// refused unless the operator opts in, because a first-use MITM there also
	// captures the node password.
	policy := HostKeyPolicy{Pinned: fingerprintOf(row.HostKeyFingerprint)}
	if policy.Pinned == "" {
		policy.AcceptUnpinned = len(credentials.PrivateKeyPEM) > 0 || auth.TrustHostKey
	}

	s.setStatus(ctx, id, StatusValidating)

	checks, info, fingerprint, err := ValidateNode(ctx, server.IP, server.Port, server.SSHUser, credentials, policy)
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

	// The handshake succeeded against a host that is now trusted: persist the
	// TOFU pin so every later validation fails closed on a different key. A
	// first pin only (fingerprint != policy.Pinned) and only when the row is
	// still unpinned: a racing validation that pinned a different key must not
	// be overwritten. A failed or losing pin write fails the validation — the
	// node never reports ready without a durable pin.
	if fingerprint != "" && fingerprint != policy.Pinned {
		if err := s.pinHostKey(ctx, id, fingerprint); err != nil {
			s.setStatus(ctx, id, StatusError)
			result.Server.Status = StatusError
			return result, fmt.Errorf("%w: %v", ErrValidation, err)
		}
	}

	if _, err := s.store.UpdateServerAgentInfo(ctx, sqlc.UpdateServerAgentInfoParams{
		ID:            pgUUID(id),
		NodeID:        row.NodeID,
		Os:            strPtr(info.OS),
		DockerVersion: strPtr(info.DockerVersion),
		Arch:          strPtr(info.Arch),
		TotalMem:      ptrInt64(info.TotalMem),
		TotalDisk:     ptrInt64(info.TotalDisk),
	}); err != nil {
		return result, fmt.Errorf("record agent info: %w", err)
	}

	// SSH reachability is not agent readiness: restore the node's status from
	// its last heartbeat at the moment of the write (A4-15/B4-9). A previously
	// ready node with a live agent stays ready; a node that heartbeated during
	// the validation stays ready; an agentless node is pending; a node whose
	// agent stopped is offline. Deriving it in one statement prevents a
	// concurrent heartbeat from being clobbered (fix round 1 U2).
	updated, err := s.store.SetServerStatusAfterValidation(ctx, sqlc.SetServerStatusAfterValidationParams{
		ID:       pgUUID(id),
		LastSeen: pgtype.Timestamptz{Time: s.now().Add(-heartbeatOfflineAfter), Valid: true},
	})
	if err != nil {
		return result, fmt.Errorf("record server status: %w", err)
	}

	s.logger.Info("servers: validation succeeded", "server_id", id.String(), "docker_version", info.DockerVersion)
	return &ValidationResult{Checks: checks, Server: serverFromRow(updated)}, nil
}

// RegisterNode handles an agent Register call: it finds or creates the server
// for the node, records the reported capabilities, and returns the CP version.
//
// It does not issue a certificate: a node certificate is only ever issued from
// a CSR bound to the authenticated identity (Gateway.Register). A certificate
// signed for a control-plane-generated key would be unusable because the CP
// never hands that key over, so the no-CSR case returns no certificate and the
// agent falls back to its own (self-signed) key.
func (s *ServerService) RegisterNode(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is required", ErrValidation)
	}
	nodeID := strings.TrimSpace(req.GetNodeId())
	if err := validateNodeID(nodeID); err != nil {
		return nil, err
	}

	// Registration is fenced on the node identity: concurrent registers of the
	// same node converge on one row. An unclaimed operator-created row whose
	// address matches is claimed first so enrollment updates it instead of
	// inserting a duplicate; otherwise the node-id unique constraint makes the
	// upsert conflict-safe. Either way a race cannot leave a duplicate or an
	// orphan node_id NULL row (A4-12).
	os, dockerVersion, arch := strPtr(req.GetOs()), strPtr(req.GetDockerVersion()), strPtr(req.GetArch())
	totalMem, totalDisk := ptrInt64(req.GetTotalMem()), ptrInt64(req.GetTotalDisk())

	if claimed, err := s.claimUnregisteredServer(ctx, nodeID); err != nil {
		return nil, err
	} else if claimed.ID.Valid {
		updated, err := s.store.UpdateServerAgentInfo(ctx, sqlc.UpdateServerAgentInfoParams{
			ID:            claimed.ID,
			NodeID:        &nodeID,
			Os:            os,
			DockerVersion: dockerVersion,
			Arch:          arch,
			TotalMem:      totalMem,
			TotalDisk:     totalDisk,
		})
		if err != nil {
			return nil, fmt.Errorf("record agent info: %w", err)
		}
		s.logger.Info("servers: node claimed unregistered server",
			"node_id", nodeID, "server_id", updated.ID.String())
		return &agentv1.RegisterResponse{CpVersion: s.version}, nil
	}

	updated, err := s.store.UpsertServerByNodeID(ctx, sqlc.UpsertServerByNodeIDParams{
		Name:          nodeID,
		Ip:            ipFromNodeID(nodeID),
		Port:          defaultSSHPort,
		NodeID:        &nodeID,
		Os:            os,
		DockerVersion: dockerVersion,
		Arch:          arch,
		TotalMem:      totalMem,
		TotalDisk:     totalDisk,
	})
	if err != nil {
		return nil, fmt.Errorf("register node: %w", err)
	}

	s.logger.Info("servers: node registered", "node_id", nodeID, "server_id", updated.ID.String())
	return &agentv1.RegisterResponse{CpVersion: s.version}, nil
}

// claimUnregisteredServer associates a node identity with an unclaimed
// operator-created server row whose address matches the node id (an IP literal
// or a hostname the operator entered). It returns a zero Server (ID.Valid false)
// when there is nothing to claim. A unique clash (an operator row and an
// already-registered row share the address) is treated as "nothing to claim":
// the caller's upsert then targets the existing node row.
func (s *ServerService) claimUnregisteredServer(ctx context.Context, nodeID string) (sqlc.Server, error) {
	if nodeID == "" {
		return sqlc.Server{}, nil
	}
	row, err := s.store.ClaimServerByNodeID(ctx, sqlc.ClaimServerByNodeIDParams{NodeID: &nodeID, Ip: nodeID})
	switch {
	case err == nil:
		return row, nil
	case errors.Is(err, pgx.ErrNoRows):
		return sqlc.Server{}, nil
	default:
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return sqlc.Server{}, nil
		}
		return sqlc.Server{}, fmt.Errorf("claim server for node %s: %w", nodeID, err)
	}
}

// uniqueViolation is PostgreSQL's unique_violation SQLSTATE.
const uniqueViolation = "23505"

// ipFromNodeID returns the node id when it is an IP literal, or "" otherwise,
// so a hostname node is stored with an empty address rather than a bogus one.
func ipFromNodeID(nodeID string) string {
	if net.ParseIP(nodeID) != nil {
		return nodeID
	}
	return ""
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

	// The time series is appended after the snapshot update: the row already
	// carries the latest values, so a failed append must not fail the
	// heartbeat.
	s.recordMetric(ctx, row.ID, req)

	// The agent reports its build version on the heartbeat, which is how the
	// version map updates after a self-update and survives a reconnect.
	s.RecordAgentVersion(nodeID, req.GetAgentVersion())

	s.logger.Debug("servers: heartbeat",
		"node_id", nodeID,
		"server_id", updated.ID.String(),
		"cpu_usage", cpu,
		"mem_usage", mem,
		"container_count", count,
	)
	return nil
}

// sweepOffline flips ready nodes whose last heartbeat is older than
// heartbeatOfflineAfter to offline. It runs on the read path so every list and
// detail response carries the live status the FE already understands (A4-6). A
// sweep failure is logged, never surfaced: a stale status is better than a
// failed read.
func (s *ServerService) sweepOffline(ctx context.Context) {
	cutoff := s.now().Add(-heartbeatOfflineAfter)
	if err := s.store.MarkStaleServersOffline(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true}); err != nil {
		s.logger.Warn("servers: offline sweep", "error", err)
	}
}

// credentials resolves the SSH credentials for a validation run.
func (s *ServerService) credentials(ctx context.Context, row sqlc.Server, auth ValidateAuth) (SSHAuth, error) {
	switch {
	case auth.Password != "":
		return SSHAuth{Password: auth.Password}, nil
	case auth.KeyID != uuid.Nil:
		return s.loadKeyAuth(ctx, pgUUID(auth.KeyID), auth.Passphrase, true)
	default:
		if !row.SshKeyID.Valid {
			return SSHAuth{}, fmt.Errorf("%w: server has no SSH key", ErrNoCredentials)
		}
		return s.loadKeyAuth(ctx, row.SshKeyID, auth.Passphrase, false)
	}
}

// loadKeyAuth loads and decrypts a private key into an SSHAuth. passphrase
// decrypts a passphrase-protected PEM for this run and is never stored. explicit
// distinguishes an operator-supplied key ID from the server's attached key so
// the error text matches the cause.
func (s *ServerService) loadKeyAuth(ctx context.Context, id pgtype.UUID, passphrase string, explicit bool) (SSHAuth, error) {
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
	return SSHAuth{PrivateKeyPEM: []byte(plain), Passphrase: passphrase}, nil
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
		ID:                 uuidFromPG(row.ID),
		Name:               row.Name,
		IP:                 row.Ip,
		Port:               int(row.Port),
		SSHUser:            row.SshUser,
		TeamID:             uuidFromPG(row.TeamID),
		Status:             row.Status,
		NodeID:             row.NodeID,
		OS:                 row.Os,
		DockerVersion:      row.DockerVersion,
		Arch:               row.Arch,
		TotalMem:           row.TotalMem,
		TotalDisk:          row.TotalDisk,
		CPUUsage:           row.CpuUsage,
		MemUsage:           row.MemUsage,
		DiskUsage:          row.DiskUsage,
		ContainerCount:     row.ContainerCount,
		LastSeen:           timePtr(row.LastSeen),
		HostKeyFingerprint: row.HostKeyFingerprint,
		CreatedAt:          row.CreatedAt.Time,
		UpdatedAt:          row.UpdatedAt.Time,
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

// fingerprintOf dereferences a nullable fingerprint column.
func fingerprintOf(fp *string) string {
	if fp == nil {
		return ""
	}
	return *fp
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
