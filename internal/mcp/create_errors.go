package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// SDK schema errors can quote input values, including pasted credentials.
func safeCreateErrors(next sdk.MethodHandler) sdk.MethodHandler {
	return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
		result, err := next(ctx, method, req)
		call, ok := req.(*sdk.CallToolRequest)
		if !ok || method != "tools/call" {
			return result, err
		}
		switch call.Params.Name {
		case "project_create", "environment_create", "application_create":
		default:
			return result, err
		}
		out, ok := result.(*sdk.CallToolResult)
		if !ok || !out.IsError {
			return result, err
		}
		if len(out.Content) == 1 {
			if content, ok := out.Content[0].(*sdk.TextContent); ok {
				switch content.Text {
				case "invalid_request", "invalid_response", "contract_mismatch", "redirect_refused", "response_too_large", "cancelled", "deadline_exceeded", "upstream_unavailable", "upstream_error", "unauthorized", "forbidden", "unsupported_or_not_found", "rate_limited", "invalid_idempotency_key", "request_conflict", "request_expired", "request_in_progress", "request_target_missing":
					return result, err
				}
			}
		}
		safe := &sdk.CallToolResult{}
		safe.SetError(&Error{Code: "invalid_request"})
		return safe, nil
	}
}
