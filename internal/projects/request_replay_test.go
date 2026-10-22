package projects

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/teams"
)

func TestReplayProjectPostcommitCountsAndAuthorization(t *testing.T) {
	if os.Getenv("GOTHAM_TEST_DSN") == "" {
		t.Skip("isolated GOTHAM_TEST_DSN required")
	}
	st := newScratchStore(t)
	user := seedUser(t, context.Background(), st, uuid.NewString()+"@example.com")
	counter := newFakeCounter()
	counter.err = errors.New("count unavailable")
	svc := NewService(Config{Store: st, Counter: counter, Logger: discardLogger()})
	router := chi.NewRouter()
	Mount(router, func(h http.Handler) http.Handler { return h }, func(context.Context) (uuid.UUID, bool) { return user, true }, svc)
	send := func(role teams.Role) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/projects", strings.NewReader(`{"name":"accepted"}`))
		r.Header.Set("Idempotency-Key", "counts")
		r = r.WithContext(teams.WithScope(r.Context(), teams.Scope{UserID: user, TeamID: user, Role: role}))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	first := send(teams.RoleAdmin)
	if first.Code != 201 {
		t.Fatalf("postcommit status=%d %s", first.Code, first.Body.String())
	}
	var accepted struct {
		Receipt requestreplay.Result `json:"receipt"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil || accepted.Receipt.ResourceID == uuid.Nil || accepted.Receipt.Outcome != "accepted_not_confirmed" {
		t.Fatalf("lost acceptance %+v %v", accepted, err)
	}
	denied := send(teams.RoleReadOnly)
	if denied.Code != 403 || strings.Contains(denied.Body.String(), accepted.Receipt.ResourceID.String()) {
		t.Fatalf("demotion replay: %d %s", denied.Code, denied.Body.String())
	}
	counter.err = nil
	replay := send(teams.RoleAdmin)
	if replay.Code != 201 {
		t.Fatalf("replay=%d %s", replay.Code, replay.Body.String())
	}
	var result struct {
		Receipt requestreplay.Result `json:"receipt"`
	}
	_ = json.Unmarshal(replay.Body.Bytes(), &result)
	if !result.Receipt.Replayed || result.Receipt.ResourceID != accepted.Receipt.ResourceID || result.Receipt.RelatedID != accepted.Receipt.RelatedID {
		t.Fatalf("recreated project %+v", result)
	}
}

func TestReplayConcurrentServiceInstances(t *testing.T) {
	if os.Getenv("GOTHAM_TEST_DSN") == "" {
		t.Skip("isolated GOTHAM_TEST_DSN required")
	}
	st := newScratchStore(t)
	secondPool, err := store.Open(context.Background(), st.DB.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer secondPool.Close()
	other := store.New(secondPool)
	principal := seedUser(t, context.Background(), st, uuid.NewString()+"@example.com")
	services := []*Service{NewService(Config{Store: st, Counter: StoreCounter{Store: st}}), NewService(Config{Store: other, Counter: StoreCounter{Store: other}})}
	type result struct {
		id  uuid.UUID
		err error
	}
	out := make(chan result, 8)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ctx := teams.WithScope(context.Background(), teams.Scope{UserID: principal, TeamID: principal, Role: teams.RoleAdmin})
			ctx = requestreplay.With(ctx, &requestreplay.Request{Principal: principal, Team: principal, Operation: "create_project", Target: uuid.Nil, Key: "concurrent"})
			project, _, err := services[i%2].CreateProject(ctx, principal, "same project", "")
			out <- result{project.ID, err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(out)
	var id uuid.UUID
	for r := range out {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if id == uuid.Nil {
			id = r.id
		}
		if id != r.id {
			t.Fatal("multiple accepted identities")
		}
	}
	rows, err := services[0].repo.ListProjects(context.Background(), principal)
	if err != nil || len(rows) != 1 {
		t.Fatalf("projects %d %v", len(rows), err)
	}
}
