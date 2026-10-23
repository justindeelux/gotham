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

const GitAllowPrivateHostsEnv = "GOTHAM_GIT_ALLOW_PRIVATE_HOSTS"

var gitAllowPrivateHostsOverride atomic.Bool

func SetGitAllowPrivateHosts(allow bool) {
	gitAllowPrivateHostsOverride.Store(allow)
}

func gitAllowPrivateHosts() bool {
	if gitAllowPrivateHostsOverride.Load() {
		return true
	}
	return parseGitAllowPrivateHosts(os.Getenv(GitAllowPrivateHostsEnv))
}

func parseGitAllowPrivateHosts(raw string) bool {
	allow, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && allow
}

type gitHostLookupFunc func(ctx context.Context, host string) ([]net.IP, error)

var defaultGitHostLookup gitHostLookupFunc = systemGitHostLookup

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

type gitRemoteHost struct {
	host  string
	port  string
	local bool
}

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

func isHexDigit(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
}

var errGitHostNotAllowed = fmt.Errorf("%w: clone URL host is not allowed", ErrValidation)

type gitDenyNet struct {
	net *net.IPNet
}

func mustGitCIDR(cidr string) gitDenyNet {
	_, parsed, err := net.ParseCIDR(cidr)
	if err != nil {
		panic("deploy: invalid git deny CIDR " + cidr)
	}
	return gitDenyNet{net: parsed}
}

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

var gitPrivateNets = []gitDenyNet{
	mustGitCIDR("127.0.0.0/8"),
	mustGitCIDR("10.0.0.0/8"),
	mustGitCIDR("172.16.0.0/12"),
	mustGitCIDR("192.168.0.0/16"),
	mustGitCIDR("::1/128"),
	mustGitCIDR("fc00::/7"),
}

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
		env = appendGitConfigOnce(env, "http.followRedirects", "false")
	}
	return env, nil
}

func gitURLScheme(raw string) string {
	scheme, _, ok := strings.Cut(strings.TrimSpace(raw), "://")
	if !ok {
		return ""
	}
	return strings.ToLower(scheme)
}

func gitDefaultPort(scheme string) string {
	if scheme == "http" {
		return "80"
	}
	return "443"
}

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

func gitConfigHas(env []string, key string) bool {
	for _, entry := range env {
		if name, value, ok := strings.Cut(entry, "="); ok &&
			strings.HasPrefix(name, "GIT_CONFIG_KEY_") && value == key {
			return true
		}
	}
	return false
}

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

var ipv4RedactPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

var ipv6RedactPattern = regexp.MustCompile(`\[[0-9a-fA-F.]*:[0-9a-fA-F:.]*\]`)

var redirectWarnPattern = regexp.MustCompile(`(?im)^[^\n]*warning:\s*redirecting to\s+[^\n]*\n?`)

func scrubNetworkAddrs(output string) string {
	output = redirectWarnPattern.ReplaceAllString(output, "")
	output = ipv4RedactPattern.ReplaceAllString(output, "[redacted]")
	return ipv6RedactPattern.ReplaceAllString(output, "[redacted]")
}

func quoteGitOutput(output string) string {
	return tail(scrubNetworkAddrs(redactCloneError(output)), 400)
}

const (
	minGitPinMajor = 2
	minGitPinMinor = 37
)

type gitVersion struct {
	major, minor int
	ok           bool
}

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
