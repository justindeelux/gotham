package terminal

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Target discovery for the add-tab picker (JUS-265).
//
// GET /v1/terminal/targets is one authorised, bounded read of everything the
// caller may open: host shells for a platform operator and running containers
// of applications the caller may write. The source enforces both capabilities
// itself (it is wired with the server's operator check and the deploy
// resolver), so this package only shapes the response. A failure for one
// application is reported as a bounded per-source entry instead of failing the
// whole list; the browser never invents a container id from this response
// because creation still goes through the existing create endpoints.

// Bounds keep one picker read cheap even on a large installation.
const (
	maxHostTargets        = 50
	maxContainerApps      = 50
	maxContainersPerApp   = 50
	maxTargetErrors       = 50
	defaultTargetsTimeout = 8 * time.Second
)

// TargetErrorCode is the bounded, translatable reason one source is missing.
type TargetErrorCode string

const (
	// TargetErrorUnavailable means the source could not be reached (a node or
	// the application listing); retrying later can succeed.
	TargetErrorUnavailable TargetErrorCode = "unavailable"
	// TargetErrorDisabled means the feature is off on this control plane.
	TargetErrorDisabled TargetErrorCode = "disabled"
	// TargetErrorInternal is an unexpected source failure. The raw error never
	// reaches the DOM.
	TargetErrorInternal TargetErrorCode = "internal"
	// TargetErrorTimeout is one collapsed fan-out deadline/cancel. Count says
	// how many applications it covers, so a slow node never becomes dozens of
	// separate "internal" rows.
	TargetErrorTimeout TargetErrorCode = "timeout"
)

// HostTarget is one node a platform operator may open a host shell on.
type HostTarget struct {
	ID     uuid.UUID
	Name   string
	Status string
}

// ContainerTargetGroup is one application with its resolved running
// containers. It mirrors the per-application discovery response so the picker
// can show the same service/replica labels.
type ContainerTargetGroup struct {
	ApplicationID   uuid.UUID
	ApplicationName string
	ServerID        uuid.UUID
	SourceType      string
	Containers      []ContainerTarget
}

// TargetError is one bounded per-source failure. Name is display metadata
// only; the UI localizes Code and never renders a backend message.
type TargetError struct {
	Source string
	ID     string
	Name   string
	Code   TargetErrorCode
	// Count is set on a collapsed timeout entry; zero means one source.
	Count int
}

// Targets is the complete add-tab discovery result. A source failure never
// clears the other list; Errors explains what is missing.
type Targets struct {
	Servers      []HostTarget
	Applications []ContainerTargetGroup
	Errors       []TargetError
	// Truncated reports that at least one list was cut by a bound (the UI can
	// say "showing the first N").
	Truncated bool
}

// TargetsSource lists the caller's authorised targets. The implementation
// proves the operator and application-write capabilities itself; the
// transport package deliberately knows neither the servers nor the deploy
// domain.
type TargetsSource interface {
	ListTargets(ctx context.Context, userID uuid.UUID) (Targets, error)
}

// TargetsSourceFunc resolves one request's source. Building it per request
// lets the server capture the request's platform-operator decision without
// widening this package's interface.
type TargetsSourceFunc func(r *http.Request) TargetsSource

type targetHandler struct {
	source     TargetsSourceFunc
	userIDFrom UserIDFunc
	logger     *slog.Logger
	timeout    time.Duration
}

// MountTargets registers the add-tab discovery route on the /api router.
//
//	GET /v1/terminal/targets
//
// The caller passes the interactive-session chain (authentication, no API
// token, active team scope), matching the gates the existing terminal routes
// use. A nil source mounts nothing.
func MountTargets(r chi.Router, authMiddleware func(http.Handler) http.Handler, source TargetsSourceFunc, userIDFrom UserIDFunc, logger *slog.Logger) {
	mountTargets(r, authMiddleware, source, userIDFrom, logger, defaultTargetsTimeout)
}

// mountTargets is MountTargets with a test-only timeout override.
func mountTargets(r chi.Router, authMiddleware func(http.Handler) http.Handler, source TargetsSourceFunc, userIDFrom UserIDFunc, logger *slog.Logger, timeout time.Duration) {
	if source == nil || userIDFrom == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	if timeout <= 0 {
		timeout = defaultTargetsTimeout
	}
	h := &targetHandler{source: source, userIDFrom: userIDFrom, logger: logger, timeout: timeout}
	r.Group(func(protected chi.Router) {
		protected.Use(authMiddleware)
		protected.Get("/v1/terminal/targets", h.list)
	})
}

// hostTargetResponse is one node row the picker can open.
type hostTargetResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// applicationTargetsResponse is one application group of running containers.
type applicationTargetsResponse struct {
	ApplicationID   string                    `json:"application_id"`
	ApplicationName string                    `json:"application_name"`
	ServerID        string                    `json:"server_id"`
	SourceType      string                    `json:"source_type"`
	Containers      []containerTargetResponse `json:"containers"`
}

// targetErrorResponse is one bounded per-source failure.
type targetErrorResponse struct {
	Source string `json:"source"`
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Code   string `json:"code"`
	// Count is present only on a collapsed timeout entry.
	Count int `json:"count,omitempty"`
}

// targetsListResponse is the picker's single read. Every slice is non-nil so
// the contract never hands the SPA a null collection.
type targetsListResponse struct {
	Servers      []hostTargetResponse         `json:"servers"`
	Applications []applicationTargetsResponse `json:"applications"`
	Errors       []targetErrorResponse        `json:"errors"`
	Truncated    bool                         `json:"truncated"`
}

func (h *targetHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userIDFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}
	source := h.source(r)
	if source == nil {
		writeJSON(w, http.StatusOK, newTargetsListResponse(Targets{}))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	targets, err := source.ListTargets(ctx, userID)
	if err != nil {
		// The source reports per-node/application failures in Targets.Errors;
		// only a failure to answer at all reaches here, and it is not exposed.
		h.logger.Warn("terminal: list targets", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, newTargetsListResponse(targets))
}

// newTargetsListResponse bounds and shapes one discovery result.
func newTargetsListResponse(targets Targets) targetsListResponse {
	response := targetsListResponse{
		Servers:      make([]hostTargetResponse, 0, len(targets.Servers)),
		Applications: make([]applicationTargetsResponse, 0, len(targets.Applications)),
		Errors:       make([]targetErrorResponse, 0, len(targets.Errors)),
		Truncated:    targets.Truncated,
	}
	servers := targets.Servers
	if len(servers) > maxHostTargets {
		servers = servers[:maxHostTargets]
		response.Truncated = true
	}
	for _, server := range servers {
		response.Servers = append(response.Servers, hostTargetResponse{
			ID:     server.ID.String(),
			Name:   server.Name,
			Status: server.Status,
		})
	}
	applications := targets.Applications
	if len(applications) > maxContainerApps {
		applications = applications[:maxContainerApps]
		response.Truncated = true
	}
	for _, application := range applications {
		group := applicationTargetsResponse{
			ApplicationID:   application.ApplicationID.String(),
			ApplicationName: application.ApplicationName,
			ServerID:        application.ServerID.String(),
			SourceType:      application.SourceType,
			Containers:      make([]containerTargetResponse, 0, len(application.Containers)),
		}
		containers := application.Containers
		if len(containers) > maxContainersPerApp {
			containers = containers[:maxContainersPerApp]
			response.Truncated = true
		}
		for _, container := range containers {
			group.Containers = append(group.Containers, containerTargetResponse{
				Service:     container.Service,
				ContainerID: container.ContainerID,
				Name:        container.Name,
				Image:       container.Image,
				State:       container.State,
				Replica:     container.Replica,
				Label:       container.Label,
			})
		}
		response.Applications = append(response.Applications, group)
	}
	errs := targets.Errors
	if len(errs) > maxTargetErrors {
		errs = errs[:maxTargetErrors]
	}
	for _, entry := range errs {
		response.Errors = append(response.Errors, targetErrorResponse{
			Source: entry.Source,
			ID:     entry.ID,
			Name:   entry.Name,
			Code:   string(sanitizeTargetError(entry.Code)),
			Count:  entry.Count,
		})
	}
	return response
}

// SortTargets gives one deterministic order for the picker (and for tests):
// servers by name, applications by name, containers by service then name.
func SortTargets(targets *Targets) {
	sort.SliceStable(targets.Servers, func(i, j int) bool {
		if targets.Servers[i].Name == targets.Servers[j].Name {
			return targets.Servers[i].ID.String() < targets.Servers[j].ID.String()
		}
		return targets.Servers[i].Name < targets.Servers[j].Name
	})
	// Applications keep the source's deliberate order (most recently updated
	// first), after the cap has been applied to resolvable targets.
	for index := range targets.Applications {
		containers := targets.Applications[index].Containers
		sort.SliceStable(containers, func(i, j int) bool {
			if containers[i].Service != containers[j].Service {
				return containers[i].Service < containers[j].Service
			}
			if containers[i].Name != containers[j].Name {
				return containers[i].Name < containers[j].Name
			}
			return containers[i].ContainerID < containers[j].ContainerID
		})
	}
	sort.SliceStable(targets.Errors, func(i, j int) bool {
		if targets.Errors[i].Source != targets.Errors[j].Source {
			return targets.Errors[i].Source < targets.Errors[j].Source
		}
		if targets.Errors[i].Name != targets.Errors[j].Name {
			return targets.Errors[i].Name < targets.Errors[j].Name
		}
		if targets.Errors[i].ID != targets.Errors[j].ID {
			return targets.Errors[i].ID < targets.Errors[j].ID
		}
		return targets.Errors[i].Code < targets.Errors[j].Code
	})
}

// sanitizeTargetError keeps an unexpected code from reaching the wire. The
// source always uses the constants above; this is the boundary that keeps a
// future mistake from turning into displayed backend text.
func sanitizeTargetError(code TargetErrorCode) TargetErrorCode {
	switch code {
	case TargetErrorUnavailable, TargetErrorDisabled, TargetErrorInternal, TargetErrorTimeout:
		return code
	default:
		return TargetErrorInternal
	}
}
