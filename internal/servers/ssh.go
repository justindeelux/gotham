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

const (
	sshDialTimeout = 15 * time.Second

	checkDocker = "docker"
	checkCPU    = "cpu"
	checkRAM    = "ram"
	checkDisk   = "disk"

	dockerVersionCmd = "docker version --format '{{.Server.Version}}'"
	unameCmd         = "uname -sm"
	meminfoCmd       = "cat /proc/meminfo"
	diskCmd          = "df -Pk / | tail -1"

	dockerPrepareTimeout = 30 * time.Second

	dockerInstallTimeout = 30 * time.Minute

	dockerInstallableTimeout = 5 * time.Second

	dockerUIDCmd       = "id -u"
	dockerSudoCheckCmd = "sudo -n true"
	dockerDistroCmd    = `sed -n -E 's/^(ID|ID_LIKE)="?([^"]*)"?$/\2/p' /etc/os-release 2>/dev/null | tr '[:upper:]' '[:lower:]'`
	dockerArchCmd      = "uname -m"
)

const dockerSudoPrefix = "sudo -n "

type SSHAuth struct {
	PrivateKeyPEM []byte
	Passphrase    string
	Password      string
}

type NodeInfo struct {
	DockerVersion string
	OS            string
	Arch          string
	TotalMem      int64
	TotalDisk     int64
}

type CheckResult struct {
	Name        string `json:"name"`
	OK          bool   `json:"ok"`
	Detail      string `json:"detail"`
	Installable bool   `json:"installable,omitempty"`
}

type HostKeyPolicy struct {
	Pinned         string
	AcceptUnpinned bool
}

type hostKeyVerifier struct {
	pinned         string
	acceptUnpinned bool
	observed       string
}

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

	out, err := runRemote(ctx, client, dockerVersionCmd)
	if err != nil {
		checks = append(checks, CheckResult{
			Name:        checkDocker,
			OK:          false,
			Detail:      dockerFailure(out, err),
			Installable: dockerMissing(out) && dockerInstallable(ctx, client),
		})
	} else if version := firstLine(out); version == "" {
		checks = append(checks, CheckResult{Name: checkDocker, OK: false, Detail: "docker returned an empty version"})
	} else {
		info.DockerVersion = version
		checks = append(checks, CheckResult{Name: checkDocker, OK: true, Detail: version})
	}

	out, err = runRemote(ctx, client, unameCmd)
	if err != nil {
		checks = append(checks, CheckResult{Name: checkCPU, OK: false, Detail: detailOrError(out, err)})
	} else if osName, arch := parseUname(out); osName == "" || arch == "" {
		checks = append(checks, CheckResult{Name: checkCPU, OK: false, Detail: "unexpected uname output: " + strings.TrimSpace(out)})
	} else {
		info.OS, info.Arch = osName, arch
		checks = append(checks, CheckResult{Name: checkCPU, OK: true, Detail: strings.TrimSpace(out)})
	}

	out, err = runRemote(ctx, client, meminfoCmd)
	if err != nil {
		checks = append(checks, CheckResult{Name: checkRAM, OK: false, Detail: detailOrError(out, err)})
	} else if total, ok := parseMemTotal(out); ok {
		info.TotalMem = total
		checks = append(checks, CheckResult{Name: checkRAM, OK: true, Detail: fmt.Sprintf("%d bytes", total)})
	} else {
		checks = append(checks, CheckResult{Name: checkRAM, OK: false, Detail: "could not parse MemTotal from /proc/meminfo"})
	}

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

func parseUname(out string) (osName, arch string) {
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return "", ""
	}
	return fields[0], fields[1]
}

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

func dockerMissing(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "not found") || strings.Contains(lower, "no such file")
}

func dockerFailure(out string, err error) string {
	detail := detailOrError(out, err)
	if dockerMissing(out) {
		return "Docker Engine is not installed on this node. Use Install Docker in the Add-server wizard, or install it from the App Store on a registered node: " + detail
	}
	return "docker version failed: " + detail
}

func detailOrError(out string, err error) string {
	if detail := strings.TrimSpace(out); detail != "" {
		return detail
	}
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}

func firstLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func nonEmptyLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

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

	prepCtx, cancel := context.WithTimeout(ctx, dockerPrepareTimeout)
	defer cancel()

	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	client, conn, err := dialSSH(prepCtx, addr, cfg)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	defer func() { _ = client.Close() }()

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

	deadline := time.Now().Add(dockerInstallTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}

	if _, err := fmt.Fprintf(out, "Installing Docker with %s\n", family); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if err := runRemoteStream(ctx, client, script, out); err != nil {
		return fmt.Errorf("docker install failed: %w", err)
	}
	return nil
}

func dockerUnsupportedReason(distroID string) string {
	if distroID != "" {
		return "automatic Docker install is not supported on " + distroID
	}
	return "automatic Docker install is not supported on this node: no supported apt, apk or dnf package manager was found"
}

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

func dockerArchSupported(arch string) bool {
	switch arch {
	case "x86_64", "amd64", "aarch64", "arm64":
		return true
	default:
		return false
	}
}

func dockerArchOK(ctx context.Context, client *ssh.Client) (bool, error) {
	out, err := runRemote(ctx, client, dockerArchCmd)
	if err != nil {
		return false, fmt.Errorf("%w: could not determine the node architecture: %s", ErrValidation, detailOrError(out, err))
	}
	return dockerArchSupported(firstLine(out)), nil
}

func dockerInstallable(ctx context.Context, client *ssh.Client) bool {
	detectCtx, cancel := context.WithTimeout(ctx, dockerInstallableTimeout)
	defer cancel()
	if _, _, ok := dockerDetect(detectCtx, client); !ok {
		return false
	}
	ok, err := dockerArchOK(detectCtx, client)
	return err == nil && ok
}

var dockerSupportedIDs = map[string]string{
	"debian":    "apt",
	"ubuntu":    "apt",
	"raspbian":  "apt",
	"linuxmint": "apt",
	"pop":       "apt",
	"kali":      "apt",
	"alpine":    "apk",
	"fedora":    "dnf",
}

var dockerRHELFamily = map[string]bool{
	"rhel": true, "centos": true, "rocky": true, "almalinux": true,
	"oracle": true, "amzn": true, "ol": true,
}

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
		return ""
	}
}

func DockerInstallExitCode(err error) int {
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return exit.ExitStatus()
	}
	return 0
}

func runRemoteStream(ctx context.Context, client *ssh.Client, cmd string, out io.Writer) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()

	sink := &syncWriter{w: out}
	session.Stdout = sink
	session.Stderr = sink
	_ = session.RequestPty("xterm", 40, 120, ssh.TerminalModes{})

	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		_ = session.Close()
		<-done
		return ctx.Err()
	case err := <-done:
		return err
	}
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
