package store

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// AddApplicationPendingChanges merges categories into the application's
// pending set and reports whether the set was empty before, which is the
// clear-to-set transition that triggers the redeploy notification.
func (s *Store) AddApplicationPendingChanges(ctx context.Context, id pgtype.UUID, changes []string) (bool, error) {
	becamePending := false
	err := s.InTx(ctx, func(tx *Store) error {
		before, err := tx.queries.GetApplicationPendingChangesForUpdate(ctx, id)
		if err != nil {
			return err
		}
		merged := mergePendingChanges(before, changes)
		if equalPendingChanges(before, merged) {
			return nil
		}
		becamePending = len(before) == 0
		return tx.queries.SetApplicationPendingChanges(ctx, sqlc.SetApplicationPendingChangesParams{
			ID:             id,
			PendingChanges: merged,
		})
	})
	return becamePending, err
}

func (s *Store) ClearApplicationPendingChanges(ctx context.Context, id pgtype.UUID) error {
	return s.queries.ClearApplicationPendingChanges(ctx, id)
}

func mergePendingChanges(before, changes []string) []string {
	seen := make(map[string]bool, len(before)+len(changes))
	merged := make([]string, 0, len(before)+len(changes))
	for _, set := range [][]string{before, changes} {
		for _, change := range set {
			if change == "" || seen[change] {
				continue
			}
			seen[change] = true
			merged = append(merged, change)
		}
	}
	sort.Strings(merged)
	return merged
}

func equalPendingChanges(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
