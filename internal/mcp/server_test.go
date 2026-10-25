package mcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSDKLifecycle(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acknowledge(w, r)
		_, _ = fmt.Fprintf(w, `{"capabilities":["context","strict_reads"]}`)
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
			if err != nil || len(list.Tools) != 18 {
				t.Fatalf("tool list=%+v %v", list, err)
			}
			if _, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "not_registered"}); err == nil {
				t.Fatal("unknown tool accepted")
			}
			sdk.AddTool(server, &sdk.Tool{Name: "test_read", Description: "Test typed REST registration"}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, testOutput, error) {
				client, _ := NewClient(testConfig(api.URL))
				_, err := client.Context(ctx)
				return nil, testOutput{}, err
			})
			result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "test_read", Arguments: map[string]any{}})
			if err != nil || result.IsError || result.StructuredContent == nil {
				t.Fatalf("typed call=%+v %v", result, err)
			}
		})
	}
}

type testOutput struct {
	Capabilities []string `json:"capabilities"`
}

func TestNewServerWithClientReadOnlySurface(t *testing.T) {
	client, err := NewClient(testConfig("https://example.com"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewServerWithClient(client, false, "test", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, &sdk.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, tool := range list.Tools {
		seen[tool.Name] = true
	}
	for _, name := range []string{"project_create", "environment_create", "application_create", "application_deploy"} {
		if seen[name] {
			t.Errorf("read-only surface exposed %s", name)
		}
	}
	if len(list.Tools) != 14 {
		t.Fatalf("read-only tools=%d want 14", len(list.Tools))
	}
	if !seen["deployments_list"] || !seen["application_logs"] {
		t.Fatalf("read-only surface missing reads: %v", seen)
	}
}

func TestToolPanicIsRecovered(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	client, err := NewClient(testConfig("https://example.com"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewServerWithClient(client, true, "test", logger)
	sdk.AddTool(server, &sdk.Tool{Name: "panic_tool", Description: "Panics on purpose"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, struct{}, error) {
		panic("gt1_panic-value-must-not-leak")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, b := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	result, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "panic_tool", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("panic escaped the transport: %v", err)
	}
	if !result.IsError || len(result.Content) != 1 {
		t.Fatalf("panic result=%+v", result)
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok || text.Text != "internal_error" {
		t.Fatalf("panic error=%+v", result.Content)
	}
	if strings.Contains(logs.String(), "gt1_panic-value-must-not-leak") {
		t.Fatal("panic value was logged")
	}
	if _, err := cs.ListTools(ctx, &sdk.ListToolsParams{}); err != nil {
		t.Fatalf("server unusable after panic: %v", err)
	}
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
