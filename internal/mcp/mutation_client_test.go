package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMutationClientFailsClosed(t *testing.T) {
	for _, mode := range []string{"old_backend", "redirect", "missing_ack", "oversized", "invalid_json", "cancelled", "unauthorized", "forbidden"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				acknowledge(w, r)
				switch mode {
				case "old_backend":
					w.WriteHeader(404)
				case "redirect":
					http.Redirect(w, r, "/legacy", http.StatusTemporaryRedirect)
				case "missing_ack":
					w.Header().Del(contractHeader)
					_, _ = fmt.Fprint(w, `{}`)
				case "oversized":
					_, _ = fmt.Fprint(w, strings.Repeat("x", maxBody+1))
				case "invalid_json":
					_, _ = fmt.Fprint(w, `{} {}`)
				case "unauthorized":
					w.WriteHeader(401)
				case "forbidden":
					w.WriteHeader(403)
				}
			}))
			defer api.Close()
			client, _ := NewClient(testConfig(api.URL))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			var out map[string]any
			err := client.mutateJSON(ctx, "/api/v1/mcp/projects", "stable-1", map[string]string{"name": "project"}, &out)
			wantCode := map[string]string{"old_backend": "unsupported_or_not_found", "redirect": "redirect_refused", "missing_ack": "contract_mismatch", "oversized": "response_too_large", "invalid_json": "invalid_response", "cancelled": "cancelled", "unauthorized": "unauthorized", "forbidden": "forbidden"}[mode]
			wantCalls := 1
			if mode == "cancelled" {
				wantCalls = 0
			}
			if err == nil || sectionError(err).Code != wantCode || calls != wantCalls {
				t.Fatalf("fail-open %v calls %d", err, calls)
			}
		})
	}
}
func TestCreateValidationDoesNotEchoSecrets(t *testing.T) {
	cs := createSession(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid input reached backend") })
	args := createArgs("image")
	args["registry_password"] = map[string]any{"password": "secret-sentinel"}
	out, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "application_create", Arguments: args})
	if err != nil {
		t.Fatalf("protocol error instead of safe tool rejection: %v", err)
	}
	assertCreateError(t, out, "invalid_request")
}
