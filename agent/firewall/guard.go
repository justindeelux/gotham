package firewall

import (
	"fmt"
	"strconv"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// accessPort names one port the operator must keep reachable: they reach the
// node over SSH, the control plane dials the agent, and a browser opens the
// control-plane UI.
type accessPort struct {
	port  uint32
	label string
}

func protectedPorts(status backendStatus, controlPlanePort uint32) []accessPort {
	seen := make(map[uint32]bool, 3)
	ports := make([]accessPort, 0, 3)
	add := func(port uint32, label string) {
		if port == 0 || seen[port] {
			return
		}
		seen[port] = true
		ports = append(ports, accessPort{port: port, label: label})
	}
	add(status.SSHPort, "SSH")
	add(status.AgentPort, "the node agent")
	add(controlPlanePort, "the control plane")
	return ports
}

// denyBlocks refuses a rule set with an inbound deny for a protected port.
// This check is deliberately conservative: it does not look at the source,
// because the control plane cannot know which address the operator connects
// from.
func denyBlocks(rules []rule, ports []accessPort) error {
	for _, target := range ports {
		for _, r := range rules {
			if !r.Enabled || r.ReadOnly || r.Direction != agentv1.FirewallDirection_FIREWALL_DIRECTION_IN ||
				r.Action != agentv1.FirewallAction_FIREWALL_ACTION_DENY ||
				!portCovers(r.Port, target.port) || !protocolCovers(r.Protocol) {
				continue
			}
			return fmt.Errorf(
				"this rule set denies port %d, which would block %s access; add an allow rule for that port instead",
				target.port, target.label)
		}
	}
	return nil
}

// lostAccess refuses a change that removes the last open allow for a protected
// port. A port that was already uncovered is not made worse by the change, so
// an operator can still add the missing access rules one at a time.
func lostAccess(before, after []rule, ports []accessPort) error {
	if err := denyBlocks(after, ports); err != nil {
		return err
	}
	for _, target := range ports {
		if portAllowed(before, target.port) && !portAllowed(after, target.port) {
			return fmt.Errorf(
				"this change would block port %d, which is needed for %s access; keep an allow rule for that port",
				target.port, target.label)
		}
	}
	return nil
}

func portAllowed(rules []rule, port uint32) bool {
	for _, r := range rules {
		if !r.Enabled || r.ReadOnly || r.Direction != agentv1.FirewallDirection_FIREWALL_DIRECTION_IN ||
			r.Action != agentv1.FirewallAction_FIREWALL_ACTION_ALLOW ||
			r.Source != "" || !portCovers(r.Port, port) || !protocolCovers(r.Protocol) {
			continue
		}
		return true
	}
	return false
}

func protocolCovers(p agentv1.FirewallProtocol) bool {
	return p == agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP ||
		p == agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY
}

// missingAccessRules returns the allow rules a rule set must keep for the
// protected ports. They are written with a fixed comment so an operator can
// recognise them on the host.
func missingAccessRules(rules []rule, ports []accessPort) []rule {
	missing := make([]rule, 0, len(ports))
	for _, target := range ports {
		if portAllowed(rules, target.port) {
			continue
		}
		missing = append(missing, rule{
			Action:    agentv1.FirewallAction_FIREWALL_ACTION_ALLOW,
			Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN,
			Protocol:  agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP,
			Port:      strconv.Itoa(int(target.port)),
			Comment:   "Gotham: keep " + target.label + " access",
			Enabled:   true,
		})
	}
	return missing
}

// markProtected flags the exact allow rules that keep a protected port open.
// Broader rules are not flagged, but lostAccess still refuses any change that
// would leave a protected port uncovered.
func markProtected(rules []rule, ports []accessPort) []rule {
	for i := range rules {
		r := &rules[i]
		if r.ReadOnly || r.Direction != agentv1.FirewallDirection_FIREWALL_DIRECTION_IN ||
			r.Action != agentv1.FirewallAction_FIREWALL_ACTION_ALLOW ||
			r.Source != "" || !protocolCovers(r.Protocol) {
			continue
		}
		for _, target := range ports {
			if r.Port == strconv.Itoa(int(target.port)) {
				r.Protected = true
				break
			}
		}
	}
	return rules
}
