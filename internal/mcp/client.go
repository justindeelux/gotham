package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxBody = 2 << 20
const contractHeader = "X-Gotham-MCP-Contract"
const teamHeader = "X-Gotham-MCP-Team-Id"

// Error contains only stable local categories and HTTP status, never upstream text.
type Error struct {
	Code   string `json:"code"`
	Status int    `json:"status,omitempty"`
}

func (e *Error) Error() string { return e.Code }

// Client never accepts per-call origins, credentials, or teams.
type Client struct {
	config Config
	http   *http.Client
}

func NewClient(cfg Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Client{config: cfg, http: &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

type Context struct {
	TeamID       uuid.UUID `json:"team_id"`
	Capabilities []string  `json:"capabilities"`
}

func (c *Client) Context(ctx context.Context) (Context, error) {
	var result Context
	err := c.get(ctx, "/api/v1/mcp/context", &result, true, false)
	if err == nil && result.TeamID != c.config.TeamID {
		err = &Error{Code: "contract_mismatch"}
	}
	if err != nil {
		return Context{}, err
	}
	return result, nil
}

// readJSON is the package-local registration seam for typed read tools.
// Callers must url.PathEscape each dynamic path segment before composing the path.
func (c *Client) readJSON(ctx context.Context, path string, out any) error {
	return c.get(ctx, path, out, true, false)
}

// Health is dependency state, including a useful degraded 503 response.
type Health struct {
	Status string `json:"status"`
	DB     string `json:"db"`
	Redis  string `json:"redis"`
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	if _, err := c.Context(ctx); err != nil {
		return Health{}, err
	}
	var result Health
	if err := c.get(ctx, "/healthz", &result, false, true); err != nil {
		return Health{}, err
	}
	return result, nil
}

func (c *Client) get(ctx context.Context, path string, out any, strict, health bool) error {
	// Paths originate in tool implementations; reject accidental origin or traversal overrides.
	if len(path) > 8192 || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "#\\") {
		return &Error{Code: "invalid_request"}
	}
	decodedPath, err := url.PathUnescape(strings.SplitN(path, "?", 2)[0])
	if err != nil {
		return &Error{Code: "invalid_request"}
	}
	for _, segment := range strings.Split(decodedPath, "/") {
		if segment == "." || segment == ".." {
			return &Error{Code: "invalid_request"}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.config.URL, "/")+path, nil)
	if err != nil {
		return &Error{Code: "invalid_request"}
	}
	req.Header.Set("Accept", "application/json")
	if strict {
		req.Header.Set("Authorization", "Bearer "+c.config.Token)
		req.Header.Set("X-Team-Id", c.config.TeamID.String())
		req.Header.Set(contractHeader, "1")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &Error{Code: "deadline_exceeded"}
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return &Error{Code: "cancelled"}
		}
		return &Error{Code: "upstream_unavailable"}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return &Error{Code: "redirect_refused", Status: resp.StatusCode}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if !health || resp.StatusCode != http.StatusServiceUnavailable {
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
			return &Error{Code: code, Status: resp.StatusCode}
		}
	}
	if strict && (resp.Header.Get(contractHeader) != "1" || resp.Header.Get(teamHeader) != c.config.TeamID.String()) {
		return &Error{Code: "contract_mismatch"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return &Error{Code: "cancelled"}
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &Error{Code: "deadline_exceeded"}
		}
		return &Error{Code: "upstream_unavailable"}
	}
	if len(body) > maxBody {
		return &Error{Code: "response_too_large"}
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(out); err != nil {
		return &Error{Code: "invalid_response"}
	}
	if dec.Decode(new(any)) != io.EOF {
		return &Error{Code: "invalid_response"}
	}
	return nil
}
