package volumes

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/backups"
	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/deploy"
	"github.com/justindeelux/gotham/internal/services"
)

type fakeApplications struct {
	app        deploy.Application
	storages   []deploy.Storage
	listing    deploy.VolumeListing
	runtime    deploy.ApplicationRuntime
	runtimeErr error
	deploying  bool
	stops      int
	starts     int
	stopErr    error
	startErr   error
}

func (f *fakeApplications) GetApplication(context.Context, uuid.UUID, uuid.UUID) (deploy.Application, error) {
	return f.app, nil
}

func (f *fakeApplications) GetStorages(context.Context, uuid.UUID, uuid.UUID) ([]deploy.Storage, error) {
	return f.storages, nil
}

func (f *fakeApplications) ListVolumes(context.Context, uuid.UUID, uuid.UUID) (deploy.VolumeListing, error) {
	return f.listing, nil
}

func (f *fakeApplications) GetApplicationRuntime(context.Context, uuid.UUID, uuid.UUID) (deploy.ApplicationRuntime, error) {
	return f.runtime, f.runtimeErr
}

func (f *fakeApplications) ListDeployments(context.Context, uuid.UUID, uuid.UUID) ([]deploy.Deployment, error) {
	if f.deploying {
		return []deploy.Deployment{{State: deploy.StateBuilding}}, nil
	}
	return []deploy.Deployment{{State: deploy.StateRunning}}, nil
}

func (f *fakeApplications) Stop(context.Context, uuid.UUID, uuid.UUID) (deploy.Deployment, error) {
	f.stops++
	return deploy.Deployment{}, f.stopErr
}

func (f *fakeApplications) Start(context.Context, uuid.UUID, uuid.UUID) (deploy.Deployment, error) {
	f.starts++
	return deploy.Deployment{}, f.startErr
}

type fakeServices struct {
	service   services.Service
	active    bool
	stops     int
	starts    int
	stopErr   error
	startErr  error
	hasActive bool
}

func (f *fakeServices) Get(context.Context, uuid.UUID, uuid.UUID) (services.Service, error) {
	return f.service, nil
}

func (f *fakeServices) Stop(context.Context, uuid.UUID, uuid.UUID) (services.Service, error) {
	f.stops++
	return f.service, f.stopErr
}

func (f *fakeServices) Restart(context.Context, uuid.UUID, uuid.UUID) (services.Service, error) {
	f.starts++
	return f.service, f.startErr
}

func (f *fakeServices) HasActiveDeploy(context.Context, uuid.UUID) (bool, error) {
	f.hasActive = true
	return f.active, nil
}

type fakeArchiver struct {
	sources []containers.ArchiveSource
	archive int
	restore int
	mode    containers.RestoreMode
	err     error
	payload string
}

func (f *fakeArchiver) ArchiveVolumes(_ context.Context, _ uuid.UUID, sources []containers.ArchiveSource, dst io.Writer) (int64, error) {
	f.archive++
	f.sources = sources
	if f.err != nil {
		return 0, f.err
	}
	if f.payload == "" {
		f.payload = "archive"
	}
	n, err := io.WriteString(dst, f.payload)
	return int64(n), err
}

func (f *fakeArchiver) RestoreVolumes(_ context.Context, _ uuid.UUID, sources []containers.ArchiveSource, mode containers.RestoreMode, src io.Reader) error {
	f.restore++
	f.sources = sources
	f.mode = mode
	if f.err != nil {
		return f.err
	}
	_, err := io.Copy(io.Discard, src)
	return err
}

type fakeRecorder struct {
	runID    uuid.UUID
	running  bool
	recorder bool
}

func (r *fakeRecorder) SetWasRunning(_ context.Context, runID uuid.UUID, running bool) error {
	r.recorder = true
	r.runID = runID
	r.running = running
	return nil
}

func serviceFixture(t *testing.T, compose string) (*Source, *fakeServices, *fakeArchiver) {
	t.Helper()
	service := services.Service{
		ID:          uuid.New(),
		UserID:      uuid.New(),
		ServerID:    uuid.New(),
		Name:        "uptime",
		Status:      services.StatusRunning,
		ComposeYAML: compose,
	}
	svc := &fakeServices{service: service}
	archiver := &fakeArchiver{}
	return NewSource(&fakeApplications{}, svc, archiver), svc, archiver
}

func serviceRef(service services.Service) backups.ResourceRef {
	return backups.ResourceRef{Type: backups.ResourceService, ID: service.ID}
}

const serviceCompose = `services:
  app:
    image: louislam/uptime-kuma:1
    volumes:
      - data:/app/data
      - /srv/cert:/certs:ro
      - /var/run/docker.sock:/var/run/docker.sock
      - /srv/backup:/backup
volumes:
  data:
`

func TestServiceCaptureStopsArchivesAndStarts(t *testing.T) {
	source, svc, archiver := serviceFixture(t, serviceCompose)
	resource, err := source.Resolve(context.Background(), serviceRef(svc.service))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	recorder := &fakeRecorder{}
	sink := &bytes.Buffer{}
	if _, err := source.Capture(context.Background(), backups.CaptureRequest{
		Resource: resource, RunID: uuid.New(), Sink: sink, Recorder: recorder, Pause: true,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if svc.stops != 1 || svc.starts != 1 {
		t.Fatalf("stops = %d, starts = %d; want 1 each", svc.stops, svc.starts)
	}
	if !recorder.recorder || !recorder.running {
		t.Fatalf("recorder = %+v; want was_running true", recorder)
	}
	if sink.String() != "archive" {
		t.Fatalf("archive = %q", sink.String())
	}
	origins := map[string]string{}
	for _, item := range archiver.sources {
		switch {
		case item.Volume != "":
			origins["volume:"+item.Volume] = item.Name
		case item.Path != "":
			origins["path:"+item.Path] = item.Name
		}
	}
	project := services.ProjectName(svc.service.ID)
	want := []string{
		"volume:" + project + "_data",
		"path:/srv/backup",
	}
	for _, key := range want {
		if _, ok := origins[key]; !ok {
			t.Fatalf("missing source %s in %+v", key, archiver.sources)
		}
	}
	for _, denied := range []string{"path:/certs", "path:/var/run/docker.sock"} {
		if _, ok := origins[denied]; ok {
			t.Fatalf("read-only or socket mount captured: %+v", archiver.sources)
		}
	}
}

func TestServiceCaptureWithoutPauseLeavesTheResourceRunning(t *testing.T) {
	source, svc, archiver := serviceFixture(t, serviceCompose)
	resource, _ := source.Resolve(context.Background(), serviceRef(svc.service))
	recorder := &fakeRecorder{}
	if _, err := source.Capture(context.Background(), backups.CaptureRequest{
		Resource: resource, RunID: uuid.New(), Sink: io.Discard, Recorder: recorder, Pause: false,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if svc.stops != 0 || svc.starts != 0 {
		t.Fatalf("stops = %d, starts = %d; want no lifecycle calls", svc.stops, svc.starts)
	}
	if recorder.running {
		t.Fatal("a run that never stopped the service must not record was_running")
	}
	if archiver.archive != 1 {
		t.Fatalf("archive = %d", archiver.archive)
	}
}

func TestServiceCaptureStartsAfterAnArchiveFailure(t *testing.T) {
	source, svc, archiver := serviceFixture(t, serviceCompose)
	archiver.err = errors.New("tar: boom")
	resource, _ := source.Resolve(context.Background(), serviceRef(svc.service))
	_, err := source.Capture(context.Background(), backups.CaptureRequest{
		Resource: resource, RunID: uuid.New(), Sink: io.Discard, Pause: true,
	})
	if err == nil || !strings.Contains(err.Error(), "tar: boom") {
		t.Fatalf("err = %v; want the archive failure", err)
	}
	if svc.starts != 1 {
		t.Fatalf("starts = %d; want the service restarted", svc.starts)
	}
}

func TestServiceCaptureFailsWhenItCannotStartAgain(t *testing.T) {
	source, svc, _ := serviceFixture(t, serviceCompose)
	svc.startErr = errors.New("compose up failed")
	resource, _ := source.Resolve(context.Background(), serviceRef(svc.service))
	_, err := source.Capture(context.Background(), backups.CaptureRequest{
		Resource: resource, RunID: uuid.New(), Sink: io.Discard, Pause: true,
	})
	if err == nil || !strings.Contains(err.Error(), "could not restart") {
		t.Fatalf("err = %v; want a restart failure", err)
	}
}

func TestServiceRestoreLeavesTheServiceStoppedOnFailure(t *testing.T) {
	source, svc, archiver := serviceFixture(t, serviceCompose)
	archiver.err = errors.New("import failed")
	resource, _ := source.Resolve(context.Background(), serviceRef(svc.service))
	err := source.Restore(context.Background(), backups.RestoreRequest{
		Resource: resource, BackupID: uuid.New(), Artifact: gzipReader(t, []byte("archive")),
	})
	if err == nil || !strings.Contains(err.Error(), "import failed") {
		t.Fatalf("err = %v; want the restore failure", err)
	}
	if svc.starts != 0 {
		t.Fatalf("starts = %d; a failed restore must leave the service stopped", svc.starts)
	}
	if archiver.mode != containers.RestoreModeReplace {
		t.Fatalf("mode = %q; want replace", archiver.mode)
	}
}

func TestServiceRestoreStartsTheServiceOnSuccess(t *testing.T) {
	source, svc, archiver := serviceFixture(t, serviceCompose)
	resource, _ := source.Resolve(context.Background(), serviceRef(svc.service))
	if err := source.Restore(context.Background(), backups.RestoreRequest{
		Resource: resource, BackupID: uuid.New(), Artifact: gzipReader(t, []byte("archive")),
	}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if svc.stops != 1 || svc.starts != 1 || archiver.restore != 1 {
		t.Fatalf("stops = %d, starts = %d, restores = %d", svc.stops, svc.starts, archiver.restore)
	}
}

func TestServiceRefusesWhileADeployIsInFlight(t *testing.T) {
	source, svc, _ := serviceFixture(t, serviceCompose)
	svc.active = true
	resource, _ := source.Resolve(context.Background(), serviceRef(svc.service))
	_, err := source.Capture(context.Background(), backups.CaptureRequest{
		Resource: resource, RunID: uuid.New(), Sink: io.Discard, Pause: true,
	})
	if !errors.Is(err, backups.ErrResourceBusy) {
		t.Fatalf("err = %v; want ErrResourceBusy", err)
	}
}

func TestServiceWithoutVolumesIsRefused(t *testing.T) {
	source, svc, _ := serviceFixture(t, `services:
  app:
    image: nginx:1.27
`)
	resource, _ := source.Resolve(context.Background(), serviceRef(svc.service))
	err := source.Validate(context.Background(), resource)
	if !errors.Is(err, backups.ErrValidation) || !strings.Contains(err.Error(), "no persistent storage") {
		t.Fatalf("err = %v; want a clear validation error", err)
	}
}

func applicationFixture(t *testing.T, hostPath string) (*Source, *fakeApplications, *fakeArchiver, deploy.Application) {
	t.Helper()
	app := deploy.Application{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		ServerID:   uuid.New(),
		Name:       "shop",
		SourceType: deploy.SourceDockerfile,
	}
	apps := &fakeApplications{
		app:      app,
		storages: []deploy.Storage{{ID: uuid.New(), Name: "uploads", HostPath: hostPath, ContainerPath: "/data"}},
		listing: deploy.VolumeListing{Volumes: []deploy.VolumeEntry{{
			Name: "uploads", Kind: deploy.VolumeKindManaged, HostPath: hostPath,
			Mounts: []deploy.VolumeMount{{Destination: "/data"}},
		}}},
		runtime: deploy.ApplicationRuntime{Containers: []deploy.RuntimeContainer{{State: "running"}}},
	}
	archiver := &fakeArchiver{}
	return NewSource(apps, &fakeServices{}, archiver), apps, archiver, app
}

func TestApplicationCaptureUsesItsStorageMapping(t *testing.T) {
	source, apps, archiver, app := applicationFixture(t, "/var/lib/gotham/volumes/app/uploads")
	ref := backups.ResourceRef{Type: backups.ResourceApplication, ID: app.ID}
	resource, err := source.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := source.Capture(context.Background(), backups.CaptureRequest{
		Resource: resource, RunID: uuid.New(), Sink: io.Discard, Pause: true,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if apps.stops != 1 || apps.starts != 1 {
		t.Fatalf("stops = %d, starts = %d", apps.stops, apps.starts)
	}
	if len(archiver.sources) != 1 || archiver.sources[0].Path != "/var/lib/gotham/volumes/app/uploads" {
		t.Fatalf("sources = %+v", archiver.sources)
	}
}

func TestApplicationWithNamedVolumeUsesTheVolumeSource(t *testing.T) {
	source, _, archiver, app := applicationFixture(t, "gotham-app-uploads")
	ref := backups.ResourceRef{Type: backups.ResourceApplication, ID: app.ID}
	resource, _ := source.Resolve(context.Background(), ref)
	if _, err := source.Capture(context.Background(), backups.CaptureRequest{
		Resource: resource, RunID: uuid.New(), Sink: io.Discard, Pause: false,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(archiver.sources) != 1 || archiver.sources[0].Volume != "gotham-app-uploads" {
		t.Fatalf("sources = %+v", archiver.sources)
	}
}

func TestApplicationRefusesWhileADeployIsInFlight(t *testing.T) {
	source, apps, _, app := applicationFixture(t, "/var/lib/gotham/volumes/app/uploads")
	apps.deploying = true
	resource, _ := source.Resolve(context.Background(), backups.ResourceRef{Type: backups.ResourceApplication, ID: app.ID})
	if _, err := source.Capture(context.Background(), backups.CaptureRequest{
		Resource: resource, RunID: uuid.New(), Sink: io.Discard, Pause: true,
	}); !errors.Is(err, backups.ErrResourceBusy) {
		t.Fatalf("err = %v; want ErrResourceBusy", err)
	}
}

func TestApplicationCaptureFailsWhenTheRuntimeLookupFails(t *testing.T) {
	source, apps, archiver, app := applicationFixture(t, "/var/lib/gotham/volumes/app/uploads")
	apps.runtimeErr = errors.New("agent unreachable")
	resource, _ := source.Resolve(context.Background(), backups.ResourceRef{Type: backups.ResourceApplication, ID: app.ID})
	_, err := source.Capture(context.Background(), backups.CaptureRequest{
		Resource: resource, RunID: uuid.New(), Sink: io.Discard, Pause: true,
	})
	if err == nil || !strings.Contains(err.Error(), "agent unreachable") {
		t.Fatalf("err = %v; want the runtime lookup failure", err)
	}
	if apps.stops != 0 || archiver.archive != 0 {
		t.Fatalf("stops = %d, archives = %d; a failed lookup must not start the run",
			apps.stops, archiver.archive)
	}
}

func TestApplicationRestoreValidatesTheArtifactBeforeStopping(t *testing.T) {
	source, apps, _, app := applicationFixture(t, "/var/lib/gotham/volumes/app/uploads")
	resource, _ := source.Resolve(context.Background(), backups.ResourceRef{Type: backups.ResourceApplication, ID: app.ID})
	err := source.Restore(context.Background(), backups.RestoreRequest{
		Resource: resource, BackupID: uuid.New(), Artifact: strings.NewReader("not gzip"),
	})
	if !errors.Is(err, backups.ErrValidation) {
		t.Fatalf("err = %v; want ErrValidation for a corrupt artifact", err)
	}
	if apps.stops != 0 {
		t.Fatalf("stops = %d; a corrupt artifact must not cost the app its uptime", apps.stops)
	}
}

func TestApplicationWithoutStorageIsRefused(t *testing.T) {
	app := deploy.Application{ID: uuid.New(), UserID: uuid.New(), ServerID: uuid.New(), Name: "empty",
		SourceType: deploy.SourceDockerfile}
	apps := &fakeApplications{app: app}
	source := NewSource(apps, &fakeServices{}, &fakeArchiver{})
	resource, _ := source.Resolve(context.Background(), backups.ResourceRef{Type: backups.ResourceApplication, ID: app.ID})
	err := source.Validate(context.Background(), resource)
	if !errors.Is(err, backups.ErrValidation) || !strings.Contains(err.Error(), "no persistent storage") {
		t.Fatalf("err = %v; want a clear validation error", err)
	}
}

func TestContentsNamesTheCapturedData(t *testing.T) {
	source, svc, _ := serviceFixture(t, serviceCompose)
	resource, _ := source.Resolve(context.Background(), serviceRef(svc.service))
	contents, err := source.Contents(context.Background(), resource)
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	project := services.ProjectName(svc.service.ID)
	names := map[string]bool{}
	for _, item := range contents {
		names[item.Name] = true
	}
	if !names[project+"_data"] || !names["/srv/backup"] {
		t.Fatalf("contents = %+v", contents)
	}
}

func gzipReader(t *testing.T, payload []byte) io.Reader {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return bytes.NewReader(buffer.Bytes())
}
