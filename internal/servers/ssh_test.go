package servers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshProbeServer is a tiny in-process SSH server that answers the fixed probe
// and Docker-install commands with canned output, so validation and install can
// be tested without a real node.
type sshProbeServer struct {
	listener      net.Listener
	password      string
	dockerMissing bool
	// uid answers `id -u`; empty means root ("0").
	uid string
	// distro is the /etc/os-release ID; empty means "debian".
	distro string
	// arch answers `uname -m`; empty means "x86_64".
	arch string
	// sudoOK answers `sudo -n true` for a non-root user.
	sudoOK bool

	mu       sync.Mutex
	commands []string
}

// startSSHProbeServer starts the probe server on a random localhost port and
// returns its address, the SHA256 fingerprint of its host key, and a stop
// function.
func startSSHProbeServer(t *testing.T, password string, dockerMissing bool) (string, string, func()) {
	t.Helper()
	return startSSHServer(t, &sshProbeServer{password: password, dockerMissing: dockerMissing})
}

// startSSHServer starts a customized probe server (privilege, distribution and
// architecture overrides) on a random localhost port.
func startSSHServer(t *testing.T, server *sshProbeServer) (string, string, func()) {
	t.Helper()

	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}

	server.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil // accept any public key
		},
	}
	if server.password != "" {
		config.PasswordCallback = func(_ ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) == server.password {
				return nil, nil
			}
			return nil, errors.New("password rejected")
		}
	}
	config.AddHostKey(signer)

	go func() {
		for {
			conn, err := server.listener.Accept()
			if err != nil {
				return
			}
			go server.handle(conn, config)
		}
	}()

	return server.listener.Addr().String(), ssh.FingerprintSHA256(signer.PublicKey()), func() { _ = server.listener.Close() }
}

// handle performs the SSH handshake and answers session exec requests.
func (s *sshProbeServer) handle(conn net.Conn, config *ssh.ServerConfig) {
	defer func() { _ = conn.Close() }()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer func() { _ = sshConn.Close() }()
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only sessions are supported")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go s.serveSession(channel, requests)
	}
}

// serveSession reads exec requests and replies with canned probe output.
func (s *sshProbeServer) serveSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer func() { _ = channel.Close() }()

	for request := range requests {
		if request.Type != "exec" {
			_ = request.Reply(false, nil)
			continue
		}

		var payload struct{ Command string }
		if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
			_ = request.Reply(false, nil)
			continue
		}
		_ = request.Reply(true, nil)

		s.mu.Lock()
		s.commands = append(s.commands, payload.Command)
		s.mu.Unlock()

		stdout, stderr, status := s.cannedOutput(payload.Command)
		if stdout != "" {
			_, _ = io.WriteString(channel, stdout)
		}
		if stderr != "" {
			_, _ = io.WriteString(channel.Stderr(), stderr)
		}
		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: status}))
		_ = channel.Close()
	}
}

// lastCommand returns the most recently exec'd command.
func (s *sshProbeServer) lastCommand() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.commands) == 0 {
		return ""
	}
	return s.commands[len(s.commands)-1]
}

// cannedOutput maps a fixed command to its canned output and exit status.
func (s *sshProbeServer) cannedOutput(command string) (stdout, stderr string, status uint32) {
	switch {
	case strings.Contains(command, "docker version"):
		if s.dockerMissing {
			return "", "bash: docker: command not found\n", 127
		}
		return "24.0.7\n", "", 0
	case command == "id -u":
		if s.uid != "" {
			return s.uid + "\n", "", 0
		}
		return "0\n", "", 0
	case command == "sudo -n true":
		if s.sudoOK {
			return "", "", 0
		}
		return "", "sudo: a password is required\n", 1
	case strings.Contains(command, "/etc/os-release"):
		if s.distro != "" {
			return s.distro + "\n", "", 0
		}
		return "debian\n", "", 0
	case command == "uname -m":
		if s.arch != "" {
			return s.arch + "\n", "", 0
		}
		return "x86_64\n", "", 0
	case strings.Contains(command, "uname -sm"):
		return "Linux x86_64\n", "", 0
	case strings.Contains(command, "apt-get install"):
		return "Setting up docker.io\n", "", 0
	case strings.Contains(command, "dnf install"):
		return "Installing docker...\n", "", 0
	case strings.Contains(command, "apk add"):
		return "OK: 12 MiB in 30 packages\n", "", 0
	case strings.Contains(command, "/proc/meminfo"):
		return "MemTotal:        8192000 kB\nMemFree:         1048576 kB\n", "", 0
	case strings.Contains(command, "df -Pk"):
		return "Filesystem     1024-blocks     Used Available Capacity Mounted on\n/dev/vda1         20480000  1000000  19000000       5% /\n", "", 0
	default:
		return "", "sh: command not found\n", 127
	}
}

// testPrivateKeyPEM generates an ed25519 key and returns its OpenSSH PEM.
func testPrivateKeyPEM(t *testing.T) []byte {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	return pem.EncodeToMemory(block)
}

// testEncryptedPrivateKeyPEM generates an ed25519 key and returns its OpenSSH
// PEM encrypted with passphrase.
func testEncryptedPrivateKeyPEM(t *testing.T, passphrase string) []byte {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(privateKey, "", []byte(passphrase))
	if err != nil {
		t.Fatalf("marshal encrypted private key: %v", err)
	}
	return pem.EncodeToMemory(block)
}

// target splits a host:port address into a probe target.
func target(t *testing.T, addr string) (string, int) {
	t.Helper()

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return host, port
}

// checkByName returns the named check result.
func checkByName(t *testing.T, checks []CheckResult, name string) CheckResult {
	t.Helper()

	for _, check := range checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("check %q not present in %+v", name, checks)
	return CheckResult{}
}

func TestValidateNodeHappyPath(t *testing.T) {
	addr, _, stop := startSSHProbeServer(t, "unused", false)
	defer stop()
	host, port := target(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	checks, info, _, err := ValidateNode(ctx, host, port, "root", SSHAuth{PrivateKeyPEM: testPrivateKeyPEM(t)}, HostKeyPolicy{AcceptUnpinned: true})
	if err != nil {
		t.Fatalf("ValidateNode: %v", err)
	}
	if len(checks) != 4 {
		t.Fatalf("checks = %d, want 4", len(checks))
	}
	for _, check := range checks {
		if !check.OK {
			t.Errorf("check %q failed: %s", check.Name, check.Detail)
		}
	}

	if info.DockerVersion != "24.0.7" {
		t.Errorf("DockerVersion = %q, want 24.0.7", info.DockerVersion)
	}
	if info.OS != "Linux" || info.Arch != "x86_64" {
		t.Errorf("OS/Arch = %q/%q, want Linux/x86_64", info.OS, info.Arch)
	}
	if want := int64(8192000) * 1024; info.TotalMem != want {
		t.Errorf("TotalMem = %d, want %d", info.TotalMem, want)
	}
	if want := int64(20480000) * 1024; info.TotalDisk != want {
		t.Errorf("TotalDisk = %d, want %d", info.TotalDisk, want)
	}
}

func TestValidateNodePasswordFallback(t *testing.T) {
	addr, _, stop := startSSHProbeServer(t, "hunter2", false)
	defer stop()
	host, port := target(t, addr)

	checks, _, _, err := ValidateNode(context.Background(), host, port, "root", SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true})
	if err != nil {
		t.Fatalf("ValidateNode: %v", err)
	}
	if !checkByName(t, checks, checkDocker).OK {
		t.Error("docker check did not pass with password auth")
	}
}

func TestValidateNodeDockerMissing(t *testing.T) {
	addr, _, stop := startSSHProbeServer(t, "hunter2", true)
	defer stop()
	host, port := target(t, addr)

	checks, _, _, err := ValidateNode(context.Background(), host, port, "root", SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true})
	if err != nil {
		t.Fatalf("ValidateNode: %v", err)
	}

	docker := checkByName(t, checks, checkDocker)
	if docker.OK {
		t.Fatalf("docker check = OK, want failure (detail %q)", docker.Detail)
	}
	if !strings.Contains(docker.Detail, "not installed") {
		t.Errorf("docker detail = %q, want it to mention docker is not installed", docker.Detail)
	}
	if checkByName(t, checks, checkCPU).OK != true {
		t.Error("cpu check should still pass when docker is missing")
	}
}

func TestValidateNodeAuthFailure(t *testing.T) {
	addr, _, stop := startSSHProbeServer(t, "correct-password", false)
	defer stop()
	host, port := target(t, addr)

	_, _, _, err := ValidateNode(context.Background(), host, port, "root", SSHAuth{Password: "wrong-password"}, HostKeyPolicy{AcceptUnpinned: true})
	if err == nil {
		t.Fatal("ValidateNode with wrong password = nil error, want handshake failure")
	}
}

func TestValidateNodeNoCredentials(t *testing.T) {
	_, _, _, err := ValidateNode(context.Background(), "127.0.0.1", 22, "root", SSHAuth{}, HostKeyPolicy{})
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("ValidateNode with no credentials = %v, want ErrNoCredentials", err)
	}
}

func TestValidateNodeRejectsBadInput(t *testing.T) {
	if _, _, _, err := ValidateNode(context.Background(), "", 22, "root", SSHAuth{Password: "x"}, HostKeyPolicy{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty ip = %v, want ErrValidation", err)
	}
	if _, _, _, err := ValidateNode(context.Background(), "127.0.0.1", 22, "", SSHAuth{Password: "x"}, HostKeyPolicy{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty user = %v, want ErrValidation", err)
	}
}

// TestValidateNodePinsHostKey covers the first-use (TOFU) path: an unpinned host
// the policy trusts reports the fingerprint the caller must persist.
func TestValidateNodePinsHostKey(t *testing.T) {
	addr, wantFingerprint, stop := startSSHProbeServer(t, "hunter2", false)
	defer stop()
	host, port := target(t, addr)

	_, _, fingerprint, err := ValidateNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true})
	if err != nil {
		t.Fatalf("ValidateNode: %v", err)
	}
	if fingerprint != wantFingerprint {
		t.Errorf("fingerprint = %q, want %q", fingerprint, wantFingerprint)
	}
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		t.Errorf("fingerprint = %q, want the OpenSSH SHA256 form", fingerprint)
	}
}

// TestValidateNodePinnedHostKeyAccepted proves a pinned host key validates even
// when the policy would not trust an unpinned host.
func TestValidateNodePinnedHostKeyAccepted(t *testing.T) {
	addr, fingerprint, stop := startSSHProbeServer(t, "hunter2", false)
	defer stop()
	host, port := target(t, addr)

	checks, _, got, err := ValidateNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{Pinned: fingerprint})
	if err != nil {
		t.Fatalf("ValidateNode with a matching pin: %v", err)
	}
	if got != fingerprint {
		t.Errorf("fingerprint = %q, want %q", got, fingerprint)
	}
	if !checkByName(t, checks, checkDocker).OK {
		t.Error("docker check did not pass with a pinned host key")
	}
}

// TestValidateNodePinnedHostKeyChangeFailsClosed proves a MITM cannot swap the
// node's key after the first pin: validation is refused.
func TestValidateNodePinnedHostKeyChangeFailsClosed(t *testing.T) {
	addr, _, stop := startSSHProbeServer(t, "hunter2", false)
	defer stop()
	host, port := target(t, addr)

	_, _, _, err := ValidateNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{Pinned: "SHA256:0000000000000000000000000000000000000000000"})
	if err == nil {
		t.Fatal("ValidateNode accepted a changed host key")
	}
	if !strings.Contains(err.Error(), "changed") {
		t.Errorf("err = %v, want it to report a changed host key", err)
	}
}

// TestValidateNodeRefusesUnpinnedHost covers the password-auth default: with no
// pin and no explicit operator trust, an unknown host is refused.
func TestValidateNodeRefusesUnpinnedHost(t *testing.T) {
	addr, _, stop := startSSHProbeServer(t, "hunter2", false)
	defer stop()
	host, port := target(t, addr)

	_, _, _, err := ValidateNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{})
	if err == nil {
		t.Fatal("ValidateNode trusted an unpinned host without explicit consent")
	}
	if !strings.Contains(err.Error(), "not pinned") {
		t.Errorf("err = %v, want it to report the host is not pinned", err)
	}
}

// TestValidateNodeStopsAtDeadlineOnStalledHandshake covers A4-14: a peer that
// accepts the TCP connection and then never completes the SSH handshake must not
// hang validation past the run's deadline.
func TestValidateNodeStopsAtDeadlineOnStalledHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	released := make(chan struct{})
	defer close(released)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		// Hold the connection open, answering nothing.
		<-released
		_ = conn.Close()
	}()

	host, port := target(t, listener.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, _, err = ValidateNode(ctx, host, port, "root", SSHAuth{Password: "x"}, HostKeyPolicy{AcceptUnpinned: true})
	if err == nil {
		t.Fatal("ValidateNode against a stalled handshake returned nil error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("ValidateNode took %s, want it bounded near the 300ms context deadline", elapsed)
	}
}

// TestParsePrivateKeyPassphrase covers the parser's passphrase branches.
func TestParsePrivateKeyPassphrase(t *testing.T) {
	pemBytes := testEncryptedPrivateKeyPEM(t, "s3cret")

	if _, err := parsePrivateKey(pemBytes, ""); err == nil || !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("parsePrivateKey without a passphrase = %v, want a passphrase-required error", err)
	}
	if _, err := parsePrivateKey(pemBytes, "s3cret"); err != nil {
		t.Fatalf("parsePrivateKey with the passphrase: %v", err)
	}
	if _, err := parsePrivateKey(pemBytes, "wrong"); err == nil {
		t.Fatal("parsePrivateKey with the wrong passphrase returned nil error")
	}
}

func TestParseHelpers(t *testing.T) {
	if osName, arch := parseUname("Linux x86_64\n"); osName != "Linux" || arch != "x86_64" {
		t.Errorf("parseUname = %q/%q", osName, arch)
	}
	if osName, arch := parseUname("Linux"); osName != "" || arch != "" {
		t.Error("parseUname with one field should return empty results")
	}
	if got, ok := parseMemTotal("MemFree: 1 kB\nMemTotal: 2048 kB\n"); !ok || got != 2048*1024 {
		t.Errorf("parseMemTotal = %d, %v", got, ok)
	}
	if got, ok := parseDiskTotal("Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/vda1 1000 10 990 1% /\n"); !ok || got != 1000*1024 {
		t.Errorf("parseDiskTotal = %d, %v", got, ok)
	}
	if detail := dockerFailure("bash: docker: command not found", fmt.Errorf("exit status 127")); !strings.Contains(detail, "not installed") {
		t.Errorf("dockerFailure = %q", detail)
	}
}

func TestInstallDockerNodeRootApt(t *testing.T) {
	server := &sshProbeServer{password: "hunter2"}
	addr, _, stop := startSSHServer(t, server)
	defer stop()
	host, port := target(t, addr)

	var out bytes.Buffer
	err := InstallDockerNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true}, &out)
	if err != nil {
		t.Fatalf("InstallDockerNode: %v", err)
	}
	if !strings.Contains(out.String(), "Setting up docker.io") {
		t.Errorf("streamed output = %q, want the installer output", out.String())
	}
	if cmd := server.lastCommand(); !strings.HasPrefix(cmd, "env DEBIAN_FRONTEND=noninteractive apt-get update") {
		t.Errorf("install command = %q, want the allow-listed apt argv", cmd)
	}
}

// TestInstallDockerNodeNonRootUsesSudo proves a non-root SSH user with
// passwordless escalation runs the same fixed argv with the sudo prefix.
func TestInstallDockerNodeNonRootUsesSudo(t *testing.T) {
	server := &sshProbeServer{password: "hunter2", uid: "1000", sudoOK: true}
	addr, _, stop := startSSHServer(t, server)
	defer stop()
	host, port := target(t, addr)

	var out bytes.Buffer
	err := InstallDockerNode(context.Background(), host, port, "deploy",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true}, &out)
	if err != nil {
		t.Fatalf("InstallDockerNode: %v", err)
	}
	if cmd := server.lastCommand(); !strings.HasPrefix(cmd, "sudo -n env DEBIAN_FRONTEND=noninteractive apt-get update") {
		t.Errorf("install command = %q, want the sudo-prefixed allow-listed argv", cmd)
	}
}

// TestInstallDockerNodeNonRootWithoutSudo refuses before any output when the
// SSH user is neither root nor able to escalate.
func TestInstallDockerNodeNonRootWithoutSudo(t *testing.T) {
	server := &sshProbeServer{password: "hunter2", uid: "1000"}
	addr, _, stop := startSSHServer(t, server)
	defer stop()
	host, port := target(t, addr)

	var out bytes.Buffer
	err := InstallDockerNode(context.Background(), host, port, "deploy",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true}, &out)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %q before the privilege check failed", out.String())
	}
	if !strings.Contains(err.Error(), "not root") {
		t.Errorf("err = %v, want an honest privilege message", err)
	}
}

func TestInstallDockerNodeUnsupportedDistro(t *testing.T) {
	server := &sshProbeServer{password: "hunter2", distro: "void"}
	addr, _, stop := startSSHServer(t, server)
	defer stop()
	host, port := target(t, addr)

	var out bytes.Buffer
	err := InstallDockerNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true}, &out)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("err = %v, want an honest unsupported-distribution message", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %q for an unsupported distribution", out.String())
	}
}

func TestInstallDockerNodeUnsupportedArch(t *testing.T) {
	server := &sshProbeServer{password: "hunter2", arch: "i686"}
	addr, _, stop := startSSHServer(t, server)
	defer stop()
	host, port := target(t, addr)

	var out bytes.Buffer
	err := InstallDockerNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true}, &out)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "architecture") {
		t.Errorf("err = %v, want an honest unsupported-architecture message", err)
	}
}

// TestInstallDockerNodeRefusesUnpinnedHost proves the install honours the same
// host-key policy as validation: an unpinned host without consent is refused.
func TestInstallDockerNodeRefusesUnpinnedHost(t *testing.T) {
	server := &sshProbeServer{password: "hunter2"}
	addr, _, stop := startSSHServer(t, server)
	defer stop()
	host, port := target(t, addr)

	var out bytes.Buffer
	err := InstallDockerNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{}, &out)
	if err == nil {
		t.Fatal("InstallDockerNode trusted an unpinned host without consent")
	}
}

func TestMatchDistroFamily(t *testing.T) {
	cases := map[string]string{
		"debian\n":        "apt",
		"ubuntu debian\n": "apt",
		"alpine\n":        "apk",
		"fedora\n":        "dnf",
		"rhel centos\n":   "dnf",
		"opensuse\n":      "",
		"":                "",
	}
	for in, want := range cases {
		if got := matchDistroFamily(in); got != want {
			t.Errorf("matchDistroFamily(%q) = %q, want %q", in, got, want)
		}
	}
}
