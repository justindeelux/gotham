package servers

import (
	"errors"
	"testing"
)

func TestLogOnceReport(t *testing.T) {
	errA := errors.New("updates: invalid signature")
	errB := errors.New("updates: connection refused")

	var log LogOnce
	steps := []struct {
		name string
		err  error
		want bool
	}{
		{"first failure", errA, true},
		{"same failure", errA, false},
		{"same failure again", errA, false},
		{"different failure", errB, true},
		{"success", nil, false},
		{"failure after success", errB, true},
	}
	for _, step := range steps {
		if got := log.Report(step.err); got != step.want {
			t.Fatalf("%s: Report = %v, want %v", step.name, got, step.want)
		}
	}
}
