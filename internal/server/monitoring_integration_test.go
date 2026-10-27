package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/monitoring"
)

func TestMonitoringSettingsRouteAccessAndValidation(t *testing.T) {
	f := newAccessFixture(t)

	owner := "Bearer " + f.owner.AccessToken
	response := doRequest(t, f.server, http.MethodGet, "/api/v1/monitoring/settings", "", owner)
	if response.Code != http.StatusOK {
		t.Fatalf("owner GET settings = %d %s", response.Code, response.Body.String())
	}

	member := "Bearer " + f.member.AccessToken
	response = doRequest(t, f.server, http.MethodGet, "/api/v1/monitoring/settings", "", member)
	if response.Code != http.StatusNotFound {
		// Owner-only platform routes hide their existence from members.
		t.Fatalf("member GET settings = %d %s; want hidden 404", response.Code, response.Body.String())
	}

	readToken, err := f.server.tokens.Create(context.Background(), uuid.MustParse(f.owner.User.ID), "owner-read", []string{auth.ScopeRead})
	if err != nil {
		t.Fatal(err)
	}
	response = doRequest(t, f.server, http.MethodGet, "/api/v1/monitoring/settings", "", "Bearer "+readToken.Token)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin owner token GET settings = %d %s; want 403", response.Code, response.Body.String())
	}

	invalid := monitoring.Defaults()
	invalid.CPU.Clear = invalid.CPU.Warning
	body, err := json.Marshal(map[string]any{"rules": invalid})
	if err != nil {
		t.Fatal(err)
	}
	response = doRequest(t, f.server, http.MethodPut, "/api/v1/monitoring/settings", string(body), owner)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid rules PUT = %d %s; want 400", response.Code, response.Body.String())
	}

	valid := monitoring.Defaults()
	valid.CPU.Warning = 80
	valid.CPU.Clear = 70
	body, err = json.Marshal(map[string]any{"rules": valid})
	if err != nil {
		t.Fatal(err)
	}
	response = doRequest(t, f.server, http.MethodPut, "/api/v1/monitoring/settings", string(body), owner)
	if response.Code != http.StatusOK {
		t.Fatalf("valid rules PUT = %d %s", response.Code, response.Body.String())
	}
}
