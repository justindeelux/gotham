package deploy

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const pendingClearTimeout = 10 * time.Second

// markPendingChanges records the categories a new container must pick up. The
// first change after a successful deploy is the clear-to-set transition that
// emits exactly one redeploy notification; later changes only widen the set.
func (s *Service) markPendingChanges(ctx context.Context, app Application, changes ...string) {
	if s == nil || s.repo == nil || len(changes) == 0 {
		return
	}
	if !s.hasRunningDeployment(ctx, app.ID) {
		return
	}
	becamePending, err := s.repo.AddPendingChanges(ctx, app.ID, changes)
	if err != nil {
		s.logger.Warn("deploy: could not mark pending changes",
			"application_id", app.ID, "changes", changes, "error", err)
		return
	}
	if !becamePending || s.notifier == nil {
		return
	}
	notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notifyTimeout)
	defer cancel()
	s.notifier.RedeployNeeded(notifyCtx, RedeployResult{
		ApplicationID: app.ID,
		Application:   app.Name,
		Changes:       SortPendingChanges(changes),
		At:            time.Now().UTC(),
	})
}

// hasRunningDeployment reports whether a container already exists, so a change
// made before the first deploy does not claim a redeploy is needed.
func (s *Service) hasRunningDeployment(ctx context.Context, appID uuid.UUID) bool {
	return s.latestContainer(ctx, appID) != ""
}

// clearPendingChanges runs after a successful deploy. A failed clear leaves the
// banner in place rather than falsely claiming the setting is live.
func (o *Orchestrator) clearPendingChanges(ctx context.Context, appID uuid.UUID) {
	if o == nil || o.repo == nil || appID == uuid.Nil {
		return
	}
	clearCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pendingClearTimeout)
	defer cancel()
	if err := o.repo.ClearPendingChanges(clearCtx, appID); err != nil {
		o.logger.Warn("deploy: could not clear pending changes",
			"application_id", appID, "error", err)
	}
}

func envPendingChanges(beforeVars []EnvVar, beforeSecrets []Secret, afterVars []EnvVar, afterSecrets []Secret) []string {
	changes := make([]string, 0, 2)
	if !equalStringMap(envVarValues(beforeVars), envVarValues(afterVars)) {
		changes = append(changes, PendingChangeEnvironment)
	}
	if !equalStringMap(secretValues(beforeSecrets), secretValues(afterSecrets)) {
		changes = append(changes, PendingChangeSecrets)
	}
	return changes
}

func envVarValues(vars []EnvVar) map[string]string {
	values := make(map[string]string, len(vars))
	for _, v := range vars {
		if v.Key != "" {
			values[v.Key] = v.Value
		}
	}
	return values
}

func secretValues(secrets []Secret) map[string]string {
	values := make(map[string]string, len(secrets))
	for _, s := range secrets {
		if s.Key != "" {
			values[s.Key] = s.Ciphertext
		}
	}
	return values
}

func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func storagesChanged(before, after []Storage) bool {
	if len(before) != len(after) {
		return true
	}
	byName := make(map[string]Storage, len(before))
	for _, storage := range before {
		byName[storage.Name] = storage
	}
	for _, storage := range after {
		previous, ok := byName[storage.Name]
		if !ok || previous.HostPath != storage.HostPath || previous.ContainerPath != storage.ContainerPath {
			return true
		}
	}
	return false
}
