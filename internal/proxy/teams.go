package proxy

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

type appTeamLookup interface {
	GetApplication(ctx context.Context, id uuid.UUID) (ApplicationInfo, error)
}

func authorizeApp(ctx context.Context, appTeam uuid.UUID, write bool) error {
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(appTeam, write); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return ErrForbidden
		}
		return ErrNotFound
	}
	return nil
}

func filterByTeam[T any](ctx context.Context, store appTeamLookup, rows []T, appID func(T) uuid.UUID) []T {
	scope := teams.ScopeFor(ctx, uuid.Nil)
	if !scope.Active() {
		return rows
	}
	known := make(map[uuid.UUID]uuid.UUID, len(rows))
	out := make([]T, 0, len(rows))
	for _, row := range rows {
		id := appID(row)
		teamID, ok := known[id]
		if !ok {
			app, err := store.GetApplication(ctx, id)
			if err != nil {
				continue
			}
			teamID = app.TeamID
			known[id] = teamID
		}
		if teamID == scope.TeamID {
			out = append(out, row)
		}
	}
	return out
}
