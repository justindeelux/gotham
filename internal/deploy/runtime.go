package deploy

import (
	"context"
	"encoding/hex"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/services"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// RuntimeContainer contains only ownership-checked runtime evidence, never node configuration.
type RuntimeContainer struct {
	ID           string     `json:"id"`
	Service      string     `json:"service"`
	Replica      string     `json:"replica"`
	State        string     `json:"state"`
	Health       string     `json:"health"`
	DeploymentID *uuid.UUID `json:"deployment_id"`
}

// ApplicationRuntime is a fresh inventory observation, independent of pipeline success.
type ApplicationRuntime struct {
	ApplicationID uuid.UUID          `json:"application_id"`
	ObservedAt    *time.Time         `json:"observed_at"`
	Coverage      string             `json:"coverage"`
	Reason        string             `json:"reason,omitempty"`
	Containers    []RuntimeContainer `json:"containers"`
}

// selectRuntimeContainers is shared with finite log reads: full IDs and exact ownership only.
func selectRuntimeContainers(app Application, inventory []*agentv1.ContainerInfo) ([]*agentv1.ContainerInfo, bool) {
	selected := make([]*agentv1.ContainerInfo, 0, len(inventory))
	limited := false
	seen := map[string]bool{}
	for _, c := range inventory {
		id := c.GetId()
		decoded, err := hex.DecodeString(id)
		owned := c.GetLabels()[labelAppID] == app.ID.String()
		if isComposeApp(app) {
			owned = c.GetLabels()[composeProjectLabel] == services.ProjectName(app.ID)
		}
		if !owned || err != nil || len(decoded) != 32 {
			limited = true
			continue
		}
		if !seen[id] {
			selected = append(selected, c)
			seen[id] = true
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].GetId() < selected[j].GetId() })
	return selected, limited
}

// GetApplicationRuntime requires ordinary application read access, including read-only members.
func (s *Service) GetApplicationRuntime(ctx context.Context, userID, appID uuid.UUID) (ApplicationRuntime, error) {
	if !Enabled() {
		return ApplicationRuntime{}, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return ApplicationRuntime{}, err
	}
	result := ApplicationRuntime{ApplicationID: app.ID, Coverage: "unavailable", Containers: []RuntimeContainer{}}
	if app.ServerID == uuid.Nil {
		result.Reason = "no_node_assigned"
		return result, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, s.controlTimeout)
	defer cancel()
	node, err := s.dialNode(callCtx, app.ServerID)
	if err != nil {
		result.Reason = "agent_unavailable"
		return result, ctx.Err()
	}
	defer func() { _ = node.Close() }()
	labels := map[string]string{labelAppID: app.ID.String()}
	if isComposeApp(app) {
		labels = map[string]string{composeProjectLabel: services.ProjectName(app.ID)}
	}
	inventory, err := node.ContainersByLabel(callCtx, labels, true, false)
	if err != nil {
		result.Reason = "inventory_unavailable"
		return result, ctx.Err()
	}
	now := time.Now().UTC()
	result.ObservedAt = &now
	current, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return ApplicationRuntime{}, err
	}
	if current.ServerID != app.ServerID || current.EnvironmentID != app.EnvironmentID || current.TeamID != app.TeamID || isComposeApp(current) != isComposeApp(app) {
		return ApplicationRuntime{}, ErrConflict
	}
	selected, limited := selectRuntimeContainers(app, inventory)
	result.Coverage = "complete"
	if len(selected) > 100 {
		selected = selected[:100]
		limited = true
	}
	if limited {
		result.Coverage = "limited"
		result.Reason = "inventory_evidence_incomplete_or_truncated"
	}
	// Bound optional release enrichment; unqueried associations remain unknown.
	associations := map[uuid.UUID]*Deployment{}
	for _, c := range selected {
		row := RuntimeContainer{ID: c.GetId(), State: c.GetState(), Health: c.GetHealth()}
		if row.State == "" {
			row.State = "unknown"
		}
		if row.Health == "" {
			row.Health = "unknown"
		}
		if isComposeApp(app) {
			row.Service = c.GetLabels()[composeServiceLabel]
			row.Replica = c.GetLabels()["com.docker.compose.container-number"]
		} else if id, parseErr := uuid.Parse(c.GetLabels()[labelDeploymentID]); parseErr == nil && id != uuid.Nil {
			dep, checked := associations[id]
			if !checked && len(associations) < 4 {
				associations[id] = nil
				if found, depErr := s.repo.GetDeployment(ctx, app.ID, id); depErr == nil {
					dep = &found
					associations[id] = dep
				}
			}
			if dep != nil && (dep.ContainerID == "" || dep.ContainerID == c.GetId()) {
				row.DeploymentID = &id
			}
		}
		result.Containers = append(result.Containers, row)
	}
	return result, nil
}

func (h *handler) getRuntime(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	result, err := h.svc.GetApplicationRuntime(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
