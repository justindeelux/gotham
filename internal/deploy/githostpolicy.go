package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	neturl "net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
// 100.64.0.0/10, benchmarking 198.18.0.0/15, NAT64 64:ff9b::/96 (plus the
// local-use 64:ff9b:1::/48), 6to4 2002::/16, IPv4-compatible ::/96 and
// IPv4-mapped IPv6 forms (classified by the unwrapped address).
// RFC1918, ULA and loopback stay denied unless the operator allows private
// hosts (self-hosted git servers are common): config key
// deploy.git_allow_private_hosts, env GOTHAM_GIT_ALLOW_PRIVATE_HOSTS,
// default false. The allow setting never lifts link-local, unspecified,
// multicast, CGNAT, benchmarking, NAT64 or 6to4. The 'localhost' NAME stays
// denied even with the setting (names rebind; an explicit 127.0.0.1 is
// allowed by it).
//
// Residuals: https is pinned via http.curloptResolve (needs git >= 2.37;
// older git ignores the option silently, and a startup warning names that —
// see checkGitVersionForPin), and every http(s) git invocation refuses
// redirects (http.followRedirects=false) so a public URL cannot bounce to an
// internal one. ssh and git:// have no equivalent pin: they are verified at
// validation and immediately before the dial (two lookups, compared), so a
// fast-flux answer racing that window still reaches the dial. An http(s)
// proxy (HTTP(S)_PROXY) bypasses the pin as well: curl resolves and connects
// through the proxy, so proxy environments must pair this policy with proxy
// rules that refuse internal targets. The probe stays throttled (see
// probeAllowed) so it cannot serve as a scan oracle.

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
	return parseGitAllowPrivateHosts(os.Getenv(GitAllowPrivateHostsEnv))
}

// parseGitAllowPrivateHosts is the single reader for the allow-private flag.
// It matches the config file path exactly: viper/cast maps bool strings
// with strconv.ParseBool and nonzero numbers to true, so "1" and "true"
// agree on both readers and anything else stays denied.
func parseGitAllowPrivateHosts(raw string) bool {
	allow, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && allow
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
		return errGitHostNotAllowed
	}
	bare := strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
	if ip := net.ParseIP(bare); ip != nil {
		return checkDeniedIP(ip, gitAllowPrivateHosts())
	}
	if ip, numeric, err := parseNumericIPv4(trimmed); numeric {
		if err != nil {
			return errGitHostNotAllowed
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

// errGitHostNotAllowed is the single refusal for every host-policy denial.
// One generic text covers literal blocks, resolved-address blocks and
// resolution failures alike: the message must not reveal the address
// category, and a name that refuses must read the same as a name that never
// resolves, so the error is no resolution oracle.
var errGitHostNotAllowed = fmt.Errorf("%w: clone URL host is not allowed", ErrValidation)

// gitDenyNet is one always-denied network.
type gitDenyNet struct {
	net *net.IPNet
}

// mustGitCIDR parses a CIDR or panics (all entries are literals).
func mustGitCIDR(cidr string) gitDenyNet {
	_, parsed, err := net.ParseCIDR(cidr)
	if err != nil {
		panic("deploy: invalid git deny CIDR " + cidr)
	}
	return gitDenyNet{net: parsed}
}

// gitAlwaysDeny holds the ranges no operator setting lifts.
var gitAlwaysDeny = []gitDenyNet{
	mustGitCIDR("0.0.0.0/8"),
	mustGitCIDR("169.254.0.0/16"),
	mustGitCIDR("224.0.0.0/4"),
	mustGitCIDR("100.64.0.0/10"),
	mustGitCIDR("198.18.0.0/15"),
	mustGitCIDR("fe80::/10"),
	mustGitCIDR("ff00::/8"),
	mustGitCIDR("64:ff9b::/96"),
	mustGitCIDR("64:ff9b:1::/48"),
	mustGitCIDR("2002::/16"),
	mustGitCIDR("::/96"),
}

// gitPrivateNets holds the ranges the allow-private setting lifts: RFC1918,
// ULA and loopback (loopback rides along for same-host git and fixtures;
// link-local, including the cloud metadata address, stays denied).
var gitPrivateNets = []gitDenyNet{
	mustGitCIDR("127.0.0.0/8"),
	mustGitCIDR("10.0.0.0/8"),
	mustGitCIDR("172.16.0.0/12"),
	mustGitCIDR("192.168.0.0/16"),
	mustGitCIDR("::1/128"),
	mustGitCIDR("fc00::/7"),
}

// checkDeniedIP refuses an address the git host policy blocks. IPv4-mapped
// IPv6 forms are classified by the unwrapped address. Every refusal is the
// generic errGitHostNotAllowed: the message must not name the category.
func checkDeniedIP(ip net.IP, allowPrivate bool) error {
	if ip.IsUnspecified() {
		return errGitHostNotAllowed
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
		for _, denied := range gitAlwaysDeny {
			if denied.net.Contains(ip) {
				return errGitHostNotAllowed
			}
		}
		if !allowPrivate {
			for _, denied := range gitPrivateNets {
				if denied.net.Contains(ip) {
					return errGitHostNotAllowed
				}
			}
		}
		return nil
	}
	// Loopback rides before the always-deny loop: ::/96 (IPv4-compatible)
	// contains ::1, and the allow-private setting lifts loopback only.
	if ip.IsLoopback() {
		if !allowPrivate {
			return errGitHostNotAllowed
		}
		return nil
	}
	for _, denied := range gitAlwaysDeny {
		if denied.net.Contains(ip) {
			return errGitHostNotAllowed
		}
	}
	if !allowPrivate {
		for _, denied := range gitPrivateNets {
			if denied.net.Contains(ip) {
				return errGitHostNotAllowed
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
			return nil, errGitHostNotAllowed
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
		return nil, errGitHostNotAllowed
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
		// A 30x to an internal host would bypass the pin: curl resolves the
		// redirect target freely, and the pin only covers the original
		// host:port. Refuse redirects on every http(s) git invocation, not
		// just the stored-credential path (see askpassEnv). Renamed
		// repositories fail closed here; that is the trade-off.
		env = appendGitConfigOnce(env, "http.followRedirects", "false")
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
// the validated address while TLS and the Host header keep the hostname. One
// entry per resolved address: every answer was classified, so every answer
// is pinned. It extends the GIT_CONFIG_COUNT sequence askpassEnv may have
// started.
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

// appendGitConfigOnce appends one single-valued GIT_CONFIG_KEY_n/VALUE_n
// pair, extending the COUNT sequence. A key already present keeps its value:
// the stored-credential path sets http.followRedirects itself, and the pin
// must not shadow it with a duplicate.
func appendGitConfigOnce(env []string, key, value string) []string {
	if gitConfigHas(env, key) {
		return env
	}
	count := gitConfigCount(env)
	env = append(env,
		"GIT_CONFIG_KEY_"+strconv.Itoa(count)+"="+key,
		"GIT_CONFIG_VALUE_"+strconv.Itoa(count)+"="+value,
	)
	return setGitConfigCount(env, count+1)
}

// gitConfigHas reports whether env already carries a GIT_CONFIG entry for
// key (whatever its index).
func gitConfigHas(env []string, key string) bool {
	for _, entry := range env {
		if name, value, ok := strings.Cut(entry, "="); ok &&
			strings.HasPrefix(name, "GIT_CONFIG_KEY_") && value == key {
			return true
		}
	}
	return false
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

// redirectWarnPattern matches git's "warning: redirecting to <url>" line.
// The target is server-chosen, so it is dropped from quoted output even
// though redirects are refused: a hostname there must never reach the log.
var redirectWarnPattern = regexp.MustCompile(`(?im)^[^\n]*warning:\s*redirecting to\s+[^\n]*\n?`)

// scrubNetworkAddrs hides address literals in git output quoted into deploy
// errors: the deploy log already carries the classified hint, and a resolved
// internal address must never be echoed next to it.
func scrubNetworkAddrs(output string) string {
	output = redirectWarnPattern.ReplaceAllString(output, "")
	output = ipv4RedactPattern.ReplaceAllString(output, "[redacted]")
	return ipv6RedactPattern.ReplaceAllString(output, "[redacted]")
}

// quoteGitOutput redacts credentials, scrubs address literals and truncates
// git output quoted into an error. Redaction runs before truncation so a cut
// inside a secret can never expose its remainder.
func quoteGitOutput(output string) string {
	return tail(scrubNetworkAddrs(redactCloneError(output)), 400)
}

// minGitPinMajor/Minor is the first git release supporting
// http.curloptResolve (2.37): older git ignores the pin silently, leaving
// only the double resolution.
const (
	minGitPinMajor = 2
	minGitPinMinor = 37
)

// gitVersion is one cached `git --version` answer.
type gitVersion struct {
	major, minor int
	ok           bool
}

// gitVersionForPin runs `git --version` once per process and answers its
// major/minor numbers. False means no usable git was found.
var gitVersionForPin = sync.OnceValue(func() gitVersion {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	git, err := exec.LookPath("git")
	if err != nil {
		return gitVersion{}
	}
	out, err := exec.CommandContext(ctx, git, "--version").Output()
	if err != nil {
		return gitVersion{}
	}
	major, minor, ok := parseGitVersion(string(out))
	return gitVersion{major: major, minor: minor, ok: ok}
})

// parseGitVersion reads "git version 2.43.0" (trailing platform text is
// tolerated) and answers its major/minor numbers.
func parseGitVersion(output string) (major, minor int, ok bool) {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return 0, 0, false
	}
	nums := strings.Split(fields[2], ".")
	if len(nums) < 2 {
		return 0, 0, false
	}
	major, majorErr := strconv.Atoi(nums[0])
	minor, minorErr := strconv.Atoi(nums[1])
	if majorErr != nil || minorErr != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// checkGitVersionForPin warns when the git binary predates the https pin:
// the policy still resolves and refuses on every path, but https remotes are
// not pinned to the validated address. A missing git stays silent (minimal
// images and tests without the binary have nothing to pin with anyway).
func checkGitVersionForPin(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	version := gitVersionForPin()
	if !version.ok {
		return
	}
	if version.major > minGitPinMajor || version.major == minGitPinMajor && version.minor >= minGitPinMinor {
		return
	}
	logger.Warn("deploy: git binary predates the https pin",
		"version", strconv.Itoa(version.major)+"."+strconv.Itoa(version.minor),
		"need", "git >= 2.37 for http.curloptResolve",
		"risk", "https remotes rely on double resolution only; upgrade git",
	)
}
