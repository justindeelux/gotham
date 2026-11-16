package deploy

type Kind string

const (
	KindDeploy   Kind = "deploy"
	KindRollback Kind = "rollback"
)

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

func (s State) Terminal() bool { return s == StateRunning || s == StateFailed }

var transitions = map[State][]State{
	StateQueued:   {StateCloning, StatePushing, StateFailed},
	StateCloning:  {StateBuilding, StateFailed},
	StateBuilding: {StatePushing, StateFailed},
	StatePushing:  {StateStarting, StateFailed},
	StateStarting: {StateRunning, StateFailed},
}

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

func stepsFor(kind Kind) []State {
	return stepsForApp(kind, Application{})
}

func stepsForApp(kind Kind, app Application) []State {
	if isComposeApp(app) {
		return []State{StateCloning, StateBuilding, StatePushing, StateStarting}
	}
	if kind == KindRollback || NormalizeSourceType(app.SourceType, app.Provider) == SourceImage {
		return []State{StatePushing, StateStarting}
	}
	return []State{StateCloning, StateBuilding, StatePushing, StateStarting}
}
