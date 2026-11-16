package firewall

// Status is one node's firewall state.
type Status struct {
	Backend          string `json:"backend"`
	Available        bool   `json:"available"`
	Enabled          bool   `json:"enabled"`
	Manageable       bool   `json:"manageable"`
	Detail           string `json:"detail,omitempty"`
	SSHPort          uint32 `json:"ssh_port"`
	AgentPort        uint32 `json:"agent_port"`
	ControlPlanePort uint32 `json:"control_plane_port"`
}

// Rule is one firewall rule in the shared model. Enabled reports whether the
// backend currently applies it; Protected marks a rule that keeps the
// operator's access open.
type Rule struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Direction string `json:"direction"`
	Protocol  string `json:"protocol"`
	Port      string `json:"port"`
	Source    string `json:"source"`
	Comment   string `json:"comment"`
	Enabled   bool   `json:"enabled"`
	Protected bool   `json:"protected"`
	ReadOnly  bool   `json:"read_only"`
}

// RuleInput is the editable part of a rule.
type RuleInput struct {
	Action    string `json:"action"`
	Direction string `json:"direction"`
	Protocol  string `json:"protocol"`
	Port      string `json:"port"`
	Source    string `json:"source"`
	Comment   string `json:"comment"`
	Enabled   bool   `json:"enabled"`
}

// State is the answer to a toggle: the new status and the resulting rules,
// including the access rules the node added before enabling.
type State struct {
	Status Status `json:"status"`
	Rules  []Rule `json:"rules"`
}
