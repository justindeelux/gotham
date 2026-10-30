package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type call struct {
	verb  string
	stdin string
}

type fakeRunner struct {
	mu    sync.Mutex
	calls []call
	out   string
	err   error
}

func (r *fakeRunner) Run(_ context.Context, verb, stdin string) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, call{verb: verb, stdin: stdin})
	out, err := r.out, r.err
	r.mu.Unlock()
	return out, err
}

func (r *fakeRunner) last() call {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return call{}
	}
	return r.calls[len(r.calls)-1]
}

func (r *fakeRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func withRunner(r runner) Option { return func(s *Server) { s.runner = r } }

func TestStatusReportsCapability(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "gotham-hostctl")

	runner := &fakeRunner{}
	missing := NewServer(WithHelper(helper), withRunner(runner))
	response, err := missing.Status(context.Background(), &agentv1.HostStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetCapability() != agentv1.HostCapability_HOST_CAPABILITY_HELPER_MISSING {
		t.Fatalf("capability = %v, want helper missing", response.GetCapability())
	}
	if runner.count() != 0 {
		t.Fatal("status must not run a helper that is not installed")
	}

	if err := os.WriteFile(helper, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	denied := NewServer(WithHelper(helper), withRunner(&fakeRunner{err: &runError{verb: "status", code: 1}}))
	response, err = denied.Status(context.Background(), &agentv1.HostStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetCapability() != agentv1.HostCapability_HOST_CAPABILITY_HELPER_DENIED {
		t.Fatalf("capability = %v, want helper denied", response.GetCapability())
	}

	unsupported := NewServer(WithHelper(helper), withRunner(&fakeRunner{out: `{"network":false,"system":true}`}))
	response, err = unsupported.Status(context.Background(), &agentv1.HostStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetCapability() != agentv1.HostCapability_HOST_CAPABILITY_UNSUPPORTED_STACK {
		t.Fatalf("capability = %v, want unsupported stack", response.GetCapability())
	}
}

func TestStatusMapsLiveHostSettings(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "gotham-hostctl")
	if err := os.WriteFile(helper, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := `{
		"network": true,
		"system": true,
		"host_network": {
			"interface": "eth0",
			"dns_servers": ["8.8.8.8", "1.1.1.1"],
			"ipv4": {"mode": "static", "address": "10.0.0.5/24", "gateway": "10.0.0.1"},
			"ipv6": {"enabled": false, "mode": "dhcp", "address": "", "gateway": ""}
		},
		"hostname": "node-1",
		"ntp_enabled": true,
		"ntp_servers": ["time.cloudflare.com"],
		"timezone": "Europe/Berlin"
	}`
	server := NewServer(WithHelper(helper), withRunner(&fakeRunner{out: out}))
	response, err := server.Status(context.Background(), &agentv1.HostStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetCapability() != agentv1.HostCapability_HOST_CAPABILITY_READY {
		t.Fatalf("capability = %v, want ready", response.GetCapability())
	}
	if response.GetHostname() != "node-1" || !response.GetNtpEnabled() ||
		len(response.GetNtpServers()) != 1 || response.GetTimezone() != "Europe/Berlin" {
		t.Fatalf("system settings = %+v", response)
	}
	network := response.GetNetwork()
	if network.GetInterface() != "eth0" || len(network.GetDnsServers()) != 2 ||
		network.GetIpv4().GetAddress() != "10.0.0.5/24" || network.GetIpv4().GetGateway() != "10.0.0.1" ||
		network.GetIpv6().GetEnabled() {
		t.Fatalf("network = %+v", network)
	}
}

func TestApplyPassesOnlyWhitelistedKeys(t *testing.T) {
	runner := &fakeRunner{}
	server := NewServer(withRunner(runner))

	_, err := server.ApplyNetwork(context.Background(), &agentv1.HostApplyNetworkRequest{
		DnsServers:  []string{"1.1.1.1", "8.8.8.8"},
		Ipv4Mode:    "static",
		Ipv4Address: "10.0.0.5/24",
		Ipv4Gateway: "10.0.0.1",
		Ipv6Enabled: false,
		Ipv6Mode:    "dhcp",
		RevertAfter: 120,
		DnsOnly:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "dns=1.1.1.1,8.8.8.8\n" +
		"ipv4_mode=static\n" +
		"ipv4_address=10.0.0.5/24\n" +
		"ipv4_gateway=10.0.0.1\n" +
		"ipv6_enabled=false\n" +
		"ipv6_mode=dhcp\n" +
		"ipv6_address=\n" +
		"ipv6_gateway=\n" +
		"revert_after=120\n" +
		"dns_only=true\n"
	last := runner.last()
	if last.verb != "apply-network" || last.stdin != want {
		t.Fatalf("apply-network call = %q %q, want the fixed key set", last.verb, last.stdin)
	}

	if _, err := server.ApplySystem(context.Background(), &agentv1.HostApplySystemRequest{
		Hostname:   "node-1",
		NtpEnabled: true,
		NtpServers: []string{"time.cloudflare.com", "pool.ntp.org"},
		Timezone:   "Europe/Berlin",
	}); err != nil {
		t.Fatal(err)
	}
	want = "hostname=node-1\nntp_enabled=true\nntp_servers=time.cloudflare.com,pool.ntp.org\ntimezone=Europe/Berlin\n"
	last = runner.last()
	if last.verb != "apply-system" || last.stdin != want {
		t.Fatalf("apply-system call = %q %q", last.verb, last.stdin)
	}

	if _, err := server.ConfirmNetwork(context.Background(), &agentv1.HostConfirmNetworkRequest{}); err != nil {
		t.Fatal(err)
	}
	if last = runner.last(); last.verb != "confirm-network" || last.stdin != "" {
		t.Fatalf("confirm-network call = %q %q", last.verb, last.stdin)
	}
	if _, err := server.RevertNetwork(context.Background(), &agentv1.HostRevertNetworkRequest{}); err != nil {
		t.Fatal(err)
	}
	if last = runner.last(); last.verb != "revert-network" || last.stdin != "" {
		t.Fatalf("revert-network call = %q %q", last.verb, last.stdin)
	}
}

func TestHelperErrorsStayOpaque(t *testing.T) {
	secret := "cannot open /etc/shadow: secret-value"
	server := NewServer(withRunner(&fakeRunner{err: &runError{verb: "apply-system", code: 2, err: errors.New(secret)}}))
	_, err := server.ApplySystem(context.Background(), &agentv1.HostApplySystemRequest{Timezone: "Europe/Berlin"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "/etc/shadow") {
		t.Fatalf("helper stderr leaked: %v", err)
	}

	cases := []struct {
		name string
		verb string
		code int
		want codes.Code
	}{
		{"unsupported stack", "apply-system", 3, codes.FailedPrecondition},
		{"pending network", "apply-network", 4, codes.FailedPrecondition},
		{"no pending network", "confirm-network", 4, codes.FailedPrecondition},
		{"helper failed", "apply-network", 1, codes.Internal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := helperStatus(tc.verb, &runError{verb: tc.verb, code: tc.code})
			if status.Code(err) != tc.want {
				t.Fatalf("code = %v, want %v", status.Code(err), tc.want)
			}
		})
	}
}

func TestSudoRunnerUsesFixedArguments(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	argsLog := filepath.Join(dir, "args")
	stdinLog := filepath.Join(dir, "stdin")
	sudo := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >" + argsLog + "\n" +
		"cat >" + stdinLog + "\n" +
		"printf '{\"network\":true,\"system\":true}\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte(sudo), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	out, err := (sudoRunner{helper: "/usr/libexec/gotham/gotham-hostctl"}).Run(context.Background(), "status", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"network":true`) {
		t.Fatalf("stdout = %q", out)
	}
	args, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(args)); got != "-n /usr/libexec/gotham/gotham-hostctl status" {
		t.Fatalf("sudo args = %q", got)
	}
}
