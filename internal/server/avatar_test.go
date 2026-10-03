package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/justindeelux/gotham/internal/auth"
)

// TestContentSecurityPolicyMatchesAvatarAllowlist proves the served img-src
// hosts are exactly the validator's allowlist: the two share one definition
// (auth.AvatarImgSources), so a stored avatar always renders and no other
// host can sneak into the policy.
func TestContentSecurityPolicyMatchesAvatarAllowlist(t *testing.T) {
	s := newTestAuthServer(t)

	rec := doRequest(t, s, http.MethodGet, "/healthz", "", "")
	got := rec.Header().Get("Content-Security-Policy")

	imgSrc := got[strings.Index(got, "img-src ")+len("img-src "):]
	imgSrc = imgSrc[:strings.Index(imgSrc, ";")]
	var hosts []string
	for _, token := range strings.Fields(imgSrc) {
		if token == "'self'" || token == "data:" {
			continue
		}
		hosts = append(hosts, token)
	}
	if !reflect.DeepEqual(hosts, auth.AvatarImgSources()) {
		t.Errorf("img-src remote hosts = %q, want validator allowlist %q", hosts, auth.AvatarImgSources())
	}
}

// TestAuthMeReturnsAvatar proves /me exposes the stored avatar the header
// renders.
func TestAuthMeReturnsAvatar(t *testing.T) {
	s := newTestAuthServer(t)

	avatar := "https://avatars.githubusercontent.com/u/123456?v=4"
	fake, ok := s.auth.(*fakeAuthService)
	if !ok {
		t.Fatalf("test server auth = %T, want *fakeAuthService", s.auth)
	}
	fake.user.Avatar = &avatar

	rec := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "Bearer valid-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var body meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode me body: %v", err)
	}
	if body.User == nil || body.User.Avatar == nil || *body.User.Avatar != avatar {
		t.Errorf("me body avatar = %+v, want %q", body.User, avatar)
	}
}
