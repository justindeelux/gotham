package firewall

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Stable codes the control plane forwards to the web, which translates one
// text per code. The English message stays the fallback for an unknown code.
const (
	codeNoFirewall            = "no_firewall"
	codeUnsupportedBackend    = "unsupported_backend"
	codeHelperMissing         = "helper_missing"
	codeHelperFailed          = "helper_failed"
	codeBackendChanged        = "backend_changed"
	codeStateReadFailed       = "state_read_failed"
	codeStateWriteFailed      = "state_write_failed"
	codeRuleNotFound          = "rule_not_found"
	codeReadOnlyRule          = "read_only_rule"
	codeWouldLockOut          = "would_lock_out"
	codeInvalidAction         = "invalid_action"
	codeInvalidDirection      = "invalid_direction"
	codeInvalidProtocol       = "invalid_protocol"
	codeInvalidPort           = "invalid_port"
	codeInvalidSource         = "invalid_source"
	codeInvalidOutboundSource = "invalid_outbound_source"
	codeInvalidComment        = "invalid_comment"
)

// detail names one stable code and the values its translated text needs.
type detail struct {
	code   string
	params map[string]string
}

// failure is a refusal that carries a stable code. It reports itself as a gRPC
// status, so the control plane reads the code from the status detail.
type failure struct {
	grpcCode codes.Code
	detail   detail
	message  string
}

func (f *failure) Error() string { return f.message }

func (f *failure) GRPCStatus() *status.Status {
	st := status.New(f.grpcCode, f.message)
	if f.detail.code == "" {
		return st
	}
	withDetail, err := st.WithDetails(&agentv1.FirewallError{
		Code:   f.detail.code,
		Params: f.detail.params,
	})
	if err != nil {
		return st
	}
	return withDetail
}

func fail(code codes.Code, d detail, format string, args ...any) *failure {
	return &failure{grpcCode: code, detail: d, message: fmt.Sprintf(format, args...)}
}

// invalid builds a rule validation refusal the web translates by code.
func invalid(code, format string, args ...any) *failure {
	return fail(codes.InvalidArgument, detail{code: code}, format, args...)
}

// ruleNotFound reports a rule id the node does not know, usually a stale page.
func ruleNotFound() *failure {
	return fail(codes.NotFound, detail{code: codeRuleNotFound}, "firewall rule not found")
}

// readOnlyRule reports a rule the operator must change on the node itself.
func readOnlyRule(message string) *failure {
	return fail(codes.FailedPrecondition, detail{code: codeReadOnlyRule}, "%s", message)
}
