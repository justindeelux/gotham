package instance

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func domainFixture(t *testing.T) (*Service, *fakeRepo, GeneralInput) {
	t.Helper()
	svc, repo := newSvc(&fakeHost{})
	old := "https://old.example.com"
	repo.rec.ControlPlaneURL = &old
	svc.ResyncProxy = func(context.Context) error { return nil }
	return svc, repo, GeneralInput{ControlPlaneURL: "https://new.example.com", InstanceName: "Gotham"}
}

func TestControlPlaneURLAppliesDirectly(t *testing.T) {
	ctx := context.Background()
	svc, repo, in := domainFixture(t)
	syncs := 0
	svc.ResyncProxy = func(ctx context.Context) error {
		syncs++
		urls, err := svc.ControlPlaneURLs(ctx)
		if err != nil || repo.rec.ControlPlaneURL == nil || len(urls) != 1 || urls[0] != *repo.rec.ControlPlaneURL {
			t.Fatalf("resync URLs=%v stored=%+v err=%v", urls, repo.rec.ControlPlaneURL, err)
		}
		return nil
	}
	state, err := svc.UpdateGeneral(ctx, uuid.Nil, in)
	if err != nil || syncs != 1 || state.General.ControlPlaneURL.Value != in.ControlPlaneURL {
		t.Fatalf("save=%+v err=%v syncs=%d", state, err, syncs)
	}
	if repo.rec.ControlPlaneURL == nil || *repo.rec.ControlPlaneURL != in.ControlPlaneURL {
		t.Fatalf("URL not active: %+v", repo.rec.ControlPlaneURL)
	}
	for _, host := range []string{"NEW.EXAMPLE.COM.", "new.example.com"} {
		if release, err := svc.GuardDomains(ctx, host); err == nil {
			release()
			t.Fatalf("reserved host accepted: %s", host)
		}
	}
	release, err := svc.GuardDomains(ctx, "old.example.com")
	if err != nil {
		t.Fatal(err)
	}
	release()
	second := in
	second.ControlPlaneURL = "https://third.example.com"
	state, err = svc.UpdateGeneral(ctx, uuid.Nil, second)
	if err != nil || state.General.ControlPlaneURL.Value != second.ControlPlaneURL || syncs != 2 {
		t.Fatalf("second save=%+v err=%v syncs=%d", state, err, syncs)
	}
	if repo.rec.ControlPlaneURL == nil || *repo.rec.ControlPlaneURL != second.ControlPlaneURL {
		t.Fatalf("second URL not active: %+v", repo.rec.ControlPlaneURL)
	}
}

func TestControlPlaneURLClearAppliesDirectly(t *testing.T) {
	svc, repo, in := domainFixture(t)
	in.ControlPlaneURL = ""
	state, err := svc.UpdateGeneral(context.Background(), uuid.Nil, in)
	if err != nil || state.General.ControlPlaneURL.Value != "" || repo.rec.ControlPlaneURL != nil {
		t.Fatalf("clear=%+v err=%v stored=%+v", state, err, repo.rec.ControlPlaneURL)
	}
}

func TestControlPlaneURLRejectsClashBeforeSave(t *testing.T) {
	svc, repo, in := domainFixture(t)
	svc.CheckControlPlaneHost = func(context.Context, string) error { return errors.New("host already used by a service") }
	svc.ResyncProxy = func(context.Context) error { t.Fatal("sync for rejected host"); return nil }
	_, err := svc.UpdateGeneral(context.Background(), uuid.Nil, in)
	var fields FieldErrors
	if !errors.As(err, &fields) || !strings.Contains(fields["control_plane_url"], "service") || len(repo.sections) != 0 {
		t.Fatalf("err=%v writes=%v", err, repo.sections)
	}
}

type lockedRepo struct {
	mu  sync.Mutex
	rec Stored
}

func (r *lockedRepo) Load(context.Context) (Stored, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rec, nil
}
func (r *lockedRepo) Save(_ context.Context, _ uuid.UUID, _ string, rec Stored, _ any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rec = rec
	return nil
}
func (r *lockedRepo) SaveCleanup(_ context.Context, _ uuid.UUID, rec Stored, _ any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rec = rec
	return nil
}

func TestControlPlaneURLSerializesWithDomainWrites(t *testing.T) {
	ctx := context.Background()
	svc, _, in := domainFixture(t)
	release, err := svc.GuardDomains(ctx, "new.example.com")
	if err != nil {
		t.Fatal(err)
	}
	checked := make(chan struct{}, 1)
	svc.CheckControlPlaneHost = func(context.Context, string) error {
		checked <- struct{}{}
		return errors.New("host claimed by concurrent application")
	}
	result := make(chan error, 1)
	go func() { _, err := svc.UpdateGeneral(ctx, uuid.Nil, in); result <- err }()
	select {
	case <-checked:
		release()
		t.Fatal("panel change raced past active domain write")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case err := <-result:
		var fields FieldErrors
		if !errors.As(err, &fields) {
			t.Fatalf("racing claim was accepted: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("domain write lock was not released")
	}
}

func TestPanelCertificateReconciliationSerializesURLChanges(t *testing.T) {
	old := "https://old.example.com"
	repo := &lockedRepo{rec: Stored{ControlPlaneURL: &old}}
	svc := NewService(repo, nil)
	svc.lookup = func(string) string { return "" }
	svc.ResyncProxy = func(context.Context) error { return nil }
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var applied []string
	svc.SyncPanelCertificates = func(_ context.Context, urls []string) error {
		if svc.domainLock.TryLock() {
			svc.domainLock.Unlock()
			t.Error("certificate mutation did not hold domain lock")
		}
		once.Do(func() { close(entered); <-release })
		applied = append(applied, urls[0])
		return nil
	}
	reconciled := make(chan error, 1)
	go func() { reconciled <- svc.ReconcilePanelCertificates(context.Background()) }()
	<-entered
	changed := make(chan error, 1)
	go func() {
		_, err := svc.UpdateGeneral(context.Background(), uuid.Nil, GeneralInput{
			ControlPlaneURL: "https://new.example.com", InstanceName: "Gotham",
		})
		changed <- err
	}()
	select {
	case err := <-changed:
		t.Errorf("URL changed during certificate reconciliation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-reconciled; err != nil {
		t.Fatal(err)
	}
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 || applied[0] != old || applied[1] != "https://new.example.com" {
		t.Fatalf("certificate snapshots=%v want old then new", applied)
	}
}
