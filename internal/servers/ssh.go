package servers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
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

	// dockerPrepareTimeout bounds the install handshake and its preflight
	// probes (uid, architecture, distribution). It is wider than a probe run so
	// a slow node is not refused before the install even starts.
	dockerPrepareTimeout = 30 * time.Second

	// dockerInstallTimeout bounds one Docker install; an apt/dnf install can
	// legitimately run for minutes, so it is far wider than a probe run.
	dockerInstallTimeout = 30 * time.Minute

	// dockerInstallableTimeout bounds the extra detection the docker probe runs
	// to decide whether to offer the automatic install, so a slow node cannot
	// spend the whole validation budget on it.
	dockerInstallableTimeout = 5 * time.Second

	// Docker-install probes. All are fixed commands: no caller value is ever
	// interpolated into a command line.
	dockerUIDCmd       = "id -u"
	dockerSudoCheckCmd = "sudo -n true"
	dockerDistroCmd    = `sed -n -E 's/^(ID|ID_LIKE)="?([^"]*)"?$/\2/p' /etc/os-release 2>/dev/null | tr '[:upper:]' '[:lower:]'`
	dockerArchCmd      = "uname -m"
)

// dockerSudoPrefix escalates the fixed install argv when the SSH user is not
// root but has passwordless privilege escalation. It is one of exactly two
// constants, never derived from caller input.
const dockerSudoPrefix = "sudo -n "

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
	// Installable reports that a failed docker check can be fixed automatically
	// on this node (a supported distribution and architecture). The wizard only
	// offers the install when it is set, so a node the installer cannot handle
	// is never offered an action that would fail.
	Installable bool `json:"installable,omitempty"`
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
	client, _, err := dialSSH(ctx, addr, cfg)
	if err != nil {
		return nil, info, verifier.observed, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	defer func() { _ = client.Close() }()

	checks := make([]CheckResult, 0, 4)

	// docker: the only probe whose failure is terminal for the node's purpose.
	out, err := runRemote(ctx, client, dockerVersionCmd)
	if err != nil {
		checks = append(checks, CheckResult{
			Name:   checkDocker,
			OK:     false,
			Detail: dockerFailure(out, err),
			// Only a missing binary can be installed; a present but broken or
			// stopped daemon is not something this installer fixes.
			Installable: dockerMissing(out) && dockerInstallable(ctx, client),
		})
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
// the context deadline. The deadline is applied to the connection itself, not
// just the dial, so the key exchange (NewClientConn) and every later session
// read are bounded too: a peer that accepts TCP then stalls cannot hang
// validation past the run's deadline (A4-14).
// The net.Conn is returned alongside the client so a caller with a longer
// deadline than a probe run (the Docker installer) can extend it after the
// handshake without redialing.
func dialSSH(ctx context.Context, addr string, cfg *ssh.ClientConfig) (*ssh.Client, net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	deadline := time.Now().Add(sshDialTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return ssh.NewClient(clientConn, chans, reqs), conn, nil
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

// dockerMissing reports whether a failed docker probe failed because the
// binary is absent (rather than a broken or stopped daemon).
func dockerMissing(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "not found") || strings.Contains(lower, "no such file")
}

// dockerFailure renders a clear message for a failed docker probe.
func dockerFailure(out string, err error) string {
	detail := detailOrError(out, err)
	if dockerMissing(out) {
		// The wizard offers an in-product install over this same SSH session
		// (JUS-187); a registered node can install it from the App Store. Point
		// at those actions instead of naming PATH.
		return "Docker Engine is not installed on this node. Use Install Docker in the Add-server wizard, or install it from the App Store on a registered node: " + detail
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

// InstallDockerNode installs Docker Engine on a node over SSH with the node's
// allow-listed package manager for its distribution, streaming the installer's
// combined output to out.
//
// Everything that can be decided before the node is touched — the SSH
// connection, the SSH user's privilege, the distribution and architecture —
// is reported as an error *before* anything is written to out, so an HTTP
// caller can still answer a real error status. The install itself is a fixed,
// allow-listed argv per distribution: no caller value is ever interpolated
// into a command. A non-zero exit after the stream started is returned to the
// caller once the output has streamed.
func InstallDockerNode(ctx context.Context, ip string, port int, user string, auth SSHAuth, policy HostKeyPolicy, out io.Writer) error {
	if ip == "" {
		return fmt.Errorf("%w: ip is required", ErrValidation)
	}
	if user == "" {
		return fmt.Errorf("%w: ssh user is required", ErrValidation)
	}
	if port <= 0 {
		port = 22
	}

	verifier := &hostKeyVerifier{pinned: policy.Pinned, acceptUnpinned: policy.AcceptUnpinned}
	cfg, err := sshClientConfig(user, auth, verifier)
	if err != nil {
		return err
	}

	// The handshake and preflight are bounded like a probe run; the install
	// session below is given the caller's context and the wider install
	// deadline instead.
	prepCtx, cancel := context.WithTimeout(ctx, dockerPrepareTimeout)
	defer cancel()

	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	client, conn, err := dialSSH(prepCtx, addr, cfg)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	defer func() { _ = client.Close() }()

	// The distribution decides support; a distro the installer does not cover
	// is refused before any privilege probe, so the operator gets the honest
	// reason rather than a privilege error.
	family, distroID, ok := dockerDetect(prepCtx, client)
	if !ok {
		return fmt.Errorf("%w: %s", ErrValidation, dockerUnsupportedReason(distroID))
	}
	archSupported, err := dockerArchOK(prepCtx, client)
	if err != nil {
		return err
	}
	if !archSupported {
		return fmt.Errorf("%w: automatic Docker install is not supported on this architecture", ErrValidation)
	}
	prefix, err := dockerPrivilege(prepCtx, client, user)
	if err != nil {
		return err
	}
	script := dockerFamilyScript(family, prefix)

	// Extend the connection deadline for the install, never past the caller's
	// own deadline.
	deadline := time.Now().Add(dockerInstallTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}

	// Commit the stream before the package manager runs: once the first byte is
	// out an HTTP caller can no longer change its status, so a failure from
	// here on must be reported in-band (the handler's terminal marker).
	if _, err := fmt.Fprintf(out, "Installing Docker with %s\n", family); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if err := runRemoteStream(ctx, client, script, out); err != nil {
		return fmt.Errorf("docker install failed: %w", err)
	}
	return nil
}

// dockerUnsupportedReason names the detected distribution when the automatic
// installer does not cover it, so the failure is honest about the node.
func dockerUnsupportedReason(distroID string) string {
	if distroID != "" {
		return "automatic Docker install is not supported on " + distroID
	}
	return "automatic Docker install is not supported on this node: no supported apt, apk or dnf package manager was found"
}

// dockerPrivilege reports the fixed command prefix the install argv needs: "" as
// root, the sudo prefix for a non-root user with passwordless escalation, or an
// honest error when neither holds.
func dockerPrivilege(ctx context.Context, client *ssh.Client, user string) (string, error) {
	out, err := runRemote(ctx, client, dockerUIDCmd)
	if err != nil {
		return "", fmt.Errorf("%w: could not determine the SSH user's privileges: %s", ErrValidation, detailOrError(out, err))
	}
	if firstLine(out) == "0" {
		return "", nil
	}
	if _, err := runRemote(ctx, client, dockerSudoCheckCmd); err != nil {
		return "", fmt.Errorf("%w: the node user %q is not root and has no passwordless privilege escalation; Docker must be installed by root, so add the node with a root user or install Docker on it yourself first", ErrValidation, user)
	}
	return dockerSudoPrefix, nil
}

// dockerArchSupported reports whether the allow-listed install material covers
// an architecture.
func dockerArchSupported(arch string) bool {
	switch arch {
	case "x86_64", "amd64", "aarch64", "arm64":
		return true
	default:
		return false
	}
}

// dockerArchOK reads the node's architecture and reports whether the automatic
// install covers it.
func dockerArchOK(ctx context.Context, client *ssh.Client) (bool, error) {
	out, err := runRemote(ctx, client, dockerArchCmd)
	if err != nil {
		return false, fmt.Errorf("%w: could not determine the node architecture: %s", ErrValidation, detailOrError(out, err))
	}
	return dockerArchSupported(firstLine(out)), nil
}

// dockerInstallable reports whether the automatic install covers this node, so
// the validate response can gate the offer on it. It is bounded and never
// fails the probe: a detection failure is simply "not offered".
func dockerInstallable(ctx context.Context, client *ssh.Client) bool {
	detectCtx, cancel := context.WithTimeout(ctx, dockerInstallableTimeout)
	defer cancel()
	// Check the distribution first: an unsupported one skips the arch probe.
	if _, _, ok := dockerDetect(detectCtx, client); !ok {
		return false
	}
	ok, err := dockerArchOK(detectCtx, client)
	return err == nil && ok
}

// dockerSupportedIDs maps the lowercase ID/ID_LIKE tokens of /etc/os-release to
// the package-manager family whose own repository ships a real Docker Engine.
// The RHEL family (rhel, centos, rocky, almalinux, oracle, amzn) is
// deliberately absent: their base repositories do not carry Docker Engine, so
// `dnf install docker` either resolves to a podman shim or fails. Installing
// those would need a third-party repository and a key we will not fetch, so
// they are reported as unsupported instead.
var dockerSupportedIDs = map[string]string{
	"debian":    "apt",
	"ubuntu":    "apt",
	"raspbian":  "apt",
	"linuxmint": "apt",
	"pop":       "apt",
	"kali":      "apt",
	"alpine":    "apk",
	// Fedora's moby-engine is the upstream Docker Engine build shipped in
	// Fedora's own repositories (the packaged `docker` was a podman alias).
	"fedora": "dnf",
}

// dockerRHELFamily is the set of ids whose base repos have no Docker Engine;
// naming them keeps the "unsupported" message specific.
var dockerRHELFamily = map[string]bool{
	"rhel": true, "centos": true, "rocky": true, "almalinux": true,
	"oracle": true, "amzn": true, "ol": true,
}

// matchDistro reads the first recognized ID/ID_LIKE token of /etc/os-release and
// returns the install family plus the token. The token is "" when nothing is
// recognized. Tokens are scanned in file order, so a distribution is classified
// by its own ID before any ID_LIKE fallback (Rocky is "rocky", not "fedora").
func matchDistro(out string) (family, id string) {
	for _, token := range strings.Fields(out) {
		if family, ok := dockerSupportedIDs[token]; ok {
			return family, token
		}
		if dockerRHELFamily[token] {
			return "", token
		}
	}
	return "", ""
}

// dockerDetect resolves the install family and the distribution id used in
// messages. It reads /etc/os-release and falls back to a fixed package-manager
// probe only for the families whose own repository ships a real engine.
func dockerDetect(ctx context.Context, client *ssh.Client) (family, distroID string, ok bool) {
	if out, err := runRemote(ctx, client, dockerDistroCmd); err == nil {
		if family, id := matchDistro(out); family != "" {
			return family, id, true
		} else if id != "" {
			return "", id, false
		}
	}
	for _, candidate := range []struct{ probe, family string }{
		{"apt-get", "apt"},
		{"apk", "apk"},
	} {
		if _, err := runRemote(ctx, client, "command -v "+candidate.probe); err == nil {
			return candidate.family, "", true
		}
	}
	return "", "", false
}

// dockerFamilyScript returns the fixed, allow-listed install argv for a family,
// prefixed with the privilege command when one is needed. Every install is
// noninteractive and enables and starts the Docker service, so the follow-up
// probe sees a running daemon.
func dockerFamilyScript(family, prefix string) string {
	switch family {
	case "apt":
		return prefix + "env DEBIAN_FRONTEND=noninteractive apt-get update && " +
			prefix + "env DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io && " +
			prefix + "systemctl enable --now docker"
	case "dnf":
		return prefix + "dnf install -y moby-engine && " +
			prefix + "systemctl enable --now docker"
	case "apk":
		return prefix + "apk add --no-cache docker && " +
			prefix + "rc-update add docker boot && " +
			prefix + "service docker start"
	default:
		// dockerDetect only ever returns a family from dockerSupportedIDs.
		return ""
	}
}

// DockerInstallExitCode reports the remote exit status of a failed install, or
// 0 when the failure carried none (a preflight error or a cancellation). The
// HTTP layer relays it in the stream's terminal marker.
func DockerInstallExitCode(err error) int {
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return exit.ExitStatus()
	}
	return 0
}

// runRemoteStream runs cmd and streams its combined output to out, closing the
// session if ctx is cancelled first. Unlike runRemote it never buffers the
// output, so a long install's progress reaches the caller as it happens.
func runRemoteStream(ctx context.Context, client *ssh.Client, cmd string, out io.Writer) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()

	// x/crypto/ssh drains stdout and stderr in two goroutines; serializing them
	// keeps a single writer (an http.ResponseWriter) safe and the line order
	// stable.
	sink := &syncWriter{w: out}
	session.Stdout = sink
	session.Stderr = sink
	// A pty makes sshd deliver SIGHUP to the install's process group when the
	// channel closes, so a cancelled request does not leave a package manager
	// running (and holding the dpkg/dnf lock) on the node. It is best-effort: a
	// server that refuses the request still gets the signal + close below.
	_ = session.RequestPty("xterm", 40, 120, ssh.TerminalModes{})

	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()

	select {
	case <-ctx.Done():
		// Ask the remote to stop, then close: with a pty the close also hangs up
		// the process group. Many sshd servers ignore a signal on a non-pty
		// session, so both are attempted.
		_ = session.Signal(ssh.SIGTERM)
		_ = session.Close()
		// Wait for Run to unwind before returning, so it never writes to the
		// response after the handler has handed it back.
		<-done
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// syncWriter serializes writes, so a caller's writer never sees concurrent
// writes from the session's stdout and stderr goroutines.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
