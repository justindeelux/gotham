package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestProjectOverviewReportsActivityWindowForQuietProject(t *testing.T) {
	project := uuid.NewString()
	cs := discoverySession(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/projects/" + project:
			_, _ = fmt.Fprintf(w, `{"project":{"id":%q},"environments":[]}`, project)
		case "/api/v1/applications":
			_, _ = fmt.Fprint(w, `{"applications":[]}`)
		case "/api/v1/deployments":
			if r.URL.Query().Get("limit") != "100" {
				t.Error("unbounded activity window")
			}
			_, _ = fmt.Fprint(w, `{"deployments":[]}`)
		default:
			t.Errorf("unexpected API path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	out := callDiscovery(t, cs, "project_overview", map[string]any{"project_id": project})
	var overview projectOverview
	if err := json.Unmarshal([]byte(resultJSON(t, out)), &overview); err != nil {
		t.Fatal(err)
	}
	if out.IsError || overview.ActivityWindowLimit != 100 || len(overview.RecentActivity) != 0 || overview.ActivityCoverage != "bounded_team_window_not_complete_project_history" {
		t.Fatalf("quiet project window ambiguous: %+v", overview)
	}
}
