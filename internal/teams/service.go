package teams

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

// FeatureEnv is the kill switch for the team management surface:
// FEATURE_TEAMS=false unmounts the teams/invites routes, so the feature can be
// turned off without a rebuild. The active-team middleware is unaffected: with
// the flag off every request resolves to the caller's personal team, which is
// exactly what the pre-teams API returned.
const FeatureEnv = "FEATURE_TEAMS"

// Enabled reports whether the teams feature is on. Only an explicit false
// disables it — unset (or any other value) keeps it enabled, matching
// deploy.Enabled and services.Enabled.
func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

// Team name and invite policy.
const (
	maxTeamNameLength = 100
	// inviteTokenBytes is the entropy of an invite token; only its SHA-256
	// hash is stored.
	inviteTokenBytes = 32
	// defaultInviteTTL bounds how long an invite can be accepted.
	defaultInviteTTL = 7 * 24 * time.Hour
)

// TeamService is the control-plane surface the HTTP layer and the active-team
// middleware depend on. It is implemented by Service and (in tests) by fakes.
type TeamService interface {
	// Membership returns the caller's role in a team, or ErrNotFound when the
	// caller is not a member.
	Membership(ctx context.Context, teamID, userID uuid.UUID) (Role, error)
	// Create stores a team owned by the caller.
	Create(ctx context.Context, userID uuid.UUID, name string) (Team, error)
	// List returns the caller's teams, each with the caller's role.
	List(ctx context.Context, userID uuid.UUID) ([]Team, error)
	// Get returns one team the caller belongs to.
	Get(ctx context.Context, userID, teamID uuid.UUID) (Team, error)
	// Rename changes a team's name (owner/admin).
	Rename(ctx context.Context, userID, teamID uuid.UUID, name string) (Team, error)
	// Delete removes a team (owner; personal teams are refused).
	Delete(ctx context.Context, userID, teamID uuid.UUID) error
	// Members returns the team's members (any member).
	Members(ctx context.Context, userID, teamID uuid.UUID) ([]Member, error)
	// SetMemberRole changes a member's role (owner/admin, with last-owner
	// protection).
	SetMemberRole(ctx context.Context, userID, teamID, memberID uuid.UUID, role Role) (Member, error)
	// RemoveMember removes a member (owner/admin, or the member themselves).
	RemoveMember(ctx context.Context, userID, teamID, memberID uuid.UUID) error
	// Invites lists a team's invites (owner/admin).
	Invites(ctx context.Context, userID, teamID uuid.UUID) ([]Invite, error)
	// Invite creates an invite and returns the raw token exactly once.
	Invite(ctx context.Context, userID, teamID uuid.UUID, email string, role Role) (Invite, string, error)
	// RevokeInvite deletes a pending invite (owner/admin).
	RevokeInvite(ctx context.Context, userID, teamID, inviteID uuid.UUID) error
	// Accept consumes an invite token and joins the caller to the team.
	Accept(ctx context.Context, userID uuid.UUID, token string) (Team, error)
}

// Config wires a Service. Store (or an explicit Repository) is required for
// anything beyond tests.
type Config struct {
	// Store is the PostgreSQL-backed repository. Ignored when Repository is set.
	Store *store.Store
	// Repository overrides Store (tests).
	Repository Repository
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// Now overrides the clock (tests).
	Now func() time.Time
}

// repository resolves the configured repository implementation.
func (c Config) repository() Repository {
	if c.Repository != nil {
		return c.Repository
	}
	if c.Store != nil {
		return newStoreRepository(c.Store)
	}
	return nil
}

// Service is the teams domain service: it owns team, member and invite
// lifecycle rules. Role checks are enforced here, on the team each call names,
// so they hold for every caller — the route middlewares only pre-filter.
// It is safe for concurrent use.
type Service struct {
	repo   Repository
	logger *slog.Logger
	now    func() time.Time
}

// Compile-time guarantee that Service satisfies the route-level contract.
var _ TeamService = (*Service)(nil)

// NewService builds a Service from cfg. The returned service has no repository
// only when cfg carries none; methods then fail with a clear error instead of
// panicking.
func NewService(cfg Config) *Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{repo: cfg.repository(), logger: loggerOr(cfg.Logger), now: now}
}

// NewDefaultService builds the production service for the HTTP wiring. It
// returns nil (a nil TeamService) when there is no database, so callers can
// pass its result to Mount unconditionally. The service is built even when
// FEATURE_TEAMS is off, because the active-team middleware still needs it; the
// routes themselves are gated by Enabled inside Mount.
func NewDefaultService(cfg Config) TeamService {
	if cfg.repository() == nil {
		return nil
	}
	return NewService(cfg)
}

// PersonalTeamID returns the personal team ID of a user: the personal team's ID
// is the owner's user ID (see migration 00019), so no lookup is needed. The
// helper exists so callers do not have to know that invariant.
func PersonalTeamID(userID uuid.UUID) uuid.UUID {
	return userID
}

// Membership implements TeamService.
func (s *Service) Membership(ctx context.Context, teamID, userID uuid.UUID) (Role, error) {
	if s == nil || s.repo == nil {
		return "", ErrNotFound
	}
	if teamID == uuid.Nil {
		return "", ErrNotFound
	}
	if teamID == PersonalTeamID(userID) {
		// The personal team always has its owner as owner, so the hot path
		// (no X-Team-Id header) needs no membership lookup.
		return RoleOwner, nil
	}
	member, err := s.repo.GetMember(ctx, teamID, userID)
	if err != nil {
		return "", err
	}
	return member.Role, nil
}

// Create implements TeamService.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, name string) (Team, error) {
	if err := s.ready(); err != nil {
		return Team{}, err
	}
	clean, err := validateTeamName(strings.TrimSpace(name))
	if err != nil {
		return Team{}, err
	}
	return s.repo.CreateTeam(ctx, Team{ID: uuid.New(), Name: clean}, userID)
}

// List implements TeamService.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Team, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	all, err := s.repo.ListTeamsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if all == nil {
		return []Team{}, nil
	}
	return all, nil
}

// Get implements TeamService.
func (s *Service) Get(ctx context.Context, userID, teamID uuid.UUID) (Team, error) {
	return s.member(ctx, userID, teamID)
}

// Rename implements TeamService.
func (s *Service) Rename(ctx context.Context, userID, teamID uuid.UUID, name string) (Team, error) {
	clean, err := validateTeamName(strings.TrimSpace(name))
	if err != nil {
		return Team{}, err
	}
	actor, err := s.manager(ctx, userID, teamID)
	if err != nil {
		return Team{}, err
	}
	team, err := s.repo.RenameTeam(ctx, teamID, clean)
	if err != nil {
		return Team{}, err
	}
	team.Role = actor.Role
	return team, nil
}

// Delete implements TeamService: only an owner may delete a team, and a
// personal team can never be deleted (it is the account's default scope).
func (s *Service) Delete(ctx context.Context, userID, teamID uuid.UUID) error {
	team, err := s.member(ctx, userID, teamID)
	if err != nil {
		return err
	}
	if !team.Role.IsOwner() {
		return ErrForbidden
	}
	if team.IsPersonal {
		return ErrPersonalTeam
	}
	if err := s.repo.DeleteTeam(ctx, teamID); err != nil {
		return err
	}
	// The team's resources (applications, databases, services) cascade with
	// the team row; their containers are not stopped by this call, so an
	// operator deletes a team with running workloads deliberately.
	s.logger.Info("teams: deleted; its resources were removed with it",
		"team_id", teamID.String(), "user_id", userID.String())
	return nil
}

// Members implements TeamService.
func (s *Service) Members(ctx context.Context, userID, teamID uuid.UUID) ([]Member, error) {
	if _, err := s.member(ctx, userID, teamID); err != nil {
		return nil, err
	}
	members, err := s.repo.ListMembers(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if members == nil {
		return []Member{}, nil
	}
	return members, nil
}

// SetMemberRole implements TeamService. Owners may set any role; admins may not
// touch owners and may not grant ownership. Demoting the last owner is refused.
func (s *Service) SetMemberRole(ctx context.Context, userID, teamID, memberID uuid.UUID, role Role) (Member, error) {
	if !role.Valid() {
		return Member{}, fmt.Errorf("%w: role must be one of owner, admin, read_only", ErrValidation)
	}
	actor, err := s.manager(ctx, userID, teamID)
	if err != nil {
		return Member{}, err
	}
	target, err := s.repo.GetMember(ctx, teamID, memberID)
	if err != nil {
		return Member{}, err
	}
	if !actor.Role.IsOwner() {
		if target.Role.IsOwner() || role.IsOwner() {
			return Member{}, fmt.Errorf("%w: only an owner may manage owners", ErrForbidden)
		}
	}
	if target.Role.IsOwner() && !role.IsOwner() {
		owners, err := s.repo.CountOwners(ctx, teamID)
		if err != nil {
			return Member{}, err
		}
		if owners <= 1 {
			return Member{}, ErrLastOwner
		}
	}
	updated, err := s.repo.UpdateMemberRole(ctx, teamID, memberID, role)
	if err != nil {
		return Member{}, err
	}
	updated.Email = target.Email
	return updated, nil
}

// RemoveMember implements TeamService. Owners and admins may remove members
// (admins not the owners); any member may remove themselves, which is how a
// user leaves a team. The last owner can never be removed.
func (s *Service) RemoveMember(ctx context.Context, userID, teamID, memberID uuid.UUID) error {
	actor, err := s.member(ctx, userID, teamID)
	if err != nil {
		return err
	}
	target, err := s.repo.GetMember(ctx, teamID, memberID)
	if err != nil {
		return err
	}
	if userID != memberID {
		if !actor.Role.CanManage() {
			return ErrForbidden
		}
		if !actor.Role.IsOwner() && target.Role.IsOwner() {
			return fmt.Errorf("%w: an admin cannot remove an owner", ErrForbidden)
		}
	}
	if target.Role.IsOwner() {
		owners, err := s.repo.CountOwners(ctx, teamID)
		if err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	return s.repo.DeleteMember(ctx, teamID, memberID)
}

// Invites implements TeamService.
func (s *Service) Invites(ctx context.Context, userID, teamID uuid.UUID) ([]Invite, error) {
	if _, err := s.manager(ctx, userID, teamID); err != nil {
		return nil, err
	}
	invites, err := s.repo.ListInvites(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if invites == nil {
		return []Invite{}, nil
	}
	return invites, nil
}

// Invite implements TeamService. It returns the raw token exactly once: only
// the token's hash is stored, so the token can never be read back. Email
// delivery is a separate work package (BE-8.3); this call only produces the
// link material.
func (s *Service) Invite(ctx context.Context, userID, teamID uuid.UUID, email string, role Role) (Invite, string, error) {
	if _, err := s.manager(ctx, userID, teamID); err != nil {
		return Invite{}, "", err
	}
	if role != RoleAdmin && role != RoleReadOnly {
		return Invite{}, "", fmt.Errorf("%w: invites may grant admin or read_only", ErrValidation)
	}
	normalized, err := normalizeInviteEmail(email)
	if err != nil {
		return Invite{}, "", err
	}
	token, hash, err := newInviteToken()
	if err != nil {
		return Invite{}, "", err
	}
	stored, err := s.repo.CreateInvite(ctx, Invite{
		ID:        uuid.New(),
		TeamID:    teamID,
		Email:     normalized,
		Role:      role,
		InvitedBy: userID,
		ExpiresAt: s.now().Add(defaultInviteTTL),
	}, hash)
	if err != nil {
		return Invite{}, "", err
	}
	// The raw token is returned to the caller and never logged or stored (only
	// its hash is). Sending the accept link to the invitee's inbox is BE-8.3;
	// until then the caller hands the link over.
	s.logger.Info("teams: invite created (email delivery is not wired yet, BE-8.3)",
		"team_id", teamID.String(), "email", normalized, "role", string(role), "expires_at", stored.ExpiresAt)
	return stored, token, nil
}

// RevokeInvite implements TeamService.
func (s *Service) RevokeInvite(ctx context.Context, userID, teamID, inviteID uuid.UUID) error {
	if _, err := s.manager(ctx, userID, teamID); err != nil {
		return err
	}
	return s.repo.DeleteInvite(ctx, teamID, inviteID)
}

// Accept implements TeamService. The invite is addressed to an email address:
// only the account with that email may accept it. A token that is unknown,
// expired, already consumed or addressed to someone else never grants a
// membership.
func (s *Service) Accept(ctx context.Context, userID uuid.UUID, token string) (Team, error) {
	if err := s.ready(); err != nil {
		return Team{}, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Team{}, fmt.Errorf("%w: invite token is required", ErrValidation)
	}
	hash := hashInviteToken(token)
	invite, err := s.repo.GetInviteByTokenHash(ctx, hash)
	if err != nil {
		return Team{}, err
	}
	if invite.AcceptedAt != nil {
		return Team{}, ErrInviteUsed
	}
	if !s.now().Before(invite.ExpiresAt) {
		return Team{}, ErrInviteExpired
	}
	email, err := s.repo.UserEmail(ctx, userID)
	if err != nil {
		return Team{}, err
	}
	if !strings.EqualFold(email, invite.Email) {
		return Team{}, fmt.Errorf("%w: this invite was issued to a different email address", ErrForbidden)
	}
	if _, err := s.repo.AcceptInvite(ctx, hash, userID); err != nil {
		return Team{}, err
	}
	team, err := s.repo.GetTeamForUser(ctx, invite.TeamID, userID)
	if err != nil {
		return Team{}, err
	}
	s.logger.Info("teams: invite accepted",
		"team_id", invite.TeamID.String(), "user_id", userID.String(), "role", string(invite.Role))
	return team, nil
}

// member loads a team the caller belongs to, mapping non-membership to
// ErrNotFound so team IDs cannot be probed.
func (s *Service) member(ctx context.Context, userID, teamID uuid.UUID) (Team, error) {
	if err := s.ready(); err != nil {
		return Team{}, err
	}
	if teamID == uuid.Nil {
		return Team{}, fmt.Errorf("%w: invalid team id", ErrValidation)
	}
	return s.repo.GetTeamForUser(ctx, teamID, userID)
}

// manager loads a team the caller may manage (owner or admin).
func (s *Service) manager(ctx context.Context, userID, teamID uuid.UUID) (Team, error) {
	team, err := s.member(ctx, userID, teamID)
	if err != nil {
		return Team{}, err
	}
	if !team.Role.CanManage() {
		return Team{}, ErrForbidden
	}
	return team, nil
}

// ready reports a service that was built without its required dependencies.
func (s *Service) ready() error {
	if s == nil || s.repo == nil {
		return errors.New("teams: repository is not configured")
	}
	return nil
}

// normalizeInviteEmail trims, lowercases and validates an invite address, the
// same shape auth.normalizeEmail stores.
func normalizeInviteEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return "", fmt.Errorf("%w: email is required", ErrValidation)
	}
	addr, err := mail.ParseAddress(normalized)
	if err != nil || addr.Address != normalized {
		return "", fmt.Errorf("%w: invalid email address", ErrValidation)
	}
	return normalized, nil
}

// newInviteToken generates an opaque invite token and the hash that is
// persisted. The raw token never reaches the database.
func newInviteToken() (token string, hash string, err error) {
	buf := make([]byte, inviteTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("teams: generate invite token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, hashInviteToken(token), nil
}

// hashInviteToken returns the hex-encoded SHA-256 of an invite token.
func hashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// loggerOr returns logger, or the process default.
func loggerOr(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
