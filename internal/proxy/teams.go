package proxy

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// appTeamLookup is the slice of the SSL/redirect stores the team filter needs.
type appTeamLookup interface {
	GetApplication(ctx context.Context, id uuid.UUID) (ApplicationInfo, error)
}

// authorizeApp authorizes an action on one application's team: a caller whose
// active team does not own the application answers ErrNotFound (so IDs cannot
// be probed) and a read_only member's mutation answers ErrForbidden. A request
// without a team scope keeps the pre-teams compatibility path (internal
// callers, tests).
func authorizeApp(ctx context.Context, appTeam uuid.UUID, write bool) error {
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(appTeam, write); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return ErrForbidden
		}
		return ErrNotFound
	}
	return nil
}

// filterByTeam keeps the rows whose application belongs to the caller's active
// team. Without a team scope every row stays visible, matching the pre-teams
// behavior of direct service use. An application that cannot be resolved drops
// its row: the list fails closed.
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
