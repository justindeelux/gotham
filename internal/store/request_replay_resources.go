package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func replayUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }
func receiptResourceError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return requestreplay.ErrMissing
	}
	return err
}

func (s *Store) createProjectReplay(ctx context.Context, params sqlc.CreateProjectParams, envID pgtype.UUID, envName string) (project sqlc.Project, env sqlc.Environment, err error) {
	err = s.withRequestReceipt(ctx, func(q *sqlc.Queries, result *requestreplay.Result) (uuid.UUID, uuid.UUID, error) {
		var e error
		if result != nil {
			project, e = q.GetProject(ctx, replayUUID(result.ResourceID))
			if e != nil {
				return uuid.Nil, uuid.Nil, receiptResourceError(e)
			}
			env, e = q.GetEnvironment(ctx, replayUUID(result.RelatedID))
			if e == nil && env.ProjectID != project.ID {
				e = requestreplay.ErrMissing
			}
		} else {
			project, e = q.CreateProject(ctx, params)
			if e != nil {
				return uuid.Nil, uuid.Nil, e
			}
			env, e = q.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{ID: envID, ProjectID: project.ID, Name: envName})
		}
		return uuid.UUID(project.ID.Bytes), uuid.UUID(env.ID.Bytes), receiptResourceError(e)
	})
	return
}

func (s *Store) createEnvironmentReplay(ctx context.Context, params sqlc.CreateEnvironmentParams) (env sqlc.Environment, err error) {
	err = s.withRequestReceipt(ctx, func(q *sqlc.Queries, result *requestreplay.Result) (uuid.UUID, uuid.UUID, error) {
		if _, e := q.GetProject(ctx, params.ProjectID); e != nil {
			return uuid.Nil, uuid.Nil, receiptResourceError(e)
		}
		var e error
		if result != nil {
			env, e = q.GetEnvironment(ctx, replayUUID(result.ResourceID))
			if e == nil && env.ProjectID != params.ProjectID {
				e = requestreplay.ErrMissing
			}
		} else {
			env, e = q.CreateEnvironment(ctx, params)
		}
		return uuid.UUID(env.ID.Bytes), uuid.UUID(params.ProjectID.Bytes), receiptResourceError(e)
	})
	return
}

func (s *Store) createApplicationReplay(ctx context.Context, params sqlc.CreateApplicationParams, env []sqlc.InsertEnvVarParams, secrets []sqlc.InsertSecretParams, storages []sqlc.InsertStorageParams) (app sqlc.Application, err error) {
	err = s.withRequestReceipt(ctx, func(q *sqlc.Queries, result *requestreplay.Result) (uuid.UUID, uuid.UUID, error) {
		if _, e := q.GetEnvironment(ctx, params.EnvironmentID); e != nil {
			return uuid.Nil, uuid.Nil, receiptResourceError(e)
		}
		var e error
		if result != nil {
			app, e = q.GetApplication(ctx, replayUUID(result.ResourceID))
			if e == nil && app.EnvironmentID != params.EnvironmentID {
				e = requestreplay.ErrMissing
			}
		} else {
			app, e = createApplicationWithConfigTx(ctx, q, params, env, secrets, storages)
		}
		return uuid.UUID(app.ID.Bytes), uuid.UUID(params.EnvironmentID.Bytes), receiptResourceError(e)
	})
	return
}

func (s *Store) createDeploymentReplay(ctx context.Context, params sqlc.CreateDeploymentParams) (dep sqlc.Deployment, err error) {
	err = s.withRequestReceipt(ctx, func(q *sqlc.Queries, result *requestreplay.Result) (uuid.UUID, uuid.UUID, error) {
		app, e := q.GetApplication(ctx, params.ApplicationID)
		if e != nil {
			return uuid.Nil, uuid.Nil, receiptResourceError(e)
		}
		if result != nil {
			if app.EnvironmentID != replayUUID(result.RelatedID) {
				return uuid.Nil, uuid.Nil, requestreplay.ErrMissing
			}
			dep, e = q.GetDeployment(ctx, sqlc.GetDeploymentParams{ID: replayUUID(result.ResourceID), ApplicationID: params.ApplicationID})
		} else {
			dep, e = q.CreateDeployment(ctx, params)
		}
		return uuid.UUID(dep.ID.Bytes), uuid.UUID(app.EnvironmentID.Bytes), receiptResourceError(e)
	})
	return
}
