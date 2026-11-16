package firewall

import (
	"regexp"
	"strings"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

var ufwRuleLine = regexp.MustCompile(`^\[\s*\d+\]\s+(.+?)\s{2,}(ALLOW|DENY|REJECT|LIMIT)\s+(IN|OUT)\s+(.+?)\s*$`)

var (
	firewalldSource = regexp.MustCompile(`source address="([^"]+)"`)
	firewalldPort   = regexp.MustCompile(`port port="([^"]+)"`)
	firewalldProto  = regexp.MustCompile(`protocol="([^"]+)"`)
)

// splitList splits the host helper's firewall-list output into its header, the
// backend's own listing and the optional second listing (ufw's added commands).
// The marker is a whole line so an empty listing (a firewalld with no rich
// rules) does not swallow the second marker.
func splitList(out string) (header, listing, extra string) {
	lines := strings.Split(out, "\n")
	first := -1
	second := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "--" {
			continue
		}
		if first < 0 {
			first = i
			continue
		}
		second = i
		break
	}
	if first < 0 {
		return out, "", ""
	}
	header = strings.Join(lines[:first], "\n")
	if second < 0 {
		return header, strings.Join(lines[first+1:], "\n"), ""
	}
	return header,
		strings.Join(lines[first+1:second], "\n"),
		strings.Join(lines[second+1:], "\n")
}

// parseUFWRules parses `ufw status numbered` and attaches the comments visible
// in `ufw show added`. IPv4 and IPv6 rows of one rule are collapsed.
func parseUFWRules(listing, added string) []rule {
	comments := parseUFWAdded(added)
	rules := make([]rule, 0)
	seen := make(map[string]bool)
	for _, line := range strings.Split(listing, "\n") {
		match := ufwRuleLine.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		target := strings.TrimSpace(match[1])
		actionText := match[2]
		directionText := match[3]
		sourceText := strings.TrimSpace(match[4])
		action := agentv1.FirewallAction_FIREWALL_ACTION_ALLOW
		if actionText == "DENY" || actionText == "REJECT" {
			action = agentv1.FirewallAction_FIREWALL_ACTION_DENY
		}
		direction := agentv1.FirewallDirection_FIREWALL_DIRECTION_IN
		if directionText == "OUT" {
			direction = agentv1.FirewallDirection_FIREWALL_DIRECTION_OUT
		}
		port, protocol, readOnly := parseUFWTarget(target)
		source, sourceOK := parseUFWSource(sourceText)
		if !sourceOK {
			readOnly = true
		}
		readOnly = readOnly || actionText == "LIMIT" || actionText == "REJECT"
		r := rule{
			Action:    action,
			Direction: direction,
			Protocol:  protocol,
			Port:      port,
			Source:    source,
			Enabled:   true,
			ReadOnly:  readOnly,
		}
		var key string
		if readOnly {
			// A read-only row is keyed by the columns ufw printed, so an app
			// profile, an interface row and a LIMIT/REJECT sibling stay
			// distinct and only the IPv4/IPv6 twin collapses.
			rawKey := ufwRowKey(target, actionText, directionText, sourceText)
			r.assignReadOnlyID(rawKey)
			key = "ro|" + rawKey
			r.Comment = ufwReadOnlyComment(target, sourceText)
		} else {
			r.assignID()
			key = "rw|" + fingerprint(r)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if !readOnly {
			r.Comment = comments[fingerprint(r)]
		}
		rules = append(rules, r)
	}
	return rules
}

// ufwRowKey identifies a read-only ufw row by its printed columns. The v6
// suffix is stripped so one rule's IPv4 and IPv6 rows collapse.
func ufwRowKey(target, action, direction, source string) string {
	clean := func(value string) string {
		return strings.TrimSuffix(strings.TrimSpace(value), " (v6)")
	}
	return strings.Join([]string{
		clean(target), action, direction, clean(source),
	}, "|")
}

// ufwReadOnlyComment keeps the text the shared model cannot store, so two app
// profiles or a source-scoped rule remain recognisable in the UI.
func ufwReadOnlyComment(target, source string) string {
	clean := func(value string) string {
		return strings.TrimSuffix(strings.TrimSpace(value), " (v6)")
	}
	text := clean(target)
	if source = clean(source); source != "" && source != "Anywhere" {
		text += " from " + source
	}
	if text == "" {
		text = "managed on the node"
	}
	return truncate("ufw: "+text, maxCommentLength)
}

func parseUFWTarget(raw string) (port string, protocol agentv1.FirewallProtocol, readOnly bool) {
	target := strings.TrimSuffix(strings.TrimSpace(raw), " (v6)")
	protocol = agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY
	if target == "" || target == "Anywhere" || target == "any" {
		return "", protocol, false
	}
	if strings.Contains(target, " on ") || strings.Contains(target, ",") {
		return "", agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY, true
	}
	portText := target
	if slash := strings.LastIndex(target, "/"); slash >= 0 {
		protoText := strings.ToLower(target[slash+1:])
		portText = target[:slash]
		switch protoText {
		case "tcp":
			protocol = agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP
		case "udp":
			protocol = agentv1.FirewallProtocol_FIREWALL_PROTOCOL_UDP
		default:
			return portText, agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY, true
		}
	}
	portText = strings.ReplaceAll(portText, " ", "")
	if normalized, err := normalizePort(strings.ReplaceAll(portText, ":", "-")); err == nil {
		return normalized, protocol, false
	}
	// An application profile ("OpenSSH", "Nginx Full") is not a port rule.
	return "", agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY, true
}

func parseUFWSource(raw string) (string, bool) {
	source := strings.TrimSuffix(strings.TrimSpace(raw), " (v6)")
	if source == "" || source == "Anywhere" || source == "any" {
		return "", true
	}
	normalized, err := normalizeSource(source)
	if err != nil {
		return "", false
	}
	return normalized, true
}

// parseUFWAdded turns the commands in `ufw show added` into comments keyed by
// rule fingerprint. Commands with an application profile or an interface are
// skipped: they are listed read-only and never get an editable comment.
func parseUFWAdded(extra string) map[string]string {
	comments := make(map[string]string)
	for _, line := range strings.Split(extra, "\n") {
		tokens := splitQuoted(strings.TrimSpace(line))
		if len(tokens) < 3 || tokens[0] != "ufw" {
			continue
		}
		r, ok := ruleFromUFWTokens(tokens[1:])
		if !ok || r.Comment == "" {
			continue
		}
		comments[fingerprint(r)] = r.Comment
	}
	return comments
}

func ruleFromUFWTokens(tokens []string) (rule, bool) {
	r := rule{
		Action:    actionValue(tokens[0]),
		Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN,
		Protocol:  agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY,
		Enabled:   true,
	}
	if r.Action == agentv1.FirewallAction_FIREWALL_ACTION_UNSPECIFIED {
		return rule{}, false
	}
	i := 1
	for i < len(tokens) {
		switch tokens[i] {
		case "in":
			i++
		case "out":
			r.Direction = agentv1.FirewallDirection_FIREWALL_DIRECTION_OUT
			i++
		case "on", "log", "log-all", "app":
			return rule{}, false
		case "proto":
			if i+1 >= len(tokens) {
				return rule{}, false
			}
			r.Protocol = protocolValue(tokens[i+1])
			if r.Protocol == agentv1.FirewallProtocol_FIREWALL_PROTOCOL_UNSPECIFIED {
				return rule{}, false
			}
			i += 2
		case "from":
			if i+1 >= len(tokens) {
				return rule{}, false
			}
			source, err := normalizeSource(tokens[i+1])
			if err != nil {
				return rule{}, false
			}
			r.Source = source
			i += 2
		case "to":
			i++
			if i < len(tokens) && tokens[i] == "any" {
				i++
			} else if i < len(tokens) {
				// A destination address is not part of the shared model.
				return rule{}, false
			}
			if i < len(tokens) && tokens[i] == "port" {
				if i+1 >= len(tokens) {
					return rule{}, false
				}
				port, err := normalizePort(strings.ReplaceAll(tokens[i+1], ":", "-"))
				if err != nil {
					return rule{}, false
				}
				r.Port = port
				i += 2
			}
		case "comment":
			if i+1 >= len(tokens) {
				return rule{}, false
			}
			r.Comment = tokens[i+1]
			i += 2
		default:
			port, protocol, readOnly := parseUFWTarget(tokens[i])
			if readOnly || r.Port != "" {
				return rule{}, false
			}
			r.Port = port
			r.Protocol = protocol
			i++
		}
	}
	if err := validateComment(r.Comment); err != nil {
		return rule{}, false
	}
	return r, true
}

func parseFirewalldRules(listing string) []rule {
	rules := make([]rule, 0)
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		r := rule{
			Action:    agentv1.FirewallAction_FIREWALL_ACTION_ALLOW,
			Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN,
			Protocol:  agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY,
			Enabled:   true,
			raw:       line,
		}
		readOnly := !strings.HasPrefix(line, "rule ")
		switch {
		case strings.HasSuffix(line, " reject"), strings.HasSuffix(line, " drop"):
			r.Action = agentv1.FirewallAction_FIREWALL_ACTION_DENY
		case strings.HasSuffix(line, " accept"):
		default:
			readOnly = true
		}
		switch {
		case strings.Contains(line, "forward-port"), strings.Contains(line, "service name="),
			strings.Contains(line, "icmp-block"), strings.Contains(line, "icmp-type"),
			strings.Contains(line, "masquerade"), strings.Contains(line, " source-port"),
			strings.Contains(line, " log"), strings.Contains(line, " audit"),
			strings.Contains(line, " destination "), strings.Contains(line, " limit "):
			readOnly = true
		}
		if match := firewalldSource.FindStringSubmatch(line); match != nil {
			source, err := normalizeSource(match[1])
			if err != nil {
				readOnly = true
			} else {
				r.Source = source
			}
		}
		if match := firewalldPort.FindStringSubmatch(line); match != nil {
			port, err := normalizePort(match[1])
			if err != nil {
				readOnly = true
			} else {
				r.Port = port
			}
		}
		if match := firewalldProto.FindStringSubmatch(line); match != nil {
			r.Protocol = protocolValue(strings.ToLower(match[1]))
			if r.Protocol == agentv1.FirewallProtocol_FIREWALL_PROTOCOL_UNSPECIFIED {
				readOnly = true
			}
		}
		if r.Port != "" && r.Protocol == agentv1.FirewallProtocol_FIREWALL_PROTOCOL_ANY {
			readOnly = true
		}
		r.ReadOnly = readOnly
		if readOnly {
			r.Comment = truncate(line, maxCommentLength)
		}
		r.assignID()
		rules = append(rules, r)
	}
	return rules
}

// splitQuoted splits a command line on spaces while keeping single- or
// double-quoted runs (a comment) as one token. Backslash escaping is not used
// by `ufw show added`.
func splitQuoted(line string) []string {
	tokens := make([]string, 0, 8)
	var current strings.Builder
	var quote rune
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return tokens
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
