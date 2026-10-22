package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSDKLifecycle(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acknowledge(w, r)
		_, _ = fmt.Fprintf(w, `{"team_id":%q,"capabilities":["context","strict_reads"]}`, r.Header.Get("X-Team-Id"))
	}))
	defer api.Close()
	for _, version := range sdk.SupportedProtocolVersions() {
		t.Run(version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			server, err := NewServer(ctx, testConfig(api.URL), "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			a, b := sdk.NewInMemoryTransports()
			ss, err := server.Connect(ctx, a, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil)
			cs, err := client.Connect(ctx, b, &sdk.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			if got := cs.InitializeResult().ProtocolVersion; got != version {
				t.Fatalf("negotiated %q want %q", got, version)
			}
			list, err := cs.ListTools(ctx, &sdk.ListToolsParams{})
			if err != nil || len(list.Tools) != 0 {
				t.Fatalf("S1 tool list=%+v %v", list, err)
			}
			if _, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "not_registered"}); err == nil {
				t.Fatal("unknown tool accepted")
			}
			// Later packages register typed handlers through the same SDK surface.
			sdk.AddTool(server, &sdk.Tool{Name: "test_read", Description: "Test typed REST registration"}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, testOutput, error) {
				client, _ := NewClient(testConfig(api.URL))
				result, err := client.Context(ctx)
				return nil, testOutput{result.TeamID.String()}, err
			})
			result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "test_read", Arguments: map[string]any{}})
			if err != nil || result.IsError || result.StructuredContent == nil {
				t.Fatalf("typed call=%+v %v", result, err)
			}
		})
	}
}

type testOutput struct {
	TeamID string `json:"team_id"`
}

func TestStartupRequiresStrictAuthorization(t *testing.T) {
	for _, status := range []int{401, 403, 503} {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		server, err := NewServer(context.Background(), testConfig(api.URL), "test", nil)
		api.Close()
		if err == nil || server != nil {
			t.Fatalf("startup status %d accepted", status)
		}
	}
}
