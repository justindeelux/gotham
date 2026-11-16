package firewall

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type recordedCall struct {
	verb  string
	stdin string
}

type fakeRunner struct {
	mu     sync.Mutex
	status string
	list   string
	calls  []recordedCall
}

func (f *fakeRunner) Run(_ context.Context, verb, stdin string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, recordedCall{verb: verb, stdin: stdin})
	switch verb {
	case "firewall-status":
		return f.status, nil
	case "firewall-list":
		return f.list, nil
	default:
		return "", nil
	}
}

func (f *fakeRunner) verbs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, call := range f.calls {
		out = append(out, call.verb)
	}
	return out
}

func (f *fakeRunner) callsFor(verb string) []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedCall, 0)
	for _, call := range f.calls {
		if call.verb == verb {
			out = append(out, call)
		}
	}
	return out
}

const ufwStatusFixture = "backend=ufw\nenabled=true\nmanageable=true\nssh_port=22\n"

const ufwSSHOnly = "backend=ufw\n--\n" +
	"Status: active\n\n" +
	"     To                         Action      From\n" +
	"     --                         ------      ----\n" +
	"[ 1] 22/tcp                     ALLOW IN    Anywhere\n" +
	"--\n" +
	"Added user rules (see 'ufw status' for running firewall):\n" +
	"ufw allow 22/tcp comment 'SSH access'\n"

func newTestServer(t *testing.T, runner *fakeRunner) *Server {
	t.Helper()
	server := NewServer(Config{
		StatePath:  filepath.Join(t.TempDir(), "firewall-state.json"),
		ListenAddr: ":9443",
		Runner:     runner,
	})
	return server
}

func TestStatusReportsBackendAndProtectedPorts(t *testing.T) {
	runner := &fakeRunner{status: ufwStatusFixture}
	server := newTestServer(t, runner)
	response, err := server.GetStatus(context.Background(), &agentv1.FirewallStatusRequest{ControlPlanePort: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetBackend() != agentv1.FirewallBackend_FIREWALL_BACKEND_UFW || !response.GetManageable() {
		t.Fatalf("status = %#v", response)
	}
	if response.GetSshPort() != 22 || response.GetAgentPort() != 9443 || response.GetControlPlanePort() != 8000 {
		t.Fatalf("ports = %d %d %d", response.GetSshPort(), response.GetAgentPort(), response.GetControlPlanePort())
	}
}

func TestUnsupportedBackendIsReportedNotAnError(t *testing.T) {
	runner := &fakeRunner{status: "backend=nftables\nenabled=true\nmanageable=false\nssh_port=22\n"}
	server := newTestServer(t, runner)
	response, err := server.GetStatus(context.Background(), &agentv1.FirewallStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetManageable() || response.GetDetail() == "" {
		t.Fatalf("status = %#v", response)
	}
	rules, err := server.ListRules(context.Background(), &agentv1.FirewallRulesRequest{})
	if err != nil || len(rules.GetRules()) != 0 {
		t.Fatalf("rules = %#v, err = %v", rules, err)
	}
}

func TestAddDisabledRuleIsSavedWithoutTouchingTheBackend(t *testing.T) {
	runner := &fakeRunner{status: ufwStatusFixture, list: ufwSSHOnly}
	server := newTestServer(t, runner)
	response, err := server.AddRule(context.Background(), &agentv1.FirewallRuleCreateRequest{
		ControlPlanePort: 8000,
		Rule: &agentv1.FirewallRule{
			Action:    agentv1.FirewallAction_FIREWALL_ACTION_ALLOW,
			Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN,
			Protocol:  agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP,
			Port:      "8080",
			Comment:   "web",
			Enabled:   false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.callsFor("firewall-add")) != 0 {
		t.Fatal("a disabled rule must not reach the backend")
	}
	found := false
	for _, r := range response.GetRules() {
		if r.GetPort() == "8080" {
			found = true
			if r.GetEnabled() || r.GetComment() != "web" {
				t.Fatalf("disabled rule = %#v", r)
			}
		}
	}
	if !found {
		t.Fatalf("rules = %#v", response.GetRules())
	}
}

func TestAddingDenyForSSHIsRefused(t *testing.T) {
	runner := &fakeRunner{status: ufwStatusFixture, list: ufwSSHOnly}
	server := newTestServer(t, runner)
	_, err := server.AddRule(context.Background(), &agentv1.FirewallRuleCreateRequest{
		ControlPlanePort: 8000,
		Rule: &agentv1.FirewallRule{
			Action:    agentv1.FirewallAction_FIREWALL_ACTION_DENY,
			Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN,
			Protocol:  agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP,
			Port:      "22",
			Enabled:   true,
		},
	})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "21") && !strings.Contains(err.Error(), "22") {
		t.Fatalf("err = %v", err)
	}
	if len(runner.callsFor("firewall-add")) != 0 {
		t.Fatal("a refused rule must not reach the backend")
	}
}

func TestDeletingTheSSHAllowRuleIsRefused(t *testing.T) {
	runner := &fakeRunner{status: ufwStatusFixture, list: ufwSSHOnly}
	server := newTestServer(t, runner)
	rules := parseUFWRules(strings.SplitN(ufwSSHOnly, "\n--\n", 3)[1], strings.SplitN(ufwSSHOnly, "\n--\n", 3)[2])
	var sshID string
	for _, r := range rules {
		if r.Port == "22" {
			sshID = r.ID
		}
	}
	if sshID == "" {
		t.Fatal("fixture has no SSH rule")
	}
	_, err := server.DeleteRule(context.Background(), &agentv1.FirewallRuleDeleteRequest{
		Id:               sshID,
		ControlPlanePort: 8000,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("err = %v", err)
	}
	if len(runner.callsFor("firewall-delete")) != 0 {
		t.Fatal("a refused delete must not reach the backend")
	}
}

func TestEnableAddsMissingAccessRulesBeforeTurningTheFirewallOn(t *testing.T) {
	runner := &fakeRunner{status: "backend=ufw\nenabled=false\nmanageable=true\nssh_port=22\n", list: ufwSSHOnly}
	server := newTestServer(t, runner)
	_, err := server.SetEnabled(context.Background(), &agentv1.FirewallEnabledRequest{
		Enabled:          true,
		ControlPlanePort: 8000,
	})
	if err != nil {
		t.Fatal(err)
	}
	adds := runner.callsFor("firewall-add")
	if len(adds) != 2 {
		t.Fatalf("adds = %#v, want the agent and control-plane rules", adds)
	}
	if !strings.Contains(adds[0].stdin, "port=9443") || !strings.Contains(adds[1].stdin, "port=8000") {
		t.Fatalf("adds = %#v", adds)
	}
	verbs := runner.verbs()
	enabledAt := -1
	for i, verb := range verbs {
		if verb == "firewall-enable" {
			enabledAt = i
		}
	}
	if enabledAt < 0 {
		t.Fatalf("verbs = %#v, want firewall-enable", verbs)
	}
	for i, verb := range verbs {
		if verb == "firewall-add" && i > enabledAt {
			t.Fatalf("verbs = %#v, access rules must be added before enabling", verbs)
		}
	}
}

func TestEnableRefusesADenyRuleForAProtectedPort(t *testing.T) {
	list := "backend=ufw\n--\n" +
		"Status: inactive\n\n" +
		"[ 1] 22/tcp                     ALLOW IN    Anywhere\n" +
		"[ 2] 9443/tcp                   ALLOW IN    Anywhere\n" +
		"[ 3] 8000/tcp                   ALLOW IN    Anywhere\n" +
		"[ 4] 22/tcp                     DENY IN     Anywhere\n" +
		"--\nAdded user rules (see 'ufw status' for running firewall):\n"
	runner := &fakeRunner{status: "backend=ufw\nenabled=false\nmanageable=true\nssh_port=22\n", list: list}
	server := newTestServer(t, runner)
	_, err := server.SetEnabled(context.Background(), &agentv1.FirewallEnabledRequest{
		Enabled:          true,
		ControlPlanePort: 8000,
	})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "denies port 22") {
		t.Fatalf("err = %v", err)
	}
	for _, verb := range runner.verbs() {
		if verb == "firewall-enable" {
			t.Fatal("the firewall must not be enabled")
		}
	}
}

func TestDeleteReadOnlyRuleIsRefused(t *testing.T) {
	list := "backend=ufw\n--\n" +
		"Status: active\n\n" +
		"[ 1] 22/tcp                     ALLOW IN    Anywhere\n" +
		"[ 2] OpenSSH                    ALLOW IN    Anywhere\n" +
		"--\nAdded user rules (see 'ufw status' for running firewall):\n"
	runner := &fakeRunner{status: ufwStatusFixture, list: list}
	server := newTestServer(t, runner)
	rules := parseUFWRules(strings.SplitN(list, "\n--\n", 3)[1], "")
	var appID string
	for _, r := range rules {
		if r.ReadOnly {
			appID = r.ID
		}
	}
	_, err := server.DeleteRule(context.Background(), &agentv1.FirewallRuleDeleteRequest{
		Id:               appID,
		ControlPlanePort: 8000,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("err = %v", err)
	}
}

func TestPreparingRulesWhileTheFirewallIsOffDoesNotNeedAccessRules(t *testing.T) {
	empty := "backend=ufw\n--\nStatus: inactive\n\n--\nAdded user rules (see 'ufw status' for running firewall):\n"
	runner := &fakeRunner{status: "backend=ufw\nenabled=false\nmanageable=true\nssh_port=22\n", list: empty}
	server := newTestServer(t, runner)
	_, err := server.AddRule(context.Background(), &agentv1.FirewallRuleCreateRequest{
		ControlPlanePort: 8000,
		Rule: &agentv1.FirewallRule{
			Action:    agentv1.FirewallAction_FIREWALL_ACTION_ALLOW,
			Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN,
			Protocol:  agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP,
			Port:      "8080",
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("a rule can be prepared while the firewall is off: %v", err)
	}
	if len(runner.callsFor("firewall-add")) != 1 {
		t.Fatal("the rule was not applied")
	}
}

func TestEnableStartsFirewalldBeforeAddingAccessRules(t *testing.T) {
	list := "backend=firewalld\n--\n--\n"
	runner := &fakeRunner{status: "backend=firewalld\nenabled=false\nmanageable=true\nssh_port=22\n", list: list}
	server := newTestServer(t, runner)
	_, err := server.SetEnabled(context.Background(), &agentv1.FirewallEnabledRequest{
		Enabled:          true,
		ControlPlanePort: 8000,
	})
	if err != nil {
		t.Fatal(err)
	}
	verbs := runner.verbs()
	enableAt := -1
	for i, verb := range verbs {
		if verb == "firewall-enable" {
			enableAt = i
			break
		}
	}
	if enableAt < 0 {
		t.Fatalf("verbs = %#v, want firewall-enable", verbs)
	}
	for i, verb := range verbs {
		if verb == "firewall-add" && i < enableAt {
			t.Fatalf("verbs = %#v, firewalld must start before its rules can be added", verbs)
		}
	}
	if len(runner.callsFor("firewall-add")) == 0 {
		t.Fatalf("missing access rules were not added: %#v", runner.verbs())
	}
}

func TestEnableDoesNotStopAnAlreadyRunningFirewalld(t *testing.T) {
	list := "backend=firewalld\n--\n" +
		"rule family=\"ipv4\" port port=\"22\" protocol=\"tcp\" reject\n" +
		"--\n"
	runner := &fakeRunner{status: "backend=firewalld\nenabled=true\nmanageable=true\nssh_port=22\n", list: list}
	server := newTestServer(t, runner)
	_, err := server.SetEnabled(context.Background(), &agentv1.FirewallEnabledRequest{
		Enabled:          true,
		ControlPlanePort: 8000,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("err = %v", err)
	}
	for _, verb := range runner.verbs() {
		if verb == "firewall-disable" {
			t.Fatal("a firewall that was already running must not be stopped")
		}
	}
}

func TestFirewalldRuleCommentSurvivesTheStateFile(t *testing.T) {
	list := "backend=firewalld\n--\n" +
		"rule family=\"ipv4\" port port=\"8080\" protocol=\"tcp\" accept\n" +
		"--\n"
	runner := &fakeRunner{status: "backend=firewalld\nenabled=true\nmanageable=true\nssh_port=22\n", list: list}
	server := newTestServer(t, runner)
	rules, err := server.ListRules(context.Background(), &agentv1.FirewallRulesRequest{ControlPlanePort: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if len(rules.GetRules()) != 1 || rules.GetRules()[0].GetComment() != "" {
		t.Fatalf("rules = %#v", rules.GetRules())
	}
	// The rule list is fixed by the fake, so an add would duplicate it; only
	// the comment metadata path is exercised here.
	state := savedState{Comments: map[string]string{
		fingerprint(rule{
			Action:    agentv1.FirewallAction_FIREWALL_ACTION_ALLOW,
			Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN,
			Protocol:  agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP,
			Port:      "8080",
		}): "web",
	}}
	if err := saveState(server.statePath, state); err != nil {
		t.Fatal(err)
	}
	rules, err = server.ListRules(context.Background(), &agentv1.FirewallRulesRequest{ControlPlanePort: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if rules.GetRules()[0].GetComment() != "web" {
		t.Fatalf("comment = %q", rules.GetRules()[0].GetComment())
	}
}
