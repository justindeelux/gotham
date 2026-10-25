package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/store"
)

func TestMCPMutationCapabilitiesRequireMountedAliasesAndStableKey(t *testing.T) {
	for _, tc := range []struct {
		name, secret                                                           string
		persisted, ephemeral, persistence, service, enabled, mutations, replay bool
	}{
		{name: "configured key", secret: "configured", persistence: true, service: true, enabled: true, mutations: true, replay: true},
		{name: "persisted fallback", persisted: true, persistence: true, service: true, enabled: true, mutations: true, replay: true},
		{name: "ephemeral fallback", ephemeral: true, persistence: true, service: true, enabled: true, mutations: true},
		{name: "disabled applications", secret: "configured", persistence: true, service: true},
		{name: "missing service", secret: "configured", persistence: true, enabled: true},
		{name: "missing persistence", secret: "configured", service: true, enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.enabled {
				t.Setenv(deploy.FeatureEnv, "true")
			} else {
				t.Setenv(deploy.FeatureEnv, "false")
			}
			cfg := &config.Config{Values: config.Values{SecretKey: tc.secret}}
			if tc.persisted {
				cfg.CA.Dir = t.TempDir()
			}
			tokens := newFakeTokenService()
			s, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), newFakeAuthService(), nil, tokens, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.deploy = nil; s.persistence = nil; s.closer() })
			if s.ephemeralSecret != tc.ephemeral {
				t.Fatalf("constructor ephemeral=%v want=%v", s.ephemeralSecret, tc.ephemeral)
			}
			if tc.persistence {
				s.persistence = &store.Store{}
			}
			if tc.service {
				s.deploy = fakeTargetDeployService{}
			}
			token := mustCreateToken(t, tokens, auth.ScopeRead)
			req := httptest.NewRequest("GET", "/api/v1/mcp/context", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set(mcpContractHeader, "1")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			if w.Code != 200 {
				t.Fatalf("context %d: %s", w.Code, w.Body.String())
			}
			var body struct {
				Capabilities []string `json:"capabilities"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if slices.Contains(body.Capabilities, "strict_mutations") != tc.mutations || slices.Contains(body.Capabilities, "request_replay") != tc.replay {
				t.Fatalf("capabilities=%v", body.Capabilities)
			}
		})
	}
}
