// Package firewall exposes one node's firewall through the control plane. The
// node agent owns the backend state and the lockout protection; this package
// validates input, maps the agent's answers to the HTTP API and audits every
// change. No rule state is stored in the control-plane database.
package firewall

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

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

// Stable codes the web translates. A node agent sends its own codes with its
// refusals; these are the control plane's own. The English message stays the
// fallback for an unknown code.
const (
	codeAgentOutdated         = "agent_outdated"
	codeAgentUnavailable      = "agent_unavailable"
	codeRuleNotFound          = "rule_not_found"
	codeInvalidRequest        = "invalid_request"
	codeInvalidRule           = "invalid_rule"
	codeInvalidAction         = "invalid_action"
	codeInvalidDirection      = "invalid_direction"
	codeInvalidProtocol       = "invalid_protocol"
	codeInvalidPort           = "invalid_port"
	codeInvalidSource         = "invalid_source"
	codeInvalidOutboundSource = "invalid_outbound_source"
	codeInvalidComment        = "invalid_comment"
)

// detail is a stable code and the values its translated text needs.
type detail struct {
	code   string
	params map[string]string
}

// withDefault fills in a code the routing layer knows but the error does not.
func (d detail) withDefault(code string) detail {
	if d.code == "" {
		d.code = code
	}
	return d
}

// codedError is a firewall failure with a stable code. It still matches its
// sentinel, so the routes keep one status mapping.
type codedError struct {
	sentinel error
	d        detail
	message  string
}

func (e *codedError) Error() string { return e.message }

func (e *codedError) Unwrap() error { return e.sentinel }

func coded(sentinel error, d detail, format string, args ...any) error {
	return &codedError{sentinel: sentinel, d: d, message: fmt.Sprintf(format, args...)}
}

// detailOf reads the code of a coded error; an older node sends none.
func detailOf(err error) detail {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.d
	}
	return detail{}
}

// nodeDetail reads the stable code a node agent attached to its refusal.
func nodeDetail(st *status.Status) (detail, bool) {
	for _, item := range st.Details() {
		if e, ok := item.(*agentv1.FirewallError); ok && e.GetCode() != "" {
			return detail{code: e.GetCode(), params: e.GetParams()}, true
		}
	}
	return detail{}, false
}

// invalid builds a rule validation failure the web translates by code.
func invalid(code, format string, args ...any) error {
	return coded(ErrValidation, detail{code: code}, format, args...)
}
