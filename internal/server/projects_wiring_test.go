package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// wiringProjectEnvelope decodes the project create/get responses just far
// enough to prove the PE-1 mount is live through the real server chain.
type wiringProjectEnvelope struct {
	Project struct {
		ID               string `json:"id"`
		Name             string `json:"name"`
		EnvironmentCount int    `json:"environment_count"`
	} `json:"project"`
	Environments []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"environments"`
}

// TestProjectsWiringEndToEnd replays the PE-1 contract against the real
// stack (real auth, real team chain, real service on a scratch database):
// unauthenticated reads are refused, and a session creates, reads and
// deletes a project with its production environment.
func TestProjectsWiringEndToEnd(t *testing.T) {
	s, _, pair := scratchProfileStack(t)
	bearer := "Bearer " + pair.AccessToken

	if rec := doRequest(t, s, http.MethodGet, "/api/v1/projects", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects", `{"name":"Shop"}`, bearer)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var created wiringProjectEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Project.Name != "Shop" || created.Project.EnvironmentCount != 1 {
		t.Fatalf("project = %+v, want Shop with one environment", created.Project)
	}
	if len(created.Environments) != 1 || created.Environments[0].Name != "production" {
		t.Fatalf("environments = %+v, want one production", created.Environments)
	}

	rec = doRequest(t, s, http.MethodGet, "/api/v1/projects/"+created.Project.ID, "", bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+created.Project.ID, "", bearer)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
}
