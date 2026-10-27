// Package monitoring turns accepted node observations into host resource
// pressure incidents. The incident lifecycle belongs to internal/incidents;
// this package only decides thresholds, identities and filesystem history.
package monitoring

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	DetectorCPU        = "host_cpu"
	DetectorMemory     = "host_memory"
	DetectorSpace      = "filesystem_space"
	DetectorInodes     = "filesystem_inode"
	DetectorPrefix     = "host_"
	ResourceNode       = "node"
	ResourceFilesystem = "filesystem"

	minDurationSeconds = 10
	maxDurationSeconds = 3600
)

var (
	ErrValidation = errors.New("monitoring: validation")
	ErrNotFound   = errors.New("monitoring: not found")
)

// Rule is a sustained percentage threshold with independent trigger and clear
// values. Values are percentages, never fractions.
type Rule struct {
	Enabled        bool    `json:"enabled"`
	Warning        float64 `json:"warning"`
	Clear          float64 `json:"clear"`
	ConfirmSeconds int     `json:"confirm_seconds"`
	ClearSeconds   int     `json:"clear_seconds"`
}

// FilesystemRule adds the critical and downgrade bands used by space and inode
// pressure.
type FilesystemRule struct {
	Enabled          bool    `json:"enabled"`
	Warning          float64 `json:"warning"`
	Critical         float64 `json:"critical"`
	Downgrade        float64 `json:"downgrade"`
	Clear            float64 `json:"clear"`
	ConfirmSeconds   int     `json:"confirm_seconds"`
	CriticalSeconds  int     `json:"critical_seconds"`
	DowngradeSeconds int     `json:"downgrade_seconds"`
	ClearSeconds     int     `json:"clear_seconds"`
}

type Rules struct {
	CPU    Rule           `json:"cpu"`
	Memory Rule           `json:"memory"`
	Space  FilesystemRule `json:"space"`
	Inode  FilesystemRule `json:"inode"`
}

// Defaults are the product defaults, not copied from another monitoring tool.
func Defaults() Rules {
	return Rules{
		CPU:    Rule{Enabled: true, Warning: 90, Clear: 85, ConfirmSeconds: 300, ClearSeconds: 60},
		Memory: Rule{Enabled: true, Warning: 90, Clear: 85, ConfirmSeconds: 300, ClearSeconds: 60},
		Space: FilesystemRule{
			Enabled: true, Warning: 90, Critical: 95, Downgrade: 90, Clear: 85,
			ConfirmSeconds: 60, CriticalSeconds: 60, DowngradeSeconds: 60, ClearSeconds: 60,
		},
		Inode: FilesystemRule{
			Enabled: true, Warning: 90, Critical: 95, Downgrade: 90, Clear: 85,
			ConfirmSeconds: 60, CriticalSeconds: 60, DowngradeSeconds: 60, ClearSeconds: 60,
		},
	}
}

func (r Rules) Validate() error {
	if err := r.CPU.validate("cpu"); err != nil {
		return err
	}
	if err := r.Memory.validate("memory"); err != nil {
		return err
	}
	if err := r.Space.validate("space"); err != nil {
		return err
	}
	return r.Inode.validate("inode")
}

func (r Rule) validate(name string) error {
	if err := finitePercent(name+".warning", r.Warning); err != nil {
		return err
	}
	if err := finitePercent(name+".clear", r.Clear); err != nil {
		return err
	}
	if r.Clear >= r.Warning {
		return fmt.Errorf("%w: %s.clear must be below %s.warning", ErrValidation, name, name)
	}
	if err := durationBounds(name+".confirm_seconds", r.ConfirmSeconds); err != nil {
		return err
	}
	return durationBounds(name+".clear_seconds", r.ClearSeconds)
}

func (r FilesystemRule) validate(name string) error {
	if err := finitePercent(name+".warning", r.Warning); err != nil {
		return err
	}
	if err := finitePercent(name+".critical", r.Critical); err != nil {
		return err
	}
	if err := finitePercent(name+".downgrade", r.Downgrade); err != nil {
		return err
	}
	if err := finitePercent(name+".clear", r.Clear); err != nil {
		return err
	}
	if !(r.Clear < r.Downgrade && r.Downgrade <= r.Warning && r.Warning < r.Critical) {
		return fmt.Errorf("%w: %s must satisfy clear < downgrade <= warning < critical", ErrValidation, name)
	}
	if err := durationBounds(name+".confirm_seconds", r.ConfirmSeconds); err != nil {
		return err
	}
	if err := durationBounds(name+".critical_seconds", r.CriticalSeconds); err != nil {
		return err
	}
	if err := durationBounds(name+".downgrade_seconds", r.DowngradeSeconds); err != nil {
		return err
	}
	return durationBounds(name+".clear_seconds", r.ClearSeconds)
}

func finitePercent(name string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > 100 {
		return fmt.Errorf("%w: %s must be a percentage above 0 and at most 100", ErrValidation, name)
	}
	return nil
}

func durationBounds(name string, value int) error {
	if value < minDurationSeconds || value > maxDurationSeconds {
		return fmt.Errorf("%w: %s must be between %d and %d seconds", ErrValidation, name, minDurationSeconds, maxDurationSeconds)
	}
	return nil
}

func decodeRules(raw []byte) (Rules, error) {
	var rules Rules
	if err := json.Unmarshal(raw, &rules); err != nil {
		return Rules{}, fmt.Errorf("monitoring: decode policy: %w", err)
	}
	if err := rules.Validate(); err != nil {
		return Rules{}, err
	}
	return rules, nil
}

// timings converts validated seconds to the durations the incident lifecycle
// expects.
func (r Rule) timings() (confirm, recover time.Duration) {
	return time.Duration(r.ConfirmSeconds) * time.Second, time.Duration(r.ClearSeconds) * time.Second
}

func (r FilesystemRule) timings() (confirm, critical, downgrade, recover time.Duration) {
	return time.Duration(r.ConfirmSeconds) * time.Second,
		time.Duration(r.CriticalSeconds) * time.Second,
		time.Duration(r.DowngradeSeconds) * time.Second,
		time.Duration(r.ClearSeconds) * time.Second
}
