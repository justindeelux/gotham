package firewall

import (
	"strings"
	"testing"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func allowRule(port string, source string) rule {
	return rule{
		ID:        "allow-" + port,
		Action:    agentv1.FirewallAction_FIREWALL_ACTION_ALLOW,
		Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN,
		Protocol:  agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP,
		Port:      port,
		Source:    source,
		Enabled:   true,
	}
}

func denyRule(port string) rule {
	return rule{
		ID:        "deny-" + port,
		Action:    agentv1.FirewallAction_FIREWALL_ACTION_DENY,
		Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN,
		Protocol:  agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP,
		Port:      port,
		Enabled:   true,
	}
}

func TestDenyBlocksRejectsADenyForAProtectedPort(t *testing.T) {
	ports := []accessPort{{port: 22, label: "SSH"}, {port: 9443, label: "the node agent"}}
	err := denyBlocks([]rule{allowRule("22", ""), allowRule("9000-9500", ""), denyRule("1-100")}, ports)
	if err == nil || !strings.Contains(err.Error(), "denies port 22") {
		t.Fatalf("deny check = %v", err)
	}
}

func TestDenyBlocksIgnoresOutboundAndUDP(t *testing.T) {
	ports := []accessPort{{port: 22, label: "SSH"}}
	outboundDeny := denyRule("22")
	outboundDeny.Direction = agentv1.FirewallDirection_FIREWALL_DIRECTION_OUT
	udpDeny := denyRule("22")
	udpDeny.Protocol = agentv1.FirewallProtocol_FIREWALL_PROTOCOL_UDP
	if err := denyBlocks([]rule{outboundDeny, udpDeny}, ports); err != nil {
		t.Fatalf("denyBlocks = %v", err)
	}
}

func TestLostAccessRefusesRemovingTheLastAllow(t *testing.T) {
	ports := []accessPort{{port: 22, label: "SSH"}}
	before := []rule{allowRule("22", "")}
	after := []rule{}
	err := lostAccess(before, after, ports)
	if err == nil || !strings.Contains(err.Error(), "block port 22") {
		t.Fatalf("lostAccess = %v", err)
	}
}

func TestLostAccessAllowsRemovingOneOfTwoAllows(t *testing.T) {
	ports := []accessPort{{port: 22, label: "SSH"}}
	first := allowRule("22", "")
	second := allowRule("1-100", "")
	second.ID = "allow-wide"
	if err := lostAccess([]rule{first, second}, []rule{second}, ports); err != nil {
		t.Fatalf("lostAccess = %v", err)
	}
}

func TestLostAccessAllowsFixingAnAlreadyUncoveredPort(t *testing.T) {
	ports := []accessPort{{port: 22, label: "SSH"}, {port: 9443, label: "the node agent"}}
	before := []rule{allowRule("22", "")}
	after := append(append([]rule{}, before...), allowRule("9443", ""))
	if err := lostAccess(before, after, ports); err != nil {
		t.Fatalf("adding the missing access rule must be possible one at a time: %v", err)
	}
}

func TestMissingAccessRulesAddsOnlyWhatIsMissing(t *testing.T) {
	ports := []accessPort{{port: 22, label: "SSH"}, {port: 8000, label: "the control plane"}}
	missing := missingAccessRules([]rule{allowRule("22", "")}, ports)
	if len(missing) != 1 || missing[0].Port != "8000" || !missing[0].Enabled {
		t.Fatalf("missing = %#v", missing)
	}
	if missing[0].Comment == "" {
		t.Fatal("an automatic access rule needs a comment")
	}
}

func TestMissingAccessRulesDoesNotTrustAReadOnlyRule(t *testing.T) {
	ports := []accessPort{{port: 22, label: "SSH"}}
	appProfile := allowRule("", "")
	appProfile.ReadOnly = true
	missing := missingAccessRules([]rule{appProfile}, ports)
	if len(missing) != 1 || missing[0].Port != "22" {
		t.Fatalf("missing = %#v", missing)
	}
}

func TestMarkProtectedOnlyFlagsExactAccessRules(t *testing.T) {
	ports := []accessPort{{port: 22, label: "SSH"}}
	rules := markProtected([]rule{
		allowRule("22", ""),
		allowRule("1-100", ""),
		allowRule("22", "10.0.0.0/8"),
	}, ports)
	if !rules[0].Protected {
		t.Fatalf("exact SSH rule must be protected: %#v", rules[0])
	}
	if rules[1].Protected || rules[2].Protected {
		t.Fatalf("broader or source-scoped rules must not be flagged: %#v", rules)
	}
}

func TestPortCovers(t *testing.T) {
	cases := []struct {
		spec string
		port uint32
		want bool
	}{
		{"", 22, true},
		{"22", 22, true},
		{"22", 23, false},
		{"8000-8100", 8050, true},
		{"8000-8100", 8101, false},
		{"bad", 22, false},
	}
	for _, tc := range cases {
		if got := portCovers(tc.spec, tc.port); got != tc.want {
			t.Errorf("portCovers(%q, %d) = %v, want %v", tc.spec, tc.port, got, tc.want)
		}
	}
}

func TestMissingAccessRulesKeepsASourceRestrictedAllow(t *testing.T) {
	ports := []accessPort{{port: 22, label: "SSH"}}
	restricted := allowRule("22", "192.0.2.10")
	missing := missingAccessRules([]rule{restricted}, ports)
	if len(missing) != 0 {
		t.Fatalf("a source-restricted allow already covers SSH: %#v", missing)
	}
}

func TestLostAccessRefusesRemovingTheOnlySourceRestrictedAllow(t *testing.T) {
	ports := []accessPort{{port: 22, label: "SSH"}}
	restricted := allowRule("22", "192.0.2.10")
	err := lostAccess([]rule{restricted}, nil, ports)
	if err == nil || !strings.Contains(err.Error(), "block port 22") {
		t.Fatalf("lostAccess = %v", err)
	}
}

func TestDenyBlocksIncludesAReadOnlyDeny(t *testing.T) {
	ports := []accessPort{{port: 22, label: "SSH"}}
	reject := denyRule("22")
	reject.ReadOnly = true
	err := denyBlocks([]rule{allowRule("22", ""), reject}, ports)
	if err == nil || !strings.Contains(err.Error(), "denies port 22") {
		t.Fatalf("denyBlocks = %v", err)
	}
}

func TestPortAllowedCountsAReadOnlyRowWithAKnownPort(t *testing.T) {
	interfaceAllow := allowRule("22", "")
	interfaceAllow.ReadOnly = true
	if !portAllowed([]rule{interfaceAllow}, 22) {
		t.Fatal("a read-only interface row for port 22 still covers port 22")
	}
}

func TestReadOnlyDeniesWithUnknownPortsDoNotBlockAccess(t *testing.T) {
	rules := parseUFWRules(fixture(t, "ufw_readonly.txt"), "")
	ports := []accessPort{{port: 22, label: "SSH"}, {port: 9443, label: "the node agent"}}

	// Samba AND 8080/tcp on docker0 are read-only denies with no parsed port.
	// They must not be read as deny-all for every protected port.
	if err := denyBlocks(rules, ports); err != nil {
		t.Fatalf("an unparsed read-only deny blocked access: %v", err)
	}

	// A read-only row with a known port still counts: the fixture's
	// 3306/tcp REJECT must block a protected port on 3306.
	err := denyBlocks(rules, []accessPort{{port: 3306, label: "the database"}})
	if err == nil || !strings.Contains(err.Error(), "denies port 3306") {
		t.Fatalf("a known-port REJECT was ignored: %v", err)
	}
}
