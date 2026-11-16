package firewall

import (
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// firewallCode reads the stable code and params of a coded refusal.
func firewallCode(t *testing.T, err error) (string, map[string]string) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("%v is not a gRPC status", err)
	}
	// The detail must survive the wire, which marshals the status proto.
	if len(st.Proto().GetDetails()) == 0 {
		t.Fatalf("%v has no serialisable detail", err)
	}
	for _, item := range st.Details() {
		if detail, ok := item.(*agentv1.FirewallError); ok {
			return detail.GetCode(), detail.GetParams()
		}
	}
	t.Fatalf("%v carries no firewall error detail", err)
	return "", nil
}

func TestParseBackendStatusCarriesADetailCode(t *testing.T) {
	cases := map[string]struct {
		out     string
		want    string
		backend string
	}{
		"nftables": {out: "backend=nftables\nmanageable=true\n", want: codeUnsupportedBackend, backend: "nftables"},
		"iptables": {out: "backend=iptables\nmanageable=true\n", want: codeUnsupportedBackend, backend: "iptables"},
		"none":     {out: "backend=\n", want: codeNoFirewall},
	}
	for name, tc := range cases {
		st := parseBackendStatus(tc.out)
		if st.DetailCode != tc.want || st.Detail == "" {
			t.Errorf("%s: code = %q detail = %q", name, st.DetailCode, st.Detail)
		}
		if tc.backend != "" && st.DetailParams["backend"] != tc.backend {
			t.Errorf("%s: params = %v", name, st.DetailParams)
		}
	}
}

func TestManageableRefusalsCarryTheirDetailCode(t *testing.T) {
	st := parseBackendStatus("backend=nftables\nmanageable=true\n")
	err := refuse(st)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v", status.Code(err))
	}
	code, params := firewallCode(t, err)
	if code != codeUnsupportedBackend || params["backend"] != "nftables" {
		t.Fatalf("code = %q params = %v", code, params)
	}
}

func TestHelperFailureCarriesTheMissingHelperCode(t *testing.T) {
	runner := &fakeRunner{statusErr: errors.New("sudo: a password is required")}
	server := newTestServer(t, runner)
	response, err := server.GetStatus(t.Context(), &agentv1.FirewallStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetDetailCode() != codeHelperMissing || response.GetDetail() == "" {
		t.Fatalf("detail code = %q detail = %q", response.GetDetailCode(), response.GetDetail())
	}
}

func TestLockoutRefusalsCarryThePortAndAccess(t *testing.T) {
	ports := []accessPort{{port: 22, kind: accessSSH}}
	err := denyBlocks([]rule{denyRule("1-100")}, ports)
	if err == nil {
		t.Fatal("denyBlocks accepted a deny for a protected port")
	}
	code, params := firewallCode(t, err)
	if code != codeWouldLockOut || params["port"] != "22" || params["access"] != accessSSH {
		t.Fatalf("code = %q params = %v", code, params)
	}

	lost := lostAccess(
		[]rule{allowRule("22", "")},
		[]rule{allowRule("8000", "")},
		ports,
	)
	if lost == nil {
		t.Fatal("lostAccess accepted a change that closes the port")
	}
	code, params = firewallCode(t, lost)
	if code != codeWouldLockOut || params["port"] != "22" || params["access"] != accessSSH {
		t.Fatalf("lostAccess code = %q params = %v", code, params)
	}
}

func TestRuleValidationCarriesACode(t *testing.T) {
	cases := map[string]struct {
		in   rule
		want string
	}{
		"action":   {in: rule{Action: agentv1.FirewallAction_FIREWALL_ACTION_UNSPECIFIED}, want: codeInvalidAction},
		"protocol": {in: rule{Action: agentv1.FirewallAction_FIREWALL_ACTION_ALLOW, Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN}, want: codeInvalidProtocol},
		"port":     {in: rule{Action: agentv1.FirewallAction_FIREWALL_ACTION_ALLOW, Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN, Protocol: agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP, Port: "70000"}, want: codeInvalidPort},
		"source":   {in: rule{Action: agentv1.FirewallAction_FIREWALL_ACTION_ALLOW, Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN, Protocol: agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP, Source: "example.com"}, want: codeInvalidSource},
		"outbound": {in: rule{Action: agentv1.FirewallAction_FIREWALL_ACTION_ALLOW, Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_OUT, Protocol: agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP, Source: "10.0.0.1"}, want: codeInvalidOutboundSource},
		"comment":  {in: rule{Action: agentv1.FirewallAction_FIREWALL_ACTION_ALLOW, Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN, Protocol: agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP, Comment: "bad;comment"}, want: codeInvalidComment},
	}
	for name, tc := range cases {
		_, err := normalizeRule(tc.in)
		if err == nil {
			t.Errorf("%s: expected an error", name)
			continue
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: gRPC code = %v", name, status.Code(err))
		}
		code, _ := firewallCode(t, err)
		if code != tc.want {
			t.Errorf("%s: code = %q, want %q", name, code, tc.want)
		}
	}
}

func TestStatusProtoCarriesTheDetailCode(t *testing.T) {
	st := parseBackendStatus("backend=nftables\nmanageable=true\n")
	response := (&Server{}).statusProto(st, 0)
	if response.GetDetailCode() != codeUnsupportedBackend || !strings.Contains(response.GetDetail(), "nftables") {
		t.Fatalf("detail code = %q detail = %q", response.GetDetailCode(), response.GetDetail())
	}
}
