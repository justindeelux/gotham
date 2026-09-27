package deploy

import "testing"

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name string
		from State
		to   State
		want bool
	}{
		{"deploy starts", StateQueued, StateCloning, true},
		{"rollback skips straight to push", StateQueued, StatePushing, true},
		{"happy path steps", StateCloning, StateBuilding, true},
		{"build to push", StateBuilding, StatePushing, true},
		{"push to start", StatePushing, StateStarting, true},
		{"start to running", StateStarting, StateRunning, true},
		{"any state may fail", StateBuilding, StateFailed, true},
		{"queued may fail", StateQueued, StateFailed, true},
		{"running is terminal", StateRunning, StateFailed, false},
		{"failed is terminal", StateFailed, StateRunning, false},
		{"no self transition", StateBuilding, StateBuilding, false},
		{"cannot skip clone", StateQueued, StateBuilding, false},
		{"cannot skip start", StatePushing, StateRunning, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanTransition(tt.from, tt.to); got != tt.want {
				t.Errorf("CanTransition(%s → %s) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestStateTerminal(t *testing.T) {
	terminal := map[State]bool{
		StateQueued:   false,
		StateCloning:  false,
		StateBuilding: false,
		StatePushing:  false,
		StateStarting: false,
		StateRunning:  true,
		StateFailed:   true,
	}
	for state, want := range terminal {
		if got := state.Terminal(); got != want {
			t.Errorf("%s.Terminal() = %v, want %v", state, got, want)
		}
	}
}

func TestStepsFor(t *testing.T) {
	deploy := stepsFor(KindDeploy)
	wantDeploy := []State{StateCloning, StateBuilding, StatePushing, StateStarting}
	if len(deploy) != len(wantDeploy) {
		t.Fatalf("deploy steps = %v, want %v", deploy, wantDeploy)
	}
	for i, state := range wantDeploy {
		if deploy[i] != state {
			t.Errorf("deploy step %d = %s, want %s", i, deploy[i], state)
		}
	}

	rollback := stepsFor(KindRollback)
	wantRollback := []State{StatePushing, StateStarting}
	if len(rollback) != len(wantRollback) {
		t.Fatalf("rollback steps = %v, want %v", rollback, wantRollback)
	}
	for i, state := range wantRollback {
		if rollback[i] != state {
			t.Errorf("rollback step %d = %s, want %s", i, rollback[i], state)
		}
	}

	if got := firstStep(KindRollback); got != StatePushing {
		t.Errorf("firstStep(rollback) = %s, want pushing", got)
	}
	if got := firstStep(KindDeploy); got != StateCloning {
		t.Errorf("firstStep(deploy) = %s, want cloning", got)
	}
}
