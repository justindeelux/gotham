package proxy

import (
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const goldenRedirectID = "dddddddd-1111-2222-3333-444444444444"

const goldenRedirectYAML = `http:
  routers:
    gotham-redirect-dddddddd-1111-2222-3333-444444444444:
      rule: Host(§old.example.com§)
      service: gotham-redirect-noop
      entryPoints:
        - web
      middlewares:
        - gotham-redirect-dddddddd-1111-2222-3333-444444444444
  services:
    gotham-redirect-noop:
      loadBalancer:
        servers: []
  middlewares:
    gotham-redirect-dddddddd-1111-2222-3333-444444444444:
      redirectRegex:
        regex: (?i)^https?://[^/?#]*(.*)$
        replacement: https://new.example.com${1}
        permanent: true
`

const goldenRedirectTOML = `[http]
[http.routers]
[http.routers.gotham-redirect-dddddddd-1111-2222-3333-444444444444]
rule = 'Host(§old.example.com§)'
service = 'gotham-redirect-noop'
entryPoints = ['web']
middlewares = ['gotham-redirect-dddddddd-1111-2222-3333-444444444444']

[http.services]
[http.services.gotham-redirect-noop]
[http.services.gotham-redirect-noop.loadBalancer]
servers = []

[http.middlewares]
[http.middlewares.gotham-redirect-dddddddd-1111-2222-3333-444444444444]
[http.middlewares.gotham-redirect-dddddddd-1111-2222-3333-444444444444.redirectRegex]
regex = '(?i)^https?://[^/?#]*(.*)$'
replacement = 'https://new.example.com${1}'
permanent = true
`

func goldenRedirect() Redirect {
	return Redirect{
		ID:           uuid.MustParse(goldenRedirectID),
		Source:       "old.example.com",
		Target:       "new.example.com",
		Code:         RedirectCodePermanent,
		PreservePath: true,
	}
}

func TestGenerateRedirectGolden(t *testing.T) {
	cases := []struct {
		format  Format
		dynamic string
	}{
		{format: FormatYAML, dynamic: goldenRedirectYAML},
		{format: FormatTOML, dynamic: goldenRedirectTOML},
	}
	for _, tc := range cases {
		files, err := Generate(BuildConfig(nil, []Redirect{goldenRedirect()}, nil, "", ""), tc.format)
		if err != nil {
			t.Fatalf("%s: Generate: %v", tc.format, err)
		}
		if len(files) != 2 {
			t.Fatalf("%s: files = %d, want 2", tc.format, len(files))
		}
		if got := string(files[1].Content); got != expandGolden(tc.dynamic) {
			t.Errorf("%s: dynamic document mismatch\n--- got ---\n%s\n--- want ---\n%s", tc.format, got, expandGolden(tc.dynamic))
		}
	}
}

func TestBuildConfigRedirectTerminatesWithoutBackend(t *testing.T) {
	redirect := goldenRedirect()
	cfg := BuildConfig(nil, []Redirect{redirect}, nil, "", "")

	name := RedirectNamePrefix + redirect.ID.String()
	router, ok := cfg.Routers[name]
	if !ok {
		t.Fatalf("missing redirect router %q in %#v", name, cfg.Routers)
	}
	if router.Rule != "Host(`old.example.com`)" || router.Service != RedirectServiceName {
		t.Errorf("router = %#v, want Host rule and service %q", router, RedirectServiceName)
	}
	if len(router.EntryPoints) != 1 || router.EntryPoints[0] != EntryPointWeb {
		t.Errorf("entryPoints = %#v, want [web]", router.EntryPoints)
	}
	if router.TLS != nil {
		t.Errorf("redirect router must not carry TLS: %#v", router.TLS)
	}
	if len(router.Middlewares) != 1 || router.Middlewares[0] != name {
		t.Fatalf("middlewares = %#v, want only %q", router.Middlewares, name)
	}
	for _, middleware := range router.Middlewares {
		if middleware == HTTPRedirectMiddleware {
			t.Fatal("redirect router must not carry the shared HTTP→HTTPS middleware")
		}
	}
	middleware, ok := cfg.Middlewares[name]
	if !ok || middleware.RedirectRegex == nil {
		t.Fatalf("middleware %q = %#v, want a redirectRegex (the terminating middleware)", name, middleware)
	}
	if middleware.RedirectScheme != nil {
		t.Errorf("redirect middleware must not also redirect the scheme: %#v", middleware.RedirectScheme)
	}

	service, ok := cfg.Services[RedirectServiceName]
	if !ok {
		t.Fatalf("missing shared service %q", RedirectServiceName)
	}
	if len(service.LoadBalancer.Servers) != 0 {
		t.Fatalf("redirect service backends = %#v, want none (the middleware always terminates)", service.LoadBalancer.Servers)
	}
}

func TestBuildConfigRedirectPathAndCodeMapping(t *testing.T) {
	id := uuid.MustParse("eeeeeeee-1111-2222-3333-444444444444")
	cases := []struct {
		name          string
		redirect      Redirect
		wantRegex     string
		wantReplace   string
		wantPermanent bool
	}{
		{
			name:          "preserve path, temporary",
			redirect:      Redirect{ID: id, Source: "a.example.com", Target: "b.example.com", Code: RedirectCodeTemporary, PreservePath: true},
			wantRegex:     `(?i)^https?://[^/?#]*(.*)$`,
			wantReplace:   "https://b.example.com${1}",
			wantPermanent: false,
		},
		{
			name:          "drop path, permanent",
			redirect:      Redirect{ID: id, Source: "a.example.com", Target: "b.example.com", Code: RedirectCodePermanent, PreservePath: false},
			wantRegex:     `(?i)^https?://.*$`,
			wantReplace:   "https://b.example.com/",
			wantPermanent: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := BuildConfig(nil, []Redirect{tc.redirect}, nil, "", "")
			middleware := cfg.Middlewares[RedirectNamePrefix+id.String()].RedirectRegex
			if middleware == nil {
				t.Fatal("missing redirectRegex middleware")
			}
			if middleware.Regex != tc.wantRegex {
				t.Errorf("regex = %q, want %q", middleware.Regex, tc.wantRegex)
			}
			if middleware.Replacement != tc.wantReplace {
				t.Errorf("replacement = %q, want %q", middleware.Replacement, tc.wantReplace)
			}
			if middleware.Permanent != tc.wantPermanent {
				t.Errorf("permanent = %v, want %v", middleware.Permanent, tc.wantPermanent)
			}
		})
	}
}

func TestBuildConfigWithoutRedirectsIsUnchanged(t *testing.T) {
	cfg := BuildConfig(nil, nil, nil, "", "")
	if len(cfg.Services) != 0 || len(cfg.Middlewares) != 0 || len(cfg.Routers) != 0 {
		t.Fatalf("empty state emitted routers=%d services=%d middlewares=%d", len(cfg.Routers), len(cfg.Services), len(cfg.Middlewares))
	}
}

func TestGenerateRedirectsDeterministic(t *testing.T) {
	first := uuid.MustParse(goldenRedirectID)
	second := uuid.MustParse("eeeeeeee-1111-2222-3333-444444444444")
	redirects := []Redirect{
		{ID: first, Source: "one.example.com", Target: "target.example.com", Code: RedirectCodePermanent, PreservePath: true},
		{ID: second, Source: "two.example.com", Target: "target.example.com", Code: RedirectCodeTemporary, PreservePath: false},
	}
	reversed := []Redirect{redirects[1], redirects[0]}
	for _, format := range []Format{FormatYAML, FormatTOML} {
		firstRun, err := Generate(BuildConfig(nil, redirects, nil, "", ""), format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		secondRun, err := Generate(BuildConfig(nil, reversed, nil, "", ""), format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		for i := range firstRun {
			if string(firstRun[i].Content) != string(secondRun[i].Content) {
				t.Errorf("%s: %s differs between rule orders", format, firstRun[i].Name)
			}
		}
	}
}

func TestRedirectRegexIsAuthorityAgnostic(t *testing.T) {
	pattern := regexp.MustCompile(RedirectRegexPattern(true))
	if strings.Contains(pattern.String(), "example.com") {
		t.Fatalf("pattern %q encodes the host; the router rule is the only host authority", pattern)
	}

	matches := []string{
		"http://old.example.com/path?q=1",
		"http://OLD.EXAMPLE.COM/path",
		"http://old.example.com:80/path",
		"http://old.example.com:/path",
		"http://old.example.com:abc/path",
		"http://old.example.com./path",
		"http://old.example.com.:80/path",
		"http://Old.Example.Com:8080/path",
		"http://[old.example.com]:80/path",
		"http://[old.example.com]/path",
		"http://[OLD.EXAMPLE.COM.]:abc/path",
		"https://old.example.com/path?q=1",
		"http://old.example.com",
		"http://old.example.com?q=1",
	}
	for _, rawURL := range matches {
		if !pattern.MatchString(rawURL) {
			t.Errorf("pattern %q does not match router-accepted URL %q", pattern, rawURL)
		}
	}

	nonMatches := []string{
		"ftp://old.example.com/path",
		"old.example.com/path",
		"http:/old.example.com/path",
		"//old.example.com/path",
	}
	for _, rawURL := range nonMatches {
		if pattern.MatchString(rawURL) {
			t.Errorf("pattern %q unexpectedly matches %q", pattern, rawURL)
		}
	}

	dropPath := regexp.MustCompile(RedirectRegexPattern(false))
	for _, rawURL := range matches {
		if !dropPath.MatchString(rawURL) {
			t.Errorf("drop-path pattern %q does not match %q", dropPath, rawURL)
		}
	}
}

func TestRedirectRegexCapturesPathAndQuery(t *testing.T) {
	pattern := regexp.MustCompile(RedirectRegexPattern(true))
	for _, rawURL := range []string{
		"http://old.example.com/a/b?q=1",
		"http://old.example.com:80/a/b?q=1",
		"http://old.example.com./a/b?q=1",
		"http://[old.example.com]:80/a/b?q=1",
		"https://OLD.EXAMPLE.COM./a/b?q=1",
	} {
		match := pattern.FindStringSubmatch(rawURL)
		if len(match) != 2 || match[1] != "/a/b?q=1" {
			t.Errorf("capture of %q = %#v, want the path and query", rawURL, match)
		}
	}
}
