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
	// HasPassword reports whether a password secret is stored for the node.
	// The secret itself is never exposed outside the domain service.
	HasPassword bool
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
// attached, and password may be empty when no password is used. Exactly one
// credential may be given: a key id or a password, not both. The password is
// encrypted with the same mechanism as private keys and is never returned.
// userID is recorded as the creator, and the node is stamped with the
// caller's active team; a request without a team scope leaves team_id NULL,
// which is the legacy shared-node behavior.
func (s *ServerService) Add(ctx context.Context, userID uuid.UUID, name, ip string, port int, sshUser string, sshKeyID uuid.UUID, password string) (*Server, error) {
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
		if password != "" {
			return nil, fmt.Errorf("%w: provide either ssh_key_id or password, not both", ErrValidation)
		}
		if s.store == nil {
			return nil, errors.New("servers: store is not configured")
		}
		key, err := s.keyForTeam(ctx, sshKeyID)
		if err != nil {
			return nil, err
		}
		keyID = key.ID
	}

	var encryptedPassword *string
	if password != "" {
		// The secret is encrypted before it touches the store and never
		// logged; validation errors below never echo it.
		secret, err := EncryptKey(password, s.secret)
		if err != nil {
			return nil, err
		}
		encryptedPassword = &secret
	}

	row, err := s.store.CreateServer(ctx, sqlc.CreateServerParams{
		Name:              name,
		Ip:                ip,
		Port:              int32(port),
		SshUser:           sshUser,
		SshKeyID:          keyID,
		TeamID:            pgUUID(teams.ScopeFor(ctx, userID).TeamID),
		EncryptedPassword: encryptedPassword,
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

// UpdateParams edits a server. A nil field leaves the column unchanged; a
// non-nil field writes it. SSHKeyID points to the new key, or to the zero
// UUID to detach the key without attaching another. Password points to the
// new secret (which replaces any attached key), or to "" to forget the stored
// secret. The secret itself is never exposed: the API layer reads Password
// from the request body only.
type UpdateParams struct {
	Name     *string
	IP       *string
	Port     *int
	SSHUser  *string
	SSHKeyID *uuid.UUID
	Password *string
}

// Update applies a PATCH edit. Changing the address (ip), port, ssh user or
// either credential clears the pinned host key and returns the node to
// pending: the pin belongs to the old endpoint/identity, so the node must be
// revalidated before it is trusted again. Only an actual change resets
// anything: re-sending the stored address or the attached key, or a name-only
// edit, leaves the pin and status alone. Revalidation itself stays an
// explicit POST /v1/servers/{id}/validate call — a metadata edit must not
// block on a 15s SSH dial. A node outside the active team answers ErrNotFound
// so node IDs cannot be probed.
//
// The read-modify-write runs under a row lock (SELECT ... FOR UPDATE): the
// merge is computed from the locked row, so a concurrent heartbeat,
// RegisterNode, ResetHostKey or Validate write is never reverted by a stale
// read, and status is rewritten only when the address or credentials actually
// changed.
func (s *ServerService) Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*Server, error) {
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

	// Scalar validation depends only on the request, so it runs before the
	// row lock. A nil field leaves the column unchanged.
	var newName, newIP, newUser *string
	var newPort *int
	if params.Name != nil {
		v := strings.TrimSpace(*params.Name)
		if v == "" {
			return nil, fmt.Errorf("%w: name is required", ErrValidation)
		}
		newName = &v
	}
	if params.IP != nil {
		v := strings.TrimSpace(*params.IP)
		if v == "" {
			return nil, fmt.Errorf("%w: ip is required", ErrValidation)
		}
		newIP = &v
	}
	if params.Port != nil {
		if *params.Port < 1 || *params.Port > 65535 {
			return nil, fmt.Errorf("%w: port must be between 1 and 65535", ErrValidation)
		}
		v := *params.Port
		newPort = &v
	}
	if params.SSHUser != nil {
		v := strings.TrimSpace(*params.SSHUser)
		if v == "" {
			return nil, fmt.Errorf("%w: ssh_user is required", ErrValidation)
		}
		newUser = &v
	}
	// Single auth mode holds for PATCH as it does for create: a key and a
	// password together are a 400.
	if params.SSHKeyID != nil && params.Password != nil &&
		*params.SSHKeyID != uuid.Nil && *params.Password != "" {
		return nil, fmt.Errorf("%w: provide either ssh_key_id or password, not both", ErrValidation)
	}

	// Resolve the requested credentials outside the row lock: the keys table
	// is immutable (no key update path), so no concurrent writer here can
	// invalidate the lookup.
	var wantKey pgtype.UUID
	wantKeySet := params.SSHKeyID != nil
	if params.SSHKeyID != nil {
		if *params.SSHKeyID != uuid.Nil {
			key, err := s.keyForTeam(ctx, *params.SSHKeyID)
			if err != nil {
				return nil, err
			}
			wantKey = key.ID
		}
	}
	var wantPassword *string
	wantPasswordSet := params.Password != nil
	if params.Password != nil && *params.Password != "" {
		secret, err := EncryptKey(*params.Password, s.secret)
		if err != nil {
			return nil, err
		}
		wantPassword = &secret
	}
	wantPasswordPlain := ""
	if params.Password != nil {
		wantPasswordPlain = *params.Password
	}

	updated, err := s.store.UpdateServerGuarded(ctx, pgUUID(id), func(locked sqlc.Server) (sqlc.UpdateServerParams, error) {
		name, ip, sshUser, port := locked.Name, locked.Ip, locked.SshUser, int(locked.Port)
		if newName != nil {
			name = *newName
		}
		if newIP != nil {
			ip = *newIP
		}
		if newPort != nil {
			port = *newPort
		}
		if newUser != nil {
			sshUser = *newUser
		}
		addressChanged := ip != locked.Ip || port != int(locked.Port) || sshUser != locked.SshUser

		keyID, encryptedPassword := locked.SshKeyID, locked.EncryptedPassword
		credentialChanged := false
		if wantKeySet {
			if !sameKeyID(keyID, wantKey) {
				credentialChanged = true
			}
			keyID = wantKey
			// Single auth mode: attaching a key forgets any stored password.
			if encryptedPassword != nil {
				encryptedPassword = nil
				credentialChanged = true
			}
		}
		if wantPasswordSet {
			if wantPassword == nil {
				// Forgetting the secret; a detached key goes with it.
				if encryptedPassword != nil {
					encryptedPassword = nil
					credentialChanged = true
				}
				if keyID.Valid {
					keyID = pgtype.UUID{}
					credentialChanged = true
				}
			} else {
				// Setting the secret detaches the key. A re-sent identical
				// secret is not a change (the nonce prevents comparing
				// ciphertext, so decrypt and compare instead).
				if keyID.Valid {
					keyID = pgtype.UUID{}
					credentialChanged = true
				}
				if !s.storedPasswordEquals(locked.EncryptedPassword, wantPasswordPlain) {
					encryptedPassword = wantPassword
					credentialChanged = true
				} else {
					encryptedPassword = locked.EncryptedPassword
				}
			}
		}

		status := locked.Status
		fingerprint := locked.HostKeyFingerprint
		if addressChanged || credentialChanged {
			// The pin and the derived status belong to the old
			// endpoint/identity: forget the pin and go back through
			// validation.
			fingerprint = nil
			status = StatusPending
		}

		return sqlc.UpdateServerParams{
			ID:                 pgUUID(id),
			Name:               name,
			Ip:                 ip,
			Port:               int32(port),
			SshUser:            sshUser,
			SshKeyID:           keyID,
			EncryptedPassword:  encryptedPassword,
			HostKeyFingerprint: fingerprint,
			Status:             status,
		}, nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update server: %w", err)
	}

	s.logger.Debug("servers: updated", "server_id", id.String())
	return serverFromRow(updated), nil
}

// sameKeyID reports whether two nullable key references identify the same key.
func sameKeyID(a, b pgtype.UUID) bool {
	if a.Valid != b.Valid {
		return false
	}
	return !a.Valid || a.Bytes == b.Bytes
}

// storedPasswordEquals reports whether plain matches the stored encrypted
// secret. An undecryptable stored value counts as a mismatch, so the secret
// is rewritten rather than kept.
func (s *ServerService) storedPasswordEquals(stored *string, plain string) bool {
	if stored == nil || *stored == "" {
		return false
	}
	decrypted, err := DecryptKey(*stored, s.secret)
	if err != nil {
		return false
	}
	return decrypted == plain
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
// semantics, guarded on the endpoint and credentials the validation ran
// against. When the guarded write affects 0 rows the row is re-read: an
// endpoint or credential mismatch means a concurrent PATCH moved the node, so
// the stale pin is dropped with errServerChanged instead of pinning the OLD
// host's key onto the NEW address (JUS-5 fix round 1, defect 1). A pin that
// landed concurrently is accepted on a match and fails closed on a mismatch;
// a row cleared in the window (an operator reset) is retried once. A pin that
// never lands is an error, so a node is never reported ready un-pinned.
func (s *ServerService) pinHostKey(ctx context.Context, id uuid.UUID, row sqlc.Server, fingerprint string) error {
	guard := endpointGuardOf(id, row)
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := s.store.PinServerHostKeyGuarded(ctx, sqlc.PinServerHostKeyGuardedParams{
			ID:                 guard.ID,
			HostKeyFingerprint: &fingerprint,
			Ip:                 guard.Ip,
			Port:               guard.Port,
			SshUser:            guard.SshUser,
			SshKeyID:           guard.SshKeyID,
			EncryptedPassword:  guard.EncryptedPassword,
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

		// Lost the guarded write: re-read and decide.
		fresh, readErr := s.store.GetServerByID(ctx, pgUUID(id))
		if readErr != nil {
			return fmt.Errorf("persist host key fingerprint: %w", readErr)
		}
		if !sameEndpoint(fresh, row) {
			return fmt.Errorf("%w: server changed during validation", ErrValidation)
		}
		switch stored := fingerprintOf(fresh.HostKeyFingerprint); {
		case stored == fingerprint:
			return nil // someone else pinned the same key: benign
		case stored != "":
			return fmt.Errorf("host key changed while pinning: got %s, want %s", fingerprint, stored)
		default:
			// The row was cleared in the window (an operator reset landed):
			// loop and re-run the guarded write so the pin is not silently
			// skipped.
		}
	}
	return fmt.Errorf("persist host key fingerprint: node %s was cleared repeatedly while pinning", id)
}

// endpointGuard captures the endpoint identity a validation ran against, so
// every later write can be conditional on the row still matching it.
type endpointGuard struct {
	ID                pgtype.UUID
	Ip                string
	Port              int32
	SshUser           string
	SshKeyID          pgtype.UUID
	EncryptedPassword *string
}

// endpointGuardOf snapshots the guarded columns of row.
func endpointGuardOf(id uuid.UUID, row sqlc.Server) endpointGuard {
	return endpointGuard{
		ID:                pgUUID(id),
		Ip:                row.Ip,
		Port:              row.Port,
		SshUser:           row.SshUser,
		SshKeyID:          row.SshKeyID,
		EncryptedPassword: row.EncryptedPassword,
	}
}

// sameEndpoint reports whether fresh still carries the endpoint and
// credentials row had when the validation read it.
func sameEndpoint(fresh, row sqlc.Server) bool {
	return fresh.Ip == row.Ip &&
		fresh.Port == row.Port &&
		fresh.SshUser == row.SshUser &&
		sameKeyID(fresh.SshKeyID, row.SshKeyID) &&
		sameSecret(fresh.EncryptedPassword, row.EncryptedPassword)
}

// sameSecret compares two stored (encrypted) secrets by ciphertext. A PATCH
// that sets even an identical password re-encrypts with a fresh nonce, so any
// ciphertext difference means a concurrent credential write.
func sameSecret(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
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
		TeamID:       pgUUID(teams.ScopeFor(ctx, uuid.Nil).TeamID),
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

// keyForTeam fetches a private key by ID and enforces that it belongs to the
// caller's active team (JUS-5 fix round 1, defect 4a). A key from another
// team answers the same unknown-key validation error as a missing key, so key
// IDs cannot be probed across teams. Legacy keys (team_id NULL) predate teams
// and stay usable by every caller, mirroring legacy shared nodes; a request
// without a team scope keeps pre-teams behavior.
func (s *ServerService) keyForTeam(ctx context.Context, id uuid.UUID) (sqlc.PrivateKey, error) {
	key, err := s.store.GetPrivateKeyByID(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.PrivateKey{}, fmt.Errorf("%w: unknown ssh_key_id", ErrValidation)
		}
		return sqlc.PrivateKey{}, fmt.Errorf("lookup ssh key: %w", err)
	}
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(uuidFromPG(key.TeamID), false); err != nil {
		return sqlc.PrivateKey{}, fmt.Errorf("%w: unknown ssh_key_id", ErrValidation)
	}
	return key, nil
}

// Validate runs the SSH probes against a server and, on success, records the
// gathered agent info and restores the status derived from the node's last
// heartbeat. auth selects the credentials; when it is empty the server's stored
// key is used.
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

	// Every write below is conditional on the row still matching the endpoint
	// and credentials validated here (JUS-5 fix round 1, defect 1): a
	// concurrent PATCH that moved the node clears the pin and returns it to
	// pending, and the stale validation must drop its writes instead of
	// pinning the OLD host's key onto the NEW address or overwriting the new
	// status. A heartbeat touches none of the guarded columns, so it never
	// blocks these writes.
	guard := endpointGuardOf(id, row)
	if _, err := s.store.SetServerStatusGuarded(ctx, sqlc.SetServerStatusGuardedParams{
		ID:                guard.ID,
		Status:            StatusValidating,
		Ip:                guard.Ip,
		Port:              guard.Port,
		SshUser:           guard.SshUser,
		SshKeyID:          guard.SshKeyID,
		EncryptedPassword: guard.EncryptedPassword,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: server changed while validating", ErrValidation)
		}
		s.logger.Warn("servers: set status", "server_id", id.String(), "status", StatusValidating, "error", err)
	}

	checks, info, fingerprint, err := ValidateNode(ctx, server.IP, server.Port, server.SSHUser, credentials, policy)
	result := &ValidationResult{Checks: checks, Server: server}

	// failValidation records a failed run without overwriting a concurrent
	// PATCH: the guarded error write affects 0 rows when the endpoint moved
	// on, leaving the new pending status alone.
	failValidation := func(format string, args ...any) (*ValidationResult, error) {
		if _, guardErr := s.store.SetServerStatusGuarded(ctx, sqlc.SetServerStatusGuardedParams{
			ID:                guard.ID,
			Status:            StatusError,
			Ip:                guard.Ip,
			Port:              guard.Port,
			SshUser:           guard.SshUser,
			SshKeyID:          guard.SshKeyID,
			EncryptedPassword: guard.EncryptedPassword,
		}); guardErr != nil && !errors.Is(guardErr, pgx.ErrNoRows) {
			s.logger.Warn("servers: set status", "server_id", id.String(), "status", StatusError, "error", guardErr)
		}
		result.Server.Status = StatusError
		return result, fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
	}

	if err != nil {
		if len(checks) == 0 {
			result.Checks = failedChecks(err.Error())
		}
		return failValidation("%v", err)
	}

	for _, check := range checks {
		if !check.OK {
			return failValidation("%s check failed: %s", check.Name, check.Detail)
		}
	}

	// The handshake succeeded against a host that is now trusted: persist the
	// TOFU pin so every later validation fails closed on a different key. A
	// first pin only (fingerprint != policy.Pinned) and only when the row
	// still matches the validated endpoint: a concurrent PATCH drops the pin
	// attempt instead of pinning the old host's key onto the new address. A
	// failed or losing pin write fails the validation — the node never
	// reports ready without a durable pin.
	if fingerprint != "" && fingerprint != policy.Pinned {
		if err := s.pinHostKey(ctx, id, row, fingerprint); err != nil {
			return failValidation("%v", err)
		}
	}

	if _, err := s.store.UpdateServerAgentInfoGuarded(ctx, sqlc.UpdateServerAgentInfoGuardedParams{
		ID:                guard.ID,
		NodeID:            row.NodeID,
		Os:                strPtr(info.OS),
		DockerVersion:     strPtr(info.DockerVersion),
		Arch:              strPtr(info.Arch),
		TotalMem:          ptrInt64(info.TotalMem),
		TotalDisk:         ptrInt64(info.TotalDisk),
		Ip:                guard.Ip,
		Port:              guard.Port,
		SshUser:           guard.SshUser,
		SshKeyID:          guard.SshKeyID,
		EncryptedPassword: guard.EncryptedPassword,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return failValidation("server changed during validation")
		}
		// The SSH run succeeded but the inventory write failed. Restore the
		// heartbeat-derived status so the node does not stay stuck in
		// validating; a restore failure is logged and the original error is
		// still returned (fix round 2 U1).
		if _, restoreErr := s.store.SetServerStatusAfterValidationGuarded(ctx, sqlc.SetServerStatusAfterValidationGuardedParams{
			ID:                guard.ID,
			LastSeen:          pgtype.Timestamptz{Time: s.now().Add(-heartbeatOfflineAfter), Valid: true},
			Ip:                guard.Ip,
			Port:              guard.Port,
			SshUser:           guard.SshUser,
			SshKeyID:          guard.SshKeyID,
			EncryptedPassword: guard.EncryptedPassword,
		}); restoreErr != nil && !errors.Is(restoreErr, pgx.ErrNoRows) {
			s.logger.Warn("servers: restore status after agent-info failure",
				"server_id", id.String(), "error", restoreErr)
		}
		return result, fmt.Errorf("record agent info: %w", err)
	}

	// SSH reachability is not agent readiness: restore the node's status from
	// its last heartbeat at the moment of the write (A4-15/B4-9). A previously
	// ready node with a live agent stays ready; a node that heartbeated during
	// the validation stays ready; an agentless node is pending; a node whose
	// agent stopped is offline. Deriving it in one guarded statement prevents
	// a concurrent heartbeat from being clobbered (fix round 1 U2) and a
	// concurrent PATCH from being overwritten (defect 1): 0 rows means the
	// endpoint moved on, and the stale success is dropped.
	updated, err := s.store.SetServerStatusAfterValidationGuarded(ctx, sqlc.SetServerStatusAfterValidationGuardedParams{
		ID:                guard.ID,
		LastSeen:          pgtype.Timestamptz{Time: s.now().Add(-heartbeatOfflineAfter), Valid: true},
		Ip:                guard.Ip,
		Port:              guard.Port,
		SshUser:           guard.SshUser,
		SshKeyID:          guard.SshKeyID,
		EncryptedPassword: guard.EncryptedPassword,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return failValidation("server changed during validation")
		}
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
		if row.SshKeyID.Valid {
			return s.loadKeyAuth(ctx, row.SshKeyID, auth.Passphrase, false)
		}
		if row.EncryptedPassword != nil && *row.EncryptedPassword != "" {
			plain, err := DecryptKey(*row.EncryptedPassword, s.secret)
			if err != nil {
				return SSHAuth{}, fmt.Errorf("decrypt server password: %w", err)
			}
			return SSHAuth{Password: plain}, nil
		}
		return SSHAuth{}, fmt.Errorf("%w: server has no SSH key", ErrNoCredentials)
	}
}

// loadKeyAuth loads and decrypts a private key into an SSHAuth. passphrase
// decrypts a passphrase-protected PEM for this run and is never stored. explicit
// distinguishes an operator-supplied key ID from the server's attached key so
// the error text matches the cause. An operator-supplied key from another
// team answers unknown-key, like Add and Update (defect 4a); an attached key
// the caller's team can no longer see answers the same attached-key error as
// a deleted key.
func (s *ServerService) loadKeyAuth(ctx context.Context, id pgtype.UUID, passphrase string, explicit bool) (SSHAuth, error) {
	if explicit {
		key, err := s.keyForTeam(ctx, uuidFromPG(id))
		if err != nil {
			return SSHAuth{}, err
		}
		return s.decryptKeyAuth(key.EncryptedKey, passphrase)
	}

	key, err := s.store.GetPrivateKeyByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SSHAuth{}, fmt.Errorf("%w: attached SSH key no longer exists", ErrNoCredentials)
		}
		return SSHAuth{}, fmt.Errorf("load ssh key: %w", err)
	}
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(uuidFromPG(key.TeamID), false); err != nil {
		return SSHAuth{}, fmt.Errorf("%w: attached SSH key no longer exists", ErrNoCredentials)
	}

	return s.decryptKeyAuth(key.EncryptedKey, passphrase)
}

// decryptKeyAuth decrypts stored key material into an SSHAuth for one run.
func (s *ServerService) decryptKeyAuth(encrypted, passphrase string) (SSHAuth, error) {
	plain, err := DecryptKey(encrypted, s.secret)
	if err != nil {
		return SSHAuth{}, fmt.Errorf("decrypt ssh key: %w", err)
	}
	return SSHAuth{PrivateKeyPEM: []byte(plain), Passphrase: passphrase}, nil
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
	if row.EncryptedPassword != nil && *row.EncryptedPassword != "" {
		server.HasPassword = true
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
