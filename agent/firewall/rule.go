package firewall

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// rule is one firewall rule in the model shared by every backend.
type rule struct {
	ID        string
	Action    agentv1.FirewallAction
	Direction agentv1.FirewallDirection
	Protocol  agentv1.FirewallProtocol
	Port      string
	Source    string
	Comment   string
	Enabled   bool
	Protected bool
	ReadOnly  bool

	// raw is the backend's own text for a rule, used to remove a rich rule
	// whose fields the shared model cannot rebuild.
	raw string
}

const maxCommentLength = 200

func normalizeRule(in rule) (rule, error) {
	in.Port = strings.TrimSpace(in.Port)
	in.Source = strings.TrimSpace(in.Source)
	in.Comment = strings.TrimSpace(in.Comment)
	if in.Action != agentv1.FirewallAction_FIREWALL_ACTION_ALLOW &&
		in.Action != agentv1.FirewallAction_FIREWALL_ACTION_DENY {
		return rule{}, fmt.Errorf("action must be allow or deny")
	}
	if in.Direction != agentv1.FirewallDirection_FIREWALL_DIRECTION_IN &&
		in.Direction != agentv1.FirewallDirection_FIREWALL_DIRECTION_OUT {
		return rule{}, fmt.Errorf("direction must be in or out")
	}
	if in.Protocol != agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP &&
		in.Protocol != agentv1.FirewallProtocol_FIREWALL_PROTOCOL_UDP &&
		in.Protocol != agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY {
		return rule{}, fmt.Errorf("protocol must be tcp, udp or any")
	}
	port, err := normalizePort(in.Port)
	if err != nil {
		return rule{}, err
	}
	in.Port = port
	source, err := normalizeSource(in.Source)
	if err != nil {
		return rule{}, err
	}
	in.Source = source
	if in.Direction == agentv1.FirewallDirection_FIREWALL_DIRECTION_OUT && in.Source != "" {
		return rule{}, fmt.Errorf("outbound rules cannot have a source address")
	}
	if err := validateComment(in.Comment); err != nil {
		return rule{}, err
	}
	return in, nil
}

func normalizePort(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	parts := strings.SplitN(raw, "-", 2)
	first, err := parsePort(parts[0])
	if err != nil {
		return "", err
	}
	if len(parts) == 1 {
		return strconv.Itoa(int(first)), nil
	}
	last, err := parsePort(parts[1])
	if err != nil {
		return "", err
	}
	if first > last {
		return "", fmt.Errorf("port range start must not be greater than its end")
	}
	return fmt.Sprintf("%d-%d", first, last), nil
}

func parsePort(raw string) (uint32, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("port must be between 1 and 65535")
	}
	return uint32(n), nil
}

func normalizeSource(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if addr, err := netip.ParseAddr(raw); err == nil {
		if addr.Zone() != "" {
			return "", fmt.Errorf("source must be an IP address or CIDR")
		}
		return addr.String(), nil
	}
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		return prefix.Masked().String(), nil
	}
	return "", fmt.Errorf("source must be an IP address or CIDR")
}

func validateComment(raw string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > maxCommentLength {
		return fmt.Errorf("comment must be at most %d characters", maxCommentLength)
	}
	if strings.HasPrefix(raw, "-") {
		return fmt.Errorf("comment must not start with a dash")
	}
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune(" ._(),:/+-@", r):
		default:
			return fmt.Errorf("comment contains an unsupported character")
		}
	}
	return nil
}

func fingerprint(r rule) string {
	return strings.Join([]string{
		actionSlug(r.Action), directionSlug(r.Direction), protocolSlug(r.Protocol), r.Port, r.Source,
	}, "|")
}

func (r *rule) assignID() {
	if r.ID != "" {
		return
	}
	sum := sha256.Sum256([]byte(fingerprint(*r)))
	r.ID = "r-" + hex.EncodeToString(sum[:8])
}

// assignReadOnlyID keys a read-only backend row by its own text instead of the
// shared model: two app profiles both look like "allow any" in the model, and
// keying them by an empty field would collapse them into one row.
func (r *rule) assignReadOnlyID(key string) {
	sum := sha256.Sum256([]byte("readonly|" + key))
	r.ID = "ro-" + hex.EncodeToString(sum[:8])
}

func actionSlug(v agentv1.FirewallAction) string {
	switch v {
	case agentv1.FirewallAction_FIREWALL_ACTION_ALLOW:
		return "allow"
	case agentv1.FirewallAction_FIREWALL_ACTION_DENY:
		return "deny"
	}
	return ""
}

func directionSlug(v agentv1.FirewallDirection) string {
	switch v {
	case agentv1.FirewallDirection_FIREWALL_DIRECTION_IN:
		return "in"
	case agentv1.FirewallDirection_FIREWALL_DIRECTION_OUT:
		return "out"
	}
	return ""
}

func protocolSlug(v agentv1.FirewallProtocol) string {
	switch v {
	case agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP:
		return "tcp"
	case agentv1.FirewallProtocol_FIREWALL_PROTOCOL_UDP:
		return "udp"
	case agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY:
		return "any"
	}
	return ""
}

func actionValue(slug string) agentv1.FirewallAction {
	switch slug {
	case "allow":
		return agentv1.FirewallAction_FIREWALL_ACTION_ALLOW
	case "deny":
		return agentv1.FirewallAction_FIREWALL_ACTION_DENY
	}
	return agentv1.FirewallAction_FIREWALL_ACTION_UNSPECIFIED
}

func directionValue(slug string) agentv1.FirewallDirection {
	switch slug {
	case "in":
		return agentv1.FirewallDirection_FIREWALL_DIRECTION_IN
	case "out":
		return agentv1.FirewallDirection_FIREWALL_DIRECTION_OUT
	}
	return agentv1.FirewallDirection_FIREWALL_DIRECTION_UNSPECIFIED
}

func protocolValue(slug string) agentv1.FirewallProtocol {
	switch slug {
	case "tcp":
		return agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP
	case "udp":
		return agentv1.FirewallProtocol_FIREWALL_PROTOCOL_UDP
	case "any":
		return agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY
	}
	return agentv1.FirewallProtocol_FIREWALL_PROTOCOL_UNSPECIFIED
}

func (r rule) proto() *agentv1.FirewallRule {
	p := &agentv1.FirewallRule{
		Id:        r.ID,
		Action:    r.Action,
		Direction: r.Direction,
		Protocol:  r.Protocol,
		Port:      r.Port,
		Source:    r.Source,
		Comment:   r.Comment,
		Enabled:   r.Enabled,
		Protected: r.Protected,
		ReadOnly:  r.ReadOnly,
	}
	return p
}

func ruleFromProto(p *agentv1.FirewallRule) rule {
	if p == nil {
		return rule{}
	}
	return rule{
		ID:        p.GetId(),
		Action:    p.GetAction(),
		Direction: p.GetDirection(),
		Protocol:  p.GetProtocol(),
		Port:      p.GetPort(),
		Source:    p.GetSource(),
		Comment:   p.GetComment(),
		Enabled:   p.GetEnabled(),
	}
}

// portCovers reports whether a rule's port selector matches p. An empty
// selector means any port.
func portCovers(spec string, p uint32) bool {
	if spec == "" {
		return true
	}
	parts := strings.SplitN(spec, "-", 2)
	first, err := parsePort(parts[0])
	if err != nil {
		return false
	}
	if len(parts) == 1 {
		return first == p
	}
	last, err := parsePort(parts[1])
	if err != nil {
		return false
	}
	return p >= first && p <= last
}
