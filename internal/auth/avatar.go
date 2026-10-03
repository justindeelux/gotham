package auth

import (
	"net/url"
	"strings"
	"unicode"
)

// avatarImageHosts is the single allowlist for OAuth avatar images, shared by
// the CSP builder (internal/server/security.go) and the storage validator
// below so the two cannot diverge: a stored avatar always renders under the
// served policy. The only OAuth provider (github.go) serves avatar_url from
// GitHub's avatar CDN, so exactly that host is allowed and nothing else.
// GitLab OAuth is not implemented; adding it means extending this list.
var avatarImageHosts = []string{"avatars.githubusercontent.com"}

// maxAvatarURLLength bounds a stored avatar URL. Provider URLs are short; the
// cap keeps a hostile provider response from bloating the users row.
const maxAvatarURLLength = 512

// AvatarImgSources returns the img-src source expressions for the avatar
// allowlist (one "https://host" per host). The CSP builder uses it verbatim.
func AvatarImgSources() []string {
	sources := make([]string, 0, len(avatarImageHosts))
	for _, host := range avatarImageHosts {
		sources = append(sources, "https://"+host)
	}
	return sources
}

// AvatarAllowedHosts returns the allowlisted avatar hosts for tests.
func AvatarAllowedHosts() []string {
	hosts := make([]string, len(avatarImageHosts))
	copy(hosts, avatarImageHosts)
	return hosts
}

// SanitizeAvatarURL validates a provider-supplied avatar URL before it is
// stored. It accepts only https URLs whose host is exactly on the allowlist,
// with no userinfo, no explicit port, no control characters and a sane length.
// Anything else reports ok=false and the caller must store nothing (NULL) and
// continue the login: a hostile or malformed provider value must never block
// authentication nor land in the users row.
func SanitizeAvatarURL(raw string) (sanitized string, ok bool) {
	if raw == "" || len(raw) > maxAvatarURLLength {
		return "", false
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return "", false
	}
	if u.User != nil {
		return "", false
	}
	if u.Port() != "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	for _, allowed := range avatarImageHosts {
		if host == allowed {
			return raw, true
		}
	}
	return "", false
}
