package servers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSH validation constants.
const (
	// sshDialTimeout bounds an entire validation run: connect plus all probes.
	sshDialTimeout = 15 * time.Second

	checkDocker = "docker"
	checkCPU    = "cpu"
	checkRAM    = "ram"
	checkDisk   = "disk"

	dockerVersionCmd = "docker version --format '{{.Server.Version}}'"
	unameCmd         = "uname -sm"
	meminfoCmd       = "cat /proc/meminfo"
	diskCmd          = "df -Pk / | tail -1"
)

// SSHAuth carries the credentials used to authenticate one SSH session.
// PrivateKeyPEM takes precedence; Password is the fallback.
type SSHAuth struct {
	PrivateKeyPEM []byte
	Passphrase    string
	Password      string
}

// NodeInfo is the hardware/software inventory gathered from a node.
type NodeInfo struct {
	DockerVersion string
	OS            string
	Arch          string
	TotalMem      int64
	TotalDisk     int64
}

// CheckResult is the outcome of one probe, reported to the API so operators can
// see exactly which step failed.
type CheckResult struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// HostKeyPolicy is the host-key trust decision for one validation run.
type HostKeyPolicy struct {
	// Pinned is the expected host key fingerprint in OpenSSH SHA256 form
	// ("SHA256:..."). Empty means the node has no pin yet.
	Pinned string
	// AcceptUnpinned trusts a node whose key is not yet pinned: TOFU after a
	// successful public-key validation, or an operator's explicit trust for
	// password auth. It is ignored once Pinned is set.
	AcceptUnpinned bool
}

// hostKeyVerifier implements ssh.HostKeyCallback. It remembers the presented
// fingerprint so a first-use pin can be persisted, and fails closed on a pin
// mismatch.
type hostKeyVerifier struct {
	pinned         string
	acceptUnpinned bool
	observed       string
}

// check accepts the key only when it matches the pin, or when the host is
// unpinned and the policy allows trusting it on first use.
func (v *hostKeyVerifier) check(hostname string, _ net.Addr, key ssh.PublicKey) error {
	v.observed = ssh.FingerprintSHA256(key)
	switch {
	case v.pinned == "":
		if !v.acceptUnpinned {
			return fmt.Errorf("ssh: host %s is not pinned; refusing to trust %s", hostname, v.observed)
		}
		return nil
	case v.observed == v.pinned:
		return nil
	default:
		return fmt.Errorf("ssh: host key for %s changed: got %s, want %s", hostname, v.observed, v.pinned)
	}
}

// ValidateNode dials the node over SSH and runs the fixed set of probe
// commands. It returns one CheckResult per probe (docker, cpu, ram, disk), the
// gathered NodeInfo, and the host key fingerprint the node presented (empty
// when no handshake completed). A failed SSH connection is returned as an
// error; a failed individual probe is reported as a CheckResult so the other
// probes still run.
func ValidateNode(ctx context.Context, ip string, port int, user string, auth SSHAuth, policy HostKeyPolicy) ([]CheckResult, NodeInfo, string, error) {
	var info NodeInfo

	if ip == "" {
		return nil, info, "", fmt.Errorf("%w: ip is required", ErrValidation)
	}
	if user == "" {
		return nil, info, "", fmt.Errorf("%w: ssh user is required", ErrValidation)
	}
	if port <= 0 {
		port = 22
	}

	verifier := &hostKeyVerifier{pinned: policy.Pinned, acceptUnpinned: policy.AcceptUnpinned}
	cfg, err := sshClientConfig(user, auth, verifier)
	if err != nil {
		return nil, info, "", err
	}

	ctx, cancel := context.WithTimeout(ctx, sshDialTimeout)
	defer cancel()

	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	client, err := dialSSH(ctx, addr, cfg)
	if err != nil {
		return nil, info, verifier.observed, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	defer func() { _ = client.Close() }()

	checks := make([]CheckResult, 0, 4)

	// docker: the only probe whose failure is terminal for the node's purpose.
	out, err := runRemote(ctx, client, dockerVersionCmd)
	if err != nil {
		checks = append(checks, CheckResult{Name: checkDocker, OK: false, Detail: dockerFailure(out, err)})
	} else if version := firstLine(out); version == "" {
		checks = append(checks, CheckResult{Name: checkDocker, OK: false, Detail: "docker returned an empty version"})
	} else {
		info.DockerVersion = version
		checks = append(checks, CheckResult{Name: checkDocker, OK: true, Detail: version})
	}

	// cpu: uname reports both the OS name and the CPU architecture.
	out, err = runRemote(ctx, client, unameCmd)
	if err != nil {
		checks = append(checks, CheckResult{Name: checkCPU, OK: false, Detail: detailOrError(out, err)})
	} else if osName, arch := parseUname(out); osName == "" || arch == "" {
		checks = append(checks, CheckResult{Name: checkCPU, OK: false, Detail: "unexpected uname output: " + strings.TrimSpace(out)})
	} else {
		info.OS, info.Arch = osName, arch
		checks = append(checks, CheckResult{Name: checkCPU, OK: true, Detail: strings.TrimSpace(out)})
	}

	// ram: MemTotal from /proc/meminfo (kB -> bytes).
	out, err = runRemote(ctx, client, meminfoCmd)
	if err != nil {
		checks = append(checks, CheckResult{Name: checkRAM, OK: false, Detail: detailOrError(out, err)})
	} else if total, ok := parseMemTotal(out); ok {
		info.TotalMem = total
		checks = append(checks, CheckResult{Name: checkRAM, OK: true, Detail: fmt.Sprintf("%d bytes", total)})
	} else {
		checks = append(checks, CheckResult{Name: checkRAM, OK: false, Detail: "could not parse MemTotal from /proc/meminfo"})
	}

	// disk: total size of the root filesystem (1K blocks -> bytes).
	out, err = runRemote(ctx, client, diskCmd)
	if err != nil {
		checks = append(checks, CheckResult{Name: checkDisk, OK: false, Detail: detailOrError(out, err)})
	} else if total, ok := parseDiskTotal(out); ok {
		info.TotalDisk = total
		checks = append(checks, CheckResult{Name: checkDisk, OK: true, Detail: fmt.Sprintf("%d bytes", total)})
	} else {
		checks = append(checks, CheckResult{Name: checkDisk, OK: false, Detail: "could not parse df output: " + strings.TrimSpace(out)})
	}

	return checks, info, verifier.observed, nil
}

// sshClientConfig builds the SSH client configuration for the given credentials.
// Private-key auth is preferred; password auth is the fallback. Host keys are
// verified by verifier, never blindly accepted.
func sshClientConfig(user string, auth SSHAuth, verifier *hostKeyVerifier) (*ssh.ClientConfig, error) {
	methods := make([]ssh.AuthMethod, 0, 2)

	if len(auth.PrivateKeyPEM) > 0 {
		signer, err := parsePrivateKey(auth.PrivateKeyPEM, auth.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("%w: parse ssh private key: %v", ErrValidation, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if auth.Password != "" {
		methods = append(methods, ssh.Password(auth.Password))
	}
	if len(methods) == 0 {
		return nil, ErrNoCredentials
	}

	return &ssh.ClientConfig{
		User:            user,
		Auth:            methods,
		HostKeyCallback: verifier.check,
		Timeout:         sshDialTimeout,
	}, nil
}

// parsePrivateKey parses a PEM private key, optionally decrypting it with a
// passphrase. A missing passphrase for an encrypted key reports a clear error.
func parsePrivateKey(pemBytes []byte, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(passphrase))
	}

	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, errors.New("private key is passphrase-protected; a passphrase is required")
		}
		return nil, err
	}
	return signer, nil
}

// dialSSH opens the TCP connection and completes the SSH handshake, honouring
// the context deadline.
func dialSSH(ctx context.Context, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ssh.NewClient(clientConn, chans, reqs), nil
}

// runRemote executes cmd and returns its combined output, aborting if ctx is
// cancelled before the command finishes.
func runRemote(ctx context.Context, client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer func() { _ = session.Close() }()

	type outcome struct {
		out []byte
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		out, err := session.CombinedOutput(cmd)
		done <- outcome{out: out, err: err}
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		return "", ctx.Err()
	case result := <-done:
		return string(result.out), result.err
	}
}

// parseUname splits "Linux x86_64" into its OS and architecture parts.
func parseUname(out string) (osName, arch string) {
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return "", ""
	}
	return fields[0], fields[1]
}

// parseMemTotal extracts MemTotal from /proc/meminfo and returns it in bytes.
func parseMemTotal(out string) (int64, bool) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "MemTotal:"))
		if len(fields) == 0 {
			return 0, false
		}
		kb, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}

// parseDiskTotal extracts the total size (in bytes) from the last line of a
// `df -Pk` output, whose second column is the size in 1K blocks.
func parseDiskTotal(out string) (int64, bool) {
	lines := nonEmptyLines(out)
	if len(lines) == 0 {
		return 0, false
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 2 {
		return 0, false
	}
	kb, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return kb * 1024, true
}

// dockerFailure renders a clear message for a failed docker probe.
func dockerFailure(out string, err error) string {
	detail := detailOrError(out, err)
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "not found") || strings.Contains(lower, "no such file") {
		return "docker is not installed or not on PATH: " + detail
	}
	return "docker version failed: " + detail
}

// detailOrError prefers the command output, falling back to the error text.
func detailOrError(out string, err error) string {
	if detail := strings.TrimSpace(out); detail != "" {
		return detail
	}
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}

// firstLine returns the first non-empty trimmed line of out.
func firstLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// nonEmptyLines returns the trimmed non-empty lines of out.
func nonEmptyLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
