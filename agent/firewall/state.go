package firewall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// savedRule is a disabled rule: the backend has no per-rule enable switch, so
// a rule the operator turns off is kept here until it is enabled again.
type savedRule struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Direction string `json:"direction"`
	Protocol  string `json:"protocol"`
	Port      string `json:"port"`
	Source    string `json:"source"`
	Comment   string `json:"comment"`
}

// savedState is the agent-local metadata the backend cannot report: disabled
// rules and the optional comment of an applied rule.
type savedState struct {
	Disabled []savedRule       `json:"disabled"`
	Comments map[string]string `json:"comments"`
}

func loadState(path string) (savedState, error) {
	state := savedState{Comments: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return state, nil
		}
		return state, fmt.Errorf("read firewall state: %w", err)
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return savedState{Comments: map[string]string{}}, fmt.Errorf("parse firewall state: %w", err)
	}
	if state.Comments == nil {
		state.Comments = map[string]string{}
	}
	return state, nil
}

func saveState(path string, state savedState) error {
	if state.Comments == nil {
		state.Comments = map[string]string{}
	}
	if state.Disabled == nil {
		state.Disabled = []savedRule{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode firewall state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("prepare firewall state directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".firewall-state-*")
	if err != nil {
		return fmt.Errorf("create firewall state: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect firewall state: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write firewall state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close firewall state: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace firewall state: %w", err)
	}
	return nil
}

func disabledRuleFromSaved(s savedRule) rule {
	return rule{
		ID:        s.ID,
		Action:    actionValue(s.Action),
		Direction: directionValue(s.Direction),
		Protocol:  protocolValue(s.Protocol),
		Port:      s.Port,
		Source:    s.Source,
		Comment:   s.Comment,
		Enabled:   false,
	}
}

func savedFromDisabled(r rule) savedRule {
	return savedRule{
		ID:        r.ID,
		Action:    actionSlug(r.Action),
		Direction: directionSlug(r.Direction),
		Protocol:  protocolSlug(r.Protocol),
		Port:      r.Port,
		Source:    r.Source,
		Comment:   r.Comment,
	}
}
