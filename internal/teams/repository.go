package teams

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type Repository interface {
	CreateTeam(ctx context.Context, team Team, ownerID uuid.UUID) (Team, error)
	GetTeam(ctx context.Context, teamID uuid.UUID) (Team, error)
	GetTeamForUser(ctx context.Context, teamID, userID uuid.UUID) (Team, error)
	ListTeamsByUser(ctx context.Context, userID uuid.UUID) ([]Team, error)
	RenameTeam(ctx context.Context, teamID uuid.UUID, name string) (Team, error)
	DeleteTeamIfEmpty(ctx context.Context, teamID uuid.UUID) error
	MutateMembership(ctx context.Context, teamID uuid.UUID, fn func(tx *MembershipTx) error) error
	GetMember(ctx context.Context, teamID, userID uuid.UUID) (Member, error)
	ListMembers(ctx context.Context, teamID uuid.UUID) ([]Member, error)
	CreateInvite(ctx context.Context, invite Invite, tokenHash string) (Invite, error)
	ListInvites(ctx context.Context, teamID uuid.UUID) ([]Invite, error)
	GetInviteByTokenHash(ctx context.Context, tokenHash string) (Invite, error)
	AcceptInvite(ctx context.Context, tokenHash string, userID uuid.UUID) (Invite, error)
	DeleteInvite(ctx context.Context, teamID, inviteID uuid.UUID) error
	UserEmail(ctx context.Context, userID uuid.UUID) (string, error)
}

type storeRepository struct {
	store *store.Store
}

var _ Repository = (*storeRepository)(nil)

func newStoreRepository(st *store.Store) *storeRepository {
	return &storeRepository{store: st}
}

func (r *storeRepository) CreateTeam(ctx context.Context, team Team, ownerID uuid.UUID) (Team, error) {
	row, err := r.store.CreateTeamWithOwner(ctx, sqlc.CreateTeamParams{
		ID:         pgUUID(team.ID),
		Name:       team.Name,
		IsPersonal: team.IsPersonal,
	}, pgUUID(ownerID))
	if err != nil {
		return Team{}, fmt.Errorf("teams: create team: %w", err)
	}
	return teamFromRow(row), nil
}

func (r *storeRepository) GetTeam(ctx context.Context, teamID uuid.UUID) (Team, error) {
	row, err := r.store.GetTeam(ctx, pgUUID(teamID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Team{}, ErrNotFound
		}
		return Team{}, fmt.Errorf("teams: get team: %w", err)
	}
	return teamFromRow(row), nil
}

func (r *storeRepository) GetTeamForUser(ctx context.Context, teamID, userID uuid.UUID) (Team, error) {
	row, err := r.store.GetTeamForUser(ctx, sqlc.GetTeamForUserParams{
		ID:     pgUUID(teamID),
		UserID: pgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Team{}, ErrNotFound
		}
		return Team{}, fmt.Errorf("teams: get team for user: %w", err)
	}
	return Team{
		ID:         uuidFromPG(row.ID),
		Name:       row.Name,
		IsPersonal: row.IsPersonal,
		Role:       Role(row.MemberRole),
		CreatedAt:  timeFromPG(row.CreatedAt),
		UpdatedAt:  timeFromPG(row.UpdatedAt),
	}, nil
}

func (r *storeRepository) ListTeamsByUser(ctx context.Context, userID uuid.UUID) ([]Team, error) {
	rows, err := r.store.ListTeamsByUser(ctx, pgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("teams: list teams: %w", err)
	}
	teams := make([]Team, 0, len(rows))
	for _, row := range rows {
		teams = append(teams, Team{
			ID:         uuidFromPG(row.ID),
			Name:       row.Name,
			IsPersonal: row.IsPersonal,
			Role:       Role(row.MemberRole),
			CreatedAt:  timeFromPG(row.CreatedAt),
			UpdatedAt:  timeFromPG(row.UpdatedAt),
		})
	}
	return teams, nil
}

func (r *storeRepository) RenameTeam(ctx context.Context, teamID uuid.UUID, name string) (Team, error) {
	row, err := r.store.UpdateTeam(ctx, sqlc.UpdateTeamParams{
		ID:   pgUUID(teamID),
		Name: name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Team{}, ErrNotFound
		}
		return Team{}, fmt.Errorf("teams: rename team: %w", err)
	}
	return teamFromRow(row), nil
}

func (r *storeRepository) DeleteTeamIfEmpty(ctx context.Context, teamID uuid.UUID) error {
	owned, err := r.store.DeleteTeamIfEmpty(ctx, pgUUID(teamID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if isForeignKeyViolation(err) {
			return ErrTeamNotEmpty
		}
		return fmt.Errorf("teams: delete team: %w", err)
	}
	if owned > 0 {
		return fmt.Errorf("%w: %d resource(s) still belong to this team; delete or move them first",
			ErrTeamNotEmpty, owned)
	}
	return nil
}

func (r *storeRepository) MutateMembership(ctx context.Context, teamID uuid.UUID, fn func(tx *MembershipTx) error) error {
	err := r.store.MutateTeamMembership(ctx, pgUUID(teamID),
		func(team sqlc.Team, members []sqlc.ListTeamMembersRow, writer *store.MembershipWriter) error {
			tx := &MembershipTx{
				Team:    teamFromRow(team),
				Members: membersFromRows(members),
				setRole: func(userID uuid.UUID, role Role) (Member, error) {
					row, err := writer.SetRole(pgUUID(userID), string(role))
					if err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return Member{}, ErrNotFound
						}
						return Member{}, fmt.Errorf("teams: update member role: %w", err)
					}
					return memberFromRow(row), nil
				},
				remove: func(userID uuid.UUID) error {
					if err := writer.Remove(pgUUID(userID)); err != nil {
						return fmt.Errorf("teams: delete member: %w", err)
					}
					return nil
				},
			}
			return fn(tx)
		})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (r *storeRepository) GetMember(ctx context.Context, teamID, userID uuid.UUID) (Member, error) {
	row, err := r.store.GetTeamMember(ctx, sqlc.GetTeamMemberParams{
		TeamID: pgUUID(teamID),
		UserID: pgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Member{}, ErrNotFound
		}
		return Member{}, fmt.Errorf("teams: get member: %w", err)
	}
	return memberFromRow(row), nil
}

func (r *storeRepository) ListMembers(ctx context.Context, teamID uuid.UUID) ([]Member, error) {
	rows, err := r.store.ListTeamMembers(ctx, pgUUID(teamID))
	if err != nil {
		return nil, fmt.Errorf("teams: list members: %w", err)
	}
	members := make([]Member, 0, len(rows))
	for _, row := range rows {
		members = append(members, Member{
			UserID:    uuidFromPG(row.UserID),
			Email:     row.Email,
			Role:      Role(row.Role),
			CreatedAt: timeFromPG(row.CreatedAt),
		})
	}
	return members, nil
}

func (r *storeRepository) CreateInvite(ctx context.Context, invite Invite, tokenHash string) (Invite, error) {
	row, err := r.store.CreateInvite(ctx, sqlc.CreateInviteParams{
		TeamID:    pgUUID(invite.TeamID),
		Email:     invite.Email,
		Role:      string(invite.Role),
		TokenHash: tokenHash,
		InvitedBy: pgUUID(invite.InvitedBy),
		ExpiresAt: pgTimestamp(invite.ExpiresAt),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Invite{}, ErrConflict
		}
		return Invite{}, fmt.Errorf("teams: create invite: %w", err)
	}
	return inviteFromRow(row), nil
}

func (r *storeRepository) ListInvites(ctx context.Context, teamID uuid.UUID) ([]Invite, error) {
	rows, err := r.store.ListInvitesByTeam(ctx, pgUUID(teamID))
	if err != nil {
		return nil, fmt.Errorf("teams: list invites: %w", err)
	}
	invites := make([]Invite, 0, len(rows))
	for _, row := range rows {
		invites = append(invites, inviteFromRow(row))
	}
	return invites, nil
}

func (r *storeRepository) GetInviteByTokenHash(ctx context.Context, tokenHash string) (Invite, error) {
	row, err := r.store.GetInviteByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Invite{}, ErrNotFound
		}
		return Invite{}, fmt.Errorf("teams: get invite: %w", err)
	}
	return inviteFromRow(row), nil
}

func (r *storeRepository) AcceptInvite(ctx context.Context, tokenHash string, userID uuid.UUID) (Invite, error) {
	row, err := r.store.AcceptInvite(ctx, tokenHash, pgUUID(userID))
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return Invite{}, ErrInviteUsed
		case isUniqueViolation(err):
			return Invite{}, ErrConflict
		default:
			return Invite{}, fmt.Errorf("teams: accept invite: %w", err)
		}
	}
	return inviteFromRow(row), nil
}

func (r *storeRepository) DeleteInvite(ctx context.Context, teamID, inviteID uuid.UUID) error {
	if err := r.store.DeleteInvite(ctx, sqlc.DeleteInviteParams{
		ID:     pgUUID(inviteID),
		TeamID: pgUUID(teamID),
	}); err != nil {
		return fmt.Errorf("teams: delete invite: %w", err)
	}
	return nil
}

func (r *storeRepository) UserEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	row, err := r.store.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("teams: get user email: %w", err)
	}
	return row.Email, nil
}

func membersFromRows(rows []sqlc.ListTeamMembersRow) []Member {
	members := make([]Member, 0, len(rows))
	for _, row := range rows {
		members = append(members, Member{
			UserID:    uuidFromPG(row.UserID),
			Email:     row.Email,
			Role:      Role(row.Role),
			CreatedAt: timeFromPG(row.CreatedAt),
		})
	}
	return members
}

func teamFromRow(row sqlc.Team) Team {
	return Team{
		ID:         uuidFromPG(row.ID),
		Name:       row.Name,
		IsPersonal: row.IsPersonal,
		CreatedAt:  timeFromPG(row.CreatedAt),
		UpdatedAt:  timeFromPG(row.UpdatedAt),
	}
}

func memberFromRow(row sqlc.TeamMember) Member {
	return Member{
		UserID:    uuidFromPG(row.UserID),
		Role:      Role(row.Role),
		CreatedAt: timeFromPG(row.CreatedAt),
	}
}

func inviteFromRow(row sqlc.Invite) Invite {
	invite := Invite{
		ID:        uuidFromPG(row.ID),
		TeamID:    uuidFromPG(row.TeamID),
		Email:     row.Email,
		Role:      Role(row.Role),
		InvitedBy: uuidFromPG(row.InvitedBy),
		ExpiresAt: timeFromPG(row.ExpiresAt),
		CreatedAt: timeFromPG(row.CreatedAt),
	}
	if row.AcceptedAt.Valid {
		accepted := row.AcceptedAt.Time
		invite.AcceptedAt = &accepted
	}
	return invite
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

func timeFromPG(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}

func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
