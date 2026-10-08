package deploy

import (
	"context"
	"fmt"
	"net"
	neturl "net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

// SSRF host policy for user-supplied git remotes (JUS-67).
//
// git_public clone/ls-remote, git_private clone/Test connection and every
// other dial of a user-supplied git host share this policy. Creation-time
// validators (ValidatePublicGitURL, ValidatePrivateGitURL, validateCloneURL)
// enforce the syntax half without DNS: literal IPs, numeric-IP tricks,
// zone ids and localhost names are refused there. The clone/probe half
// (pinGitRemoteHost) resolves the host, refuses blocked addresses, refuses a
// changed second answer (DNS rebinding) and pins the resolved address into
// the git child environment for http(s).
//
// Default DENY: loopback, link-local (including the 169.254.169.254 cloud
// metadata address and fe80::/10), unspecified, multicast, carrier-grade NAT
// 100.64.0.0/10, benchmarking 198.18.0.0/15, NAT64 64:ff9b::/96, 6to4
// 2002::/16 and IPv4-mapped IPv6 forms (classified by the unwrapped address).
// RFC1918, ULA and loopback stay denied unless the operator allows private
// hosts (self-hosted git servers are common): config key
// deploy.git_allow_private_hosts, env GOTHAM_GIT_ALLOW_PRIVATE_HOSTS,
// default false. The allow setting never lifts link-local, unspecified,
// multicast, CGNAT, benchmarking, NAT64 or 6to4.
//
// Residuals: https is pinned via http.curloptResolve, so curl never
// re-resolves. ssh and git:// have no equivalent pin: they are verified at
// validation and immediately before the dial (two lookups, compared), so a
// fast-flux answer racing that window still reaches the dial. The probe stays
// throttled (see probeAllowed) so it cannot serve as a scan oracle.

// GitAllowPrivateHostsEnv is the operator escape hatch for self-hosted git
// servers on private ranges. It also covers loopback (same-host git in
// development and fixtures); link-local and the other always-denied ranges
// stay denied.
const GitAllowPrivateHostsEnv = "GOTHAM_GIT_ALLOW_PRIVATE_HOSTS"

// gitAllowPrivateHostsOverride is the process-wide override the server sets
// from its config snapshot (see Config.GitAllowPrivateHosts). The env var
// wins for tests and operators without a config file; either source allows.
var gitAllowPrivateHostsOverride atomic.Bool

// SetGitAllowPrivateHosts installs the process-wide private-hosts override.
// The server calls it once from its config snapshot; tests use the env var.
func SetGitAllowPrivateHosts(allow bool) {
	gitAllowPrivateHostsOverride.Store(allow)
}

// gitAllowPrivateHosts reports whether private git hosts are permitted.
func gitAllowPrivateHosts() bool {
	if gitAllowPrivateHostsOverride.Load() {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv(GitAllowPrivateHostsEnv)), "true")
}

// gitHostLookupFunc resolves a git remote hostname to addresses. It is a
// field so tests pin answers without DNS; nil selects defaultGitHostLookup.
type gitHostLookupFunc func(ctx context.Context, host string) ([]net.IP, error)

// defaultGitHostLookup is the production resolver. Tests replace it in
// TestMain with a stub so no unit test touches real DNS.
var defaultGitHostLookup gitHostLookupFunc = systemGitHostLookup

// systemGitHostLookup resolves host through the system resolver.
func systemGitHostLookup(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP)
	}
	return ips, nil
}

// gitRemoteHost is the dial target extracted from a clone URL.
type gitRemoteHost struct {
	host string
	port string
	// local is a local path or file:// URL: no network dial, nothing to pin.
	local bool
}

// extractGitHost pulls the dial target out of a clone URL. Userinfo is kept
// out of the answer (the GS-5 installation-token URL carries a credential;
// the policy judges the host, never the credential).
func extractGitHost(raw string) (gitRemoteHost, error) {
	url := strings.TrimSpace(raw)
	if url == "" {
		return gitRemoteHost{}, fmt.Errorf("%w: application has no clone URL", ErrValidation)
	}
	if !strings.Contains(url, "://") {
		if isSSHTransportURL(url) {
			_, host := splitScpAuthority(url)
			return gitRemoteHost{host: host}, nil
		}
		if strings.HasPrefix(url, "/") {
			return gitRemoteHost{local: true}, nil
		}
		return gitRemoteHost{}, fmt.Errorf("%w: unsupported clone URL", ErrValidation)
	}
	parsed, err := neturl.Parse(url)
	if err != nil {
		return gitRemoteHost{}, fmt.Errorf("%w: unsupported clone URL", ErrValidation)
	}
	if strings.EqualFold(parsed.Scheme, "file") {
		return gitRemoteHost{local: true}, nil
	}
	return gitRemoteHost{host: parsed.Hostname(), port: parsed.Port()}, nil
}

// checkGitHostLiteral enforces the no-DNS half of the host policy: empty
// hosts, IPv6 zone ids, localhost names and literal/numeric IPs in denied
// ranges are refused. DNS names pass: their addresses are checked at
// clone/probe time by pinGitRemoteHost.
func checkGitHostLiteral(host string) error {
	trimmed := strings.TrimSuffix(strings.TrimSpace(host), ".")
	if trimmed == "" {
		return fmt.Errorf("%w: clone URL has no host", ErrValidation)
	}
	if strings.Contains(trimmed, "%") {
		return fmt.Errorf("%w: clone URL host must not carry an IPv6 zone id", ErrValidation)
	}
	lower := strings.ToLower(trimmed)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("%w: clone URL host is blocked (loopback)", ErrValidation)
	}
	bare := strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
	if ip := net.ParseIP(bare); ip != nil {
		return checkDeniedIP(ip, gitAllowPrivateHosts())
	}
	if ip, numeric, err := parseNumericIPv4(trimmed); numeric {
		if err != nil {
			return fmt.Errorf("%w: clone URL host %q is not a valid address", ErrValidation, trimmed)
		}
		return checkDeniedIP(ip, gitAllowPrivateHosts())
	}
	return nil
}

// parseNumericIPv4 parses the numeric IPv4 forms git/curl accept but
// net.ParseIP rejects: single decimal/hex/octal values (2130706433,
// 0x7f000001, 017700000001), per-part hex/octal (0x7f.0.0.1, 0177.0.0.1) and
// inet_aton shorthand (127.1). numeric reports whether host looks numeric at
// all; when true the caller must not fall through to DNS — an invalid
// numeric host is refused, never resolved.
func parseNumericIPv4(host string) (ip net.IP, numeric bool, err error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(host), ".")
	if trimmed == "" || strings.ContainsAny(trimmed, ":/%") {
		return nil, false, nil
	}
	parts := strings.Split(trimmed, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return nil, false, nil
	}
	for _, part := range parts {
		if part == "" || !isNumericPart(part) {
			return nil, false, nil
		}
	}
	values := make([]uint64, 0, len(parts))
	for _, part := range parts {
		value, ok := parseNumericPart(part)
		if !ok {
			return nil, true, fmt.Errorf("invalid numeric host %q", host)
		}
		values = append(values, value)
	}
	var wide uint64
	switch len(values) {
	case 1:
		if values[0] > 0xffffffff {
			return nil, true, fmt.Errorf("invalid numeric host %q", host)
		}
		wide = values[0]
	case 2:
		if values[0] > 0xff || values[1] > 0xffffff {
			return nil, true, fmt.Errorf("invalid numeric host %q", host)
		}
		wide = values[0]<<24 | values[1]
	case 3:
		if values[0] > 0xff || values[1] > 0xff || values[2] > 0xffff {
			return nil, true, fmt.Errorf("invalid numeric host %q", host)
		}
		wide = values[0]<<24 | values[1]<<16 | values[2]
	default:
		for _, value := range values {
			if value > 0xff {
				return nil, true, fmt.Errorf("invalid numeric host %q", host)
			}
		}
		wide = values[0]<<24 | values[1]<<16 | values[2]<<8 | values[3]
	}
	return net.IPv4(byte(wide>>24), byte(wide>>16), byte(wide>>8), byte(wide)), true, nil
}

// isNumericPart reports the charset a numeric IPv4 part may use: digits with
// an optional 0x hex prefix. Anything else (including a hostname label) is
// not numeric and takes the DNS path.
func isNumericPart(part string) bool {
	rest := part
	if strings.HasPrefix(rest, "0x") || strings.HasPrefix(rest, "0X") {
		rest = rest[2:]
		if rest == "" {
			return false
		}
		for _, r := range rest {
			if !isHexDigit(r) {
				return false
			}
		}
		return true
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseNumericPart parses one numeric IPv4 part with C/inet_aton bases: 0x
// hex, leading-0 octal, otherwise decimal. A leading zero with 8/9 or an
// empty digit run is invalid rather than decimal — curl disagrees with a
// lax reading, and the strict reading fails closed.
func parseNumericPart(part string) (uint64, bool) {
	if strings.HasPrefix(part, "0x") || strings.HasPrefix(part, "0X") {
		value, err := strconv.ParseUint(part[2:], 16, 32)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	if len(part) > 1 && strings.HasPrefix(part, "0") {
		for _, r := range part {
			if r < '0' || r > '7' {
				return 0, false
			}
		}
		value, err := strconv.ParseUint(part, 8, 32)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	value, err := strconv.ParseUint(part, 10, 32)
	if err != nil {
		return 0, false
	}
	return value, true
}

// isHexDigit reports one hexadecimal digit.
func isHexDigit(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
}

// gitDenyNet is one always-denied network.
type gitDenyNet struct {
	net    *net.IPNet
	reason string
}

// mustGitCIDR parses a CIDR or panics (all entries are literals).
func mustGitCIDR(cidr, reason string) gitDenyNet {
	_, parsed, err := net.ParseCIDR(cidr)
	if err != nil {
		panic("deploy: invalid git deny CIDR " + cidr)
	}
	return gitDenyNet{net: parsed, reason: reason}
}

// gitAlwaysDeny holds the ranges no operator setting lifts.
var gitAlwaysDeny = []gitDenyNet{
	mustGitCIDR("0.0.0.0/8", "this network"),
	mustGitCIDR("169.254.0.0/16", "link-local"),
	mustGitCIDR("224.0.0.0/4", "multicast"),
	mustGitCIDR("100.64.0.0/10", "carrier-grade NAT"),
	mustGitCIDR("198.18.0.0/15", "benchmarking"),
	mustGitCIDR("fe80::/10", "link-local"),
	mustGitCIDR("ff00::/8", "multicast"),
	mustGitCIDR("64:ff9b::/96", "NAT64"),
	mustGitCIDR("2002::/16", "6to4"),
}

// gitPrivateNets holds the ranges the allow-private setting lifts: RFC1918,
// ULA and loopback (loopback rides along for same-host git and fixtures;
// link-local, including the cloud metadata address, stays denied).
var gitPrivateNets = []gitDenyNet{
	mustGitCIDR("127.0.0.0/8", "loopback"),
	mustGitCIDR("10.0.0.0/8", "private network"),
	mustGitCIDR("172.16.0.0/12", "private network"),
	mustGitCIDR("192.168.0.0/16", "private network"),
	mustGitCIDR("::1/128", "loopback"),
	mustGitCIDR("fc00::/7", "private network"),
}

// checkDeniedIP refuses an address the git host policy blocks. IPv4-mapped
// IPv6 forms are classified by the unwrapped address. Denial messages never
// carry the address: the resolved value must not reach the deploy log.
func checkDeniedIP(ip net.IP, allowPrivate bool) error {
	if ip.IsUnspecified() {
		return fmt.Errorf("%w: clone URL host is blocked (unspecified address)", ErrValidation)
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
		for _, denied := range gitAlwaysDeny {
			if denied.net.Contains(ip) {
				return fmt.Errorf("%w: clone URL host is blocked (%s)", ErrValidation, denied.reason)
			}
		}
		if !allowPrivate {
			for _, denied := range gitPrivateNets {
				if v4net := denied.net; v4net.Contains(ip) {
					return fmt.Errorf("%w: clone URL host is blocked (%s)", ErrValidation, denied.reason)
				}
			}
		}
		return nil
	}
	for _, denied := range gitAlwaysDeny {
		if denied.net.Contains(ip) {
			return fmt.Errorf("%w: clone URL host is blocked (%s)", ErrValidation, denied.reason)
		}
	}
	if ip.IsLoopback() {
		if !allowPrivate {
			return fmt.Errorf("%w: clone URL host is blocked (loopback)", ErrValidation)
		}
		return nil
	}
	if !allowPrivate {
		for _, denied := range gitPrivateNets {
			if denied.net.Contains(ip) {
				return fmt.Errorf("%w: clone URL host is blocked (%s)", ErrValidation, denied.reason)
			}
		}
	}
	return nil
}

// resolveGitHostAddrs resolves a git remote host and refuses blocked
// answers. Literals and numeric forms are classified without DNS; DNS names
// resolve through lookup and every answer is classified. Failures refuse:
// an unverifiable host never dials.
func resolveGitHostAddrs(ctx context.Context, lookup gitHostLookupFunc, host string) ([]net.IP, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(host), ".")
	if trimmed == "" {
		return nil, fmt.Errorf("%w: clone URL has no host", ErrValidation)
	}
	if strings.Contains(trimmed, "%") {
		return nil, fmt.Errorf("%w: clone URL host must not carry an IPv6 zone id", ErrValidation)
	}
	bare := strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
	if ip := net.ParseIP(bare); ip != nil {
		if err := checkDeniedIP(ip, gitAllowPrivateHosts()); err != nil {
			return nil, err
		}
		return []net.IP{ip}, nil
	}
	if ip, numeric, err := parseNumericIPv4(trimmed); numeric {
		if err != nil {
			return nil, fmt.Errorf("%w: clone URL host %q is not a valid address", ErrValidation, trimmed)
		}
		if err := checkDeniedIP(ip, gitAllowPrivateHosts()); err != nil {
			return nil, err
		}
		return []net.IP{ip}, nil
	}
	if lookup == nil {
		lookup = defaultGitHostLookup
	}
	ips, err := lookup(ctx, trimmed)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("%w: clone URL host does not resolve", ErrValidation)
	}
	for _, ip := range ips {
		if err := checkDeniedIP(ip, gitAllowPrivateHosts()); err != nil {
			return nil, err
		}
	}
	return ips, nil
}

// pinGitRemoteHost is the dial-time half of the host policy, shared by clone
// and the connection probe. It resolves the clone URL host, refuses blocked
// addresses, resolves again and refuses a changed answer (DNS rebinding),
// and pins the resolved address for http(s) remotes via http.curloptResolve
// so the git child never re-resolves. ssh and git:// remotes cannot be
// pinned this way; their two verified resolutions are the whole mitigation
// (see the package residual note).
func pinGitRemoteHost(ctx context.Context, lookup gitHostLookupFunc, rawURL string, env []string) ([]string, error) {
	remote, err := extractGitHost(rawURL)
	if err != nil {
		return nil, err
	}
	if remote.local || remote.host == "" {
		if remote.host == "" && !remote.local {
			return nil, fmt.Errorf("%w: clone URL has no host", ErrValidation)
		}
		return env, nil
	}
	first, err := resolveGitHostAddrs(ctx, lookup, remote.host)
	if err != nil {
		return nil, err
	}
	second, err := resolveGitHostAddrs(ctx, lookup, remote.host)
	if err != nil {
		return nil, err
	}
	if !sameIPSet(first, second) {
		return nil, fmt.Errorf("%w: clone URL host address changed during validation (possible DNS rebinding)",
			ErrValidation)
	}
	switch gitURLScheme(rawURL) {
	case "http", "https":
		port := remote.port
		if port == "" {
			port = gitDefaultPort(gitURLScheme(rawURL))
		}
		env = appendCurloptResolve(env, remote.host, port, first)
	}
	return env, nil
}

// gitURLScheme answers the lowercased scheme of rawURL, or "" without one.
func gitURLScheme(raw string) string {
	scheme, _, ok := strings.Cut(strings.TrimSpace(raw), "://")
	if !ok {
		return ""
	}
	return strings.ToLower(scheme)
}

// gitDefaultPort answers the default port for an http(s) clone URL.
func gitDefaultPort(scheme string) string {
	if scheme == "http" {
		return "80"
	}
	return "443"
}

// sameIPSet reports whether two resolutions carry the same addresses.
func sameIPSet(first, second []net.IP) bool {
	if len(first) != len(second) {
		return false
	}
	seen := make(map[string]int, len(first))
	for _, ip := range first {
		seen[ip.String()]++
	}
	for _, ip := range second {
		key := ip.String()
		if seen[key] == 0 {
			return false
		}
		seen[key]--
	}
	return true
}

// appendCurloptResolve pins host:port to ips for the git child through
// http.curloptResolve entries (curl CURLOPT_RESOLVE): the connection goes to
// the validated address while TLS and the Host header keep the hostname. It
// extends the GIT_CONFIG_COUNT sequence askpassEnv may have started.
func appendCurloptResolve(env []string, host, port string, ips []net.IP) []string {
	count := gitConfigCount(env)
	for _, ip := range ips {
		env = append(env,
			"GIT_CONFIG_KEY_"+strconv.Itoa(count)+"=http.curloptResolve",
			"GIT_CONFIG_VALUE_"+strconv.Itoa(count)+"="+host+":"+port+":"+ip.String(),
		)
		count++
	}
	return setGitConfigCount(env, count)
}

// gitConfigCount reads the GIT_CONFIG_COUNT entry of env, or 0.
func gitConfigCount(env []string) int {
	for _, entry := range env {
		value, ok := strings.CutPrefix(entry, "GIT_CONFIG_COUNT=")
		if !ok {
			continue
		}
		if count, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && count >= 0 {
			return count
		}
	}
	return 0
}

// setGitConfigCount writes the GIT_CONFIG_COUNT entry of env.
func setGitConfigCount(env []string, count int) []string {
	want := "GIT_CONFIG_COUNT=" + strconv.Itoa(count)
	for i, entry := range env {
		if strings.HasPrefix(entry, "GIT_CONFIG_COUNT=") {
			env[i] = want
			return env
		}
	}
	return append(env, want)
}

// ipv4RedactPattern matches dotted-quad literals in quoted git output.
var ipv4RedactPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

// ipv6RedactPattern matches bracketed IPv6 literals in quoted git output.
// The inner class excludes hostname characters outside hex/colons/dots, and
// a colon is required, so [ssh.github.com]:443-style text never matches.
var ipv6RedactPattern = regexp.MustCompile(`\[[0-9a-fA-F.]*:[0-9a-fA-F:.]*\]`)

// scrubNetworkAddrs hides address literals in git output quoted into deploy
// errors: the deploy log already carries the classified hint, and a resolved
// internal address must never be echoed next to it.
func scrubNetworkAddrs(output string) string {
	output = ipv4RedactPattern.ReplaceAllString(output, "[redacted]")
	return ipv6RedactPattern.ReplaceAllString(output, "[redacted]")
}

// quoteGitOutput redacts credentials, scrubs address literals and truncates
// git output quoted into an error. Redaction runs before truncation so a cut
// inside a secret can never expose its remainder.
func quoteGitOutput(output string) string {
	return tail(scrubNetworkAddrs(redactCloneError(output)), 400)
}
