package firewall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseBackendStatus(t *testing.T) {
	st := parseBackendStatus(fixture(t, "hostctl_status.txt"))
	if st.Backend != agentv1.FirewallBackend_FIREWALL_BACKEND_UFW {
		t.Fatalf("backend = %v", st.Backend)
	}
	if !st.Enabled || !st.Manageable {
		t.Fatalf("enabled = %v, manageable = %v", st.Enabled, st.Manageable)
	}
	if len(st.SSHPorts) != 1 || st.SSHPorts[0] != 2222 {
		t.Fatalf("ssh ports = %v, want [2222]", st.SSHPorts)
	}

	none := parseBackendStatus(fixture(t, "hostctl_status_none.txt"))
	if none.Backend != agentv1.FirewallBackend_FIREWALL_BACKEND_NONE || none.Manageable {
		t.Fatalf("none = %#v", none)
	}
	if none.Detail == "" {
		t.Fatal("a node without a firewall needs a detail for the UI")
	}
}

func TestParseUFWRules(t *testing.T) {
	rules := parseUFWRules(fixture(t, "ufw_status.txt"), fixture(t, "ufw_show_added.txt"))
	if len(rules) != 7 {
		t.Fatalf("rules = %d, want 7: %#v", len(rules), rules)
	}

	ssh := rules[0]
	if ssh.Port != "22" || ssh.Protocol != agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP ||
		ssh.Action != agentv1.FirewallAction_FIREWALL_ACTION_ALLOW || ssh.Comment != "SSH access" {
		t.Fatalf("ssh rule = %#v", ssh)
	}

	rangeRule := rules[1]
	if rangeRule.Port != "8000-8100" || rangeRule.Source != "10.0.0.0/8" {
		t.Fatalf("range rule = %#v", rangeRule)
	}

	deny := rules[2]
	if deny.Action != agentv1.FirewallAction_FIREWALL_ACTION_DENY || deny.Comment != "no database" {
		t.Fatalf("deny rule = %#v", deny)
	}

	out := rules[3]
	if out.Direction != agentv1.FirewallDirection_FIREWALL_DIRECTION_OUT || out.Port != "53" {
		t.Fatalf("out rule = %#v", out)
	}

	app := rules[4]
	if !app.ReadOnly {
		t.Fatalf("an app profile must be read-only: %#v", app)
	}

	specific := rules[5]
	if specific.Source != "192.0.2.10" || specific.Protocol != agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP {
		t.Fatalf("source rule = %#v", specific)
	}

	iface := rules[6]
	if !iface.ReadOnly {
		t.Fatalf("an interface rule must be read-only: %#v", iface)
	}
}

func TestParseUFWRulesDeduplicatesIPv6Twin(t *testing.T) {
	out := "Status: active\n\n" +
		"[ 1] 22/tcp                     ALLOW IN    Anywhere\n" +
		"[ 2] 22/tcp (v6)                ALLOW IN    Anywhere (v6)\n"
	rules := parseUFWRules(out, "")
	if len(rules) != 1 {
		t.Fatalf("rules = %d, want 1: %#v", len(rules), rules)
	}
	if rules[0].Port != "22" || rules[0].Source != "" {
		t.Fatalf("rule = %#v", rules[0])
	}
}

func TestParseUFWMultiplePortsIsReadOnly(t *testing.T) {
	out := "[ 1] 80,443/tcp                ALLOW IN    Anywhere\n"
	rules := parseUFWRules(out, "")
	if len(rules) != 1 || !rules[0].ReadOnly {
		t.Fatalf("rules = %#v", rules)
	}
}

func TestParseFirewalldRules(t *testing.T) {
	rules := parseFirewalldRules(fixture(t, "firewalld_rules.txt"))
	if len(rules) != 6 {
		t.Fatalf("rules = %d, want 6: %#v", len(rules), rules)
	}
	if rules[0].Port != "22" || rules[0].Protocol != agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP ||
		rules[0].ReadOnly {
		t.Fatalf("ssh rule = %#v", rules[0])
	}
	if rules[1].Port != "8000-8100" || rules[1].Source != "10.0.0.0/8" {
		t.Fatalf("range rule = %#v", rules[1])
	}
	if rules[2].Action != agentv1.FirewallAction_FIREWALL_ACTION_DENY {
		t.Fatalf("deny rule = %#v", rules[2])
	}
	if !rules[4].ReadOnly || !rules[5].ReadOnly {
		t.Fatalf("service and forward rules must be read-only: %#v %#v", rules[4], rules[5])
	}
}

func TestSplitListAndHeader(t *testing.T) {
	header, listing, extra := splitList(fixture(t, "hostctl_list.txt"))
	if headerValue(header, "backend") != "ufw" {
		t.Fatalf("header = %q", header)
	}
	if listing == "" || extra == "" {
		t.Fatalf("listing = %q, extra = %q", listing, extra)
	}
	rules := parseUFWRules(listing, extra)
	if len(rules) != 2 || rules[0].Comment != "SSH access" {
		t.Fatalf("rules = %#v", rules)
	}
}

func TestSplitQuoted(t *testing.T) {
	tokens := splitQuoted("allow 22/tcp comment 'SSH access'")
	if len(tokens) != 4 || tokens[3] != "SSH access" {
		t.Fatalf("tokens = %#v", tokens)
	}
}

func TestParseUFWKeepsReadOnlyRowsDistinct(t *testing.T) {
	rules := parseUFWRules(fixture(t, "ufw_readonly.txt"), "")
	if len(rules) != 9 {
		t.Fatalf("rules = %d, want 9: %#v", len(rules), rules)
	}
	ids := make(map[string]bool, len(rules))
	for _, r := range rules {
		if ids[r.ID] {
			t.Fatalf("duplicate rule id %q: %#v", r.ID, rules)
		}
		ids[r.ID] = true
	}

	profiles := make(map[string]bool)
	for _, r := range rules {
		if strings.HasPrefix(r.Comment, "ufw: OpenSSH") {
			profiles["OpenSSH"] = true
			if !r.ReadOnly {
				t.Fatalf("an app profile must be read-only: %#v", r)
			}
		}
		if strings.HasPrefix(r.Comment, "ufw: Nginx Full") {
			profiles["Nginx Full"] = true
			if !r.ReadOnly {
				t.Fatalf("an app profile must be read-only: %#v", r)
			}
		}
	}
	if !profiles["OpenSSH"] || !profiles["Nginx Full"] {
		t.Fatalf("app profiles collapsed: %#v", rules)
	}

	anywhere := false
	interfaceRule := false
	for _, r := range rules {
		if r.Port == "443" && r.Source == "" {
			if r.ReadOnly {
				iface := strings.Contains(r.Comment, "Anywhere on eth0")
				if !iface {
					t.Fatalf("unexpected read-only 443 rule: %#v", r)
				}
				interfaceRule = true
			} else {
				anywhere = true
			}
		}
	}
	if !anywhere || !interfaceRule {
		t.Fatalf("443 rows collapsed: %#v", rules)
	}

	limit := false
	plain := false
	for _, r := range rules {
		if r.Port != "22" {
			continue
		}
		if r.ReadOnly {
			limit = true
			if !strings.Contains(r.Comment, "LIMIT") && !strings.Contains(r.Comment, "22/tcp") {
				t.Fatalf("limit rule = %#v", r)
			}
		} else {
			plain = true
		}
	}
	if !limit || !plain {
		t.Fatalf("LIMIT and ALLOW rows collapsed: %#v", rules)
	}

	for _, r := range rules {
		if r.Port == "3306" {
			if !r.ReadOnly || r.Action != agentv1.FirewallAction_FIREWALL_ACTION_DENY {
				t.Fatalf("REJECT must be a read-only deny: %#v", r)
			}
			return
		}
	}
	t.Fatalf("REJECT row missing: %#v", rules)
}
