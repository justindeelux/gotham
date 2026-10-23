package files

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type SearchHit struct {
	Path  string `json:"path"`
	Entry Entry  `json:"entry"`
}

type SearchListing struct {
	Results   []SearchHit `json:"results"`
	Truncated bool        `json:"truncated"`
}

func (s *Service) Search(ctx context.Context, serverID uuid.UUID, root, path, query string, recursive bool) (SearchListing, error) {
	if !Enabled() {
		return SearchListing{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return SearchListing{}, err
	}
	if err := validatePathArg("path", path); err != nil {
		return SearchListing{}, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchListing{}, fmt.Errorf("%w: query is required", ErrValidation)
	}
	if err := validateFileArg("query", query); err != nil {
		return SearchListing{}, err
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, false)
	if err != nil {
		return SearchListing{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.Search(ctx, &agentv1.SearchRequest{
		Root:      root,
		Path:      path,
		Query:     query,
		Recursive: recursive,
	})
	if err != nil {
		return SearchListing{}, mapRPCError(err)
	}
	results := make([]SearchHit, 0, len(response.GetResults()))
	for _, hit := range response.GetResults() {
		if hit.GetEntry() == nil {
			continue
		}
		results = append(results, SearchHit{Path: hit.GetPath(), Entry: newEntry(hit.GetEntry())})
	}
	return SearchListing{Results: results, Truncated: response.GetTruncated()}, nil
}
