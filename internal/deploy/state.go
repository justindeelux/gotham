package deploy

// Kind discriminates deployment intent.
type Kind string

const (
	// KindDeploy is a normal deployment of the current revision.
	KindDeploy Kind = "deploy"
	// KindRollback re-runs a previous deployment's revision.
	KindRollback Kind = "rollback"
)

// State is the deploy orchestration state machine:
//
//	queued → cloning → building → pushing → starting → running
//	                                                    ↘ failed
//
// A rollback reuses an image that was already built, so it skips cloning and
// building:
//
//	queued → pushing → starting → running
//
// Any non-terminal state may go to failed. Re-queued implies a prior failure.
type State string

const (
	StateQueued   State = "queued"
	StateCloning  State = "cloning"
	StateBuilding State = "building"
	StatePushing  State = "pushing"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateFailed   State = "failed"
)

// Terminal reports whether the state ends the deployment lifecycle.
func (s State) Terminal() bool { return s == StateRunning || s == StateFailed }

// transitions is the allowed state machine edge set. StateQueued carries both
// deploy (cloning) and rollback (pushing) successors; stepsFor(kind) narrows
// the walk to the single legal path for a run.
var transitions = map[State][]State{
	StateQueued:   {StateCloning, StatePushing, StateFailed},
	StateCloning:  {StateBuilding, StateFailed},
	StateBuilding: {StatePushing, StateFailed},
	StatePushing:  {StateStarting, StateFailed},
	StateStarting: {StateRunning, StateFailed},
}

// CanTransition reports whether from → to is a legal state machine edge.
func CanTransition(from, to State) bool {
	if from == to {
		return false
	}
	if to == StateFailed {
		return !from.Terminal()
	}
	allowed, ok := transitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// stepsFor returns the ordered non-terminal states for a deployment run of
// the given kind, ending just before running (the caller transitions to
// running on success). KindRollback skips cloning and building because it
// redeploys an image an earlier deployment already pushed.
func stepsFor(kind Kind) []State {
	return stepsForApp(kind, "", "")
}

// stepsForApp narrows the walk to the single legal path for a run. Image
// sources (GS-9) have no repository to clone and nothing to build, so their
// deploys walk the rollback path (pushing, where the pull happens, then
// starting); queued → pushing is a legal edge.
func stepsForApp(kind Kind, sourceType, provider string) []State {
	if kind == KindRollback || NormalizeSourceType(sourceType, provider) == SourceImage {
		return []State{StatePushing, StateStarting}
	}
	return []State{StateCloning, StateBuilding, StatePushing, StateStarting}
}
