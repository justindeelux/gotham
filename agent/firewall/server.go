package firewall

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Config describes one node agent's firewall service.
type Config struct {
	// StatePath holds disabled rules and comments the backend cannot report.
	StatePath string
	// ListenAddr is the agent's own listen address; its port is protected.
	ListenAddr string
	// Helper overrides the root-owned host helper path (tests).
	Helper string
	Runner Runner
	Logger *slog.Logger
}

// Server implements the FirewallService on one node.
type Server struct {
	agentv1.UnimplementedFirewallServiceServer

	statePath string
	agentPort uint32
	runner    Runner
	log       *slog.Logger
	mu        sync.Mutex
}

func NewServer(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	runner := cfg.Runner
	if runner == nil {
		helper := cfg.Helper
		if helper == "" {
			helper = HelperPath
		}
		runner = sudoRunner{helper: helper}
	}
	return &Server{
		statePath: cfg.StatePath,
		agentPort: listenPort(cfg.ListenAddr),
		runner:    runner,
		log:       logger,
	}
}

func listenPort(addr string) uint32 {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return 0
	}
	return uint32(n)
}

type backendStatus struct {
	Backend    agentv1.FirewallBackend
	Enabled    bool
	Manageable bool
	SSHPorts   []uint32
	AgentPort  uint32
	Detail     string
	// DetailCode is the stable code of Detail; DetailParams are the values its
	// translated text needs.
	DetailCode   string
	DetailParams map[string]string
}

func (st backendStatus) detail() detail {
	return detail{code: st.DetailCode, params: st.DetailParams}
}

// setDetail records why this backend cannot be managed, with its stable code.
func (st *backendStatus) setDetail(code string, params map[string]string, message string) {
	st.DetailCode = code
	st.DetailParams = params
	st.Detail = message
}

// refuse reports why this node's firewall cannot be managed, with its code.
func refuse(st backendStatus) error {
	return fail(codes.FailedPrecondition, st.detail(), "%s", st.Detail)
}

func (s *Server) GetStatus(ctx context.Context, req *agentv1.FirewallStatusRequest) (*agentv1.FirewallStatusResponse, error) {
	st, _ := s.probe(ctx)
	return s.statusProto(st, req.GetControlPlanePort()), nil
}

func (s *Server) probe(ctx context.Context) (backendStatus, error) {
	out, err := s.runner.Run(ctx, "firewall-status", "")
	if err != nil {
		reasonText := "The node's host helper could not report the firewall. Update this node (run its Gotham update) and try again."
		if reason := strings.TrimSpace(err.Error()); reason != "" {
			reasonText += " Reason: " + truncate(reason, maxCommentLength)
		}
		return backendStatus{
			Backend:    agentv1.FirewallBackend_FIREWALL_BACKEND_NONE,
			Detail:     reasonText,
			DetailCode: codeHelperMissing,
		}, err
	}
	st := parseBackendStatus(out)
	st.AgentPort = s.agentPort
	return st, nil
}

func parseBackendStatus(out string) backendStatus {
	st := backendStatus{Backend: agentv1.FirewallBackend_FIREWALL_BACKEND_NONE}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "backend":
			st.Backend = backendValue(value)
		case "enabled":
			st.Enabled = value == "true"
		case "manageable":
			st.Manageable = value == "true"
		case "ssh_ports":
			st.SSHPorts = parsePortList(value)
		case "ssh_port":
			// Older helpers reported one port; keep reading it so an agent
			// update that lands before the helper update still works.
			st.SSHPorts = appendUniquePort(st.SSHPorts, value)
		}
	}
	switch st.Backend {
	case agentv1.FirewallBackend_FIREWALL_BACKEND_UFW, agentv1.FirewallBackend_FIREWALL_BACKEND_FIREWALLD:
	case agentv1.FirewallBackend_FIREWALL_BACKEND_NFTABLES:
		st.Manageable = false
		st.setDetail(codeUnsupportedBackend, map[string]string{"backend": "nftables"},
			"This node uses nftables. Gotham can detect it but cannot edit its rules yet; manage them on the node.")
	case agentv1.FirewallBackend_FIREWALL_BACKEND_IPTABLES:
		st.Manageable = false
		st.setDetail(codeUnsupportedBackend, map[string]string{"backend": "iptables"},
			"This node uses iptables. Gotham can detect it but cannot edit its rules yet; manage them on the node.")
	default:
		st.Manageable = false
		st.setDetail(codeNoFirewall, nil, "No supported firewall is installed on this node.")
	}
	return st
}

func parsePortList(value string) []uint32 {
	ports := make([]uint32, 0, 2)
	for _, part := range strings.Split(value, ",") {
		ports = appendUniquePort(ports, strings.TrimSpace(part))
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	return ports
}

func appendUniquePort(ports []uint32, raw string) []uint32 {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 16)
	if err != nil || n == 0 {
		return ports
	}
	port := uint32(n)
	for _, existing := range ports {
		if existing == port {
			return ports
		}
	}
	return append(ports, port)
}

func backendValue(slug string) agentv1.FirewallBackend {
	switch slug {
	case "ufw":
		return agentv1.FirewallBackend_FIREWALL_BACKEND_UFW
	case "firewalld":
		return agentv1.FirewallBackend_FIREWALL_BACKEND_FIREWALLD
	case "nftables":
		return agentv1.FirewallBackend_FIREWALL_BACKEND_NFTABLES
	case "iptables":
		return agentv1.FirewallBackend_FIREWALL_BACKEND_IPTABLES
	default:
		return agentv1.FirewallBackend_FIREWALL_BACKEND_NONE
	}
}

func (s *Server) statusProto(st backendStatus, controlPlanePort uint32) *agentv1.FirewallStatusResponse {
	return &agentv1.FirewallStatusResponse{
		Backend:          st.Backend,
		Available:        st.Backend != agentv1.FirewallBackend_FIREWALL_BACKEND_NONE,
		Enabled:          st.Enabled,
		Manageable:       st.Manageable,
		Detail:           st.Detail,
		DetailCode:       st.DetailCode,
		DetailParams:     st.DetailParams,
		SshPorts:         st.SSHPorts,
		AgentPort:        st.AgentPort,
		ControlPlanePort: controlPlanePort,
	}
}

func (s *Server) requireManageable(ctx context.Context) (backendStatus, []rule, error) {
	st, err := s.probe(ctx)
	if err != nil {
		return st, nil, refuse(st)
	}
	if !st.Manageable {
		return st, nil, refuse(st)
	}
	rules, err := s.list(ctx, st)
	if err != nil {
		return st, nil, err
	}
	return st, rules, nil
}

// list returns applied rules followed by the disabled rules saved on this node.
func (s *Server) list(ctx context.Context, st backendStatus) ([]rule, error) {
	applied, err := s.appliedRules(ctx, st)
	if err != nil {
		return nil, err
	}
	state, err := loadState(s.statePath)
	if err != nil {
		return nil, fail(codes.Internal, detail{code: codeStateReadFailed},
			"cannot read the node's saved firewall rules")
	}
	for i := range applied {
		if comment := state.Comments[fingerprint(applied[i])]; comment != "" {
			applied[i].Comment = comment
		}
	}
	rules := append([]rule{}, applied...)
	for _, saved := range state.Disabled {
		rules = append(rules, disabledRuleFromSaved(saved))
	}
	return markProtected(rules, protectedPorts(st, 0)), nil
}

func (s *Server) appliedRules(ctx context.Context, st backendStatus) ([]rule, error) {
	out, err := s.runner.Run(ctx, "firewall-list", "")
	if err != nil {
		return nil, fail(codes.FailedPrecondition, detail{code: codeHelperFailed}, "%s", err)
	}
	header, listing, extra := splitList(out)
	if backendValue(headerValue(header, "backend")) != st.Backend {
		return nil, fail(codes.FailedPrecondition, detail{code: codeBackendChanged},
			"the node's firewall backend changed; reload the page")
	}
	switch st.Backend {
	case agentv1.FirewallBackend_FIREWALL_BACKEND_UFW:
		return parseUFWRules(listing, extra), nil
	case agentv1.FirewallBackend_FIREWALL_BACKEND_FIREWALLD:
		return parseFirewalldRules(listing), nil
	}
	return nil, nil
}

func headerValue(header, key string) string {
	for _, line := range strings.Split(header, "\n") {
		k, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && k == key {
			return value
		}
	}
	return ""
}

func (s *Server) ListRules(ctx context.Context, req *agentv1.FirewallRulesRequest) (*agentv1.FirewallRules, error) {
	st, err := s.probe(ctx)
	if err != nil {
		return nil, refuse(st)
	}
	if !st.Manageable {
		return &agentv1.FirewallRules{}, nil
	}
	rules, err := s.list(ctx, st)
	if err != nil {
		return nil, err
	}
	rules = markProtected(rules, protectedPorts(st, req.GetControlPlanePort()))
	return &agentv1.FirewallRules{Rules: protoRules(rules)}, nil
}

func (s *Server) SetEnabled(ctx context.Context, req *agentv1.FirewallEnabledRequest) (*agentv1.FirewallState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.probe(ctx)
	if err != nil {
		return nil, refuse(st)
	}
	if !st.Manageable {
		return nil, refuse(st)
	}
	ports := protectedPorts(st, req.GetControlPlanePort())
	if req.GetEnabled() {
		if err := s.enable(ctx, st, ports); err != nil {
			return nil, err
		}
	} else if err := s.run(ctx, "firewall-disable", ""); err != nil {
		return nil, fail(codes.FailedPrecondition, detail{code: codeHelperFailed}, "%s", err)
	}

	st, err = s.probe(ctx)
	if err != nil {
		return nil, refuse(st)
	}
	rules, err := s.list(ctx, st)
	if err != nil {
		return nil, err
	}
	rules = markProtected(rules, ports)
	s.log.Info("firewall: enabled state changed", "enabled", req.GetEnabled(), "backend", st.Backend.String())
	return &agentv1.FirewallState{Status: s.statusProto(st, req.GetControlPlanePort()), Rules: protoRules(rules)}, nil
}

// InstallFirewall installs the distribution's firewall when the node has none,
// keeps the operator's access rules and turns it on. A node that already has a
// backend is left unchanged.
func (s *Server) InstallFirewall(ctx context.Context, req *agentv1.FirewallInstallRequest) (*agentv1.FirewallState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.probe(ctx)
	if err != nil {
		return nil, refuse(st)
	}
	if st.Backend != agentv1.FirewallBackend_FIREWALL_BACKEND_NONE {
		return s.installState(ctx, st, req.GetControlPlanePort())
	}
	if err := s.run(ctx, "firewall-install", ""); err != nil {
		return nil, installError(err)
	}
	st, err = s.probe(ctx)
	if err != nil {
		return nil, refuse(st)
	}
	if st.Backend == agentv1.FirewallBackend_FIREWALL_BACKEND_NONE || !st.Manageable {
		return nil, fail(codes.FailedPrecondition, detail{code: codeInstallFailed},
			"the firewall could not be installed on this node")
	}
	if err := s.enable(ctx, st, protectedPorts(st, req.GetControlPlanePort())); err != nil {
		return nil, err
	}
	st, err = s.probe(ctx)
	if err != nil {
		return nil, refuse(st)
	}
	s.log.Info("firewall: installed", "backend", st.Backend.String())
	return s.installState(ctx, st, req.GetControlPlanePort())
}

// installState reports a node after an install without changing its rules.
func (s *Server) installState(ctx context.Context, st backendStatus, controlPlanePort uint32) (*agentv1.FirewallState, error) {
	rules := []rule{}
	if st.Manageable {
		var err error
		rules, err = s.list(ctx, st)
		if err != nil {
			return nil, err
		}
		rules = markProtected(rules, protectedPorts(st, controlPlanePort))
	}
	return &agentv1.FirewallState{
		Status: s.statusProto(st, controlPlanePort),
		Rules:  protoRules(rules),
	}, nil
}

// installError maps the helper's refusals: exit 3 is a distribution with no
// supported package, every other failure is an install that did not complete.
func installError(err error) error {
	if helperExitCode(err) == 3 {
		return fail(codes.FailedPrecondition, detail{code: codeInstallUnsupported},
			"no supported firewall can be installed on this node")
	}
	return fail(codes.FailedPrecondition, detail{code: codeInstallFailed}, "%s", err)
}

func helperExitCode(err error) int {
	var helper *helperError
	if errors.As(err, &helper) {
		return helper.code
	}
	return 0
}

// enable keeps the operator's access rules before turning the backend on.
// ufw can change its rule set while inactive, so it is prepared first. A
// stopped firewalld cannot be configured, so it is started first and disabled
// again if the access rules cannot be added.
func (s *Server) enable(ctx context.Context, st backendStatus, ports []accessPort) error {
	if st.Backend == agentv1.FirewallBackend_FIREWALL_BACKEND_FIREWALLD {
		started := !st.Enabled
		if started {
			if err := s.run(ctx, "firewall-enable", ""); err != nil {
				return fail(codes.FailedPrecondition, detail{code: codeHelperFailed}, "%s", err)
			}
			st.Enabled = true
		}
		if err := s.ensureAccess(ctx, st, ports); err != nil {
			if started {
				_ = s.run(ctx, "firewall-disable", "")
			}
			return err
		}
		return nil
	}
	if err := s.ensureAccess(ctx, st, ports); err != nil {
		return err
	}
	if err := s.run(ctx, "firewall-enable", ""); err != nil {
		return fail(codes.FailedPrecondition, detail{code: codeHelperFailed}, "%s", err)
	}
	return nil
}

func (s *Server) ensureAccess(ctx context.Context, st backendStatus, ports []accessPort) error {
	rules, err := s.appliedRules(ctx, st)
	if err != nil {
		return err
	}
	if err := denyBlocks(rules, ports); err != nil {
		return err
	}
	for _, missing := range missingAccessRules(rules, ports) {
		if err := s.applyAdd(ctx, missing); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) AddRule(ctx context.Context, req *agentv1.FirewallRuleCreateRequest) (*agentv1.FirewallRules, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	in, err := normalizeRule(ruleFromProto(req.GetRule()))
	if err != nil {
		return nil, err
	}
	st, rules, err := s.requireManageable(ctx)
	if err != nil {
		return nil, err
	}
	ports := protectedPorts(st, req.GetControlPlanePort())
	state, err := loadState(s.statePath)
	if err != nil {
		return nil, fail(codes.Internal, detail{code: codeStateReadFailed},
			"cannot read the node's saved firewall rules")
	}
	if in.Enabled {
		in.assignID()
		if st.Enabled {
			if err := denyBlocks(append(enabledRules(rules), in), ports); err != nil {
				return nil, err
			}
		}
		if err := s.applyAdd(ctx, in); err != nil {
			return nil, err
		}
		if in.Comment == "" {
			delete(state.Comments, fingerprint(in))
		} else {
			state.Comments[fingerprint(in)] = in.Comment
		}
	} else {
		in.ID = "disabled-" + uuid.NewString()
		state.Disabled = append(state.Disabled, savedFromDisabled(in))
	}
	if err := saveState(s.statePath, state); err != nil {
		return s.stateError(err)
	}
	return s.rulesReply(ctx, st, ports)
}

func (s *Server) UpdateRule(ctx context.Context, req *agentv1.FirewallRuleUpdateRequest) (*agentv1.FirewallRules, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	in, err := normalizeRule(ruleFromProto(req.GetRule()))
	if err != nil {
		return nil, err
	}
	st, rules, err := s.requireManageable(ctx)
	if err != nil {
		return nil, err
	}
	old, ok := findRule(rules, req.GetId())
	if !ok {
		return nil, ruleNotFound()
	}
	if old.ReadOnly {
		return nil, readOnlyRule("this rule is managed on the node and cannot be edited from Gotham")
	}
	in.ID = old.ID
	ports := protectedPorts(st, req.GetControlPlanePort())
	state, err := loadState(s.statePath)
	if err != nil {
		return nil, fail(codes.Internal, detail{code: codeStateReadFailed},
			"cannot read the node's saved firewall rules")
	}

	switch {
	case old.Enabled && in.Enabled:
		applied := enabledRules(rules)
		applied = replaceRule(applied, old.ID, in)
		if st.Enabled {
			if err := lostAccess(enabledRules(rules), applied, ports); err != nil {
				return nil, err
			}
		}
		if fingerprint(old) != fingerprint(in) {
			if err := s.applyAdd(ctx, in); err != nil {
				return nil, err
			}
		}
		if fingerprint(old) != fingerprint(in) {
			if err := s.applyDelete(ctx, old); err != nil {
				return nil, err
			}
		}
		if old.Comment != in.Comment {
			delete(state.Comments, fingerprint(old))
			if in.Comment == "" {
				delete(state.Comments, fingerprint(in))
			} else {
				state.Comments[fingerprint(in)] = in.Comment
			}
		}
	case old.Enabled && !in.Enabled:
		applied := removeRule(enabledRules(rules), old.ID)
		if st.Enabled {
			if err := lostAccess(enabledRules(rules), applied, ports); err != nil {
				return nil, err
			}
		}
		if err := s.applyDelete(ctx, old); err != nil {
			return nil, err
		}
		delete(state.Comments, fingerprint(old))
		state.Disabled = append(removeSavedRule(state.Disabled, old.ID), savedFromDisabled(in))
	case !old.Enabled && in.Enabled:
		if st.Enabled {
			if err := denyBlocks(append(enabledRules(rules), in), ports); err != nil {
				return nil, err
			}
		}
		if err := s.applyAdd(ctx, in); err != nil {
			return nil, err
		}
		state.Disabled = removeSavedRule(state.Disabled, old.ID)
		if in.Comment != "" {
			state.Comments[fingerprint(in)] = in.Comment
		}
	case !old.Enabled && !in.Enabled:
		state.Disabled = append(removeSavedRule(state.Disabled, old.ID), savedFromDisabled(in))
	}
	if err := saveState(s.statePath, state); err != nil {
		return s.stateError(err)
	}
	return s.rulesReply(ctx, st, ports)
}

func (s *Server) DeleteRule(ctx context.Context, req *agentv1.FirewallRuleDeleteRequest) (*agentv1.FirewallRules, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, rules, err := s.requireManageable(ctx)
	if err != nil {
		return nil, err
	}
	old, ok := findRule(rules, req.GetId())
	if !ok {
		return nil, ruleNotFound()
	}
	if old.ReadOnly {
		return nil, readOnlyRule("this rule is managed on the node and cannot be deleted from Gotham")
	}
	ports := protectedPorts(st, req.GetControlPlanePort())
	state, err := loadState(s.statePath)
	if err != nil {
		return nil, fail(codes.Internal, detail{code: codeStateReadFailed},
			"cannot read the node's saved firewall rules")
	}
	if old.Enabled {
		if st.Enabled {
			if err := lostAccess(enabledRules(rules), removeRule(enabledRules(rules), old.ID), ports); err != nil {
				return nil, err
			}
		}
		if err := s.applyDelete(ctx, old); err != nil {
			return nil, err
		}
		delete(state.Comments, fingerprint(old))
	} else {
		state.Disabled = removeSavedRule(state.Disabled, old.ID)
	}
	if err := saveState(s.statePath, state); err != nil {
		return s.stateError(err)
	}
	return s.rulesReply(ctx, st, ports)
}

func (s *Server) rulesReply(ctx context.Context, st backendStatus, ports []accessPort) (*agentv1.FirewallRules, error) {
	rules, err := s.list(ctx, st)
	if err != nil {
		return nil, err
	}
	rules = markProtected(rules, ports)
	return &agentv1.FirewallRules{Rules: protoRules(rules)}, nil
}

func (s *Server) stateError(err error) (*agentv1.FirewallRules, error) {
	s.log.Error("firewall: saved rule state failed", "error", err)
	return nil, fail(codes.Internal, detail{code: codeStateWriteFailed},
		"the firewall change was applied but its saved rule state could not be written")
}

func (s *Server) applyAdd(ctx context.Context, r rule) error {
	mode := "firewall-add"
	input := hostctlInput(r)
	if err := s.run(ctx, mode, input); err != nil {
		return fail(codes.FailedPrecondition, detail{code: codeHelperFailed}, "%s", err)
	}
	return nil
}

func (s *Server) applyDelete(ctx context.Context, r rule) error {
	mode := "firewall-delete"
	input := hostctlInput(r)
	if r.raw != "" {
		input = "raw=" + r.raw + "\n"
	}
	if err := s.run(ctx, mode, input); err != nil {
		return fail(codes.FailedPrecondition, detail{code: codeHelperFailed}, "%s", err)
	}
	return nil
}

func (s *Server) run(ctx context.Context, verb, stdin string) error {
	_, err := s.runner.Run(ctx, verb, stdin)
	return err
}

func hostctlInput(r rule) string {
	lines := []string{
		"action=" + actionSlug(r.Action),
		"direction=" + directionSlug(r.Direction),
		"protocol=" + protocolSlug(r.Protocol),
		"port=" + r.Port,
		"source=" + r.Source,
		"comment=" + r.Comment,
	}
	return strings.Join(lines, "\n") + "\n"
}

func findRule(rules []rule, id string) (rule, bool) {
	for _, r := range rules {
		if r.ID == id {
			return r, true
		}
	}
	return rule{}, false
}

func enabledRules(rules []rule) []rule {
	out := make([]rule, 0, len(rules))
	for _, r := range rules {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out
}

func replaceRule(rules []rule, id string, next rule) []rule {
	out := make([]rule, 0, len(rules))
	for _, r := range rules {
		if r.ID == id {
			out = append(out, next)
			continue
		}
		out = append(out, r)
	}
	return out
}

func removeRule(rules []rule, id string) []rule {
	out := make([]rule, 0, len(rules))
	for _, r := range rules {
		if r.ID != id {
			out = append(out, r)
		}
	}
	return out
}

func removeSavedRule(rules []savedRule, id string) []savedRule {
	out := make([]savedRule, 0, len(rules))
	for _, r := range rules {
		if r.ID != id {
			out = append(out, r)
		}
	}
	return out
}

func protoRules(rules []rule) []*agentv1.FirewallRule {
	out := make([]*agentv1.FirewallRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.proto())
	}
	return out
}
