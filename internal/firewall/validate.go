package firewall

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

const maxCommentLength = 200

// normalizeRule validates and canonicalizes one rule. The node repeats every
// check: the control plane is a convenience, never the only guard.
func normalizeRule(in RuleInput) (RuleInput, error) {
	in.Action = strings.TrimSpace(in.Action)
	in.Direction = strings.TrimSpace(in.Direction)
	in.Protocol = strings.TrimSpace(in.Protocol)
	in.Port = strings.TrimSpace(in.Port)
	in.Source = strings.TrimSpace(in.Source)
	in.Comment = strings.TrimSpace(in.Comment)

	switch in.Action {
	case ActionAllow, ActionDeny:
	default:
		return RuleInput{}, fmt.Errorf("action must be allow or deny")
	}
	switch in.Direction {
	case DirectionIn, DirectionOut:
	default:
		return RuleInput{}, fmt.Errorf("direction must be in or out")
	}
	switch in.Protocol {
	case ProtocolTCP, ProtocolUDP, ProtocolAny:
	default:
		return RuleInput{}, fmt.Errorf("protocol must be tcp, udp or any")
	}
	port, err := normalizePort(in.Port)
	if err != nil {
		return RuleInput{}, err
	}
	in.Port = port
	source, err := normalizeSource(in.Source)
	if err != nil {
		return RuleInput{}, err
	}
	in.Source = source
	if err := validateComment(in.Comment); err != nil {
		return RuleInput{}, err
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
