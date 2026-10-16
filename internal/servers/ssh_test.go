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
	// osRelease is the output of the /etc/os-release probe; empty means
	// "debian".
	osRelease string
	// aptGet makes the `command -v apt-get` fallback probe succeed.
	aptGet bool
	// arch answers `uname -m`; empty means "x86_64".
	arch string
	// sudoOK answers `sudo -n true` for a non-root user.
	sudoOK bool
	// installStatus is the exit status of the install commands (0 by default).
	installStatus uint32
	// installBlock, when non-nil, holds the install command open until closed,
	// so a cancellation can be exercised.
	installBlock chan struct{}

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
		block := s.installBlock
		s.mu.Unlock()

		// Hold an install command open until the test releases it, so a
		// cancellation can be observed before the command finishes.
		if block != nil && strings.Contains(payload.Command, "install") {
			<-block
		}

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
		if s.osRelease != "" {
			return s.osRelease, "", 0
		}
		return "debian\n", "", 0
	case command == "command -v apt-get":
		if s.aptGet {
			return "/usr/bin/apt-get\n", "", 0
		}
		return "", "sh: apt-get: not found\n", 127
	case command == "uname -m":
		if s.arch != "" {
			return s.arch + "\n", "", 0
		}
		return "x86_64\n", "", 0
	case strings.Contains(command, "uname -sm"):
		return "Linux x86_64\n", "", 0
	case strings.Contains(command, "apt-get install"):
		return "Setting up docker.io\n", "", s.installStatus
	case strings.Contains(command, "dnf install"):
		return "Installing moby-engine...\n", "", s.installStatus
	case strings.Contains(command, "apk add"):
		return "OK: 12 MiB in 30 packages\n", "", s.installStatus
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

// TestInstallDockerNodeFedoraMobyEngine proves the Fedora plan installs the
// upstream engine package, not the podman `docker` alias.
func TestInstallDockerNodeFedoraMobyEngine(t *testing.T) {
	server := &sshProbeServer{password: "hunter2", osRelease: "fedora\n"}
	addr, _, stop := startSSHServer(t, server)
	defer stop()
	host, port := target(t, addr)

	var out bytes.Buffer
	if err := InstallDockerNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true}, &out); err != nil {
		t.Fatalf("InstallDockerNode: %v", err)
	}
	cmd := server.lastCommand()
	if !strings.Contains(cmd, "dnf install -y moby-engine") {
		t.Errorf("install command = %q, want the Fedora moby-engine argv", cmd)
	}
	if !strings.Contains(cmd, "systemctl enable --now docker") {
		t.Errorf("install command = %q, want the docker service enabled", cmd)
	}
}

// TestInstallDockerNodeAlpineStartsService proves the Alpine plan enables and
// starts the daemon with OpenRC, so the follow-up probe sees it running.
func TestInstallDockerNodeAlpineStartsService(t *testing.T) {
	server := &sshProbeServer{password: "hunter2", osRelease: "alpine\n"}
	addr, _, stop := startSSHServer(t, server)
	defer stop()
	host, port := target(t, addr)

	var out bytes.Buffer
	if err := InstallDockerNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true}, &out); err != nil {
		t.Fatalf("InstallDockerNode: %v", err)
	}
	cmd := server.lastCommand()
	if !strings.Contains(cmd, "rc-update add docker boot") || !strings.Contains(cmd, "service docker start") {
		t.Errorf("install command = %q, want the Alpine enable+start argv", cmd)
	}
}

// TestInstallDockerNodeRHELFamilyUnsupported proves the RHEL family is refused
// honestly: their base repositories ship no Docker Engine, so the automatic
// installer must not claim support (and must not add a third-party repo).
func TestInstallDockerNodeRHELFamilyUnsupported(t *testing.T) {
	for _, id := range []string{"rhel", "centos", "rocky", "almalinux", "oracle", "amzn"} {
		t.Run(id, func(t *testing.T) {
			server := &sshProbeServer{password: "hunter2", osRelease: id + "\n"}
			addr, _, stop := startSSHServer(t, server)
			defer stop()
			host, port := target(t, addr)

			var out bytes.Buffer
			err := InstallDockerNode(context.Background(), host, port, "root",
				SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true}, &out)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if !strings.Contains(err.Error(), "not supported on "+id) {
				t.Errorf("err = %v, want the detected distribution named", err)
			}
			if out.Len() != 0 {
				t.Errorf("wrote %q for an unsupported distribution", out.String())
			}
		})
	}
}

// TestMatchDistroClassifiesBeforeIDLike proves an id is classified by its own
// token first: Rocky's ID_LIKE carries fedora, which must not make it supported.
func TestMatchDistroClassifiesBeforeIDLike(t *testing.T) {
	if family, id := matchDistro("rocky\nrhel centos fedora\n"); family != "" || id != "rocky" {
		t.Errorf("matchDistro(rocky) = %q/%q, want unsupported/rocky", family, id)
	}
	if family, id := matchDistro("fedora\n"); family != "dnf" || id != "fedora" {
		t.Errorf("matchDistro(fedora) = %q/%q, want dnf/fedora", family, id)
	}
	if family, id := matchDistro("alpine\n"); family != "apk" || id != "alpine" {
		t.Errorf("matchDistro(alpine) = %q/%q, want apk/alpine", family, id)
	}
	if family, id := matchDistro("debian\n"); family != "apt" || id != "debian" {
		t.Errorf("matchDistro(debian) = %q/%q, want apt/debian", family, id)
	}
	if family, id := matchDistro("opensuse\n"); family != "" || id != "" {
		t.Errorf("matchDistro(opensuse) = %q/%q, want empty/empty", family, id)
	}
}

// TestInstallDockerNodeFallbackProbe covers the /etc/os-release-less node: an
// unknown id falls back to the fixed apt-get probe.
func TestInstallDockerNodeFallbackProbe(t *testing.T) {
	server := &sshProbeServer{password: "hunter2", osRelease: "void\n", aptGet: true}
	addr, _, stop := startSSHServer(t, server)
	defer stop()
	host, port := target(t, addr)

	var out bytes.Buffer
	if err := InstallDockerNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true}, &out); err != nil {
		t.Fatalf("InstallDockerNode: %v", err)
	}
	if cmd := server.lastCommand(); !strings.HasPrefix(cmd, "env DEBIAN_FRONTEND=noninteractive apt-get update") {
		t.Errorf("install command = %q, want the apt fallback argv", cmd)
	}
}

func TestInstallDockerNodeUnsupportedDistro(t *testing.T) {
	server := &sshProbeServer{password: "hunter2", osRelease: "void\n"}
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

// TestInstallDockerNodeNonZeroExit proves a failing install streams the header
// and returns an error that still carries the remote exit status, so the client
// marker can report it.
func TestInstallDockerNodeNonZeroExit(t *testing.T) {
	server := &sshProbeServer{password: "hunter2", installStatus: 100}
	addr, _, stop := startSSHServer(t, server)
	defer stop()
	host, port := target(t, addr)

	var out bytes.Buffer
	err := InstallDockerNode(context.Background(), host, port, "root",
		SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true}, &out)
	if err == nil {
		t.Fatal("InstallDockerNode with a failing package manager returned nil")
	}
	if !strings.Contains(out.String(), "Installing Docker") {
		t.Errorf("stream = %q, want the committed header line", out.String())
	}
	if code := DockerInstallExitCode(err); code != 100 {
		t.Errorf("DockerInstallExitCode = %d, want 100 (%v)", code, err)
	}
}

// TestInstallDockerNodeCancelledStopsPromptly proves a cancelled context stops
// waiting on a still-running remote install instead of hanging.
func TestInstallDockerNodeCancelledStopsPromptly(t *testing.T) {
	block := make(chan struct{})
	server := &sshProbeServer{password: "hunter2", installBlock: block}
	addr, _, stop := startSSHServer(t, server)
	defer stop()
	host, port := target(t, addr)

	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- InstallDockerNode(ctx, host, port, "root",
			SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true}, &out)
	}()

	waitFor(t, func() bool { return strings.Contains(server.lastCommand(), "apt-get install") })
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled install returned nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled install did not stop")
	}
	close(block)
}

// TestValidateNodeDockerInstallableFlag proves the probe tells the UI whether
// the automatic install covers the node, so it is never offered where it fails.
func TestValidateNodeDockerInstallableFlag(t *testing.T) {
	cases := []struct {
		name   string
		server *sshProbeServer
		want   bool
	}{
		{"debian", &sshProbeServer{password: "hunter2", dockerMissing: true}, true},
		{"fedora", &sshProbeServer{password: "hunter2", dockerMissing: true, osRelease: "fedora\n"}, true},
		{"alpine", &sshProbeServer{password: "hunter2", dockerMissing: true, osRelease: "alpine\n"}, true},
		{"rhel", &sshProbeServer{password: "hunter2", dockerMissing: true, osRelease: "rocky\nrhel fedora\n"}, false},
		{"unknown distro", &sshProbeServer{password: "hunter2", dockerMissing: true, osRelease: "void\n"}, false},
		{"unsupported arch", &sshProbeServer{password: "hunter2", dockerMissing: true, arch: "i686"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, _, stop := startSSHServer(t, tc.server)
			defer stop()
			host, port := target(t, addr)

			checks, _, _, err := ValidateNode(context.Background(), host, port, "root",
				SSHAuth{Password: "hunter2"}, HostKeyPolicy{AcceptUnpinned: true})
			if err != nil {
				t.Fatalf("ValidateNode: %v", err)
			}
			if got := checkByName(t, checks, checkDocker).Installable; got != tc.want {
				t.Errorf("docker installable = %v, want %v", got, tc.want)
			}
		})
	}
}

// waitFor polls cond until it holds or a short deadline passes.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
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
