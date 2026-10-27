package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type Store struct {
	DB      *pgxpool.Pool
	queries *sqlc.Queries

	BeforeFamilyRevoke func()

	BeforePinServerHostKey func() error

	BeforeUpdateServerAgentInfo func() error

	BeforeCollectionClear func()

	BeforeRepoCacheInsert func() error

	AfterEnvReadBeforeSecrets func()

	BeforeBackupRunCommit func()
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{
		DB:      pool,
		queries: sqlc.New(pool),
	}
}

func (s *Store) InTx(ctx context.Context, fn func(tx *Store) error) error {
	dbTx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = dbTx.Rollback(ctx) }()
	tx := &Store{DB: s.DB, queries: s.queries.WithTx(dbTx)}
	if err := fn(tx); err != nil {
		return err
	}
	return dbTx.Commit(ctx)
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (sqlc.User, error) {
	return s.queries.GetUserByEmail(ctx, email)
}

func (s *Store) GetUserByID(ctx context.Context, id pgtype.UUID) (sqlc.User, error) {
	return s.queries.GetUserByID(ctx, id)
}

func (s *Store) CreateSession(ctx context.Context, params sqlc.CreateSessionParams) (sqlc.Session, error) {
	return s.queries.CreateSession(ctx, params)
}

func (s *Store) GetSessionByRefreshHash(ctx context.Context, refreshHash string) (sqlc.Session, error) {
	return s.queries.GetSessionByRefreshHash(ctx, refreshHash)
}

func (s *Store) RevokeSession(ctx context.Context, refreshHash string) error {
	return s.queries.RevokeSession(ctx, refreshHash)
}

func (s *Store) DeleteSession(ctx context.Context, refreshHash string) error {
	return s.queries.DeleteSession(ctx, refreshHash)
}

func (s *Store) RevokeSessionIfLive(ctx context.Context, refreshHash string) (bool, error) {
	_, err := s.queries.RevokeSessionIfLive(ctx, refreshHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Store) RevokeFamilyIfStolen(ctx context.Context, userID pgtype.UUID, refreshHash string) (bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if err := queries.LockUserSessions(ctx, sessionLockKey(userID)); err != nil {
		return false, err
	}

	presented, err := queries.GetSessionByRefreshHash(ctx, refreshHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if presented.UserID != userID {
		return false, errors.New("store: presented session belongs to another user")
	}
	if !presented.RevokedAt.Valid {
		return false, nil
	}
	user, err := queries.GetUserByID(ctx, presented.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if presented.CredentialVersion != user.CredentialVersion {
		return false, nil
	}
	if err := queries.RevokeUserSessions(ctx, presented.UserID); err != nil {
		return false, err
	}
	if s.BeforeFamilyRevoke != nil {
		s.BeforeFamilyRevoke()
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) RotateSession(ctx context.Context, userID pgtype.UUID, params sqlc.RotateSessionParams) (sqlc.Session, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.Session{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if err := queries.LockUserSessions(ctx, sessionLockKey(userID)); err != nil {
		return sqlc.Session{}, err
	}
	session, err := queries.RotateSession(ctx, params)
	if err != nil {
		return sqlc.Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Session{}, err
	}
	return session, nil
}

func sessionLockKey(userID pgtype.UUID) string {
	return uuid.UUID(userID.Bytes).String()
}

func (s *Store) DeleteStaleSessions(ctx context.Context, expiredBefore, revokedBefore time.Time) (int64, error) {
	return s.queries.DeleteStaleSessions(ctx, sqlc.DeleteStaleSessionsParams{
		ExpiredBefore: pgtype.Timestamptz{Time: expiredBefore, Valid: true},
		RevokedBefore: pgtype.Timestamptz{Time: revokedBefore, Valid: true},
	})
}

func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	return s.queries.CountUsers(ctx)
}

func (s *Store) UpdateUserAvatar(ctx context.Context, id pgtype.UUID, avatar *string) (sqlc.User, error) {
	return s.queries.UpdateUserAvatar(ctx, sqlc.UpdateUserAvatarParams{
		ID:     id,
		Avatar: avatar,
	})
}

func (s *Store) UpdateUserPasswordHash(ctx context.Context, email, passwordHash string) error {
	return s.queries.UpdateUserPasswordHash(ctx, sqlc.UpdateUserPasswordHashParams{
		Lower:        email,
		PasswordHash: &passwordHash,
	})
}

func (s *Store) ResetUserPassword(ctx context.Context, userID pgtype.UUID, email, passwordHash string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if err := queries.LockUserSessions(ctx, sessionLockKey(userID)); err != nil {
		return err
	}
	if err := queries.DeleteUserSessions(ctx, userID); err != nil {
		return err
	}
	if err := queries.UpdateUserPasswordHash(ctx, sqlc.UpdateUserPasswordHashParams{
		Lower:        email,
		PasswordHash: &passwordHash,
	}); err != nil {
		return err
	}
	if err := queries.BumpCredentialVersion(ctx, userID); err != nil {
		return err
	}
	if err := queries.DeleteUserTOTP(ctx, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID pgtype.UUID) error {
	return s.queries.DeleteUserSessions(ctx, userID)
}

func (s *Store) ListSessionsByUser(ctx context.Context, userID pgtype.UUID, now time.Time) ([]sqlc.ListSessionsByUserRow, error) {
	return s.queries.ListSessionsByUser(ctx, sqlc.ListSessionsByUserParams{
		UserID:    userID,
		ExpiresAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
}

func (s *Store) SessionSignInAt(ctx context.Context, id, userID pgtype.UUID) (time.Time, error) {
	createdAt, err := s.queries.SessionSignInAt(ctx, sqlc.SessionSignInAtParams{
		ID:     id,
		UserID: userID,
	})
	if err != nil {
		return time.Time{}, err
	}
	return createdAt.Time, nil
}

func (s *Store) DeleteSessionByID(ctx context.Context, id, userID pgtype.UUID) (int64, error) {
	return s.queries.DeleteSessionByID(ctx, sqlc.DeleteSessionByIDParams{
		ID:     id,
		UserID: userID,
	})
}

func (s *Store) DeleteOtherSessionsGuarded(ctx context.Context, userID, sessionID pgtype.UUID) (int64, bool, error) {
	var deleted int64
	var live bool
	err := s.InTx(ctx, func(tx *Store) error {
		var err error
		deleted, live, err = tx.RevokeOtherSessionsGuarded(ctx, userID, sessionID)
		return err
	})
	if err != nil {
		return 0, false, err
	}
	return deleted, live, nil
}

func (s *Store) RevokeOtherSessionsGuarded(ctx context.Context, userID, sessionID pgtype.UUID) (int64, bool, error) {
	if err := s.queries.LockUserSessions(ctx, sessionLockKey(userID)); err != nil {
		return 0, false, err
	}
	if _, err := s.queries.CheckSessionLive(ctx, sqlc.CheckSessionLiveParams{
		ID:     sessionID,
		UserID: userID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	deleted, err := s.queries.DeleteOtherSessions(ctx, sqlc.DeleteOtherSessionsParams{
		UserID: userID,
		ID:     sessionID,
	})
	if err != nil {
		return 0, false, err
	}
	return deleted, true, nil
}

func (s *Store) UpdateUserDisplayName(ctx context.Context, params sqlc.UpdateUserDisplayNameParams) (sqlc.User, error) {
	return s.queries.UpdateUserDisplayName(ctx, params)
}

func (s *Store) ChangeUserPassword(ctx context.Context, userID pgtype.UUID, expectedHash *string, newHash *string) (sqlc.User, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if err := queries.LockUserSessions(ctx, sessionLockKey(userID)); err != nil {
		return sqlc.User{}, err
	}
	if err := queries.DeleteUserSessions(ctx, userID); err != nil {
		return sqlc.User{}, err
	}
	updated, err := queries.SetUserPasswordHash(ctx, sqlc.SetUserPasswordHashParams{
		ID:             userID,
		PasswordHash:   newHash,
		PasswordHash_2: expectedHash,
	})
	if err != nil {
		return sqlc.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.User{}, err
	}
	return updated, nil
}

func (s *Store) CreateAPIToken(ctx context.Context, params sqlc.CreateAPITokenParams) (sqlc.ApiToken, error) {
	return s.queries.CreateAPIToken(ctx, params)
}

func (s *Store) ListAPITokensByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.ApiToken, error) {
	return s.queries.ListAPITokensByUser(ctx, userID)
}

func (s *Store) GetAPITokenByHash(ctx context.Context, hash string) (sqlc.ApiToken, error) {
	return s.queries.GetAPITokenByHash(ctx, hash)
}

func (s *Store) GetAPITokenByIDAndUser(ctx context.Context, params sqlc.GetAPITokenByIDAndUserParams) (sqlc.ApiToken, error) {
	return s.queries.GetAPITokenByIDAndUser(ctx, params)
}

func (s *Store) RevokeAPIToken(ctx context.Context, params sqlc.RevokeAPITokenParams) (int64, error) {
	return s.queries.RevokeAPIToken(ctx, params)
}

func (s *Store) TouchAPITokenLastUsed(ctx context.Context, params sqlc.TouchAPITokenLastUsedParams) error {
	return s.queries.TouchAPITokenLastUsed(ctx, params)
}

func (s *Store) CreateServer(ctx context.Context, params sqlc.CreateServerParams) (sqlc.Server, error) {
	return s.queries.CreateServer(ctx, params)
}

func (s *Store) GetServerByID(ctx context.Context, id pgtype.UUID) (sqlc.Server, error) {
	return s.queries.GetServerByID(ctx, id)
}

func (s *Store) GetServerByNodeID(ctx context.Context, nodeID *string) (sqlc.Server, error) {
	return s.queries.GetServerByNodeID(ctx, nodeID)
}

func (s *Store) SetServerAgentLeaf(ctx context.Context, params sqlc.SetServerAgentLeafParams) (sqlc.Server, error) {
	return s.queries.SetServerAgentLeaf(ctx, params)
}

func (s *Store) CountLegacyAgentLeaves(ctx context.Context) (int64, error) {
	return s.queries.CountLegacyAgentLeaves(ctx)
}

func (s *Store) CountUnobservedAgentLeaves(ctx context.Context) (int64, error) {
	return s.queries.CountUnobservedAgentLeaves(ctx)
}

func (s *Store) UpsertServerByNodeID(ctx context.Context, params sqlc.UpsertServerByNodeIDParams) (sqlc.Server, error) {
	return s.queries.UpsertServerByNodeID(ctx, params)
}

func (s *Store) ClaimServerByNodeID(ctx context.Context, params sqlc.ClaimServerByNodeIDParams) (sqlc.Server, error) {
	return s.queries.ClaimServerByNodeID(ctx, params)
}

func (s *Store) GetServerByIDForUpdate(ctx context.Context, id pgtype.UUID) (sqlc.Server, error) {
	return s.queries.GetServerByIDForUpdate(ctx, id)
}

func (s *Store) MarkStaleServersOffline(ctx context.Context, cutoff pgtype.Timestamptz) ([]sqlc.Server, error) {
	return s.queries.MarkStaleServersOffline(ctx, cutoff)
}

func (s *Store) ListServers(ctx context.Context) ([]sqlc.Server, error) {
	return s.queries.ListServers(ctx)
}

func (s *Store) DeleteServer(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteServer(ctx, id)
}

func (s *Store) DeleteServerPurging(ctx context.Context, id pgtype.UUID) ([]string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if _, err := queries.PurgeTombstonedServicesByServer(ctx, id); err != nil {
		return nil, err
	}
	volumes, err := queries.PurgeTombstonedDatabasesByServer(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := queries.DeleteServer(ctx, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return volumes, nil
}

func (s *Store) UpdateServerAgentInfo(ctx context.Context, params sqlc.UpdateServerAgentInfoParams) (sqlc.Server, error) {
	if s.BeforeUpdateServerAgentInfo != nil {
		if err := s.BeforeUpdateServerAgentInfo(); err != nil {
			return sqlc.Server{}, err
		}
	}
	return s.queries.UpdateServerAgentInfo(ctx, params)
}

func (s *Store) UpdateServerMetrics(ctx context.Context, params sqlc.UpdateServerMetricsParams) (sqlc.Server, error) {
	return s.queries.UpdateServerMetrics(ctx, params)
}

func (s *Store) UpsertNodeObservation(ctx context.Context, params sqlc.UpsertNodeObservationParams) (sqlc.NodeObservation, error) {
	return s.queries.UpsertNodeObservation(ctx, params)
}

func (s *Store) GetNodeObservation(ctx context.Context, serverID pgtype.UUID) (sqlc.NodeObservation, error) {
	return s.queries.GetNodeObservation(ctx, serverID)
}

func (s *Store) DeleteNodeObservation(ctx context.Context, serverID pgtype.UUID) error {
	return s.queries.DeleteNodeObservation(ctx, serverID)
}

func (s *Store) SetServerStatus(ctx context.Context, params sqlc.SetServerStatusParams) (sqlc.Server, error) {
	return s.queries.SetServerStatus(ctx, params)
}

func (s *Store) SetServerStatusAfterValidation(ctx context.Context, params sqlc.SetServerStatusAfterValidationParams) (sqlc.Server, error) {
	return s.queries.SetServerStatusAfterValidation(ctx, params)
}

func (s *Store) PinServerHostKey(ctx context.Context, params sqlc.PinServerHostKeyParams) (sqlc.Server, error) {
	if s.BeforePinServerHostKey != nil {
		if err := s.BeforePinServerHostKey(); err != nil {
			return sqlc.Server{}, err
		}
	}
	return s.queries.PinServerHostKey(ctx, params)
}

func (s *Store) ClearServerHostKey(ctx context.Context, id pgtype.UUID) (sqlc.Server, error) {
	return s.queries.ClearServerHostKey(ctx, id)
}

func (s *Store) UpdateServer(ctx context.Context, params sqlc.UpdateServerParams) (sqlc.Server, error) {
	return s.queries.UpdateServer(ctx, params)
}

func (s *Store) UpdateServerGuarded(ctx context.Context, id pgtype.UUID, apply func(sqlc.Server) (sqlc.UpdateServerParams, error)) (sqlc.Server, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.Server{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	row, err := queries.GetServerByIDForUpdate(ctx, id)
	if err != nil {
		return sqlc.Server{}, err
	}
	params, err := apply(row)
	if err != nil {
		return sqlc.Server{}, err
	}
	updated, err := queries.UpdateServer(ctx, params)
	if err != nil {
		return sqlc.Server{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Server{}, err
	}
	return updated, nil
}

func (s *Store) PinServerHostKeyGuarded(ctx context.Context, params sqlc.PinServerHostKeyGuardedParams) (sqlc.Server, error) {
	if s.BeforePinServerHostKey != nil {
		if err := s.BeforePinServerHostKey(); err != nil {
			return sqlc.Server{}, err
		}
	}
	return s.queries.PinServerHostKeyGuarded(ctx, params)
}

func (s *Store) SetServerStatusGuarded(ctx context.Context, params sqlc.SetServerStatusGuardedParams) (sqlc.Server, error) {
	return s.queries.SetServerStatusGuarded(ctx, params)
}

func (s *Store) SetServerStatusAfterValidationGuarded(ctx context.Context, params sqlc.SetServerStatusAfterValidationGuardedParams) (sqlc.Server, error) {
	return s.queries.SetServerStatusAfterValidationGuarded(ctx, params)
}

func (s *Store) UpdateServerAgentInfoGuarded(ctx context.Context, params sqlc.UpdateServerAgentInfoGuardedParams) (sqlc.Server, error) {
	if s.BeforeUpdateServerAgentInfo != nil {
		if err := s.BeforeUpdateServerAgentInfo(); err != nil {
			return sqlc.Server{}, err
		}
	}
	return s.queries.UpdateServerAgentInfoGuarded(ctx, params)
}

func (s *Store) CreatePrivateKey(ctx context.Context, params sqlc.CreatePrivateKeyParams) (sqlc.CreatePrivateKeyRow, error) {
	return s.queries.CreatePrivateKey(ctx, params)
}

func (s *Store) GetPrivateKeyByID(ctx context.Context, id pgtype.UUID) (sqlc.PrivateKey, error) {
	return s.queries.GetPrivateKeyByID(ctx, id)
}

func (s *Store) ListPrivateKeys(ctx context.Context) ([]sqlc.ListPrivateKeysRow, error) {
	return s.queries.ListPrivateKeys(ctx)
}
