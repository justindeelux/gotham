package containers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type fakeVolumeService struct {
	volumes   []OrphanedVolume
	listErr   error
	removeErr error
	removed   string
}

func (f *fakeVolumeService) ListOrphanedVolumes(context.Context, uuid.UUID) ([]OrphanedVolume, error) {
	return f.volumes, f.listErr
}

func (f *fakeVolumeService) RemoveOrphanedVolume(_ context.Context, _ uuid.UUID, name string) error {
	f.removed = name
	return f.removeErr
}

func newVolumeTestRouter(svc VolumeService) http.Handler {
	r := chi.NewRouter()
	MountVolumes(r, passthroughAuth, svc)
	return r
}

func TestVolumeRoutesList(t *testing.T) {
	svc := &fakeVolumeService{volumes: []OrphanedVolume{{Name: "gotham-orphan"}}}
	rec := doRouteRequest(t, newVolumeTestRouter(svc), http.MethodGet,
		"/v1/servers/"+uuid.New().String()+"/orphaned-volumes", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body volumeListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Volumes) != 1 || body.Volumes[0].Name != "gotham-orphan" {
		t.Errorf("volumes = %+v; want the orphan", body.Volumes)
	}
}

func TestVolumeRoutesRemove(t *testing.T) {
	svc := &fakeVolumeService{}
	rec := doRouteRequest(t, newVolumeTestRouter(svc), http.MethodDelete,
		"/v1/servers/"+uuid.New().String()+"/orphaned-volumes/gotham-orphan", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if svc.removed != "gotham-orphan" {
		t.Errorf("removed = %q; want gotham-orphan", svc.removed)
	}
}

func TestVolumeRoutesMapErrors(t *testing.T) {
	cases := map[error]int{
		ErrServerNotFound:        http.StatusNotFound,
		ErrForbidden:             http.StatusForbidden,
		ErrValidation:            http.StatusBadRequest,
		ErrVolumeInUse:           http.StatusConflict,
		ErrVolumeListUnsupported: http.StatusBadGateway,
		ErrAgentUnavailable:      http.StatusBadGateway,
	}
	for err, want := range cases {
		svc := &fakeVolumeService{listErr: err}
		rec := doRouteRequest(t, newVolumeTestRouter(svc), http.MethodGet,
			"/v1/servers/"+uuid.New().String()+"/orphaned-volumes", "")
		if rec.Code != want {
			t.Errorf("%v: status = %d, want %d", err, rec.Code, want)
		}
	}
}

var _ VolumeService = (*fakeVolumeService)(nil)
