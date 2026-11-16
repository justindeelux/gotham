package firewall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	readTimeout  = 30 * time.Second
	writeTimeout = 90 * time.Second
)

type AgentClient interface {
	agentv1.FirewallServiceClient
}

type AgentConn interface {
	AgentClient
	io.Closer
}

type DialFunc func(ctx context.Context, serverID uuid.UUID) (AgentConn, error)

type Config struct {
	Dial             DialFunc
	ControlPlanePort uint32
	// ProtectControlPlane reports whether this server's firewall must keep the
	// control-plane port open. Nil protects it everywhere (tests and a single
	// local deployment); the server wiring protects it on the local node only.
	ProtectControlPlane func(ctx context.Context, serverID uuid.UUID) bool
	Logger              *slog.Logger
}

type Service struct {
	dial                DialFunc
	controlPlanePort    uint32
	protectControlPlane func(ctx context.Context, serverID uuid.UUID) bool
	logger              *slog.Logger
}

func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		dial:                cfg.Dial,
		controlPlanePort:    cfg.ControlPlanePort,
		protectControlPlane: cfg.ProtectControlPlane,
		logger:              logger,
	}
}

// controlPlanePort is the port the node must keep allowed. A remote node only
// protects its own SSH and agent ports; the control plane runs elsewhere.
func (s *Service) controlPlanePortFor(ctx context.Context, serverID uuid.UUID) uint32 {
	if s.protectControlPlane != nil && !s.protectControlPlane(ctx, serverID) {
		return 0
	}
	return s.controlPlanePort
}

func (s *Service) Status(ctx context.Context, serverID uuid.UUID) (Status, error) {
	client, closeConn, err := s.client(ctx, serverID)
	if err != nil {
		return Status{}, err
	}
	defer closeConn()
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	response, err := client.GetStatus(ctx, &agentv1.FirewallStatusRequest{ControlPlanePort: s.controlPlanePortFor(ctx, serverID)})
	if err != nil {
		return Status{}, nodeError(err)
	}
	return statusFromProto(response), nil
}

func (s *Service) SetEnabled(ctx context.Context, actor, serverID uuid.UUID, enabled bool) (State, error) {
	client, closeConn, err := s.client(ctx, serverID)
	if err != nil {
		return State{}, err
	}
	defer closeConn()
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	response, err := client.SetEnabled(ctx, &agentv1.FirewallEnabledRequest{
		Enabled:          enabled,
		ControlPlanePort: s.controlPlanePortFor(ctx, serverID),
	})
	action := "firewall.disable"
	if enabled {
		action = "firewall.enable"
	}
	if err != nil {
		s.audit(actor, serverID, action, "", "failed", err)
		return State{}, nodeError(err)
	}
	state := stateFromProto(response)
	s.audit(actor, serverID, action, "", "ok", nil, "enabled", state.Status.Enabled)
	return state, nil
}

func (s *Service) ListRules(ctx context.Context, serverID uuid.UUID) ([]Rule, error) {
	client, closeConn, err := s.client(ctx, serverID)
	if err != nil {
		return nil, err
	}
	defer closeConn()
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	response, err := client.ListRules(ctx, &agentv1.FirewallRulesRequest{ControlPlanePort: s.controlPlanePortFor(ctx, serverID)})
	if err != nil {
		return nil, nodeError(err)
	}
	return rulesFromProto(response.GetRules()), nil
}

func (s *Service) AddRule(ctx context.Context, actor, serverID uuid.UUID, in RuleInput) ([]Rule, error) {
	in, err := normalizeRule(in)
	if err != nil {
		return nil, err
	}
	client, closeConn, err := s.client(ctx, serverID)
	if err != nil {
		return nil, err
	}
	defer closeConn()
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	response, err := client.AddRule(ctx, &agentv1.FirewallRuleCreateRequest{
		Rule:             ruleInputProto(in),
		ControlPlanePort: s.controlPlanePortFor(ctx, serverID),
	})
	if err != nil {
		s.audit(actor, serverID, "firewall.rule.add", "", "failed", err, ruleAttrs(in)...)
		return nil, nodeError(err)
	}
	s.audit(actor, serverID, "firewall.rule.add", "", "ok", nil, ruleAttrs(in)...)
	return rulesFromProto(response.GetRules()), nil
}

func (s *Service) UpdateRule(ctx context.Context, actor, serverID uuid.UUID, ruleID string, in RuleInput) ([]Rule, error) {
	if ruleID == "" {
		return nil, coded(ErrValidation, detail{code: codeInvalidRule}, "rule id is required")
	}
	in, err := normalizeRule(in)
	if err != nil {
		return nil, err
	}
	client, closeConn, err := s.client(ctx, serverID)
	if err != nil {
		return nil, err
	}
	defer closeConn()
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	response, err := client.UpdateRule(ctx, &agentv1.FirewallRuleUpdateRequest{
		Id:               ruleID,
		Rule:             ruleInputProto(in),
		ControlPlanePort: s.controlPlanePortFor(ctx, serverID),
	})
	if err != nil {
		s.audit(actor, serverID, "firewall.rule.update", ruleID, "failed", err, ruleAttrs(in)...)
		return nil, nodeError(err)
	}
	s.audit(actor, serverID, "firewall.rule.update", ruleID, "ok", nil, ruleAttrs(in)...)
	return rulesFromProto(response.GetRules()), nil
}

func (s *Service) DeleteRule(ctx context.Context, actor, serverID uuid.UUID, ruleID string) ([]Rule, error) {
	if ruleID == "" {
		return nil, coded(ErrValidation, detail{code: codeInvalidRule}, "rule id is required")
	}
	client, closeConn, err := s.client(ctx, serverID)
	if err != nil {
		return nil, err
	}
	defer closeConn()
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	response, err := client.DeleteRule(ctx, &agentv1.FirewallRuleDeleteRequest{
		Id:               ruleID,
		ControlPlanePort: s.controlPlanePortFor(ctx, serverID),
	})
	if err != nil {
		s.audit(actor, serverID, "firewall.rule.delete", ruleID, "failed", err)
		return nil, nodeError(err)
	}
	s.audit(actor, serverID, "firewall.rule.delete", ruleID, "ok", nil)
	return rulesFromProto(response.GetRules()), nil
}

func (s *Service) client(ctx context.Context, serverID uuid.UUID) (AgentClient, func(), error) {
	if s.dial == nil {
		return nil, nil, ErrUnavailable
	}
	conn, err := s.dial(ctx, serverID)
	if err != nil {
		return nil, nil, err
	}
	return conn, func() { _ = conn.Close() }, nil
}

func (s *Service) audit(actor, serverID uuid.UUID, action, ruleID, result string, err error, extra ...any) {
	fields := []any{
		"actor_id", actor.String(),
		"server_id", serverID.String(),
		"action", action,
		"result", result,
	}
	if ruleID != "" {
		fields = append(fields, "rule_id", ruleID)
	}
	if err != nil {
		fields = append(fields, "error", err.Error())
	}
	fields = append(fields, extra...)
	s.logger.Info("firewall: audit", fields...)
}

func ruleAttrs(in RuleInput) []any {
	return []any{
		"rule_action", in.Action,
		"direction", in.Direction,
		"protocol", in.Protocol,
		"port", in.Port,
		"source", in.Source,
		"enabled", in.Enabled,
	}
}

func nodeError(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnavailable) || errors.Is(err, ErrUnsupported) {
		return err
	}
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	d, hasCode := nodeDetail(st)
	refuse := func(sentinel error) error {
		if !hasCode {
			// An older agent sends no code; its English message is all we
			// have, so the web keeps showing it.
			return fmt.Errorf("%w: %s", sentinel, st.Message())
		}
		return &codedError{sentinel: sentinel, d: d, message: st.Message()}
	}
	switch st.Code() {
	case codes.NotFound:
		return refuse(ErrNotFound)
	case codes.InvalidArgument:
		return refuse(ErrValidation)
	case codes.FailedPrecondition, codes.Aborted, codes.ResourceExhausted:
		return refuse(ErrConflict)
	case codes.Unimplemented:
		return ErrUnsupported
	case codes.Unavailable, codes.DeadlineExceeded:
		return refuse(ErrUnavailable)
	default:
		return refuse(ErrUnavailable)
	}
}

func statusFromProto(in *agentv1.FirewallStatusResponse) Status {
	if in == nil {
		return Status{Backend: BackendNone, SSHPorts: []uint32{}}
	}
	ports := in.GetSshPorts()
	if ports == nil {
		ports = []uint32{}
	}
	return Status{
		Backend:          backendSlug(in.GetBackend()),
		Available:        in.GetAvailable(),
		Enabled:          in.GetEnabled(),
		Manageable:       in.GetManageable(),
		Detail:           in.GetDetail(),
		DetailCode:       in.GetDetailCode(),
		DetailParams:     in.GetDetailParams(),
		SSHPorts:         ports,
		AgentPort:        in.GetAgentPort(),
		ControlPlanePort: in.GetControlPlanePort(),
	}
}

func stateFromProto(in *agentv1.FirewallState) State {
	return State{Status: statusFromProto(in.GetStatus()), Rules: rulesFromProto(in.GetRules())}
}

func rulesFromProto(in []*agentv1.FirewallRule) []Rule {
	rules := make([]Rule, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		rules = append(rules, Rule{
			ID:        item.GetId(),
			Action:    actionSlug(item.GetAction()),
			Direction: directionSlug(item.GetDirection()),
			Protocol:  protocolSlug(item.GetProtocol()),
			Port:      item.GetPort(),
			Source:    item.GetSource(),
			Comment:   item.GetComment(),
			Enabled:   item.GetEnabled(),
			Protected: item.GetProtected(),
			ReadOnly:  item.GetReadOnly(),
		})
	}
	return rules
}

func ruleInputProto(in RuleInput) *agentv1.FirewallRule {
	return &agentv1.FirewallRule{
		Action:    actionValue(in.Action),
		Direction: directionValue(in.Direction),
		Protocol:  protocolValue(in.Protocol),
		Port:      in.Port,
		Source:    in.Source,
		Comment:   in.Comment,
		Enabled:   in.Enabled,
	}
}

func backendSlug(v agentv1.FirewallBackend) string {
	switch v {
	case agentv1.FirewallBackend_FIREWALL_BACKEND_UFW:
		return BackendUFW
	case agentv1.FirewallBackend_FIREWALL_BACKEND_FIREWALLD:
		return BackendFirewalld
	case agentv1.FirewallBackend_FIREWALL_BACKEND_NFTABLES:
		return BackendNftables
	case agentv1.FirewallBackend_FIREWALL_BACKEND_IPTABLES:
		return BackendIptables
	default:
		return BackendNone
	}
}

func actionSlug(v agentv1.FirewallAction) string {
	switch v {
	case agentv1.FirewallAction_FIREWALL_ACTION_ALLOW:
		return ActionAllow
	case agentv1.FirewallAction_FIREWALL_ACTION_DENY:
		return ActionDeny
	default:
		return ""
	}
}

func actionValue(slug string) agentv1.FirewallAction {
	switch slug {
	case ActionAllow:
		return agentv1.FirewallAction_FIREWALL_ACTION_ALLOW
	case ActionDeny:
		return agentv1.FirewallAction_FIREWALL_ACTION_DENY
	default:
		return agentv1.FirewallAction_FIREWALL_ACTION_UNSPECIFIED
	}
}

func directionSlug(v agentv1.FirewallDirection) string {
	switch v {
	case agentv1.FirewallDirection_FIREWALL_DIRECTION_IN:
		return DirectionIn
	case agentv1.FirewallDirection_FIREWALL_DIRECTION_OUT:
		return DirectionOut
	default:
		return ""
	}
}

func directionValue(slug string) agentv1.FirewallDirection {
	switch slug {
	case DirectionIn:
		return agentv1.FirewallDirection_FIREWALL_DIRECTION_IN
	case DirectionOut:
		return agentv1.FirewallDirection_FIREWALL_DIRECTION_OUT
	default:
		return agentv1.FirewallDirection_FIREWALL_DIRECTION_UNSPECIFIED
	}
}

func protocolSlug(v agentv1.FirewallProtocol) string {
	switch v {
	case agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP:
		return ProtocolTCP
	case agentv1.FirewallProtocol_FIREWALL_PROTOCOL_UDP:
		return ProtocolUDP
	case agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY:
		return ProtocolAny
	default:
		return ""
	}
}

func protocolValue(slug string) agentv1.FirewallProtocol {
	switch slug {
	case ProtocolTCP:
		return agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP
	case ProtocolUDP:
		return agentv1.FirewallProtocol_FIREWALL_PROTOCOL_UDP
	case ProtocolAny:
		return agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY
	default:
		return agentv1.FirewallProtocol_FIREWALL_PROTOCOL_UNSPECIFIED
	}
}
