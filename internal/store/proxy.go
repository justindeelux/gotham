package store

import (
	"context"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// ListProxiedApplications returns every application that declares a base
// domain, ordered by id so the proxy generator's input is deterministic. The
// caller is responsible for filtering a row down to the node it is hosted on
// and for rejecting rows that cannot be routed (invalid domain, no pinned
// host port).
func (s *Store) ListProxiedApplications(ctx context.Context) ([]sqlc.Application, error) {
	return s.queries.ListProxiedApplications(ctx)
}
