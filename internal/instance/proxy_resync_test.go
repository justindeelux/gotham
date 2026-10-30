package instance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestGeneralProxyResync(t *testing.T) {
	svc, repo := newSvc(&fakeHost{})
	calls := 0
	svc.ResyncProxy = func(ctx context.Context) error {
		calls++
		if repo.rec.ControlPlaneURL == nil {
			t.Fatal("resync ran before save")
		}
		effective, err := svc.ControlPlaneURL(ctx)
		if err != nil || effective != *repo.rec.ControlPlaneURL {
			t.Fatalf("saved URL unavailable: %q %v", effective, err)
		}
		return errors.New("agent offline")
	}
	input := GeneralInput{ControlPlaneURL: "https://panel.example.com", InstanceName: "Gotham"}
	state, err := svc.UpdateGeneral(context.Background(), uuid.Nil, input)
	if err != nil || state.General.ControlPlaneURL.Value != input.ControlPlaneURL || calls != 1 {
		t.Fatalf("save=%+v err=%v calls=%d", state, err, calls)
	}
	input.InstanceName = "renamed"
	if _, err = svc.UpdateGeneral(context.Background(), uuid.Nil, input); err != nil || calls != 2 {
		t.Fatalf("pending URL retry: %v calls=%d", err, calls)
	}
	input.ControlPlaneURL = "invalid"
	if _, err = svc.UpdateGeneral(context.Background(), uuid.Nil, input); err == nil || calls != 3 {
		t.Fatalf("pending URL retry before validation: %v calls=%d", err, calls)
	}
}

type failingGeneralRepo struct{ Repository }

func (f failingGeneralRepo) Save(context.Context, uuid.UUID, string, Stored, any) error {
	return errors.New("save failed")
}
func TestGeneralFailedSaveDoesNotResync(t *testing.T) {
	svc, repo := newSvc(&fakeHost{})
	svc.repo = failingGeneralRepo{repo}
	svc.ResyncProxy = func(context.Context) error { t.Fatal("resync after failed save"); return nil }
	_, err := svc.UpdateGeneral(context.Background(), uuid.Nil, GeneralInput{ControlPlaneURL: "https://panel.example.com", InstanceName: "Gotham"})
	if err == nil {
		t.Fatal("save failure swallowed")
	}
}
