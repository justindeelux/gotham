package instance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func domainFixture(t *testing.T) (*Service, *fakeRepo, GeneralInput) {
	t.Helper()
	svc, repo := newSvc(&fakeHost{})
	old := "https://old.example.com"
	repo.rec.ControlPlaneURL = &old
	svc.ResyncProxy = func(context.Context) error { return nil }
	t.Cleanup(func() {
		svc.domainLock.Lock()
		defer svc.domainLock.Unlock()
		if svc.domainTimer != nil {
			svc.domainTimer.Stop()
		}
	})
	return svc, repo, GeneralInput{ControlPlaneURL: "https://new.example.com", InstanceName: "Gotham", Timezone: "UTC"}
}

func TestControlPlaneURLTrial(t *testing.T) {
	ctx := context.Background()
	svc, repo, in := domainFixture(t)
	syncs := 0
	svc.ResyncProxy = func(ctx context.Context) error {
		syncs++
		previous, err := svc.ControlPlaneURLs(ctx)
		if syncs == 1 && (err != nil || (len(previous) != 2 || previous[1] != "https://old.example.com")) {
			t.Fatalf("fallback during sync=%v %v", previous, err)
		}
		return nil
	}
	state, err := svc.UpdateGeneral(ctx, uuid.Nil, in)
	if err != nil || state.PendingControlPlaneURL == nil || !state.PendingControlPlaneURL.Synced {
		t.Fatalf("save=%+v err=%v", state, err)
	}
	if state.PendingControlPlaneURL.Deadline.Sub(svc.now()) > ControlPlaneConfirmWindow {
		t.Fatal("invalid deadline")
	}
	for _, host := range []string{"OLD.EXAMPLE.COM.", "new.example.com"} {
		if release, err := svc.GuardDomains(ctx, host); err == nil {
			release()
			t.Fatalf("reserved host accepted: %s", host)
		}
	}
	release, err := svc.GuardDomains(ctx, "other.example.com")
	if err != nil {
		t.Fatal(err)
	}
	release()
	second := in
	second.ControlPlaneURL = "https://third.example.com"
	if _, err := svc.UpdateGeneral(ctx, uuid.Nil, second); !errors.Is(err, ErrDomainPending) {
		t.Fatalf("second trial=%v", err)
	}
	if _, err := svc.ConfirmControlPlaneURL(ctx, uuid.Nil, "old.example.com"); !errors.Is(err, ErrConfirmDomainHost) {
		t.Fatalf("old-host confirm=%v", err)
	}
	state, err = svc.ConfirmControlPlaneURL(ctx, uuid.Nil, "NEW.EXAMPLE.COM:443")
	if err != nil || state.PendingControlPlaneURL != nil || syncs != 2 {
		t.Fatalf("confirm=%+v err=%v syncs=%d", state, err, syncs)
	}
	release, err = svc.GuardDomains(ctx, "old.example.com")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if repo.rec.ControlPlaneURL == nil || *repo.rec.ControlPlaneURL != in.ControlPlaneURL {
		t.Fatal("confirmed URL lost")
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

func TestControlPlaneURLRollbackBranches(t *testing.T) {
	for _, mode := range []string{"operator", "expired", "restart-expired", "failed-sync", "clear", "initial"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			svc, repo, in := domainFixture(t)
			old := repo.rec.ControlPlaneURL
			now := time.Now()
			svc.now = func() time.Time { return now }
			if mode == "initial" {
				old = nil
				repo.rec.ControlPlaneURL = nil
			}
			if mode == "clear" {
				in.ControlPlaneURL = ""
			}
			if mode == "failed-sync" {
				svc.ResyncProxy = func(context.Context) error { return errors.New("agent offline") }
			}
			state, err := svc.UpdateGeneral(ctx, uuid.Nil, in)
			if err != nil || state.PendingControlPlaneURL == nil {
				t.Fatalf("save=%+v err=%v", state, err)
			}
			if mode == "failed-sync" {
				if state.PendingControlPlaneURL.Synced {
					t.Fatal("failed sync marked ready")
				}
				if _, err := svc.ConfirmControlPlaneURL(ctx, uuid.Nil, "new.example.com"); !errors.Is(err, ErrDomainNotSynced) {
					t.Fatalf("confirm=%v", err)
				}
			}
			if mode == "operator" || mode == "clear" {
				state, err = svc.RevertControlPlaneURL(ctx, uuid.Nil)
			} else {
				now = state.PendingControlPlaneURL.Deadline
				if mode == "restart-expired" {
					restarted := NewService(repo, nil, svc.logger)
					restarted.lookup, restarted.now, restarted.ResyncProxy = svc.lookup, svc.now, svc.ResyncProxy
					svc = restarted
				}
				err = svc.Reconcile(ctx)
				if err == nil {
					state, err = svc.Get(ctx)
				}
			}
			if err != nil || state.PendingControlPlaneURL != nil {
				t.Fatalf("revert=%+v err=%v", state, err)
			}
			expected := ""
			if old != nil {
				expected = *old
			}
			if state.General.ControlPlaneURL.Value != expected {
				t.Fatalf("got URL=%q want=%q", state.General.ControlPlaneURL.Value, expected)
			}
			if _, err := svc.ConfirmControlPlaneURL(ctx, uuid.Nil, "new.example.com"); !errors.Is(err, ErrNoDomainPending) {
				t.Fatalf("late confirm=%v", err)
			}
		})
	}
}

func TestControlPlaneURLConfirmClear(t *testing.T) {
	svc, _, in := domainFixture(t)
	in.ControlPlaneURL = ""
	if _, err := svc.UpdateGeneral(context.Background(), uuid.Nil, in); err != nil {
		t.Fatal(err)
	}
	state, err := svc.ConfirmControlPlaneURL(context.Background(), uuid.Nil, "old.example.com")
	if err != nil || state.General.ControlPlaneURL.Value != "" || state.PendingControlPlaneURL != nil {
		t.Fatalf("clear=%+v err=%v", state, err)
	}
}

type timerRepo struct {
	mu  sync.Mutex
	rec Stored
}

func (r *timerRepo) Load(context.Context) (Stored, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rec, nil
}
func (r *timerRepo) Save(_ context.Context, _ uuid.UUID, _ string, rec Stored, _ any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rec = rec
	return nil
}

func TestControlPlaneURLRestartRearmsAutomaticRollback(t *testing.T) {
	old, next := "https://old.example.com", "https://new.example.com"
	repo := &timerRepo{rec: Stored{ControlPlaneURL: &next, PendingControlPlaneURL: &PendingControlPlaneURL{Previous: &old, Proposed: next, Deadline: time.Now().Add(20 * time.Millisecond), Synced: true}}}
	svc := NewService(repo, nil, nil)
	svc.lookup = func(string) string { return "" }
	done := make(chan struct{}, 1)
	svc.ResyncProxy = func(context.Context) error { done <- struct{}{}; return nil }
	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("automatic rollback did not run")
	}
	rec, _ := repo.Load(context.Background())
	if rec.PendingControlPlaneURL != nil || *rec.ControlPlaneURL != old {
		t.Fatalf("not reverted: %+v", rec)
	}
}

func TestControlPlaneURLRoutesAdminAndHostChecks(t *testing.T) {
	svc, _, in := domainFixture(t)
	if _, err := svc.UpdateGeneral(context.Background(), uuid.Nil, in); err != nil {
		t.Fatal(err)
	}
	actor := func(context.Context) (uuid.UUID, bool) { return uuid.Nil, true }
	blocked := chi.NewRouter()
	Mount(blocked, func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	}, actor, svc)
	for _, action := range []string{"confirm", "revert"} {
		rr := httptest.NewRecorder()
		blocked.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/instance/settings/control-plane-url/"+action, nil))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d", action, rr.Code)
		}
	}
	router := chi.NewRouter()
	Mount(router, func(h http.Handler) http.Handler { return h }, actor, svc)
	for _, tc := range []struct {
		host   string
		status int
	}{{"old.example.com", 409}, {"new.example.com", 200}, {"new.example.com", 409}} {
		req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/v1/instance/settings/control-plane-url/confirm", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != tc.status {
			t.Fatalf("host=%s status=%d body=%s", tc.host, rr.Code, rr.Body.String())
		}
	}
}

func TestControlPlaneURLFailedRollbackKeepsPending(t *testing.T) {
	ctx := context.Background()
	svc, repo, in := domainFixture(t)
	state, err := svc.UpdateGeneral(ctx, uuid.Nil, in)
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return state.PendingControlPlaneURL.Deadline }
	svc.repo = failingGeneralRepo{repo}
	svc.ResyncProxy = func(context.Context) error { t.Fatal("resync after failed rollback save"); return nil }
	if err := svc.Reconcile(ctx); err == nil {
		t.Fatal("rollback save failure swallowed")
	}
	previous, err := svc.ControlPlaneURLs(ctx)
	if err != nil || (len(previous) != 2 || previous[1] != "https://old.example.com") || repo.rec.PendingControlPlaneURL == nil {
		t.Fatalf("lost fallback: %v %v", previous, err)
	}
	svc.repo = repo
	svc.ResyncProxy = func(context.Context) error { return nil }
	if err := svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if *repo.rec.ControlPlaneURL != "https://old.example.com" || repo.rec.PendingControlPlaneURL != nil {
		t.Fatal("rollback retry failed")
	}
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

func TestControlPlaneURLClosePreservesTrialForRestart(t *testing.T) {
	old, next := "https://old.example.com", "https://new.example.com"
	repo := &timerRepo{rec: Stored{ControlPlaneURL: &next, PendingControlPlaneURL: &PendingControlPlaneURL{Previous: &old, Proposed: next, Deadline: time.Now().Add(20 * time.Millisecond), Synced: true}}}
	svc := NewService(repo, nil, nil)
	synced := make(chan struct{}, 1)
	svc.ResyncProxy = func(context.Context) error { synced <- struct{}{}; return nil }
	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc.Close()
	select {
	case <-synced:
		t.Fatal("timer ran after shutdown")
	case <-time.After(40 * time.Millisecond):
	}
	rec, _ := repo.Load(context.Background())
	if rec.PendingControlPlaneURL == nil {
		t.Fatal("shutdown discarded the persisted trial")
	}
	restarted := NewService(repo, nil, nil)
	defer restarted.Close()
	restarted.ResyncProxy = func(context.Context) error { return nil }
	if err := restarted.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec, _ = repo.Load(context.Background())
	if rec.PendingControlPlaneURL != nil || *rec.ControlPlaneURL != old {
		t.Fatalf("restart did not revert: %+v", rec)
	}
}

func TestPanelCertificateReconciliationSerializesURLChanges(t *testing.T) {
	for _, action := range []string{"confirm", "revert"} {
		t.Run(action, func(t *testing.T) {
			old, proposed := "https://old.example.com", "https://new.example.com"
			repo := &timerRepo{rec: Stored{
				ControlPlaneURL: &proposed,
				PendingControlPlaneURL: &PendingControlPlaneURL{
					Previous: &old, Proposed: proposed, Deadline: time.Now().Add(time.Minute), Synced: true,
				},
			}}
			svc := NewService(repo, nil, nil)
			t.Cleanup(svc.Close)
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
			changed, started := make(chan error, 1), make(chan struct{})
			go func() {
				close(started)
				var err error
				if action == "confirm" {
					_, err = svc.ConfirmControlPlaneURL(context.Background(), uuid.Nil, "new.example.com")
				} else {
					_, err = svc.RevertControlPlaneURL(context.Background(), uuid.Nil)
				}
				changed <- err
			}()
			<-started
			completed := false
			select {
			case err := <-changed:
				completed = true
				t.Errorf("URL changed during certificate reconciliation: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			if err := <-reconciled; err != nil {
				t.Fatal(err)
			}
			if !completed {
				if err := <-changed; err != nil {
					t.Fatal(err)
				}
			}
			want := old
			if action == "confirm" {
				want = proposed
			}
			if len(applied) != 2 || applied[0] != old || applied[1] != want {
				t.Fatalf("certificate snapshots=%v want old then %s", applied, want)
			}
		})
	}
}
