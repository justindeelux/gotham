package mcp

import (
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func assertCreateError(t *testing.T, result *sdk.CallToolResult, code string) {
	t.Helper()
	if result == nil || !result.IsError || len(result.Content) != 1 {
		t.Fatalf("want %s tool error, got %+v", code, result)
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok || text.Text != code {
		t.Fatalf("want error %q got %+v", code, result.Content)
	}
}
