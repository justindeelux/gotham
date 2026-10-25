package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/deploy"
	mcpsdk "github.com/justindeelux/gotham/internal/mcp"
	"github.com/justindeelux/gotham/internal/requestreplay"
)

const mcpContractHeader = "X-Gotham-MCP-Contract"

const mcpHTTPPath = "/mcp"

// acknowledgeMCPContract echoes the strict contract marker so the adapter can
// reject a backend that does not implement the contract it was asked for.
func acknowledgeMCPContract(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(mcpContractHeader) == "1" {
			w.Header().Set(mcpContractHeader, "1")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireMCPStrict(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(mcpContractHeader) != "1" {
			writeJSON(w, http.StatusBadRequest, apiError{Message: "unsupported MCP contract"})
			return
		}
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead
		if mutation && (r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/api/v1/mcp/")) {
			writeJSON(w, http.StatusMethodNotAllowed, apiError{Message: "unsupported MCP operation"})
			return
		}
		if mutation {
			if !IsAPITokenRequest(r.Context()) {
				writeJSON(w, http.StatusForbidden, apiError{Message: "API token required"})
				return
			}
			keys := r.Header.Values("Idempotency-Key")
			if len(keys) != 1 || !requestreplay.ValidKey(keys[0]) {
				requestreplay.WriteError(w, requestreplay.ErrKey)
				return
			}
		}
		w.Header().Set(mcpContractHeader, "1")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) mountMCPRoutes(api chi.Router) {
	api.With(s.readScopeAuth, s.requireMCPStrict).Get("/v1/mcp/context", func(w http.ResponseWriter, r *http.Request) {
		capabilities := []string{"context", "strict_reads"}
		if s.persistence != nil && s.deploy != nil && deploy.Enabled() {
			capabilities = append(capabilities, "strict_mutations")
			if !s.ephemeralSecret {
				capabilities = append(capabilities, "request_replay")
			}
		}
		writeJSON(w, http.StatusOK, struct {
			Capabilities []string `json:"capabilities"`
		}{capabilities})
	})
}

// MCPHandlerConfig describes the remote streamable HTTP MCP endpoint.
type MCPHandlerConfig struct {
	Tokens         TokenService
	BaseURL        func(*http.Request) string
	AllowedOrigins func(context.Context) []string
	Version        string
	Logger         *slog.Logger
}

// NewMCPHandler serves the MCP tools over streamable HTTP. Only API tokens
// authenticate; JWT sessions and query-string credentials are refused. The tool
// surface follows the token scopes and the REST layer still authorizes calls.
func NewMCPHandler(cfg MCPHandlerConfig) http.Handler {
	streamable := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		token, ok := bearerToken(r)
		if !ok {
			return nil
		}
		client, err := mcpsdk.NewClient(mcpsdk.Config{URL: cfg.BaseURL(r), Token: token})
		if err != nil {
			return nil
		}
		scopes, _ := ScopesFromContext(r.Context())
		return mcpsdk.NewServerWithClient(client, auth.ScopesAuthorize(scopes, auth.ScopeDeploy), cfg.Version, cfg.Logger)
	}, &sdk.StreamableHTTPOptions{
		Stateless: true,
		// Origin is validated here: the SDK guard rejects requests relayed by a
		// TLS proxy because it only sees the proxy's loopback address.
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        mcpsdk.MaxBody,
		Logger:                     cfg.Logger,
	})
	authorized := requireAPIToken(cfg.Tokens)(RequireScopes(auth.ScopeRead)(streamable))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mcpOriginAllowed(r, cfg.allowedOrigins(r.Context())) {
			writeJSON(w, http.StatusForbidden, apiError{Message: "origin not allowed"})
			return
		}
		authorized.ServeHTTP(w, r)
	})
}

func (cfg MCPHandlerConfig) allowedOrigins(ctx context.Context) []string {
	if cfg.AllowedOrigins == nil {
		return nil
	}
	return cfg.AllowedOrigins(ctx)
}

func (s *Server) mountMCPHTTP(r chi.Router) {
	handler := NewMCPHandler(MCPHandlerConfig{
		Tokens:         s.tokens,
		BaseURL:        s.mcpBaseURLForRequest,
		AllowedOrigins: s.mcpAllowedOrigins,
		Version:        s.currentVersion(),
		Logger:         s.logger,
	})
	limited := s.refreshRateLimit(handler)
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		r.Method(method, mcpHTTPPath, limited)
	}
}

// mcpBaseURLForRequest points tool calls at the control plane itself. Tests
// without a configured port fall back to the request origin.
func (s *Server) mcpBaseURLForRequest(r *http.Request) string {
	if s.mcpBaseURL != "" {
		return s.mcpBaseURL
	}
	scheme := "http"
	if s.isSecureRequest(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) mcpAllowedOrigins(ctx context.Context) []string {
	if s.instanceSvc == nil {
		return nil
	}
	urls, err := s.instanceSvc.ControlPlaneURLs(ctx)
	if err != nil {
		return nil
	}
	return urls
}

// mcpOriginAllowed rejects a present Origin that is not the control plane
// itself. Requests without an Origin (CLI and agent clients) are accepted.
func mcpOriginAllowed(r *http.Request, allowed []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}
	for _, candidate := range allowed {
		allowedURL, err := url.Parse(candidate)
		if err != nil || allowedURL.User != nil || allowedURL.RawQuery != "" || allowedURL.Fragment != "" ||
			(allowedURL.Path != "" && allowedURL.Path != "/") {
			continue
		}
		if strings.EqualFold(parsed.Scheme, allowedURL.Scheme) && strings.EqualFold(parsed.Host, allowedURL.Host) {
			return true
		}
	}
	return false
}

func requireAPIToken(tokens TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok || !strings.HasPrefix(token, auth.APITokenPrefix) {
				writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
				return
			}
			authenticateAPIToken(tokens, w, r, next, token)
		})
	}
}
