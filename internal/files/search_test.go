package files

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func TestServiceSearchMapping(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{
		searchResp: &agentv1.SearchResponse{
			Results: []*agentv1.SearchResult{
				{Path: "app/config.yaml", Entry: &agentv1.FileEntry{Name: "config.yaml", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 3}},
				{Path: "app/sub/config.json", Entry: &agentv1.FileEntry{Name: "config.json", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 2}},
			},
			Truncated: true,
		},
	}
	svc := newTestService(t, registry, client)

	// A read-only member may search, exactly like List.
	listing, err := svc.Search(scopeCtx(teams.RoleReadOnly, teamID), server.ID, "volumes", "app", "config", true)
	if err != nil {
		t.Fatalf("Search err = %v", err)
	}
	if len(listing.Results) != 2 || !listing.Truncated {
		t.Fatalf("listing = %+v", listing)
	}
	if listing.Results[0].Path != "app/config.yaml" || listing.Results[1].Entry.Name != "config.json" {
		t.Fatalf("results = %+v", listing.Results)
	}
	if client.seenSearch == nil || client.seenSearch.GetQuery() != "config" || !client.seenSearch.GetRecursive() || client.seenSearch.GetRoot() != "volumes" {
		t.Fatalf("agent request = %+v", client.seenSearch)
	}
}

func TestServiceSearchValidation(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)

	cases := []struct {
		name      string
		root      string
		query     string
		recursive bool
	}{
		{"empty root", "", "config", false},
		{"blank query", "volumes", "   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeFileClient{}
			svc := newTestService(t, registry, client)
			_, err := svc.Search(scopeCtx(teams.RoleOwner, teamID), server.ID, tc.root, "", tc.query, tc.recursive)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if client.seenSearch != nil {
				t.Fatal("an invalid request reached the agent")
			}
		})
	}
}

func TestServiceSearchDisabled(t *testing.T) {
	svc := newTestService(t, newFakeRegistry(), &fakeFileClient{})
	t.Setenv(FeatureEnv, "false")
	if _, err := svc.Search(scopeCtx(teams.RoleOwner, uuid.New()), uuid.New(), "volumes", "", "x", false); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestSearchRoute(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{
		search: SearchListing{
			Results:   []SearchHit{{Path: "app/config.yaml", Entry: Entry{Name: "config.yaml", Kind: "file"}}},
			Truncated: true,
		},
	}
	h := newTestRouter(svc)

	rec := doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/search?root=volumes&path=app&query=config&recursive=true")
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var listing SearchListing
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if !listing.Truncated || len(listing.Results) != 1 || listing.Results[0].Path != "app/config.yaml" {
		t.Fatalf("listing = %+v", listing)
	}
	if svc.seenSearch == nil || svc.seenSearch.root != "volumes" || svc.seenSearch.path != "app" || svc.seenSearch.query != "config" || !svc.seenSearch.recursive {
		t.Fatalf("service call = %+v", svc.seenSearch)
	}

	// recursive defaults to false when the flag is absent.
	svc.seenSearch = nil
	if rec := doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/search?root=volumes&query=config"); rec.Code != http.StatusOK {
		t.Fatalf("search status = %d", rec.Code)
	}
	if svc.seenSearch == nil || svc.seenSearch.recursive {
		t.Fatalf("recursive = %+v, want false", svc.seenSearch)
	}
}

func TestSearchRouteEmptyResults(t *testing.T) {
	id := uuid.New()
	h := newTestRouter(&fakeService{})
	rec := doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/search?root=volumes&query=x")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	// results is always an array, never null, so the client can map it directly.
	if got := rec.Body.String(); got != "{\"results\":[],\"truncated\":false}\n" {
		t.Fatalf("body = %q", got)
	}
}
