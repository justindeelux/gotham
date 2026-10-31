package cleanup

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

// exclusionIndex is the loaded exclusion list a scan or clean checks items
// against. A nil server id protects the item on every node.
type exclusionIndex []store.CleanupExclusion

// loadExclusions reads the owner's protect list. A read failure is an error:
// the caller must not offer or delete a possibly protected item.
func (s *Service) loadExclusions(ctx context.Context) (exclusionIndex, error) {
	if s.store == nil {
		return nil, nil
	}
	list, err := s.store.ListCleanupExclusions(ctx)
	if err != nil {
		return nil, fmt.Errorf("cleanup: read exclusions: %w", err)
	}
	return exclusionIndex(list), nil
}

func (e exclusionIndex) matches(serverID uuid.UUID, category, name, id string) bool {
	for _, exclusion := range e {
		if exclusion.Category != category {
			continue
		}
		if exclusion.Match != name && exclusion.Match != id {
			continue
		}
		if exclusion.ServerID != nil && *exclusion.ServerID != serverID {
			continue
		}
		return true
	}
	return false
}

// filter drops the items an exclusion protects from a node's scan.
func (e exclusionIndex) filter(serverID uuid.UUID, items []Item) []Item {
	if len(e) == 0 {
		return items
	}
	kept := make([]Item, 0, len(items))
	for _, item := range items {
		if e.matches(serverID, item.Category, item.Name, item.ID) {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

// ExclusionInput is the owner's request to protect one item.
type ExclusionInput struct {
	ServerID *uuid.UUID
	Category string
	Match    string
	Note     string
}

// ListExclusions lists every exclusion, global and per-node.
func (s *Service) ListExclusions(ctx context.Context) ([]store.CleanupExclusion, error) {
	if s.center == nil {
		return nil, ErrUnavailable
	}
	return s.center.ListCleanupExclusions(ctx)
}

// CreateExclusion protects an item on one node, or on every node when ServerID
// is nil. The same server/category/match is idempotent.
func (s *Service) CreateExclusion(
	ctx context.Context, actor uuid.UUID, input ExclusionInput,
) (store.CleanupExclusion, error) {
	if s.center == nil {
		return store.CleanupExclusion{}, ErrUnavailable
	}
	category := strings.TrimSpace(input.Category)
	if !KnownCategory(category) {
		return store.CleanupExclusion{}, fmt.Errorf("%w: %q", ErrInvalidCategory, category)
	}
	match := strings.TrimSpace(input.Match)
	if match == "" || len(match) > 255 {
		return store.CleanupExclusion{}, fmt.Errorf("%w: match must be 1 to 255 characters", ErrValidation)
	}
	if input.ServerID != nil {
		if _, err := s.registry.Get(ctx, *input.ServerID); err != nil {
			return store.CleanupExclusion{}, fmt.Errorf("%w: %v", ErrInvalidServerID, err)
		}
	}
	exclusion := store.CleanupExclusion{
		ID:        uuid.New(),
		ServerID:  input.ServerID,
		Category:  category,
		Match:     match,
		Note:      strings.TrimSpace(input.Note),
		CreatedAt: s.now(),
	}
	if actor != uuid.Nil {
		exclusion.CreatedBy = &actor
	}
	return s.center.CreateCleanupExclusion(ctx, exclusion)
}

// DeleteExclusion removes one exclusion.
func (s *Service) DeleteExclusion(ctx context.Context, id uuid.UUID) error {
	if s.center == nil {
		return ErrUnavailable
	}
	return s.center.DeleteCleanupExclusion(ctx, id)
}
