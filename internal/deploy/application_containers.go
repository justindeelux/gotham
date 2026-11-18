package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/services"
)

// ApplicationContainer is one container the application owns: its single
// container, or one service container of a compose project.
type ApplicationContainer struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Service string     `json:"service,omitempty"`
	Image   string     `json:"image"`
	State   string     `json:"state"`
	Status  string     `json:"status"`
	Created *time.Time `json:"created,omitempty"`
}

// ApplicationContainerService is the slice of containers.ContainerService the
// application containers view uses.
type ApplicationContainerService interface {
	ListFresh(ctx context.Context, serverID uuid.UUID) ([]containers.Container, error)
	Start(ctx context.Context, serverID uuid.UUID, containerID string) error
	Stop(ctx context.Context, serverID uuid.UUID, containerID string) error
	Restart(ctx context.Context, serverID uuid.UUID, containerID string) error
}

func (s *Service) ListApplicationContainers(ctx context.Context, userID, appID uuid.UUID) ([]ApplicationContainer, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	app, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return nil, err
	}
	return s.applicationContainers(ctx, app)
}

func (s *Service) StartApplicationContainer(ctx context.Context, userID, appID uuid.UUID, containerID string) error {
	return s.controlApplicationContainer(ctx, userID, appID, containerID, "start")
}

func (s *Service) StopApplicationContainer(ctx context.Context, userID, appID uuid.UUID, containerID string) error {
	return s.controlApplicationContainer(ctx, userID, appID, containerID, "stop")
}

func (s *Service) RestartApplicationContainer(ctx context.Context, userID, appID uuid.UUID, containerID string) error {
	return s.controlApplicationContainer(ctx, userID, appID, containerID, "restart")
}

func (s *Service) controlApplicationContainer(ctx context.Context, userID, appID uuid.UUID, containerID, action string) error {
	if s.containers == nil {
		return fmt.Errorf("%w: container operations are not configured", ErrAgentUnavailable)
	}
	app, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return err
	}
	if s.refuseDuringBackup(ctx, app.ID) {
		return ErrBackupInFlight
	}
	owned, err := s.applicationContainers(ctx, app)
	if err != nil {
		return err
	}
	if !containsContainer(owned, containerID) {
		return fmt.Errorf("%w: container %s is not part of this application", ErrContainerNotFound, containerID)
	}
	switch action {
	case "start":
		err = s.containers.Start(ctx, app.ServerID, containerID)
	case "stop":
		err = s.containers.Stop(ctx, app.ServerID, containerID)
	case "restart":
		err = s.containers.Restart(ctx, app.ServerID, containerID)
	default:
		return fmt.Errorf("%w: unknown container action %q", ErrValidation, action)
	}
	if err != nil {
		return mapApplicationContainerError(err)
	}
	s.syncApplicationDesiredState(ctx, app)
	return nil
}

func (s *Service) applicationContainers(ctx context.Context, app Application) ([]ApplicationContainer, error) {
	rows := []ApplicationContainer{}
	if app.ServerID == uuid.Nil {
		return rows, nil
	}
	if s.containers == nil {
		return nil, fmt.Errorf("%w: container operations are not configured", ErrAgentUnavailable)
	}
	listed, err := s.containers.ListFresh(ctx, app.ServerID)
	if err != nil {
		return nil, mapApplicationContainerError(err)
	}
	for _, container := range listed {
		if !applicationOwnsContainer(app, container) {
			continue
		}
		row := ApplicationContainer{
			ID:      container.ID,
			Name:    container.Name,
			Image:   container.Image,
			State:   container.State,
			Status:  container.Status,
			Created: container.Created,
		}
		if isComposeApp(app) {
			row.Service = container.Labels[composeServiceLabel]
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Service != rows[j].Service {
			return rows[i].Service < rows[j].Service
		}
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].ID < rows[j].ID
	})
	return rows, nil
}

func applicationOwnsContainer(app Application, container containers.Container) bool {
	if isComposeApp(app) {
		return container.Labels[composeProjectLabel] == services.ProjectName(app.ID)
	}
	return container.Labels[labelAppID] == app.ID.String()
}

func containsContainer(rows []ApplicationContainer, containerID string) bool {
	for _, row := range rows {
		if row.ID == containerID {
			return true
		}
	}
	return false
}

// syncApplicationDesiredState keeps the status badge and the persisted stop
// after a refresh consistent with a manual per-container action.
func (s *Service) syncApplicationDesiredState(ctx context.Context, app Application) {
	rows, err := s.applicationContainers(ctx, app)
	if err != nil {
		s.logger.Warn("deploy: container action left the desired state unconfirmed",
			"application_id", app.ID, "error", err)
		return
	}
	desired := "stopped"
	for _, row := range rows {
		if containerRunning(row.State) {
			desired = "running"
			break
		}
	}
	s.setDesiredState(ctx, app.ID, desired)
}

func containerRunning(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running", "restarting", "paused":
		return true
	default:
		return false
	}
}

func mapApplicationContainerError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, containers.ErrContainerNotFound):
		return fmt.Errorf("%w: %v", ErrContainerNotFound, err)
	case errors.Is(err, containers.ErrServerNotFound):
		return ErrServerNotFound
	case errors.Is(err, containers.ErrAgentUnavailable):
		return ErrAgentUnavailable
	case errors.Is(err, containers.ErrValidation):
		return fmt.Errorf("%w: %v", ErrValidation, err)
	default:
		return err
	}
}

type applicationContainerListEnvelope struct {
	Containers []ApplicationContainer `json:"containers"`
}

type applicationContainerActionEnvelope struct {
	ContainerID string `json:"container_id"`
}

func (h *handler) listApplicationContainers(w http.ResponseWriter, r *http.Request) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.ListApplicationContainers(r.Context(), userID, appID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, applicationContainerListEnvelope{Containers: rows})
}

func (h *handler) startApplicationContainer(w http.ResponseWriter, r *http.Request) {
	h.controlApplicationContainer(w, r, "start")
}

func (h *handler) stopApplicationContainer(w http.ResponseWriter, r *http.Request) {
	h.controlApplicationContainer(w, r, "stop")
}

func (h *handler) restartApplicationContainer(w http.ResponseWriter, r *http.Request) {
	h.controlApplicationContainer(w, r, "restart")
}

func (h *handler) controlApplicationContainer(w http.ResponseWriter, r *http.Request, action string) {
	userID, appID, ok := h.requestTarget(w, r)
	if !ok {
		return
	}
	containerID := chi.URLParam(r, "containerID")
	var err error
	switch action {
	case "start":
		err = h.svc.StartApplicationContainer(r.Context(), userID, appID, containerID)
	case "stop":
		err = h.svc.StopApplicationContainer(r.Context(), userID, appID, containerID)
	default:
		err = h.svc.RestartApplicationContainer(r.Context(), userID, appID, containerID)
	}
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, applicationContainerActionEnvelope{ContainerID: containerID})
}
