package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
)

var mutationKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func (c *Client) mutateJSON(ctx context.Context, path, requestID string, input, out any) error {
	if !mutationKeyPattern.MatchString(requestID) || !strings.HasPrefix(path, "/api/v1/mcp/") || strings.ContainsAny(path, "?#\\%") || strings.Contains(path, "..") {
		return &Error{Code: "invalid_request"}
	}
	body, err := json.Marshal(input)
	if err != nil || len(body) > maxBody {
		return &Error{Code: "invalid_request"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.config.URL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return &Error{Code: "invalid_request"}
	}
	req.GetBody = nil
	req.Header.Set("Authorization", "Bearer "+c.config.Token)
	req.Header.Set(contractHeader, "1")
	req.Header.Set("Idempotency-Key", requestID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return mutationTransportError(ctx)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return &Error{Code: "redirect_refused", Status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return mutationTransportError(ctx)
	}
	if len(data) > maxBody {
		return &Error{Code: "response_too_large"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code := "upstream_error"
		switch resp.StatusCode {
		case 401:
			code = "unauthorized"
		case 403:
			code = "forbidden"
		case 404:
			code = "unsupported_or_not_found"
		case 429:
			code = "rate_limited"
		}
		if resp.Header.Get(contractHeader) == "1" {
			var failure struct {
				Code string `json:"code"`
			}
			if json.Unmarshal(data, &failure) == nil {
				switch failure.Code {
				case "invalid_idempotency_key", "request_conflict", "request_expired", "request_in_progress", "request_target_missing":
					code = failure.Code
				}
			}
		}
		return &Error{Code: code, Status: resp.StatusCode}
	}
	if resp.Header.Get(contractHeader) != "1" {
		return &Error{Code: "contract_mismatch"}
	}
	if json.Unmarshal(data, out) != nil {
		return &Error{Code: "invalid_response"}
	}
	return nil
}

func mutationTransportError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return &Error{Code: "cancelled"}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &Error{Code: "deadline_exceeded"}
	}
	return &Error{Code: "upstream_unavailable"}
}
