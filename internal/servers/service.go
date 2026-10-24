package servers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
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

const (
	StatusPending    = "pending"
	StatusValidating = "validating"
	StatusReady      = "ready"
	StatusOffline    = "offline"
	StatusError      = "error"
)

const defaultSSHPort = 22

const heartbeatOfflineAfter = 30 * time.Second

type Server struct {
	ID                 uuid.UUID
	Name               string
	IP                 string
	Port               int
	SSHUser            string
	SSHKeyID           *uuid.UUID
	HasPassword        bool
	TeamID             uuid.UUID
	Status             string
	NodeID             *string
	OS                 *string
	DockerVersion      *string
	Arch               *string
	TotalMem           *int64
	TotalDisk          *int64
	CPUUsage           *float64
	MemUsage           *float64
	DiskUsage          *float64
	ContainerCount     *int64
	LastSeen           *time.Time
	HostKeyFingerprint *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type PrivateKey struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

type ValidationResult struct {
	Checks []CheckResult
	Server *Server
}

type ValidateAuth struct {
	KeyID        uuid.UUID
	Password     string
	Passphrase   string
	TrustHostKey bool
}

type Config struct {
	Store     *store.Store
	Authority *Authority
	Secret    string
	Version   string
	Logger    *slog.Logger
	Updater   AgentUpdateOfferer
}

type ServerService struct {
	store       *store.Store
	authority   *Authority
	secret      string
	version     string
	logger      *slog.Logger
	updater     AgentUpdateOfferer
	agentUpdate agentUpdateState

	metricMu     sync.Mutex
	lastMetricAt map[string]time.Time
	installs     sync.Map
	now          func() time.Time
}

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

func (s *ServerService) Version() string { return s.version }

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
	if row.NodeID != nil && *row.NodeID == "localhost" {
		return fmt.Errorf("%w: the localhost node cannot be deleted", ErrConflict)
	}
	if blocking, err := s.blockingResources(ctx, id); err != nil {
		return err
	} else if blocking != "" {
		return fmt.Errorf("%w: server still has resources: %s", ErrConflict, blocking)
	}
	volumes, err := s.store.DeleteServerPurging(ctx, pgUUID(id))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation {
			if blocking, listErr := s.blockingResources(ctx, id); listErr == nil && blocking != "" {
				return fmt.Errorf("%w: server still has resources: %s", ErrConflict, blocking)
			}
			return fmt.Errorf("%w: server still has resources", ErrConflict)
		}
		return fmt.Errorf("delete server: %w", err)
	}
	for _, volume := range volumes {
		if volume == "" {
			continue
		}
		s.logger.Warn("servers: purged database tombstone left its volume on the node",
			"server_id", id, "volume", volume)
	}
	return nil
}

func (s *ServerService) blockingResources(ctx context.Context, id uuid.UUID) (string, error) {
	groups := make([]string, 0, 3)
	applications, err := s.store.ListApplicationsByServer(ctx, pgUUID(id))
	if err != nil {
		return "", fmt.Errorf("list server applications: %w", err)
	}
	if names := resourceNames(applications); names != "" {
		groups = append(groups, "applications ("+names+")")
	}
	services, err := s.store.ListServicesByServer(ctx, pgUUID(id))
	if err != nil {
		return "", fmt.Errorf("list server services: %w", err)
	}
	if names := serviceNames(services); names != "" {
		groups = append(groups, "services ("+names+")")
	}
	databases, err := s.store.ListDatabasesByServer(ctx, pgUUID(id))
	if err != nil {
		return "", fmt.Errorf("list server databases: %w", err)
	}
	if names := databaseNames(databases); names != "" {
		groups = append(groups, "databases ("+names+")")
	}
	return strings.Join(groups, ", "), nil
}

const maxBlockers = 5

func blockerNames(names []string) string {
	if len(names) > maxBlockers {
		return strings.Join(names[:maxBlockers], ", ") +
			fmt.Sprintf(", and %d more", len(names)-maxBlockers)
	}
	return strings.Join(names, ", ")
}

func resourceNames(rows []sqlc.ListApplicationsByServerRow) string {
	if len(rows) == 0 {
		return ""
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return blockerNames(names)
}

func serviceNames(rows []sqlc.ListServicesByServerRow) string {
	if len(rows) == 0 {
		return ""
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return blockerNames(names)
}

func databaseNames(rows []sqlc.ListDatabasesByServerRow) string {
	if len(rows) == 0 {
		return ""
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return blockerNames(names)
}

type UpdateParams struct {
	Name     *string
	IP       *string
	Port     *int
	SSHUser  *string
	SSHKeyID *uuid.UUID
	Password *string
}

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

	if row.NodeID != nil && *row.NodeID == "localhost" {
		return nil, fmt.Errorf("%w: the localhost node cannot be edited", ErrConflict)
	}

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
	if params.SSHKeyID != nil && params.Password != nil &&
		*params.SSHKeyID != uuid.Nil && *params.Password != "" {
		return nil, fmt.Errorf("%w: provide either ssh_key_id or password, not both", ErrValidation)
	}

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
		if locked.NodeID != nil && *locked.NodeID == "localhost" {
			return sqlc.UpdateServerParams{}, fmt.Errorf("%w: the localhost node cannot be edited", ErrConflict)
		}
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
			if encryptedPassword != nil {
				encryptedPassword = nil
				credentialChanged = true
			}
		}
		if wantPasswordSet {
			if wantPassword == nil {
				if encryptedPassword != nil {
					encryptedPassword = nil
					credentialChanged = true
				}
				if keyID.Valid {
					keyID = pgtype.UUID{}
					credentialChanged = true
				}
			} else {
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

func sameKeyID(a, b pgtype.UUID) bool {
	if a.Valid != b.Valid {
		return false
	}
	return !a.Valid || a.Bytes == b.Bytes
}

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

func scopeID(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

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
			s.logger.Info("servers: host key pinned on first use",
				"server_id", id.String(),
				"host_key_fingerprint", fingerprint,
			)
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("persist host key fingerprint: %w", err)
		}

		fresh, readErr := s.store.GetServerByID(ctx, pgUUID(id))
		if readErr != nil {
			return fmt.Errorf("persist host key fingerprint: %w", readErr)
		}
		if !sameEndpoint(fresh, row) {
			return fmt.Errorf("%w: server changed during validation", ErrValidation)
		}
		switch stored := fingerprintOf(fresh.HostKeyFingerprint); {
		case stored == fingerprint:
			return nil
		case stored != "":
			return fmt.Errorf("host key changed while pinning: got %s, want %s", fingerprint, stored)
		default:
		}
	}
	return fmt.Errorf("persist host key fingerprint: node %s was cleared repeatedly while pinning", id)
}

type endpointGuard struct {
	ID                pgtype.UUID
	Ip                string
	Port              int32
	SshUser           string
	SshKeyID          pgtype.UUID
	EncryptedPassword *string
}

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

func sameEndpoint(fresh, row sqlc.Server) bool {
	return fresh.Ip == row.Ip &&
		fresh.Port == row.Port &&
		fresh.SshUser == row.SshUser &&
		sameKeyID(fresh.SshKeyID, row.SshKeyID) &&
		sameSecret(fresh.EncryptedPassword, row.EncryptedPassword)
}

func sameSecret(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

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

func (s *ServerService) keyForTeam(ctx context.Context, id uuid.UUID) (sqlc.PrivateKey, error) {
	key, err := s.store.GetPrivateKeyByID(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.PrivateKey{}, fmt.Errorf("%w: unknown ssh_key_id", ErrValidation)
		}
		return sqlc.PrivateKey{}, fmt.Errorf("lookup ssh key: %w", err)
	}
	scope := teams.ScopeFor(ctx, uuid.Nil)
	if !scope.Active() {
		return key, nil
	}
	if key.TeamID.Valid {
		if uuidFromPG(key.TeamID) != scope.TeamID {
			return sqlc.PrivateKey{}, fmt.Errorf("%w: unknown ssh_key_id", ErrValidation)
		}
		return key, nil
	}
	referenced, err := s.store.TeamReferencesPrivateKey(ctx, sqlc.TeamReferencesPrivateKeyParams{
		PrivateKeyID: key.ID,
		TeamID:       pgUUID(scope.TeamID),
	})
	if err != nil {
		return sqlc.PrivateKey{}, fmt.Errorf("lookup ssh key: %w", err)
	}
	if !referenced {
		return sqlc.PrivateKey{}, fmt.Errorf("%w: unknown ssh_key_id", ErrValidation)
	}
	return key, nil
}

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

	policy := HostKeyPolicy{Pinned: fingerprintOf(row.HostKeyFingerprint)}
	if policy.Pinned == "" {
		policy.AcceptUnpinned = len(credentials.PrivateKeyPEM) > 0 || auth.TrustHostKey
	}

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

func (s *ServerService) InstallDocker(ctx context.Context, id uuid.UUID, auth ValidateAuth, out io.Writer) error {
	row, err := s.store.GetServerByID(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("get server: %w", err)
	}
	server := serverFromRow(row)
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(server.TeamID, true); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return err
		}
		return ErrNotFound
	}

	if !s.lockInstall(id) {
		return fmt.Errorf("%w: a Docker install is already running for this server", ErrConflict)
	}
	defer s.unlockInstall(id)

	credentials, err := s.credentials(ctx, row, auth)
	if err != nil {
		return err
	}

	policy := HostKeyPolicy{Pinned: fingerprintOf(row.HostKeyFingerprint)}
	if policy.Pinned == "" {
		policy.AcceptUnpinned = len(credentials.PrivateKeyPEM) > 0 || auth.TrustHostKey
	}

	return InstallDockerNode(ctx, server.IP, server.Port, server.SSHUser, credentials, policy, out)
}

func (s *ServerService) lockInstall(id uuid.UUID) bool {
	_, running := s.installs.LoadOrStore(id, struct{}{})
	return !running
}

func (s *ServerService) unlockInstall(id uuid.UUID) {
	s.installs.Delete(id)
}

func (s *ServerService) NodeKeyFingerprint(ctx context.Context, nodeID string) (string, bool, error) {
	row, err := s.store.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("lookup node key: %w", err)
	}
	if row.AgentKeyFingerprint == nil {
		// EnsureLocalhost reserves this row before any agent has enrolled.
		if nodeID == "localhost" && row.Status == StatusPending && !row.LastSeen.Valid &&
			row.Os == nil && row.DockerVersion == nil && row.Arch == nil &&
			row.TotalMem == nil && row.TotalDisk == nil && row.AgentLegacyLeaf == nil &&
			row.SshUser == "" && !row.TeamID.Valid {
			return "", false, nil
		}
		return "", true, nil
	}
	return *row.AgentKeyFingerprint, true, nil
}

func (s *ServerService) AuthorizeNodeKey(ctx context.Context, nodeID, fingerprint string, legacyLeaf bool) error {
	if nodeID == "" {
		return fmt.Errorf("%w: node id is required", ErrValidation)
	}
	if fingerprint == "" {
		return fmt.Errorf("%w: node key fingerprint is required", ErrValidation)
	}
	row, err := s.store.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lookup node key: %w", err)
	}
	if row.AgentKeyFingerprint != nil && *row.AgentKeyFingerprint != fingerprint {
		s.logger.Warn("servers: agent key does not match the enrolled node",
			"node_id", nodeID, "server_id", row.ID.String())
		return fmt.Errorf("%w: node is bound to a different agent key", ErrConflict)
	}
	binding := row.AgentKeyFingerprint == nil
	leafKindChanged := row.AgentLegacyLeaf == nil || *row.AgentLegacyLeaf != legacyLeaf
	if !binding && !leafKindChanged {
		return nil
	}
	if _, err := s.store.SetServerAgentLeaf(ctx, sqlc.SetServerAgentLeafParams{
		ID:                  row.ID,
		AgentKeyFingerprint: &fingerprint,
		AgentLegacyLeaf:     &legacyLeaf,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: node is bound to a different agent key", ErrConflict)
		}
		return fmt.Errorf("bind node key: %w", err)
	}
	if binding {
		s.logger.Info("servers: bound agent key to node", "node_id", nodeID, "server_id", row.ID.String())
	}
	return nil
}

func (s *ServerService) LegacyLeafNodeCount(ctx context.Context) (int64, error) {
	count, err := s.store.CountLegacyAgentLeaves(ctx)
	if err != nil {
		return 0, fmt.Errorf("count legacy agent leaves: %w", err)
	}
	return count, nil
}

func (s *ServerService) UnobservedLeafNodeCount(ctx context.Context) (int64, error) {
	count, err := s.store.CountUnobservedAgentLeaves(ctx)
	if err != nil {
		return 0, fmt.Errorf("count unobserved agent leaves: %w", err)
	}
	return count, nil
}

func (s *ServerService) RegisterNode(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is required", ErrValidation)
	}
	nodeID := strings.TrimSpace(req.GetNodeId())
	if err := validateNodeID(nodeID); err != nil {
		return nil, err
	}

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

const uniqueViolation = "23505"

const foreignKeyViolation = "23503"

func ipFromNodeID(nodeID string) string {
	if net.ParseIP(nodeID) != nil {
		return nodeID
	}
	return ""
}

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

	s.recordMetric(ctx, row.ID, req)

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

func (s *ServerService) sweepOffline(ctx context.Context) {
	cutoff := s.now().Add(-heartbeatOfflineAfter)
	if err := s.store.MarkStaleServersOffline(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true}); err != nil {
		s.logger.Warn("servers: offline sweep", "error", err)
	}
}

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
	if key.TeamID.Valid {
		if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(uuidFromPG(key.TeamID), false); err != nil {
			return SSHAuth{}, fmt.Errorf("%w: attached SSH key no longer exists", ErrNoCredentials)
		}
	}

	return s.decryptKeyAuth(key.EncryptedKey, passphrase)
}

func (s *ServerService) decryptKeyAuth(encrypted, passphrase string) (SSHAuth, error) {
	plain, err := DecryptKey(encrypted, s.secret)
	if err != nil {
		return SSHAuth{}, fmt.Errorf("decrypt ssh key: %w", err)
	}
	return SSHAuth{PrivateKeyPEM: []byte(plain), Passphrase: passphrase}, nil
}

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

func failedChecks(detail string) []CheckResult {
	return []CheckResult{
		{Name: checkDocker, OK: false, Detail: detail},
		{Name: checkCPU, OK: false, Detail: detail},
		{Name: checkRAM, OK: false, Detail: detail},
		{Name: checkDisk, OK: false, Detail: detail},
	}
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func uuidFromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

func timePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func fingerprintOf(fp *string) string {
	if fp == nil {
		return ""
	}
	return *fp
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptrInt64(v int64) *int64 {
	return &v
}

func randomSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("servers: generate secret: %v", err))
	}
	return base64.StdEncoding.EncodeToString(buf)
}
