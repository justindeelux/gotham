package deploy

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (r *storeRepository) GetBoundedBuildLog(ctx context.Context, appID, deploymentID uuid.UUID, options LogOptions) (string, State, bool, error) {
	row, err := r.store.GetBoundedBuildLog(ctx, sqlc.GetBoundedBuildLogParams{ApplicationID: pgUUID(appID), DeploymentID: pgUUID(deploymentID), Tail: int32(options.Tail), MaxBytes: int32(options.MaxBytes)})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, ErrNotFound
	}
	if err != nil {
		return "", "", false, errors.New("deploy: build log unavailable")
	}
	return row.Log, State(row.State), row.More, nil
}
