package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateUser(ctx context.Context, email string, passwordHash *string) (sqlc.User, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	user, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        email,
		PasswordHash: passwordHash,
	})
	if err != nil {
		return sqlc.User{}, err
	}
	if _, err := queries.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:         user.ID,
		Name:       personalTeamName(email),
		IsPersonal: true,
	}); err != nil {
		return sqlc.User{}, err
	}
	if _, err := queries.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: user.ID,
		UserID: user.ID,
		Role:   "owner",
	}); err != nil {
		return sqlc.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.User{}, err
	}
	return user, nil
}

const firstUserLockKey = int64(0x676f7468616d)

var ErrInstanceHasAccount = errors.New("store: instance already has an account")

func (s *Store) CreateFirstUser(ctx context.Context, email string, passwordHash *string) (sqlc.User, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", firstUserLockKey); err != nil {
		return sqlc.User{}, err
	}

	queries := s.queries.WithTx(tx)
	user, err := queries.CreateFirstUser(ctx, sqlc.CreateFirstUserParams{
		Email:        email,
		PasswordHash: passwordHash,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.User{}, ErrInstanceHasAccount
		}
		return sqlc.User{}, err
	}
	if _, err := queries.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:         user.ID,
		Name:       personalTeamName(email),
		IsPersonal: true,
	}); err != nil {
		return sqlc.User{}, err
	}
	if _, err := queries.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: user.ID,
		UserID: user.ID,
		Role:   "owner",
	}); err != nil {
		return sqlc.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.User{}, err
	}
	return user, nil
}

func (s *Store) DeleteUserAndPersonalTeam(ctx context.Context, userID pgtype.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if err := queries.DeleteTeamMember(ctx, sqlc.DeleteTeamMemberParams{TeamID: userID, UserID: userID}); err != nil {
		return err
	}
	if err := queries.DeleteTeam(ctx, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM users WHERE id = $1", userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func personalTeamOrDefault(teamID, userID pgtype.UUID) pgtype.UUID {
	if teamID.Valid {
		return teamID
	}
	return userID
}

func personalTeamName(email string) string {
	return email + "'s team"
}

func (s *Store) CreateTeamWithOwner(ctx context.Context, params sqlc.CreateTeamParams, ownerID pgtype.UUID) (sqlc.Team, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.Team{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	team, err := queries.CreateTeam(ctx, params)
	if err != nil {
		return sqlc.Team{}, err
	}
	if _, err := queries.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: team.ID,
		UserID: ownerID,
		Role:   "owner",
	}); err != nil {
		return sqlc.Team{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Team{}, err
	}
	return team, nil
}

func (s *Store) CreateTeam(ctx context.Context, params sqlc.CreateTeamParams) (sqlc.Team, error) {
	return s.queries.CreateTeam(ctx, params)
}

func (s *Store) GetTeam(ctx context.Context, id pgtype.UUID) (sqlc.Team, error) {
	return s.queries.GetTeam(ctx, id)
}

func (s *Store) CountTeamResources(ctx context.Context, id pgtype.UUID) (int64, error) {
	count, err := s.queries.CountTeamResources(ctx, id)
	if err != nil {
		return 0, err
	}
	return int64(count), nil
}

type MembershipWriter struct {
	ctx     context.Context
	queries *sqlc.Queries
	teamID  pgtype.UUID
}

func (w *MembershipWriter) SetRole(userID pgtype.UUID, role string) (sqlc.TeamMember, error) {
	return w.queries.UpdateTeamMemberRole(w.ctx, sqlc.UpdateTeamMemberRoleParams{
		TeamID: w.teamID,
		UserID: userID,
		Role:   role,
	})
}

func (w *MembershipWriter) Remove(userID pgtype.UUID) error {
	return w.queries.DeleteTeamMember(w.ctx, sqlc.DeleteTeamMemberParams{
		TeamID: w.teamID,
		UserID: userID,
	})
}

func (s *Store) MutateTeamMembership(ctx context.Context, teamID pgtype.UUID, fn func(team sqlc.Team, members []sqlc.ListTeamMembersRow, writer *MembershipWriter) error) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	team, err := queries.GetTeamForUpdate(ctx, teamID)
	if err != nil {
		return err
	}
	members, err := queries.ListTeamMembers(ctx, teamID)
	if err != nil {
		return err
	}
	if err := fn(team, members, &MembershipWriter{ctx: ctx, queries: queries, teamID: teamID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetTeamForUser(ctx context.Context, params sqlc.GetTeamForUserParams) (sqlc.GetTeamForUserRow, error) {
	return s.queries.GetTeamForUser(ctx, params)
}

func (s *Store) ListTeamsByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.ListTeamsByUserRow, error) {
	return s.queries.ListTeamsByUser(ctx, userID)
}

func (s *Store) UpdateTeam(ctx context.Context, params sqlc.UpdateTeamParams) (sqlc.Team, error) {
	return s.queries.UpdateTeam(ctx, params)
}

func (s *Store) DeleteTeam(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteTeam(ctx, id)
}

func (s *Store) DeleteTeamIfEmpty(ctx context.Context, id pgtype.UUID) (int64, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if _, err := queries.GetTeamForUpdate(ctx, id); err != nil {
		return 0, err
	}
	count, err := queries.CountTeamResources(ctx, id)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		return int64(count), nil
	}
	if err := queries.DeleteTeam(ctx, id); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return 0, nil
}

func (s *Store) CreateTeamMember(ctx context.Context, params sqlc.CreateTeamMemberParams) (sqlc.TeamMember, error) {
	return s.queries.CreateTeamMember(ctx, params)
}

func (s *Store) GetTeamMember(ctx context.Context, params sqlc.GetTeamMemberParams) (sqlc.TeamMember, error) {
	return s.queries.GetTeamMember(ctx, params)
}

func (s *Store) ListTeamMembers(ctx context.Context, teamID pgtype.UUID) ([]sqlc.ListTeamMembersRow, error) {
	return s.queries.ListTeamMembers(ctx, teamID)
}

func (s *Store) CountTeamOwners(ctx context.Context, teamID pgtype.UUID) (int64, error) {
	return s.queries.CountTeamOwners(ctx, teamID)
}

func (s *Store) CreateInvite(ctx context.Context, params sqlc.CreateInviteParams) (sqlc.Invite, error) {
	return s.queries.CreateInvite(ctx, params)
}

func (s *Store) ListInvitesByTeam(ctx context.Context, teamID pgtype.UUID) ([]sqlc.Invite, error) {
	return s.queries.ListInvitesByTeam(ctx, teamID)
}

func (s *Store) GetInviteByTokenHash(ctx context.Context, tokenHash string) (sqlc.Invite, error) {
	return s.queries.GetInviteByTokenHash(ctx, tokenHash)
}

func (s *Store) AcceptInvite(ctx context.Context, tokenHash string, userID pgtype.UUID) (sqlc.Invite, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.Invite{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	invite, err := queries.AcceptInvite(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Invite{}, err
		}
		return sqlc.Invite{}, fmt.Errorf("accept invite: %w", err)
	}
	if _, err := queries.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: invite.TeamID,
		UserID: userID,
		Role:   invite.Role,
	}); err != nil {
		return sqlc.Invite{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Invite{}, err
	}
	return invite, nil
}

func (s *Store) DeleteInvite(ctx context.Context, params sqlc.DeleteInviteParams) error {
	return s.queries.DeleteInvite(ctx, params)
}
