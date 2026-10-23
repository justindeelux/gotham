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

const FeatureEnv = "FEATURE_TEAMS"

func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

const (
	maxTeamNameLength = 100
	inviteTokenBytes  = 32
	defaultInviteTTL  = 7 * 24 * time.Hour
)

type TeamService interface {
	Membership(ctx context.Context, teamID, userID uuid.UUID) (Role, error)
	Create(ctx context.Context, userID uuid.UUID, name string) (Team, error)
	List(ctx context.Context, userID uuid.UUID) ([]Team, error)
	Get(ctx context.Context, userID, teamID uuid.UUID) (Team, error)
	Rename(ctx context.Context, userID, teamID uuid.UUID, name string) (Team, error)
	Delete(ctx context.Context, userID, teamID uuid.UUID) error
	Members(ctx context.Context, userID, teamID uuid.UUID) ([]Member, error)
	SetMemberRole(ctx context.Context, userID, teamID, memberID uuid.UUID, role Role) (Member, error)
	RemoveMember(ctx context.Context, userID, teamID, memberID uuid.UUID) error
	Invites(ctx context.Context, userID, teamID uuid.UUID) ([]Invite, error)
	Invite(ctx context.Context, userID, teamID uuid.UUID, email string, role Role) (Invite, string, error)
	RevokeInvite(ctx context.Context, userID, teamID, inviteID uuid.UUID) error
	Accept(ctx context.Context, userID uuid.UUID, token string) (Team, error)
}

type Config struct {
	Store      *store.Store
	Repository Repository
	Logger     *slog.Logger
	Now        func() time.Time
}

func (c Config) repository() Repository {
	if c.Repository != nil {
		return c.Repository
	}
	if c.Store != nil {
		return newStoreRepository(c.Store)
	}
	return nil
}

type Service struct {
	repo   Repository
	logger *slog.Logger
	now    func() time.Time
}

var _ TeamService = (*Service)(nil)

func NewService(cfg Config) *Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{repo: cfg.repository(), logger: loggerOr(cfg.Logger), now: now}
}

func PersonalTeamID(userID uuid.UUID) uuid.UUID {
	return userID
}

func (s *Service) Membership(ctx context.Context, teamID, userID uuid.UUID) (Role, error) {
	if s == nil || s.repo == nil {
		return "", ErrNotFound
	}
	if teamID == uuid.Nil {
		return "", ErrNotFound
	}
	if teamID == PersonalTeamID(userID) {
		return RoleOwner, nil
	}
	member, err := s.repo.GetMember(ctx, teamID, userID)
	if err != nil {
		return "", err
	}
	return member.Role, nil
}

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

func (s *Service) Get(ctx context.Context, userID, teamID uuid.UUID) (Team, error) {
	return s.member(ctx, userID, teamID)
}

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
	if err := s.repo.DeleteTeamIfEmpty(ctx, teamID); err != nil {
		return err
	}
	s.logger.Info("teams: deleted; it owned no resources",
		"team_id", teamID.String(), "user_id", userID.String())
	return nil
}

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

func (s *Service) SetMemberRole(ctx context.Context, userID, teamID, memberID uuid.UUID, role Role) (Member, error) {
	if !role.Valid() {
		return Member{}, fmt.Errorf("%w: role must be one of owner, admin, read_only", ErrValidation)
	}
	var updated Member
	err := s.repo.MutateMembership(ctx, teamID, func(tx *MembershipTx) error {
		actor, ok := tx.Member(userID)
		if !ok {
			return ErrNotFound
		}
		target, ok := tx.Member(memberID)
		if !ok {
			return ErrNotFound
		}
		if !actor.Role.CanManage() {
			return ErrForbidden
		}
		if err := checkPersonalOwner(tx.Team, target, role.IsOwner()); err != nil {
			return err
		}
		if !actor.Role.IsOwner() {
			if target.Role.IsOwner() || role.IsOwner() {
				return fmt.Errorf("%w: only an owner may manage owners", ErrForbidden)
			}
		}
		if target.Role.IsOwner() && !role.IsOwner() && tx.Owners() <= 1 {
			return ErrLastOwner
		}
		changed, err := tx.SetRole(memberID, role)
		if err != nil {
			return err
		}
		updated = changed
		return nil
	})
	if err != nil {
		return Member{}, err
	}
	return updated, nil
}

func (s *Service) RemoveMember(ctx context.Context, userID, teamID, memberID uuid.UUID) error {
	return s.repo.MutateMembership(ctx, teamID, func(tx *MembershipTx) error {
		actor, ok := tx.Member(userID)
		if !ok {
			return ErrNotFound
		}
		target, ok := tx.Member(memberID)
		if !ok {
			return ErrNotFound
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
			if err := checkPersonalOwner(tx.Team, target, false); err != nil {
				return err
			}
			if tx.Owners() <= 1 {
				return ErrLastOwner
			}
		}
		return tx.Remove(memberID)
	})
}

func checkPersonalOwner(team Team, target Member, keepsOwnership bool) error {
	if !team.IsPersonal || target.UserID != team.ID || keepsOwnership {
		return nil
	}
	return ErrPersonalOwner
}

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
	s.logger.Info("teams: invite created (email delivery is not wired yet, BE-8.3)",
		"team_id", teamID.String(), "email", normalized, "role", string(role), "expires_at", stored.ExpiresAt)
	return stored, token, nil
}

func (s *Service) RevokeInvite(ctx context.Context, userID, teamID, inviteID uuid.UUID) error {
	if _, err := s.manager(ctx, userID, teamID); err != nil {
		return err
	}
	return s.repo.DeleteInvite(ctx, teamID, inviteID)
}

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
	team, err := s.repo.GetTeamForUser(context.WithoutCancel(ctx), invite.TeamID, userID)
	if err != nil {
		s.logger.Warn("teams: invite accepted but the team read failed",
			"team_id", invite.TeamID.String(), "user_id", userID.String(), "error", err)
		team = Team{ID: invite.TeamID, Role: invite.Role}
	}
	s.logger.Info("teams: invite accepted",
		"team_id", invite.TeamID.String(), "user_id", userID.String(), "role", string(invite.Role))
	return team, nil
}

func (s *Service) PeekInvite(ctx context.Context, token string) (teamName, email string, err error) {
	if err := s.ready(); err != nil {
		return "", "", err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", fmt.Errorf("%w: invite token is required", ErrValidation)
	}
	invite, err := s.repo.GetInviteByTokenHash(ctx, hashInviteToken(token))
	if err != nil {
		return "", "", err
	}
	if invite.AcceptedAt != nil {
		return "", "", ErrInviteUsed
	}
	if !s.now().Before(invite.ExpiresAt) {
		return "", "", ErrInviteExpired
	}
	team, err := s.repo.GetTeam(ctx, invite.TeamID)
	if err != nil {
		return "", "", err
	}
	return team.Name, invite.Email, nil
}

func (s *Service) AcceptInvite(ctx context.Context, userID uuid.UUID, token string) error {
	_, err := s.Accept(ctx, userID, token)
	return err
}

func (s *Service) member(ctx context.Context, userID, teamID uuid.UUID) (Team, error) {
	if err := s.ready(); err != nil {
		return Team{}, err
	}
	if teamID == uuid.Nil {
		return Team{}, fmt.Errorf("%w: invalid team id", ErrValidation)
	}
	return s.repo.GetTeamForUser(ctx, teamID, userID)
}

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

func (s *Service) ready() error {
	if s == nil || s.repo == nil {
		return errors.New("teams: repository is not configured")
	}
	return nil
}

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

func newInviteToken() (token string, hash string, err error) {
	buf := make([]byte, inviteTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("teams: generate invite token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, hashInviteToken(token), nil
}

func hashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func loggerOr(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
