// Package volumes is the backup driver for the persistent data of applications
// and services. Applications and services are rebuilt from source, so only the
// data they keep beside the container is in scope: storage mappings, managed
// compose volumes and bind mounts. Databases keep their engine dump driver.
package volumes

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/backups"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/jobguard"
	"github.com/justindeelux/gotham/internal/services"
)

// ApplicationService is the slice of the deploy service this driver needs.
// The real *deploy.Service satisfies it.
type ApplicationService interface {
	GetApplication(ctx context.Context, userID, appID uuid.UUID) (deploy.Application, error)
	GetStorages(ctx context.Context, userID, appID uuid.UUID) ([]deploy.Storage, error)
	ListVolumes(ctx context.Context, userID, appID uuid.UUID) (deploy.VolumeListing, error)
	GetApplicationRuntime(ctx context.Context, userID, appID uuid.UUID) (deploy.ApplicationRuntime, error)
	ListDeployments(ctx context.Context, userID, appID uuid.UUID) ([]deploy.Deployment, error)
	Stop(ctx context.Context, userID, appID uuid.UUID) (deploy.Deployment, error)
	Start(ctx context.Context, userID, appID uuid.UUID) (deploy.Deployment, error)
}

// ServiceService is the slice of the compose service this driver needs. The
// real service service satisfies it.
type ServiceService interface {
	Get(ctx context.Context, userID, serviceID uuid.UUID) (services.Service, error)
	Stop(ctx context.Context, userID, serviceID uuid.UUID) (services.Service, error)
	Restart(ctx context.Context, userID, serviceID uuid.UUID) (services.Service, error)
}

type activeDeployChecker interface {
	HasActiveDeploy(ctx context.Context, serviceID uuid.UUID) (bool, error)
}

// Archiver streams a resource's volumes through the node agent.
type Archiver interface {
	ArchiveVolumes(ctx context.Context, serverID uuid.UUID, sources []containers.ArchiveSource, dst io.Writer) (int64, error)
	RestoreVolumes(ctx context.Context, serverID uuid.UUID, sources []containers.ArchiveSource, mode containers.RestoreMode, src io.Reader) error
}

type Source struct {
	applications ApplicationService
	services     ServiceService
	archiver     Archiver
	now          func() time.Time
}

var (
	_ backups.Source        = (*Source)(nil)
	_ backups.Validator     = (*Source)(nil)
	_ backups.ContentLister = (*Source)(nil)
)

func NewSource(applications ApplicationService, servicesService ServiceService, archiver Archiver) *Source {
	return &Source{
		applications: applications,
		services:     servicesService,
		archiver:     archiver,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

// plan is the resolved data of one resource plus how to pause it.
type plan struct {
	resource backups.Resource
	serverID uuid.UUID
	sources  []containers.ArchiveSource
	contents []backups.Content
	running  bool
	stop     func(context.Context) error
	start    func(context.Context) error
}

func (s *Source) Resolve(ctx context.Context, ref backups.ResourceRef) (backups.Resource, error) {
	switch ref.Type {
	case backups.ResourceApplication:
		app, err := s.application(ctx, ref)
		if err != nil {
			return backups.Resource{}, err
		}
		return backups.Resource{
			Type: backups.ResourceApplication, ID: app.ID, OwnerID: app.UserID, Name: app.Name,
		}, nil
	case backups.ResourceService:
		service, err := s.service(ctx, ref)
		if err != nil {
			return backups.Resource{}, err
		}
		return backups.Resource{
			Type: backups.ResourceService, ID: service.ID, OwnerID: service.UserID, Name: service.Name,
		}, nil
	default:
		return backups.Resource{}, fmt.Errorf("%w: %s", backups.ErrUnsupportedResource, ref.Type)
	}
}

func (s *Source) Describe(ctx context.Context, ref backups.ResourceRef) (string, error) {
	resource, err := s.Resolve(ctx, ref)
	if err != nil {
		return "", err
	}
	return resource.Name, nil
}

func (s *Source) Validate(ctx context.Context, resource backups.Resource) error {
	if err := s.busy(ctx, resource); err != nil {
		return err
	}
	plan, err := s.plan(ctx, resource.Ref())
	if err != nil {
		return err
	}
	if len(plan.sources) == 0 {
		return emptyResourceError(resource.Type)
	}
	return nil
}

func (s *Source) Contents(ctx context.Context, resource backups.Resource) ([]backups.Content, error) {
	plan, err := s.plan(ctx, resource.Ref())
	if err != nil {
		return nil, err
	}
	if plan.contents == nil {
		return []backups.Content{}, nil
	}
	return plan.contents, nil
}

func (s *Source) Capture(ctx context.Context, req backups.CaptureRequest) (result backups.CaptureResult, retErr error) {
	if err := s.busy(ctx, req.Resource); err != nil {
		return result, err
	}
	plan, err := s.plan(ctx, req.Resource.Ref())
	if err != nil {
		return result, err
	}
	result.Extension = "tar.gz"
	if len(plan.sources) == 0 {
		return result, emptyResourceError(req.Resource.Type)
	}
	if plan.running {
		if err := s.recordWasRunning(ctx, req, req.Pause); err != nil {
			return result, err
		}
		if req.Pause {
			if err := plan.stop(ctx); err != nil {
				return result, err
			}
			defer func() {
				// A failed archive still puts the resource back the way the
				// operator left it.
				if startErr := plan.start(jobguard.WithInternal(context.Background())); startErr != nil {
					retErr = errors.Join(retErr,
						fmt.Errorf("volumes: could not restart the resource after the backup: %w", startErr))
				}
			}()
		}
	} else if err := s.recordWasRunning(ctx, req, false); err != nil {
		return result, err
	}

	if _, err := s.archiver.ArchiveVolumes(ctx, plan.serverID, plan.sources, req.Sink); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Source) Restore(ctx context.Context, req backups.RestoreRequest) error {
	if err := s.busy(ctx, req.Resource); err != nil {
		return err
	}
	plan, err := s.plan(ctx, req.Resource.Ref())
	if err != nil {
		return err
	}
	if len(plan.sources) == 0 {
		return emptyResourceError(req.Resource.Type)
	}
	if plan.running {
		if err := plan.stop(ctx); err != nil {
			return err
		}
	}
	// The core stores every artifact gzip-compressed; the agent takes a
	// tar.gz. Undo only the core's layer here.
	archive, err := gzip.NewReader(req.Artifact)
	if err != nil {
		return fmt.Errorf("%w: the backup artifact is not valid gzip", backups.ErrValidation)
	}
	defer func() { _ = archive.Close() }()
	if err := s.archiver.RestoreVolumes(ctx, plan.serverID, plan.sources,
		containers.RestoreModeReplace, archive); err != nil {
		// A replace restore has already cleared the target. Leave the
		// resource stopped so nobody serves half-restored data.
		return err
	}
	if plan.running {
		if err := plan.start(jobguard.WithInternal(ctx)); err != nil {
			return fmt.Errorf("volumes: the data was restored but the resource could not start: %w", err)
		}
	}
	return nil
}

func (s *Source) Recover(ctx context.Context, req backups.RecoverRequest) error {
	plan, err := s.plan(ctx, req.Resource.Ref())
	if err != nil {
		if errors.Is(err, backups.ErrNotFound) {
			return nil
		}
		return err
	}
	// A restore interrupted mid-import must never start on partial data.
	if req.WasRestore || !req.WasRunning {
		return nil
	}
	return plan.start(jobguard.WithInternal(ctx))
}

func (s *Source) recordWasRunning(ctx context.Context, req backups.CaptureRequest, running bool) error {
	if req.Recorder == nil {
		return nil
	}
	if err := req.Recorder.SetWasRunning(ctx, req.RunID, running); err != nil {
		return fmt.Errorf("volumes: record the pre-backup resource state: %w", err)
	}
	return nil
}

func (s *Source) plan(ctx context.Context, ref backups.ResourceRef) (plan, error) {
	switch ref.Type {
	case backups.ResourceApplication:
		return s.applicationPlan(ctx, ref)
	case backups.ResourceService:
		return s.servicePlan(ctx, ref)
	default:
		return plan{}, fmt.Errorf("%w: %s", backups.ErrUnsupportedResource, ref.Type)
	}
}

func (s *Source) applicationPlan(ctx context.Context, ref backups.ResourceRef) (plan, error) {
	app, err := s.application(ctx, ref)
	if err != nil {
		return plan{}, err
	}
	if app.ServerID == uuid.Nil {
		return plan{}, fmt.Errorf("%w: this application has no node assigned", backups.ErrValidation)
	}
	result := plan{
		resource: backups.Resource{
			Type: backups.ResourceApplication, ID: app.ID, OwnerID: app.UserID, Name: app.Name,
		},
		serverID: app.ServerID,
		stop: func(stopCtx context.Context) error {
			_, err := s.applications.Stop(jobguard.WithInternal(stopCtx), app.UserID, app.ID)
			return mapDeployError(err)
		},
		start: func(startCtx context.Context) error {
			_, err := s.applications.Start(jobguard.WithInternal(startCtx), app.UserID, app.ID)
			return mapDeployError(err)
		},
	}

	configured, err := s.configuredApplicationVolumes(ctx, app)
	if err != nil {
		return plan{}, err
	}
	listing, err := s.applications.ListVolumes(ctx, app.UserID, app.ID)
	if err != nil {
		return plan{}, mapDeployError(err)
	}
	seenOrigin := map[string]bool{}
	for _, entry := range listing.Volumes {
		if !configured[entry.Name] || strings.TrimSpace(entry.HostPath) == "" {
			continue
		}
		source, ok := applicationVolumeSource(entry)
		if !ok {
			continue
		}
		if seenOrigin[source.origin] {
			continue
		}
		seenOrigin[source.origin] = true
		result.sources = append(result.sources, source.archive)
		result.contents = append(result.contents, backups.Content{
			Name:   entry.Name,
			Kind:   source.kind,
			Target: mountTargets(entry.Mounts),
		})
	}
	result.running = s.applicationRunning(ctx, app)
	return result, nil
}

// configuredApplicationVolumes names the data an application owns: its storage
// mappings, or the named volumes of a compose application (which the deploy
// layer materialises under the managed volume root).
func (s *Source) configuredApplicationVolumes(ctx context.Context, app deploy.Application) (map[string]bool, error) {
	names := map[string]bool{}
	if isComposeApplication(app) {
		// shortcut: an external or explicitly named compose volume is
		// listed at its managed path, not its real Docker name. Upgrading
		// means exposing the rendered volume names from the deploy layer.
		if strings.TrimSpace(app.ComposeContent) == "" {
			return names, nil
		}
		spec, err := services.Parse(app.ComposeContent)
		if err != nil {
			return nil, err
		}
		for _, name := range spec.NamedVolumes {
			names[name] = true
		}
		return names, nil
	}
	storages, err := s.applications.GetStorages(ctx, app.UserID, app.ID)
	if err != nil {
		return nil, mapDeployError(err)
	}
	for _, storage := range storages {
		names[storage.Name] = true
	}
	return names, nil
}

type volumeSource struct {
	origin  string
	kind    string
	archive containers.ArchiveSource
}

func applicationVolumeSource(entry deploy.VolumeEntry) (volumeSource, bool) {
	origin := strings.TrimSpace(entry.HostPath)
	if origin == "" {
		return volumeSource{}, false
	}
	source := containers.ArchiveSource{Name: archiveName(entry.Name)}
	if strings.HasPrefix(origin, "/") {
		source.Path = origin
		return volumeSource{origin: origin, kind: "bind", archive: source}, true
	}
	source.Volume = origin
	return volumeSource{origin: origin, kind: "volume", archive: source}, true
}

// busy refuses a run or restore while a deploy or restart of the same resource
// is in flight, so the archive never captures a half-swapped container set.
func (s *Source) busy(ctx context.Context, resource backups.Resource) error {
	switch resource.Type {
	case backups.ResourceApplication:
		deployments, err := s.applications.ListDeployments(ctx, resource.OwnerID, resource.ID)
		if err != nil {
			return mapDeployError(err)
		}
		for _, item := range deployments {
			if !item.State.Terminal() {
				return backups.ErrResourceBusy
			}
		}
	case backups.ResourceService:
		checker, ok := s.services.(activeDeployChecker)
		if !ok {
			return nil
		}
		active, err := checker.HasActiveDeploy(ctx, resource.ID)
		if err != nil {
			return err
		}
		if active {
			return backups.ErrResourceBusy
		}
	}
	return nil
}

func (s *Source) applicationRunning(ctx context.Context, app deploy.Application) bool {
	runtime, err := s.applications.GetApplicationRuntime(ctx, app.UserID, app.ID)
	if err != nil {
		return false
	}
	for _, container := range runtime.Containers {
		if container.State == "running" {
			return true
		}
	}
	return false
}

func (s *Source) servicePlan(ctx context.Context, ref backups.ResourceRef) (plan, error) {
	service, err := s.service(ctx, ref)
	if err != nil {
		return plan{}, err
	}
	if service.ServerID == uuid.Nil {
		return plan{}, fmt.Errorf("%w: this service has no node assigned", backups.ErrValidation)
	}
	rendered, _, err := services.RenderService(service.ComposeYAML, service.Env, nil)
	if err != nil {
		return plan{}, fmt.Errorf("%w: %v", backups.ErrValidation, err)
	}
	result := plan{
		resource: backups.Resource{
			Type: backups.ResourceService, ID: service.ID, OwnerID: service.UserID, Name: service.Name,
		},
		serverID: service.ServerID,
		running:  service.Status == services.StatusRunning,
		stop: func(stopCtx context.Context) error {
			_, err := s.services.Stop(jobguard.WithInternal(stopCtx), service.UserID, service.ID)
			return mapServiceError(err)
		},
		start: func(startCtx context.Context) error {
			_, err := s.services.Restart(jobguard.WithInternal(startCtx), service.UserID, service.ID)
			return mapServiceError(err)
		},
	}
	project := services.ProjectName(service.ID)
	seen := map[string]bool{}
	add := func(origin, kind, target string) {
		if seen[origin] {
			return
		}
		seen[origin] = true
		source := containers.ArchiveSource{Name: archiveName(origin)}
		if kind == "volume" {
			source.Volume = origin
		} else {
			source.Path = origin
		}
		result.sources = append(result.sources, source)
		result.contents = append(result.contents, backups.Content{Name: origin, Kind: kind, Target: target})
	}
	for _, mount := range rendered.Spec.Mounts {
		if mount.ReadOnly || deniedMount(mount.Source) {
			continue
		}
		switch {
		case mount.Named && strings.TrimSpace(mount.Source) != "":
			add(project+"_"+mount.Source, "volume", mount.Target)
		case strings.HasPrefix(mount.Source, "/"):
			add(filepath.Clean(mount.Source), "bind", mount.Target)
		}
	}
	return result, nil
}

func (s *Source) application(ctx context.Context, ref backups.ResourceRef) (deploy.Application, error) {
	if ref.Type != backups.ResourceApplication {
		return deploy.Application{}, fmt.Errorf("%w: %s", backups.ErrUnsupportedResource, ref.Type)
	}
	if ref.ID == uuid.Nil {
		return deploy.Application{}, fmt.Errorf("%w: application id is required", backups.ErrValidation)
	}
	app, err := s.applications.GetApplication(ctx, uuid.Nil, ref.ID)
	if err != nil {
		return deploy.Application{}, mapDeployError(err)
	}
	return app, nil
}

func (s *Source) service(ctx context.Context, ref backups.ResourceRef) (services.Service, error) {
	if ref.Type != backups.ResourceService {
		return services.Service{}, fmt.Errorf("%w: %s", backups.ErrUnsupportedResource, ref.Type)
	}
	if ref.ID == uuid.Nil {
		return services.Service{}, fmt.Errorf("%w: service id is required", backups.ErrValidation)
	}
	service, err := s.services.Get(ctx, uuid.Nil, ref.ID)
	if err != nil {
		return services.Service{}, mapServiceError(err)
	}
	return service, nil
}

func mapDeployError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, deploy.ErrNotFound):
		return fmt.Errorf("%w: %v", backups.ErrNotFound, err)
	case errors.Is(err, deploy.ErrValidation):
		return fmt.Errorf("%w: %v", backups.ErrValidation, err)
	case errors.Is(err, deploy.ErrDeployInFlight), errors.Is(err, deploy.ErrConflict):
		return backups.ErrResourceBusy
	default:
		return err
	}
}

func mapServiceError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, services.ErrNotFound):
		return fmt.Errorf("%w: %v", backups.ErrNotFound, err)
	case errors.Is(err, services.ErrValidation):
		return fmt.Errorf("%w: %v", backups.ErrValidation, err)
	case errors.Is(err, services.ErrDeployInFlight), errors.Is(err, services.ErrConflict):
		return backups.ErrResourceBusy
	default:
		return err
	}
}

func emptyResourceError(kind backups.ResourceType) error {
	label := "resource"
	switch kind {
	case backups.ResourceApplication:
		label = "application"
	case backups.ResourceService:
		label = "service"
	}
	return fmt.Errorf("%w: this %s has no persistent storage", backups.ErrValidation, label)
}

func isComposeApplication(app deploy.Application) bool {
	return deploy.NormalizeSourceType(app.SourceType, app.Provider) == deploy.SourceCompose
}

func deniedMount(source string) bool {
	switch filepath.Clean(strings.TrimSpace(source)) {
	case "/var/run/docker.sock", "/run/docker.sock",
		"/var/run/containerd/containerd.sock", "/run/containerd/containerd.sock":
		return true
	}
	return false
}

func mountTargets(mounts []deploy.VolumeMount) string {
	targets := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		if mount.Destination != "" {
			targets = append(targets, mount.Destination)
		}
	}
	return strings.Join(targets, ", ")
}

var invalidArchiveChars = strings.NewReplacer(
	" ", "-", "/", "-", ":", "-", "\\", "-", "..", "-",
)

// archiveName satisfies the agent's source-name contract (letters, digits,
// dot, underscore, dash). The agent only needs uniqueness, not the real path.
func archiveName(raw string) string {
	name := invalidArchiveChars.Replace(strings.TrimSpace(raw))
	name = strings.Trim(name, "-._")
	if name == "" {
		name = "data"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}
