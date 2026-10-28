package domainwatch

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testTarget() Target {
	settings := DefaultSettings()
	return Target{
		DomainID: [16]byte{1},
		Domain:   "app.example.com",
		Protocol: "https",
		Settings: settings,
	}
}

func TestBuildURLDerivesOnlyTheManagedDomain(t *testing.T) {
	target := testTarget()
	got, err := buildURL(target)
	if err != nil {
		t.Fatalf("buildURL: %v", err)
	}
	if got != "https://app.example.com/" {
		t.Fatalf("url = %q", got)
	}

	withPath := target
	withPath.Settings.Path = "/health"
	if got, err := buildURL(withPath); err != nil || got != "https://app.example.com/health" {
		t.Fatalf("path url = %q, err = %v", got, err)
	}
}

func TestBuildURLRejectsHostPortAndUnsafePaths(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Target)
	}{
		{"host override", func(t *Target) { t.Domain = "https://evil.example" }},
		{"port override", func(t *Target) { t.Domain = "app.example.com:8443" }},
		{"credentials", func(t *Target) { t.Domain = "user@app.example.com" }},
		{"absolute path", func(t *Target) { t.Settings.Path = "http://evil.example/" }},
		{"query", func(t *Target) { t.Settings.Path = "/?token=secret" }},
		{"fragment", func(t *Target) { t.Settings.Path = "/#fragment" }},
		{"scheme", func(t *Target) { t.Protocol = "ftp" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := testTarget()
			tc.mutate(&target)
			if _, err := buildURL(target); err == nil {
				t.Fatalf("expected rejection")
			}
		})
	}
}

func TestNormalizePathBounds(t *testing.T) {
	if got, err := normalizePath(""); err != nil || got != "/" {
		t.Fatalf("empty path = %q, %v", got, err)
	}
	for _, path := range []string{"health", "//double", "/a\tb", strings.Repeat("/a", maxPathLen)} {
		if _, err := normalizePath(path); err == nil {
			t.Fatalf("expected %q to be rejected", path)
		}
	}
}

func TestRedirectPolicyRejectsOtherHostsAndSchemes(t *testing.T) {
	original, _ := url.Parse("https://app.example.com/")
	same, _ := http.NewRequest(http.MethodGet, "https://app.example.com/health", nil)
	if err := redirectPolicy(original, same, []*http.Request{same}); err != nil {
		t.Fatalf("same-host redirect rejected: %v", err)
	}
	other, _ := http.NewRequest(http.MethodGet, "https://169.254.169.254/latest", nil)
	if err := redirectPolicy(original, other, []*http.Request{other}); !errors.Is(err, ErrUnsafeRedirect) {
		t.Fatalf("cross-host redirect error = %v", err)
	}
	ftp, _ := http.NewRequest(http.MethodGet, "ftp://app.example.com/file", nil)
	if err := redirectPolicy(original, ftp, []*http.Request{ftp}); !errors.Is(err, ErrUnsafeRedirect) {
		t.Fatalf("scheme redirect error = %v", err)
	}
	withUser, _ := http.NewRequest(http.MethodGet, "https://user:pass@app.example.com/", nil)
	if err := redirectPolicy(original, withUser, []*http.Request{withUser}); !errors.Is(err, ErrUnsafeRedirect) {
		t.Fatalf("credential redirect error = %v", err)
	}
	tooMany := make([]*http.Request, maxRedirects)
	for i := range tooMany {
		tooMany[i] = same
	}
	if err := redirectPolicy(original, same, tooMany); !errors.Is(err, ErrUnsafeRedirect) {
		t.Fatalf("redirect bound error = %v", err)
	}
}

func TestBlockedIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "::1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "0.0.0.0", "fe80::1"}
	for _, raw := range blocked {
		if !blockedIP(net.ParseIP(raw)) {
			t.Fatalf("%s should be blocked", raw)
		}
	}
	if blockedIP(net.ParseIP("93.184.216.34")) {
		t.Fatal("public address must be allowed")
	}
}

func TestClassifyProbeError(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		reason      string
		unsupported bool
	}{
		{"blocked", ErrBlockedDestination, ReasonBlockedDestination, true},
		{"unsafe redirect", fmt.Errorf("%w: host changed", ErrUnsafeRedirect), ReasonUnsafeRedirect, false},
		{"dns", &dnsFailure{err: &net.DNSError{Err: "no such host", Name: "x"}}, ReasonDNS, false},
		{"timeout", context.DeadlineExceeded, ReasonTimeout, false},
		{"tls", x509.HostnameError{Certificate: &x509.Certificate{}, Host: "x"}, ReasonTLS, false},
		{"transport", errors.New("connection reset"), ReasonTransport, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, unsupported := classifyProbeError(tc.err, context.Background())
			if reason != tc.reason || unsupported != tc.unsupported {
				t.Fatalf("got %q %v", reason, unsupported)
			}
		})
	}
}

// dialTo lets the prober reach a local test server while the derived URL still
// names the managed domain, so host-override rules stay under test.
func dialTo(address string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
}

func TestProbeHealthyAndUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := testTarget()
	target.Protocol = "http"
	target.Settings.Path = "/health"
	prober := &Prober{Now: time.Now, Dial: dialTo(server.Listener.Addr().String())}
	result := prober.Probe(context.Background(), target)
	if result.Outcome != OutcomeHealthy || result.StatusCode != http.StatusOK {
		t.Fatalf("result = %+v", result)
	}

	target.Settings.ExpectedMin = 204
	target.Settings.ExpectedMax = 299
	result = prober.Probe(context.Background(), target)
	if result.Outcome != OutcomeFailed || result.Reason != ReasonUnexpectedStatus {
		t.Fatalf("mismatch result = %+v", result)
	}
}

func TestProbeTLSFailureIsSeparateEvidence(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := testTarget()
	prober := &Prober{Now: time.Now, Dial: dialTo(server.Listener.Addr().String())}
	result := prober.Probe(context.Background(), target)
	if result.Outcome != OutcomeFailed || result.Reason != ReasonTLS {
		t.Fatalf("result = %+v", result)
	}
}

func TestProbeRejectsCrossHostRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer server.Close()

	target := testTarget()
	target.Protocol = "http"
	prober := &Prober{Now: time.Now, Dial: dialTo(server.Listener.Addr().String())}
	result := prober.Probe(context.Background(), target)
	if result.Outcome != OutcomeFailed || result.Reason != ReasonUnsafeRedirect {
		t.Fatalf("result = %+v", result)
	}
}

func TestProbeTimeoutIsReportedAsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer server.Close()

	target := testTarget()
	target.Protocol = "http"
	target.Settings.Timeout = time.Second
	prober := &Prober{Now: time.Now, Dial: dialTo(server.Listener.Addr().String())}
	result := prober.Probe(context.Background(), target)
	if result.Outcome != OutcomeFailed || result.Reason != ReasonTimeout {
		t.Fatalf("result = %+v", result)
	}
}
