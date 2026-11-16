package firewall

import (
	"strings"
	"testing"
)

func TestNormalizeRuleAcceptsBothDirectionsAndShapes(t *testing.T) {
	cases := []RuleInput{
		{Action: ActionAllow, Direction: DirectionIn, Protocol: ProtocolTCP, Port: "22", Enabled: true},
		{Action: ActionDeny, Direction: DirectionOut, Protocol: ProtocolUDP, Port: "8000-8100", Enabled: true},
		{Action: ActionAllow, Direction: DirectionIn, Protocol: ProtocolAny, Source: "10.0.0.0/8", Enabled: true},
		{Action: ActionAllow, Direction: DirectionIn, Protocol: ProtocolTCP, Source: "2001:db8::1"},
	}
	for _, in := range cases {
		if _, err := normalizeRule(in); err != nil {
			t.Errorf("normalizeRule(%#v) = %v", in, err)
		}
	}
}

func TestNormalizeRuleNormalizesFields(t *testing.T) {
	out, err := normalizeRule(RuleInput{
		Action: " allow ", Direction: "in", Protocol: "tcp",
		Port: "08000-08100", Source: "10.1.2.3/8", Comment: "  web  ", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Action != ActionAllow || out.Port != "8000-8100" || out.Source != "10.0.0.0/8" || out.Comment != "web" {
		t.Fatalf("normalized = %#v", out)
	}
}

func TestNormalizeRuleRejectsBadInput(t *testing.T) {
	cases := map[string]RuleInput{
		"action":    {Action: "block", Direction: DirectionIn, Protocol: ProtocolTCP},
		"direction": {Action: ActionAllow, Direction: "sideways", Protocol: ProtocolTCP},
		"protocol":  {Action: ActionAllow, Direction: DirectionIn, Protocol: "icmp"},
		"port zero": {Action: ActionAllow, Direction: DirectionIn, Protocol: ProtocolTCP, Port: "0"},
		"port big":  {Action: ActionAllow, Direction: DirectionIn, Protocol: ProtocolTCP, Port: "70000"},
		"range":     {Action: ActionAllow, Direction: DirectionIn, Protocol: ProtocolTCP, Port: "9000-8000"},
		"source":    {Action: ActionAllow, Direction: DirectionIn, Protocol: ProtocolTCP, Source: "example.com"},
		"comment":   {Action: ActionAllow, Direction: DirectionIn, Protocol: ProtocolTCP, Comment: "bad;comment"},
		"newline":   {Action: ActionAllow, Direction: DirectionIn, Protocol: ProtocolTCP, Comment: "a\nb"},
		"dash":      {Action: ActionAllow, Direction: DirectionIn, Protocol: ProtocolTCP, Comment: "-ssh"},
		"outbound":  {Action: ActionAllow, Direction: DirectionOut, Protocol: ProtocolTCP, Source: "192.0.2.10"},
	}
	for name, in := range cases {
		if _, err := normalizeRule(in); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestNormalizeRuleCommentLength(t *testing.T) {
	in := RuleInput{
		Action: ActionAllow, Direction: DirectionIn, Protocol: ProtocolTCP,
		Comment: strings.Repeat("a", maxCommentLength+1),
	}
	if _, err := normalizeRule(in); err == nil {
		t.Fatal("expected a length error")
	}
	in.Comment = strings.Repeat("a", maxCommentLength)
	if _, err := normalizeRule(in); err != nil {
		t.Fatalf("max length comment = %v", err)
	}
}
