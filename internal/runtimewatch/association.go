package runtimewatch

import (
	"sort"
	"time"

	"github.com/justindeelux/gotham/internal/services"
)

// ownedContainer applies the control plane's ownership rules to pushed
// evidence. The inventory already passed the node-assignment check, so a
// label that matches a target on this node is authoritative; unrelated
// containers (registry, proxy, another application) are excluded.
func ownedContainer(target Target, c ContainerEvidence) bool {
	if c.Role == "registry" || c.ID == "" {
		return false
	}
	switch target.ResourceType {
	case ResourceApplication:
		if target.Compose {
			return c.ComposeProject == services.ProjectName(target.ResourceID)
		}
		return c.AppID == target.ResourceID.String()
	case ResourceDatabase:
		return c.DatabaseID == target.ResourceID.String()
	default:
		return false
	}
}

// replicaKey keeps Compose replicas distinct with the stable service +
// container-number identity. A pre-upgrade agent has no replica number, so
// the container ID is the fallback evidence key instead of collapsing all
// replicas of a service into one.
func replicaKey(target Target, c ContainerEvidence) (service, replica string) {
	if target.ResourceType != ResourceApplication || !target.Compose {
		return "", ""
	}
	service = c.ComposeService
	replica = c.Replica
	if replica == "" {
		replica = c.ID
	}
	return service, replica
}

// replicaEvidence is one aggregated replica: the container that represents
// its current state plus every container the replica group holds.
type replicaEvidence struct {
	Service   string
	Replica   string
	Container ContainerEvidence
	Group     []ContainerEvidence
}

func associatedReplicas(target Target, inventory []ContainerEvidence) []replicaEvidence {
	groups := map[string]*replicaEvidence{}
	order := make([]string, 0)
	for _, c := range inventory {
		if !ownedContainer(target, c) {
			continue
		}
		service, replica := replicaKey(target, c)
		key := service + "\x00" + replica
		group, ok := groups[key]
		if !ok {
			group = &replicaEvidence{Service: service, Replica: replica}
			groups[key] = group
			order = append(order, key)
		}
		group.Group = append(group.Group, c)
		if preferContainer(c, group.Container) {
			group.Container = c
		}
	}
	sort.Strings(order)
	out := make([]replicaEvidence, 0, len(order))
	for _, key := range order {
		out = append(out, *groups[key])
	}
	return out
}

func stateRank(state string) int {
	switch state {
	case "running":
		return 0
	case "restarting":
		return 1
	case "created":
		return 2
	case "paused":
		return 3
	case "exited":
		return 4
	case "dead":
		return 5
	default:
		return 6
	}
}

func containerTime(c ContainerEvidence) time.Time {
	if c.StartedAt != nil {
		return *c.StartedAt
	}
	if c.CreatedAt != nil {
		return *c.CreatedAt
	}
	return time.Time{}
}

// preferContainer picks the container that best represents the replica now:
// a running one over a terminated sibling, then the most recently started.
func preferContainer(candidate, current ContainerEvidence) bool {
	if current.ID == "" {
		return true
	}
	candidateRank, currentRank := stateRank(candidate.State), stateRank(current.State)
	if candidateRank != currentRank {
		return candidateRank < currentRank
	}
	return containerTime(candidate).After(containerTime(current))
}

func isTerminal(state string) bool {
	switch state {
	case "exited", "dead", "removing":
		return true
	default:
		return false
	}
}

// oneShotCompleted is the supported one-shot definition: a Compose/job
// container that exited successfully under the Docker "no" restart policy.
// Exit code 137 or any other non-zero code is a failure, not a guess at OOM.
func oneShotCompleted(c ContainerEvidence) bool {
	return c.RestartPolicy == "no" && c.State == "exited" && c.ExitCode == 0
}

// oomOccurrence reports a new explicit OOM kill. The same old terminated
// container is not reported again unless it finished after the previous
// observation.
func oomOccurrence(state ReplicaState, group []ContainerEvidence) *ContainerEvidence {
	var found *ContainerEvidence
	for i := range group {
		c := group[i]
		if !c.OOMKilled || !isTerminal(c.State) {
			continue
		}
		if c.ID == state.OOMContainer {
			if c.FinishedAt == nil || !c.FinishedAt.After(state.OOMAt) {
				continue
			}
		}
		if found == nil || finishedAt(c).After(finishedAt(*found)) {
			found = &group[i]
		}
	}
	return found
}

func finishedAt(c ContainerEvidence) time.Time {
	if c.FinishedAt != nil {
		return *c.FinishedAt
	}
	return time.Time{}
}

// updateRestartWindow baselines the first observation and every recreated or
// reset container, then counts the increases seen inside the rolling window.
// lastEvent is the newest increase seen before the window is pruned, so
// recovery can wait five minutes after the last restart rather than after the
// oldest one.
func updateRestartWindow(state ReplicaState, c ContainerEvidence, now time.Time) ([]time.Time, bool, time.Time) {
	events := make([]time.Time, 0, len(state.RestartEvents))
	var lastEvent time.Time
	stale := state.ObservedAt.IsZero() || now.Sub(state.ObservedAt) > restartWindow
	if state.ContainerID != "" && state.ContainerID == c.ID && !stale && c.RestartCount >= state.RestartCount {
		for _, at := range state.RestartEvents {
			if at.After(lastEvent) {
				lastEvent = at
			}
			if now.Sub(at) <= restartWindow {
				events = append(events, at)
			}
		}
		if delta := c.RestartCount - state.RestartCount; delta > 0 {
			for i := int64(0); i < delta; i++ {
				if len(events) >= maxRestartEvents {
					break
				}
				events = append(events, now)
			}
		}
	}
	if len(events) > 0 && events[len(events)-1].After(lastEvent) {
		lastEvent = events[len(events)-1]
	}
	return events, len(events) >= 3, lastEvent
}

// restartRecoveryAllowed is true only once five minutes have passed since the
// last restart increase. The three-in-window rule opens the incident; this
// rule closes it.
func restartRecoveryAllowed(lastEvent, now time.Time) bool {
	return lastEvent.IsZero() || now.Sub(lastEvent) >= restartWindow
}

// suppressionReason says why an expected transition should hide a failure.
// The zero value means there is no active suppression.
func suppressionReason(target Target, now time.Time) string {
	settings := target.Settings
	switch {
	case settings.DesiredState == "stopped":
		return "intentionally_stopped"
	case target.ResourceType == ResourceDatabase && target.DatabaseStatus == "stopped":
		return "intentionally_stopped"
	case target.ResourceType == ResourceDatabase && target.DatabaseStatus == "creating":
		return "operation_in_progress"
	case target.ResourceType == ResourceDatabase && target.DatabaseStatus == "deleting":
		return "operation_in_progress"
	case settings.GraceUntil.After(now):
		return "startup_grace"
	}
	if target.DeploymentState != "" && !terminalDeployment(target.DeploymentState) {
		if target.DeploymentStarted == nil || now.Sub(*target.DeploymentStarted) < maxOperationSuppression {
			return "deployment_in_progress"
		}
		// A deployment that stopped moving no longer suppresses real failures.
		return ""
	}
	if target.DeploymentState == "running" && target.DeploymentFinished != nil &&
		now.Sub(*target.DeploymentFinished) < settings.Grace {
		return "startup_grace"
	}
	return ""
}

func terminalDeployment(state string) bool {
	return state == "running" || state == "failed"
}
