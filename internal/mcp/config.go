// Package mcp implements the isolated stdio adapter to a fixed Gotham REST origin.
package mcp

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

// Config binds one process to an instance, API token, and explicit team.
type Config struct {
	URL    string
	Token  string
	TeamID uuid.UUID
}

// ConfigFromEnv reads only MCP settings, never control-plane configuration.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	team, err := uuid.Parse(getenv("GOTHAM_MCP_TEAM_ID"))
	if err != nil || team == uuid.Nil {
		return Config{}, errors.New("MCP requires a nonzero team UUID")
	}
	cfg := Config{URL: getenv("GOTHAM_MCP_URL"), Token: getenv("GOTHAM_MCP_TOKEN"), TeamID: team}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if c.TeamID == uuid.Nil {
		return errors.New("MCP requires a nonzero team UUID")
	}
	if len(c.Token) <= 4 || len(c.Token) > 4096 || !strings.HasPrefix(c.Token, "gt1_") || strings.ContainsFunc(c.Token, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return errors.New("MCP requires a gt1_ API token")
	}
	u, err := url.Parse(c.URL)
	if err != nil || len(c.URL) > 2048 || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(c.URL, "#") || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.Opaque != "" {
		return errors.New("MCP requires an origin URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if u.Scheme != "http" || err != nil || !ip.IsLoopback() {
		return errors.New("MCP requires HTTPS or literal loopback HTTP")
	}
	return nil
}
