package backups

import (
	"context"
	"io"

	"github.com/google/uuid"
)

// Source is one kind of resource the backup core knows how to capture. The
// core owns providers, schedules, runs, restores and the job lifecycle; a
// source owns everything that is specific to the resource: credentials, the
// temporary container or archive, stopping and resuming the resource.
type Source interface {
	// Resolve loads a live resource. Deleted resources and unknown ids return
	// ErrNotFound; a type without a driver returns ErrUnsupportedResource.
	Resolve(ctx context.Context, ref ResourceRef) (Resource, error)
	// Describe returns the display name used to name the resource in
	// notifications and API responses.
	Describe(ctx context.Context, ref ResourceRef) (string, error)
	// Capture writes the resource's data into the sink and reports the job
	// container plus the artifact extension used in its storage key.
	Capture(ctx context.Context, req CaptureRequest) (CaptureResult, error)
	// Restore applies an artifact to the resource. The artifact is the stored
	// byte stream (gzip-compressed) of the run Capture produced.
	Restore(ctx context.Context, req RestoreRequest) error
	// Recover releases whatever a run interrupted by a control-plane restart
	// left behind: orphaned job containers, a paused resource, a staged
	// artifact.
	Recover(ctx context.Context, req RecoverRequest) error
}

// Validator is implemented by a source that can refuse a run before it is
// queued, for example a resource with no persistent data to capture.
type Validator interface {
	Validate(ctx context.Context, resource Resource) error
}

// ContentLister is implemented by a source that can name what a run captures.
type ContentLister interface {
	Contents(ctx context.Context, resource Resource) ([]Content, error)
}

// RunRecorder lets a source record observations the core must persist while the
// run is still in flight, so a crash leaves enough evidence to recover.
type RunRecorder interface {
	SetWasRunning(ctx context.Context, runID uuid.UUID, running bool) error
}

type CaptureRequest struct {
	Resource Resource
	RunID    uuid.UUID
	Sink     io.Writer
	Recorder RunRecorder
	// Pause asks the source to stop the resource while it reads, when the
	// source has a stop policy at all. A false value is the explicit
	// "do not stop, files may be inconsistent" choice.
	Pause bool
}

type CaptureResult struct {
	ContainerID string
	Extension   string
}

type RestoreRequest struct {
	Resource Resource
	BackupID uuid.UUID
	Artifact io.Reader
}

type RecoverRequest struct {
	Resource   Resource
	BackupID   uuid.UUID
	WasRestore bool
	// WasRunning is the resource state recorded before the interrupted
	// capture; only a run that stopped the resource may start it again.
	WasRunning bool
}
