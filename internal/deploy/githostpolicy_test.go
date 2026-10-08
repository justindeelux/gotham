package deploy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// policyTestLookup resolves the table's DNS names to fixed answers without
// DNS: public names to TEST-NET-3, git.internal to the cloud metadata
// address (so the table pins that resolution defers to clone time and then
// denies), and everything else to an error (fail-closed resolution).
func policyTestLookup(_ context.Context, host string) ([]net.IP, error) {
	switch host {
	case "github.com", "git.example", "gitea.example":
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	case "git.internal":
		return []net.IP{net.ParseIP("169.254.169.254")}, nil
	default:
		return nil, errors.New("no such host")
	}
}

// gitHostPolicyRow is one entry of the shared allow/deny table. The same row
// runs through the creation validators (public, private and deploy gates)
// and the dial-time pin, so the two stages cannot drift: literals and tricks
// refuse early, DNS names defer to the pin, and the pin refuses what the
// validator could not see.
type gitHostPolicyRow struct {
	name string
	url  string
	// allowPrivate and devLocal select the operator settings for the row.
	allowPrivate bool
	devLocal     bool
	// wantPublic, wantPrivate and wantDeploy are the creation-gate outcomes;
	// wantPin is the dial-time outcome through policyTestLookup.
	wantPublic  bool
	wantPrivate bool
	wantDeploy  bool
	wantPin     bool
}

// gitHostPolicyTable is the shared table: allowed/denied URLs including all
// the numeric tricks, used by both the validators and the clone paths.
var gitHostPolicyTable = []gitHostPolicyRow{
	{name: "public https", url: "https://github.com/acme/demo.git", wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "public https with port", url: "https://git.example:8443/acme/demo.git", wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "git scheme", url: "git://git.example/acme/demo.git", wantPublic: true, wantPrivate: false, wantDeploy: true, wantPin: true},
	{name: "ssh scheme", url: "ssh://git@git.example/acme/demo.git", wantPublic: false, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "ssh with port", url: "ssh://git@git.example:2222/acme/demo.git", wantPublic: false, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "scp-like", url: "git@git.example:acme/demo.git", wantPublic: false, wantPrivate: true, wantDeploy: true, wantPin: true},

	// Internal names defer: no literal to judge, so creation allows and the
	// pin refuses the metadata address they resolve to.
	{name: "internal name defers to pin", url: "https://git.internal/acme/demo.git", wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: false},
	{name: "internal ssh defers to pin", url: "git@git.internal:acme/demo.git", wantPublic: false, wantPrivate: true, wantDeploy: true, wantPin: false},

	// Loopback literals refuse at creation already.
	{name: "loopback v4", url: "http://127.0.0.1/acme/demo.git", wantPin: false},
	{name: "loopback with port", url: "https://127.0.0.1:8443/acme/demo.git", wantPin: false},
	{name: "loopback v6", url: "http://[::1]/acme/demo.git", wantPin: false},
	{name: "localhost", url: "http://localhost/acme/demo.git", wantPin: false},
	{name: "localhost subdomain", url: "http://git.localhost/acme/demo.git", wantPin: false},

	// Numeric-host tricks: decimal, hex, octal, shorthand and mapped forms.
	{name: "decimal loopback", url: "http://2130706433/acme/demo.git", wantPin: false},
	{name: "hex loopback", url: "http://0x7f000001/acme/demo.git", wantPin: false},
	{name: "octal loopback", url: "http://017700000001/acme/demo.git", wantPin: false},
	{name: "hex parts loopback", url: "http://0x7f.0.0.1/acme/demo.git", wantPin: false},
	{name: "octal parts loopback", url: "http://0177.0.0.1/acme/demo.git", wantPin: false},
	{name: "shorthand loopback", url: "http://127.1/acme/demo.git", wantPin: false},
	{name: "mixed hex loopback", url: "http://0x7f.1/acme/demo.git", wantPin: false},
	{name: "small decimal is this-network", url: "http://12345/acme/demo.git", wantPin: false},
	{name: "invalid numeric octet", url: "http://999.1.1.1/acme/demo.git", wantPin: false},
	{name: "mapped loopback", url: "http://[::ffff:127.0.0.1]/acme/demo.git", wantPin: false},
	{name: "hex private", url: "http://0xC0.0xA8.0x01.0x01/acme/demo.git", wantPin: false},
	{name: "scp loopback", url: "git@127.0.0.1:acme/demo.git", wantPublic: false, wantPrivate: false, wantDeploy: false, wantPin: false},
	{name: "ssh private", url: "ssh://git@10.0.0.5/acme/demo.git", wantPublic: false, wantPrivate: false, wantDeploy: false, wantPin: false},

	// Private ranges refuse by default.
	{name: "rfc1918 10/8", url: "http://10.0.0.5/acme/demo.git", wantPin: false},
	{name: "rfc1918 192.168/16", url: "https://192.168.1.10/acme/demo.git", wantPin: false},
	{name: "rfc1918 172.16/12", url: "http://172.16.5.4/acme/demo.git", wantPin: false},
	{name: "ula", url: "http://[fd00::1]/acme/demo.git", wantPin: false},
	{name: "mapped private", url: "http://[::ffff:10.0.0.1]/acme/demo.git", wantPin: false},

	// Always-denied ranges: link-local, unspecified, multicast, CGNAT,
	// benchmarking, NAT64, 6to4.
	{name: "metadata address", url: "http://169.254.169.254/latest/meta-data/", wantPin: false},
	{name: "link-local v6", url: "http://[fe80::1]/acme/demo.git", wantPin: false},
	{name: "zone id", url: "https://[fe80::1%25eth0]/acme/demo.git", wantPin: false},
	{name: "unspecified v4", url: "http://0.0.0.0/acme/demo.git", wantPin: false},
	{name: "unspecified v6", url: "http://[::]/acme/demo.git", wantPin: false},
	{name: "multicast v4", url: "http://224.0.0.1/acme/demo.git", wantPin: false},
	{name: "multicast v6", url: "http://[ff02::1]/acme/demo.git", wantPin: false},
	{name: "carrier-grade nat", url: "http://100.64.0.1/acme/demo.git", wantPin: false},
	{name: "benchmarking", url: "http://198.18.0.1/acme/demo.git", wantPin: false},
	{name: "nat64", url: "http://[64:ff9b::7f00:1]/acme/demo.git", wantPin: false},
	{name: "nat64 local-use", url: "http://[64:ff9b:1::7f00:1]/acme/demo.git", wantPin: false},
	{name: "ipv4-compatible", url: "http://[::7f00:1]/acme/demo.git", wantPin: false},
	{name: "6to4", url: "http://[2002:7f00:1::1]/acme/demo.git", wantPin: false},

	// Credentials stay a creation refusal, never a dial-time surprise: the
	// deploy gate keeps accepting the token-bearing shape the GS-5 flow
	// builds internally, and the pin judges its host.
	{name: "embedded token", url: "https://user:ghp_SECRET@git.example/acme/demo.git", wantPublic: false, wantPrivate: false, wantDeploy: true, wantPin: true},

	// Unresolvable names pass creation (no DNS there) and refuse at the pin.
	{name: "unresolvable defers to pin", url: "https://git.example.invalid/acme/demo.git", wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: false},

	// The dev escape hatch keeps working for local fixtures.
	{name: "local path dev", url: "/srv/fixtures/demo", devLocal: true, wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "file url dev", url: "file:///srv/fixtures/demo", devLocal: true, wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "local path default", url: "/srv/fixtures/demo", wantPublic: false, wantPrivate: false, wantDeploy: false, wantPin: true},

	// The allow-private setting lifts RFC1918, ULA and loopback only.
	{name: "private allowed 10/8", url: "http://10.0.0.5/acme/demo.git", allowPrivate: true, wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "private allowed 192.168/16", url: "https://192.168.1.10/acme/demo.git", allowPrivate: true, wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "private allowed loopback", url: "http://127.0.0.1/acme/demo.git", allowPrivate: true, wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "private allowed loopback v6", url: "http://[::1]/acme/demo.git", allowPrivate: true, wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "private allowed ula", url: "http://[fd00::1]/acme/demo.git", allowPrivate: true, wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "private allowed hex private", url: "http://0xC0.0xA8.0x01.0x01/acme/demo.git", allowPrivate: true, wantPublic: true, wantPrivate: true, wantDeploy: true, wantPin: true},
	{name: "allow keeps metadata denied", url: "http://169.254.169.254/latest/meta-data/", allowPrivate: true, wantPin: false},
	{name: "allow keeps link-local denied", url: "http://[fe80::1]/acme/demo.git", allowPrivate: true, wantPin: false},
	{name: "allow keeps cgnat denied", url: "http://100.64.0.1/acme/demo.git", allowPrivate: true, wantPin: false},
	{name: "allow keeps unspecified denied", url: "http://0.0.0.0/acme/demo.git", allowPrivate: true, wantPin: false},
	{name: "allow keeps multicast denied", url: "http://224.0.0.1/acme/demo.git", allowPrivate: true, wantPin: false},
	{name: "allow keeps nat64 denied", url: "http://[64:ff9b::7f00:1]/acme/demo.git", allowPrivate: true, wantPin: false},
	{name: "allow keeps nat64 local-use denied", url: "http://[64:ff9b:1::7f00:1]/acme/demo.git", allowPrivate: true, wantPin: false},
	{name: "allow keeps ipv4-compatible denied", url: "http://[::7f00:1]/acme/demo.git", allowPrivate: true, wantPin: false},
	{name: "allow keeps localhost denied", url: "http://localhost/acme/demo.git", allowPrivate: true, wantPin: false},
}

// TestGitHostPolicyTable runs the shared allow/deny table through the
// creation validators and the dial-time pin: one list drives both stages.
func TestGitHostPolicyTable(t *testing.T) {
	for _, row := range gitHostPolicyTable {
		t.Run(row.name, func(t *testing.T) {
			if row.allowPrivate {
				t.Setenv(GitAllowPrivateHostsEnv, "true")
			} else {
				t.Setenv(GitAllowPrivateHostsEnv, "false")
			}
			if row.devLocal {
				t.Setenv(devLocalCloneEnv, "true")
			} else {
				t.Setenv(devLocalCloneEnv, "false")
			}
			check := func(what string, err error, want bool) {
				t.Helper()
				if want && err != nil {
					t.Errorf("%s(%q) = %v, want nil", what, row.url, err)
				}
				if !want && err == nil {
					t.Errorf("%s(%q) = nil, want an error", what, row.url)
				}
				if err != nil && !errors.Is(err, ErrValidation) {
					t.Errorf("%s(%q) = %v, want it wrapped in ErrValidation", what, row.url, err)
				}
			}
			check("ValidatePublicGitURL", ValidatePublicGitURL(row.url), row.wantPublic)
			check("ValidatePrivateGitURL", ValidatePrivateGitURL(row.url), row.wantPrivate)
			check("validateCloneURL", validateCloneURL(row.url), row.wantDeploy)
			_, err := pinGitRemoteHost(context.Background(), policyTestLookup, row.url, nil)
			check("pinGitRemoteHost", err, row.wantPin)
		})
	}
}

// TestGitCloneRefusesBlockedHost pins the clone gate end to end: a blocked
// remote fails validation before git runs, for every transport shape.
func TestGitCloneRefusesBlockedHost(t *testing.T) {
	t.Setenv(GitAllowPrivateHostsEnv, "false")
	for _, url := range []string{
		"https://127.0.0.1/acme/demo.git",
		"http://2130706433/acme/demo.git",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.5/acme/demo.git",
		"ssh://git@10.0.0.5/acme/demo.git",
		"git@127.0.0.1:acme/demo.git",
	} {
		t.Run(url, func(t *testing.T) {
			app := testApplication(uuid.New())
			app.SourceType = SourceGitPublic
			app.Provider = ""
			app.CloneURL = url
			app.Branch = "main"
			ran := false
			source := gitSource{
				keys:   &staticKeyResolver{},
				lookup: policyTestLookup,
				run: func(context.Context, []string, []string) ([]byte, error) {
					ran = true
					return nil, nil
				},
			}
			err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if ran {
				t.Error("git ran for a blocked host")
			}
		})
	}
}

// redirectFixture is a two-server redirect oracle for the HIGH finding:
// the first answers every request with a 302 to the second, which records
// every hit. A git run that refuses redirects fails at the first server and
// never touches the second; a run that follows them (the bypass) hits both.
type redirectFixture struct {
	first  *httptest.Server
	second *httptest.Server

	mu         sync.Mutex
	firstHits  int
	secondHits int
}

// newRedirectFixture starts the oracle pair. Both serve on loopback, so
// callers set the allow-private setting (the point under test is the
// redirect, not the loopback block).
func newRedirectFixture(t *testing.T) *redirectFixture {
	t.Helper()
	fix := &redirectFixture{}
	fix.second = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fix.mu.Lock()
		fix.secondHits++
		fix.mu.Unlock()
		http.Error(w, "must not be reached", http.StatusForbidden)
	}))
	t.Cleanup(fix.second.Close)
	fix.first = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fix.mu.Lock()
		fix.firstHits++
		fix.mu.Unlock()
		http.Redirect(w, r, fix.second.URL+r.URL.RequestURI(), http.StatusFound)
	}))
	t.Cleanup(fix.first.Close)
	return fix
}

// hits returns the per-server request counts.
func (f *redirectFixture) hits() (first, second int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.firstHits, f.secondHits
}

// requireRealGit skips the caller without a git binary: the redirect tests
// prove the production git/curl behaviour, which a fake runner cannot.
func requireRealGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is not available")
	}
}

// TestGitCloneRefusesRedirect pins the HIGH fix on the clone path: a public
// URL answering 302 to an internal host fails at the redirector — the
// second server never receives a request, and the error quotes the 302
// (proving git reached the redirector) without the redirect target.
func TestGitCloneRefusesRedirect(t *testing.T) {
	requireRealGit(t)
	t.Setenv(GitAllowPrivateHostsEnv, "true")
	fix := newRedirectFixture(t)

	app := testApplication(uuid.New())
	app.SourceType = SourceGitPublic
	app.Provider = ""
	app.CloneURL = fix.first.URL + "/repo.git"
	app.Branch = "main"
	err := (gitSource{}).Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil)
	if err == nil {
		t.Fatal("Clone = nil, want the refused redirect to fail")
	}
	if !strings.Contains(err.Error(), "302") {
		t.Errorf("err = %v, want it to quote the refused 302", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "redirecting to") {
		t.Errorf("err leaked the redirect target: %v", err)
	}
	first, second := fix.hits()
	if first == 0 {
		t.Error("the redirector saw no request: the test proved nothing")
	}
	if second != 0 {
		t.Errorf("the redirect target saw %d requests, want 0", second)
	}
}

// TestGitLsRemoteRefusesRedirect pins the HIGH fix on the default-branch
// resolution path: with no branch pinned, the ls-remote fails at the
// redirector before any clone runs.
func TestGitLsRemoteRefusesRedirect(t *testing.T) {
	requireRealGit(t)
	t.Setenv(GitAllowPrivateHostsEnv, "true")
	fix := newRedirectFixture(t)

	app := testApplication(uuid.New())
	app.SourceType = SourceGitPublic
	app.Provider = ""
	app.CloneURL = fix.first.URL + "/repo.git"
	app.Branch = ""
	err := (gitSource{}).Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil)
	if err == nil {
		t.Fatal("Clone = nil, want the refused redirect to fail")
	}
	if !strings.HasPrefix(err.Error(), "git ls-remote:") {
		t.Errorf("err = %v, want the ls-remote step to fail first", err)
	}
	if !strings.Contains(err.Error(), "302") {
		t.Errorf("err = %v, want it to quote the refused 302", err)
	}
	first, second := fix.hits()
	if first == 0 {
		t.Error("the redirector saw no request: the test proved nothing")
	}
	if second != 0 {
		t.Errorf("the redirect target saw %d requests, want 0", second)
	}
}

// TestGitProbeRefusesRedirect pins the HIGH fix on the Test-connection
// path: an anonymous https probe against a redirector fails without
// touching the redirect target and without echoing its URL.
func TestGitProbeRefusesRedirect(t *testing.T) {
	requireRealGit(t)
	t.Setenv(GitAllowPrivateHostsEnv, "true")
	fix := newRedirectFixture(t)

	userID := uuid.New()
	app := privateTestApp(userID, fix.first.URL+"/repo.git")
	repo := &fakeRepository{app: app}
	svc := NewService(Config{Repository: repo, Secret: testSecretKey, Logger: discardLogger()})
	t.Cleanup(func() { _ = svc.Close() })

	// The probe is throttled per caller+app; this service is fresh, so the
	// single probe below always runs.
	result, err := svc.TestGitConnection(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("TestGitConnection: %v", err)
	}
	if result.OK {
		t.Errorf("result = %+v, want the refused redirect to fail", result)
	}
	if strings.Contains(strings.ToLower(result.Message), "redirecting to") {
		t.Errorf("probe message leaked the redirect target: %q", result.Message)
	}
	first, second := fix.hits()
	if first == 0 {
		t.Error("the redirector saw no request: the test proved nothing")
	}
	if second != 0 {
		t.Errorf("the redirect target saw %d requests, want 0", second)
	}
}

// TestGitClonePinsPublicHost pins the TOCTOU mitigation: the ls-remote and
// the clone both carry the resolved address via http.curloptResolve while
// the log keeps the hostname.
func TestGitClonePinsPublicHost(t *testing.T) {
	t.Setenv(GitAllowPrivateHostsEnv, "false")
	const url = "https://git.example/acme/demo.git"
	app := testApplication(uuid.New())
	app.SourceType = SourceGitPublic
	app.Provider = ""
	app.CloneURL = url
	app.Branch = ""

	var envs [][]string
	var lines []string
	source := gitSource{
		lookup: policyTestLookup,
		run: func(_ context.Context, argv []string, env []string) ([]byte, error) {
			envs = append(envs, append([]string(nil), env...))
			if len(argv) > 1 && argv[1] == "ls-remote" {
				return []byte("ref: refs/heads/main\tHEAD\n"), nil
			}
			return nil, nil
		},
	}
	if err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), func(line string) {
		lines = append(lines, line)
	}); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if len(envs) != 2 {
		t.Fatalf("git ran %d times, want 2 (ls-remote and clone)", len(envs))
	}
	for i, env := range envs {
		joined := strings.Join(env, "\n")
		if !strings.Contains(joined, "http.curloptResolve") {
			t.Errorf("call %d env is missing the curloptResolve key:\n%s", i, joined)
		}
		if !strings.Contains(joined, "git.example:443:203.0.113.10") {
			t.Errorf("call %d env is missing the pinned address:\n%s", i, joined)
		}
		// The HIGH fix: every http(s) git invocation refuses redirects, on
		// the keyless path as well as the credential path.
		if value, ok := gitConfigValue(env, "http.followRedirects"); !ok || value != "false" {
			t.Errorf("call %d env http.followRedirects = %q, want false:\n%s", i, value, joined)
		}
	}
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, url) {
		t.Errorf("log = %q, want the hostname still logged", joined)
	}
}

// TestGitMixedARecordsRefused pins the any-denied refusal: one public and
// one blocked answer deny the whole host, however the answers are ordered.
func TestGitMixedARecordsRefused(t *testing.T) {
	t.Setenv(GitAllowPrivateHostsEnv, "false")
	mixed := func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.10"), net.ParseIP("10.0.0.5")}, nil
	}
	if _, err := pinGitRemoteHost(context.Background(), mixed, "https://git.example/acme/demo.git", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("pin = %v, want ErrValidation", err)
	} else if err.Error() != errGitHostNotAllowed.Error() {
		t.Errorf("err = %q, want the generic refusal", err)
	}

	ran := false
	app := testApplication(uuid.New())
	app.CloneURL = "https://git.example/acme/demo.git"
	app.Branch = "main"
	source := gitSource{
		lookup: mixed,
		run: func(context.Context, []string, []string) ([]byte, error) {
			ran = true
			return nil, nil
		},
	}
	if err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil); err == nil {
		t.Fatal("Clone = nil, want the mixed-answer refusal")
	}
	if ran {
		t.Error("git ran for a mixed-answer host")
	}
}

// TestParseGitAllowPrivateHosts pins the single flag reader: it matches the
// config-file path (viper/cast: ParseBool strings, nonzero numbers), so "1"
// and "true" agree on both readers and anything else stays denied.
func TestParseGitAllowPrivateHosts(t *testing.T) {
	for _, truthy := range []string{"true", "TRUE", "True", "1", "t", "T", "  true  "} {
		if !parseGitAllowPrivateHosts(truthy) {
			t.Errorf("parseGitAllowPrivateHosts(%q) = false, want true", truthy)
		}
	}
	for _, falsy := range []string{"", "false", "FALSE", "0", "f", "yes", "on", "2", "tru"} {
		if parseGitAllowPrivateHosts(falsy) {
			t.Errorf("parseGitAllowPrivateHosts(%q) = true, want false", falsy)
		}
	}
}

// TestParseGitVersion pins the `git --version` reader behind the pin
// startup warning, including trailing platform text.
func TestParseGitVersion(t *testing.T) {
	cases := []struct {
		in     string
		major  int
		minor  int
		wantOK bool
	}{
		{"git version 2.43.0", 2, 43, true},
		{"git version 2.37.1\n", 2, 37, true},
		{"git version 1.8.3.1", 1, 8, true},
		{"git version 2.43.0 (Apple Git-176)", 2, 43, true},
		{"", 0, 0, false},
		{"git version", 0, 0, false},
		{"git version x.y", 0, 0, false},
		{"not git at all", 0, 0, false},
	}
	for _, tc := range cases {
		major, minor, ok := parseGitVersion(tc.in)
		if ok != tc.wantOK || major != tc.major || minor != tc.minor {
			t.Errorf("parseGitVersion(%q) = (%d, %d, %v), want (%d, %d, %v)",
				tc.in, major, minor, ok, tc.major, tc.minor, tc.wantOK)
		}
	}
}

// TestGitRebindingRefused pins the DNS-rebinding guard: a host whose two
// answers differ never dials, even when both answers are public.
func TestGitRebindingRefused(t *testing.T) {
	t.Setenv(GitAllowPrivateHostsEnv, "false")
	var calls atomic.Int32
	flapping := func(context.Context, string) ([]net.IP, error) {
		if calls.Add(1)%2 == 1 {
			return []net.IP{net.ParseIP("203.0.113.10")}, nil
		}
		return []net.IP{net.ParseIP("203.0.113.11")}, nil
	}
	if _, err := pinGitRemoteHost(context.Background(), flapping, "https://git.example/acme/demo.git", nil); err == nil {
		t.Fatal("pin = nil, want the rebinding refusal")
	} else if !strings.Contains(err.Error(), "rebinding") {
		t.Fatalf("err = %v, want it to name DNS rebinding", err)
	}

	ran := false
	app := testApplication(uuid.New())
	app.CloneURL = "https://git.example/acme/demo.git"
	app.Branch = "main"
	source := gitSource{
		lookup: flapping,
		run: func(context.Context, []string, []string) ([]byte, error) {
			ran = true
			return nil, nil
		},
	}
	if err := source.Clone(context.Background(), app, filepath.Join(t.TempDir(), "repo"), nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if ran {
		t.Error("git ran for a rebinding host")
	}
}

// TestGitProbeRefusesBlockedHost pins the Test-connection gates: a blocked
// literal is a validation error, a name resolving to a blocked address is a
// failed outcome — and neither dials nor leaks the address.
func TestGitProbeRefusesBlockedHost(t *testing.T) {
	t.Setenv(GitAllowPrivateHostsEnv, "false")

	t.Run("blocked literal is a validation error", func(t *testing.T) {
		userID := uuid.New()
		app := privateTestApp(userID, "https://127.0.0.1/acme/demo.git")
		repo := &fakeRepository{app: app}
		ran := false
		svc := serviceWithRunner(t, repo, func(context.Context, []string, []string) ([]byte, error) {
			ran = true
			return nil, nil
		})
		_, err := svc.TestGitConnection(context.Background(), userID, app.ID)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		// The refusal reads exactly like every other policy denial: no
		// category, no address, no resolution signal.
		if err.Error() != errGitHostNotAllowed.Error() {
			t.Errorf("err = %q, want the generic refusal %q", err, errGitHostNotAllowed)
		}
		if strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("err leaked the address: %v", err)
		}
		if ran {
			t.Error("git ran for a blocked probe host")
		}
	})

	t.Run("blocked resolution is a failed outcome", func(t *testing.T) {
		userID := uuid.New()
		app := privateTestApp(userID, "https://git.internal/acme/demo.git")
		repo := &fakeRepository{app: app}
		ran := false
		run := func(context.Context, []string, []string) ([]byte, error) {
			ran = true
			return nil, nil
		}
		svc := NewService(Config{
			Repository: repo,
			Secret:     testSecretKey,
			Logger:     discardLogger(),
			Source:     gitSource{keys: repo, creds: repo, run: run},
			// git.internal resolves to the metadata address here.
			GitLookupHost: policyTestLookup,
		})
		t.Cleanup(func() { _ = svc.Close() })
		result, err := svc.TestGitConnection(context.Background(), userID, app.ID)
		if err != nil {
			t.Fatalf("TestGitConnection: %v", err)
		}
		if result.OK {
			t.Errorf("result = %+v, want a refused probe", result)
		}
		// Same generic text as the literal path above: a refused name reads
		// exactly like an unresolvable one, so the message is no oracle.
		if result.Message != errGitHostNotAllowed.Error() {
			t.Errorf("message = %q, want the generic refusal %q", result.Message, errGitHostNotAllowed)
		}
		if strings.Contains(result.Message, "169.254.169.254") {
			t.Errorf("message leaked the resolved address: %q", result.Message)
		}
		if result.Host != "git.internal" {
			t.Errorf("host = %q, want git.internal", result.Host)
		}
		if ran {
			t.Error("git ran for a blocked probe host")
		}
	})
}

// TestGitProbeAllowsPrivateHostWithSetting pins the escape hatch at the
// probe: with the setting, a loopback probe reaches git.
func TestGitProbeAllowsPrivateHostWithSetting(t *testing.T) {
	t.Setenv(GitAllowPrivateHostsEnv, "true")
	userID := uuid.New()
	app := privateTestApp(userID, "https://127.0.0.1/acme/demo.git")
	repo := &fakeRepository{app: app}
	ran := false
	svc := serviceWithRunner(t, repo, func(context.Context, []string, []string) ([]byte, error) {
		ran = true
		return []byte("deadbeef\tHEAD\n"), nil
	})
	result, err := svc.TestGitConnection(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("TestGitConnection: %v", err)
	}
	if !result.OK {
		t.Errorf("result = %+v, want success with the allow setting", result)
	}
	if !ran {
		t.Error("git never ran for an allowed probe host")
	}
}

// gitConfigValue reads one GIT_CONFIG_KEY_n/VALUE_n pair out of env for
// assertions: the index links the key to its value.
func gitConfigValue(env []string, key string) (string, bool) {
	index := -1
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(name, "GIT_CONFIG_KEY_") || value != key {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, "GIT_CONFIG_KEY_"))
		if err != nil {
			continue
		}
		index = n
	}
	if index < 0 {
		return "", false
	}
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok && name == "GIT_CONFIG_VALUE_"+strconv.Itoa(index) {
			return value, true
		}
	}
	return "", false
}

// TestScrubNetworkAddrs pins the deploy-log scrub: address literals in quoted
// git output are hidden while hostnames and versions survive.
func TestScrubNetworkAddrs(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"fatal: unable to connect to 127.0.0.1:8080", "fatal: unable to connect to [redacted]:8080"},
		{"dial tcp 10.0.0.5:443: connection refused", "dial tcp [redacted]:443: connection refused"},
		{"could not resolve host: git.example", "could not resolve host: git.example"},
		{"fetching https://github.com/acme/demo.git", "fetching https://github.com/acme/demo.git"},
		{"connect to [::1]:22 failed", "connect to [redacted]:22 failed"},
		{"connect to [2001:db8::1]:22 failed", "connect to [redacted]:22 failed"},
		{"git version 2.39.0", "git version 2.39.0"},
		{"[ssh.github.com]:443 ssh-ed25519 AAAA", "[ssh.github.com]:443 ssh-ed25519 AAAA"},
	}
	for _, tc := range cases {
		if got := scrubNetworkAddrs(tc.in); got != tc.want {
			t.Errorf("scrubNetworkAddrs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestQuoteGitOutputRedactsBeforeTruncation pins the error-quote order:
// credentials and addresses are hidden before the 400-byte cut, so a cut
// inside either can never expose its remainder.
func TestQuoteGitOutputRedactsBeforeTruncation(t *testing.T) {
	output := "https://user:secretpw@host/repo dial tcp 10.9.9.9:443" + strings.Repeat("B", 500)
	quoted := quoteGitOutput(output)
	if strings.Contains(quoted, "secretpw") || strings.Contains(quoted, "10.9.9.9") {
		t.Errorf("quoted output leaked a secret or an address: %q", quoted)
	}
	if !strings.Contains(quoted, "…") {
		t.Error("quoted output was never truncated")
	}
}
