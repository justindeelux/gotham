package auth

import (
	"net/url"
	"strings"
	"unicode"
)

var avatarImageHosts = []string{"avatars.githubusercontent.com"}

const maxAvatarURLLength = 512

func AvatarImgSources() []string {
	sources := make([]string, 0, len(avatarImageHosts))
	for _, host := range avatarImageHosts {
		sources = append(sources, "https://"+host)
	}
	return sources
}

func AvatarAllowedHosts() []string {
	hosts := make([]string, len(avatarImageHosts))
	copy(hosts, avatarImageHosts)
	return hosts
}

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
