package clientip

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// mustParse parses entries and fails the test on a malformed value.
func mustParse(t *testing.T, entries ...string) []netip.Prefix {
	t.Helper()
	prefixes, err := Parse(entries)
	if err != nil {
		t.Fatalf("Parse(%v): %v", entries, err)
	}
	return prefixes
}

func TestParse(t *testing.T) {
	t.Run("bare addresses become single-host prefixes", func(t *testing.T) {
		got := mustParse(t, "127.0.0.1", "::1")
		want := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128")}
		if len(got) != len(want) {
			t.Fatalf("Parse = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("Parse[%d] = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("blank entries are skipped", func(t *testing.T) {
		got := mustParse(t, "", "  ", "10.0.0.0/8")
		if len(got) != 1 || got[0] != netip.MustParsePrefix("10.0.0.0/8") {
			t.Fatalf("Parse = %v, want one 10.0.0.0/8", got)
		}
	})

	t.Run("invalid entries are rejected", func(t *testing.T) {
		for _, entry := range []string{"not-an-ip", "10.0.0.0/99", "300.1.2.3"} {
			if _, err := Parse([]string{entry}); err == nil {
				t.Errorf("Parse(%q) = nil error, want error", entry)
			}
		}
	})
}

func TestClientIP(t *testing.T) {
	private := mustParse(t, "10.0.0.0/8", "127.0.0.1", "::1")

	cases := []struct {
		name    string
		remote  string
		trusted []netip.Prefix
		xff     []string
		want    string
	}{
		{
			name:   "xff ignored from untrusted peer",
			remote: "203.0.113.7:1234",
			xff:    []string{"198.51.100.1"},
			want:   "203.0.113.7",
		},
		{
			name:    "xff honored from trusted peer",
			remote:  "127.0.0.1:1234",
			trusted: private,
			xff:     []string{"198.51.100.1"},
			want:    "198.51.100.1",
		},
		{
			name:    "multi-hop chain skips trusted proxies",
			remote:  "10.0.0.1:1234",
			trusted: private,
			xff:     []string{"203.0.113.5, 10.0.0.2, 10.0.0.3"},
			want:    "203.0.113.5",
		},
		{
			name:    "client-supplied leftmost entry cannot spoof the real client",
			remote:  "10.0.0.1:1234",
			trusted: private,
			xff:     []string{"1.2.3.4, 198.51.100.9"},
			want:    "198.51.100.9",
		},
		{
			name:    "repeated xff headers are flattened in order",
			remote:  "10.0.0.1:1234",
			trusted: private,
			xff:     []string{"203.0.113.5", "10.0.0.2"},
			want:    "203.0.113.5",
		},
		{
			name:    "all hops trusted falls back to the peer",
			remote:  "10.0.0.1:1234",
			trusted: private,
			xff:     []string{"10.0.0.2, 10.0.0.3"},
			want:    "10.0.0.1",
		},
		{
			name:    "malformed hops are skipped",
			remote:  "10.0.0.1:1234",
			trusted: private,
			xff:     []string{"garbage, 198.51.100.4"},
			want:    "198.51.100.4",
		},
		{
			name:   "no trusted proxies keeps remoteaddr behavior",
			remote: "203.0.113.7:1234",
			xff:    []string{"198.51.100.1"},
			want:   "203.0.113.7",
		},
		{
			name:    "ipv6 client behind an ipv6 proxy",
			remote:  "[::1]:1234",
			trusted: private,
			xff:     []string{"2001:db8::1"},
			want:    "2001:db8::1",
		},
		{
			name:    "ipv4-mapped client is unmapped",
			remote:  "10.0.0.1:1234",
			trusted: private,
			xff:     []string{"::ffff:198.51.100.1"},
			want:    "198.51.100.1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remote
			for _, value := range tc.xff {
				req.Header.Add("X-Forwarded-For", value)
			}
			if got := ClientIP(req, tc.trusted); got != tc.want {
				t.Errorf("ClientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsSecure(t *testing.T) {
	private := mustParse(t, "10.0.0.0/8", "127.0.0.1")

	newReq := func(remote string, tlsOn bool, proto string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remote
		if tlsOn {
			req.TLS = &tls.ConnectionState{}
		}
		if proto != "" {
			req.Header.Set("X-Forwarded-Proto", proto)
		}
		return req
	}

	cases := []struct {
		name    string
		req     *http.Request
		trusted []netip.Prefix
		want    bool
	}{
		{"spoofed https from untrusted peer stays insecure", newReq("203.0.113.7:1234", false, "https"), private, false},
		{"https forwarded by a trusted peer is secure", newReq("127.0.0.1:1234", false, "https"), private, true},
		{"http forwarded by a trusted peer is insecure", newReq("127.0.0.1:1234", false, "http"), private, false},
		{"direct tls is secure regardless of peer", newReq("203.0.113.7:1234", true, ""), private, true},
		{"no trusted proxies ignores the header", newReq("127.0.0.1:1234", false, "https"), nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSecure(tc.req, tc.trusted); got != tc.want {
				t.Errorf("IsSecure = %v, want %v", got, tc.want)
			}
		})
	}
}
