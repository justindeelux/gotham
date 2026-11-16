// Package firewall exposes one node's firewall through the control plane. The
// node agent owns the backend state and the lockout protection; this package
// validates input, maps the agent's answers to the HTTP API and audits every
// change. No rule state is stored in the control-plane database.
package firewall

import "errors"

var (
	ErrNotFound    = errors.New("firewall: server not found")
	ErrValidation  = errors.New("firewall: validation failed")
	ErrUnavailable = errors.New("firewall: node agent unavailable")
	ErrUnsupported = errors.New("firewall: this node's agent does not support firewall management; update the agent")
	// ErrConflict carries a refusal from the node, including lockout
	// protection. Its message is safe to show the operator.
	ErrConflict = errors.New("firewall: the node refused the change")
)

// Backend names in the API.
const (
	BackendNone      = "none"
	BackendUFW       = "ufw"
	BackendFirewalld = "firewalld"
	BackendNftables  = "nftables"
	BackendIptables  = "iptables"
	ActionAllow      = "allow"
	ActionDeny       = "deny"
	DirectionIn      = "in"
	DirectionOut     = "out"
	ProtocolTCP      = "tcp"
	ProtocolUDP      = "udp"
	ProtocolAny      = "any"
)
