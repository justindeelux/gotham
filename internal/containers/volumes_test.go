package containers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func volumeFixture(t *testing.T, mock *mockDockerClient, owners VolumeOwners) (*Service, uuid.UUID) {
	t.Helper()
	registry := newFakeRegistry()
	server := registry.seed()
	svc := fixture(registry, mock, newFakeCache())
	svc.SetVolumeOwners(func(context.Context) (VolumeOwners, error) { return owners, nil })
	return svc, server.ID
}

func TestListOrphanedVolumesFiltersOwnedExternalAndInUse(t *testing.T) {
	ownedApp := uuid.New()
	ownedService := uuid.New()
	ownedDB := uuid.New()
	orphanApp := uuid.New()
	orphanCompose := uuid.New()
	owners := NewVolumeOwners(
		[]uuid.UUID{ownedApp}, []uuid.UUID{ownedService}, []string{"gotham-db-" + ownedDB.String()},
	)
	created := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	mock := &mockDockerClient{volumesResp: &agentv1.ListVolumesResponse{Volumes: []*agentv1.VolumeInfo{
		{Name: "gotham-app-" + ownedApp.String() + "-data", RefCount: 0},
		{Name: "gotham-" + ownedService.String() + "_data", RefCount: 0},
		{Name: "gotham-db-" + ownedDB.String(), RefCount: 0},
		{Name: "gotham-app-" + orphanApp.String() + "-uploads", SizeBytes: 42, RefCount: 0, CreatedAt: timestamppb.New(created)},
		{Name: "gotham-" + orphanCompose.String() + "_cache", RefCount: 0},
		{Name: "gotham-app-" + orphanApp.String() + "-busy", RefCount: 1},
		{Name: "somebody-elses-volume", RefCount: 0},
		{Name: "custom_named_volume", RefCount: 0},
	}}}
	svc, serverID := volumeFixture(t, mock, owners)

	volumes, err := svc.ListOrphanedVolumes(context.Background(), serverID)
	if err != nil {
		t.Fatalf("ListOrphanedVolumes: %v", err)
	}
	if len(volumes) != 2 {
		t.Fatalf("volumes = %+v; want the two unowned managed volumes", volumes)
	}
	if volumes[0].Name != "gotham-"+orphanCompose.String()+"_cache" ||
		volumes[1].Name != "gotham-app-"+orphanApp.String()+"-uploads" {
		t.Errorf("volumes = %+v; want sorted orphan names", volumes)
	}
	if volumes[1].SizeBytes != 42 || volumes[1].CreatedAt == nil || !volumes[1].CreatedAt.Equal(created) {
		t.Errorf("orphan metadata = %+v; want size 42 and the created time", volumes[1])
	}
}

func TestListOrphanedVolumesRefusesOldAgent(t *testing.T) {
	mock := &mockDockerClient{volumesErr: status.Error(codes.Unimplemented, "unknown method")}
	svc, serverID := volumeFixture(t, mock, NewVolumeOwners(nil, nil, nil))

	_, err := svc.ListOrphanedVolumes(context.Background(), serverID)
	if !errors.Is(err, ErrVolumeListUnsupported) {
		t.Fatalf("err = %v; want ErrVolumeListUnsupported", err)
	}
}

func TestRemoveOrphanedVolumeConfinesOwnership(t *testing.T) {
	appID := uuid.New()
	owners := NewVolumeOwners([]uuid.UUID{appID}, nil, nil)
	managed := "gotham-app-" + uuid.New().String() + "-data"
	mock := &mockDockerClient{}
	svc, serverID := volumeFixture(t, mock, owners)

	if err := svc.RemoveOrphanedVolume(context.Background(), serverID, "etc"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v; want ErrForbidden for an external volume", err)
	}
	owned := "gotham-app-" + appID.String() + "-data"
	if err := svc.RemoveOrphanedVolume(context.Background(), serverID, owned); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v; want ErrForbidden for an owned volume", err)
	}
	if mock.removeVolumes != 0 {
		t.Fatalf("removeVolumes = %d; want no agent call", mock.removeVolumes)
	}
	if err := svc.RemoveOrphanedVolume(context.Background(), serverID, managed); err != nil {
		t.Fatalf("RemoveOrphanedVolume: %v", err)
	}
	if mock.removeVolumeSeen != managed {
		t.Errorf("removed = %q; want %q", mock.removeVolumeSeen, managed)
	}
}

func TestRemoveOrphanedVolumeMapsInUse(t *testing.T) {
	mock := &mockDockerClient{actionErr: status.Error(codes.FailedPrecondition, "volume is in use - [abc]")}
	svc, serverID := volumeFixture(t, mock, NewVolumeOwners(nil, nil, nil))

	err := svc.RemoveOrphanedVolume(context.Background(), serverID, "gotham-"+uuid.New().String()+"_data")
	if !errors.Is(err, ErrVolumeInUse) {
		t.Fatalf("err = %v; want ErrVolumeInUse", err)
	}
}

func TestListOrphanedVolumesFailsClosedWithoutOwnerSource(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed()
	svc := fixture(registry, &mockDockerClient{}, newFakeCache())

	if _, err := svc.ListOrphanedVolumes(context.Background(), server.ID); err == nil {
		t.Fatal("ListOrphanedVolumes succeeded without an owner source")
	}
}
