package proxy

import (
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// goldenRedirectID is a fixed rule id so generated names are stable.
const goldenRedirectID = "dddddddd-1111-2222-3333-444444444444"

// goldenRedirectYAML is the dynamic document for the redirect below: one
// router + middleware pair on the web entrypoint, and the shared service with
// no servers that Traefik requires on the router.
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
        regex: (?i)^http://old\.example\.com\.?(?::[^/]*)?/(.*)
        replacement: https://new.example.com/${1}
        permanent: true
`

// goldenRedirectTOML is the TOML form of goldenRedirectYAML.
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
regex = '(?i)^http://old\.example\.com\.?(?::[^/]*)?/(.*)'
replacement = 'https://new.example.com/${1}'
permanent = true
`

// goldenRedirect is the generation input the redirect goldens render from.
func goldenRedirect() Redirect {
	return Redirect{
		ID:           uuid.MustParse(goldenRedirectID),
		Source:       "old.example.com",
		Target:       "new.example.com",
		Code:         RedirectCodePermanent,
		PreservePath: true,
	}
}

// TestGenerateRedirectGolden locks the generated router/middleware/service
// trio for a path-preserving permanent redirect in both document syntaxes.
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

// TestBuildConfigRedirectTerminatesWithoutBackend proves the two safety
// properties of a redirect router: it references a service with no servers
// (so it can never proxy anywhere), and its only middleware is the
// redirectRegex that terminates the request before the service is consulted.
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

// TestBuildConfigRedirectPathAndCodeMapping proves the preserve_path and code
// mapping onto Traefik's redirectRegex semantics.
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
			wantRegex:     `(?i)^http://a\.example\.com\.?(?::[^/]*)?/(.*)`,
			wantReplace:   "https://b.example.com/${1}",
			wantPermanent: false,
		},
		{
			name:          "drop path, permanent",
			redirect:      Redirect{ID: id, Source: "a.example.com", Target: "b.example.com", Code: RedirectCodePermanent, PreservePath: false},
			wantRegex:     `(?i)^http://a\.example\.com\.?(?::[^/]*)?/.*`,
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

// TestBuildConfigWithoutRedirectsIsUnchanged proves a redirect-free node emits
// no service and no middleware, so BE-6.1/BE-6.2 documents are untouched.
func TestBuildConfigWithoutRedirectsIsUnchanged(t *testing.T) {
	cfg := BuildConfig(nil, nil, nil, "", "")
	if len(cfg.Services) != 0 || len(cfg.Middlewares) != 0 || len(cfg.Routers) != 0 {
		t.Fatalf("empty state emitted routers=%d services=%d middlewares=%d", len(cfg.Routers), len(cfg.Services), len(cfg.Middlewares))
	}
}

// TestGenerateRedirectsDeterministic proves redirect generation is
// order-independent: the same rules in any order yield identical bytes.
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

// TestRedirectRegexEscapesHost proves a host with regex metacharacters can
// never escape the match expression (the domain validator already forbids
// them, and the generator quotes defensively).
func TestRedirectRegexEscapesHost(t *testing.T) {
	pattern := RedirectRegexPattern("a.b.example.com", true)
	if !strings.Contains(pattern, `a\.b\.example\.com`) {
		t.Fatalf("pattern = %q, want quoted host dots", pattern)
	}
}

// TestRedirectRegexMatchesEveryRouterHostForm proves the match expression
// terminates for every host form Traefik's Host(source) router accepts but the
// raw URL retains: an optional port (numeric, empty or otherwise, because the
// router strips it with net.SplitHostPort which accepts non-numeric ports), one
// optional fully-qualified trailing dot, and any case. A form the router
// accepts but the regex misses would fall through to the backend-less service,
// violating the always-terminates guarantee.
func TestRedirectRegexMatchesEveryRouterHostForm(t *testing.T) {
	pattern := regexp.MustCompile(RedirectRegexPattern("old.example.com", true))

	matches := []string{
		"http://old.example.com/path?q=1",
		"http://OLD.EXAMPLE.COM/path",
		"http://old.example.com:80/path",
		"http://old.example.com:/path",
		"http://old.example.com:abc/path",
		"http://old.example.com./path",
		"http://old.example.com.:80/path",
		"http://Old.Example.Com:8080/path",
	}
	for _, rawURL := range matches {
		if !pattern.MatchString(rawURL) {
			t.Errorf("pattern %q does not match router-accepted URL %q", pattern, rawURL)
		}
	}

	// Forms the router does not route to this rule must not be redirected by
	// the regex either: a different host, a longer host, an extra label, or
	// more than one trailing dot.
	nonMatches := []string{
		"http://elsewhere.example.com/path",
		"http://old.example.comx/path",
		"http://old.example.com.evil/path",
		"http://old.example.com../path",
		"http://sub.old.example.com/path",
	}
	for _, rawURL := range nonMatches {
		if pattern.MatchString(rawURL) {
			t.Errorf("pattern %q unexpectedly matches %q", pattern, rawURL)
		}
	}
}

// TestRedirectRegexCapturesPathAndQuery proves the captured group keeps the
// path and query, including for the ported and dotted host forms.
func TestRedirectRegexCapturesPathAndQuery(t *testing.T) {
	pattern := regexp.MustCompile(RedirectRegexPattern("old.example.com", true))
	for _, rawURL := range []string{
		"http://old.example.com/a/b?q=1",
		"http://old.example.com:80/a/b?q=1",
		"http://old.example.com./a/b?q=1",
	} {
		match := pattern.FindStringSubmatch(rawURL)
		if len(match) != 2 || match[1] != "a/b?q=1" {
			t.Errorf("capture of %q = %#v, want the path and query", rawURL, match)
		}
	}
}
