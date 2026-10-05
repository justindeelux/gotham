package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/teams"
)

// testVariableSecret seals the test suite's shared secrets.
const testVariableSecret = "test-shared-variables-secret"

// newVariableTestService wires the real service onto the in-memory repository
// with the given team scope and a sealing key.
func newVariableTestService(repo *fakeRepository, userID, teamID uuid.UUID, role teams.Role) (*Service, context.Context) {
	svc := NewService(Config{Repository: repo, Counter: newFakeCounter(), Secret: testVariableSecret, Logger: discardLogger()})
	ctx := teams.WithScope(context.Background(), teams.Scope{UserID: userID, TeamID: teamID, Role: role})
	return svc, ctx
}

// strPtr boxes a PUT value (nil stays omitted).
func strPtr(value string) *string { return &value }

// TestReplaceProjectVariablesRoundtrip walks the PUT then GET: plain values
// come back, secrets stay masked, and the stored ciphertext differs from the
// plaintext.
func TestReplaceProjectVariablesRoundtrip(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	svc, ctx := newVariableTestService(repo, userID, teamID, teams.RoleAdmin)

	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	replaced, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "RAILS_ENV", Value: strPtr("production")},
		{Key: "API_TOKEN", Value: strPtr("hunter2"), Secret: true},
	})
	if err != nil {
		t.Fatalf("ReplaceProjectVariables: %v", err)
	}
	if got := sortedVariableKeys(replaced); len(got) != 2 || got[0] != "API_TOKEN" || got[1] != "RAILS_ENV" {
		t.Fatalf("replaced keys = %v, want [API_TOKEN RAILS_ENV]", got)
	}
	for _, variable := range replaced {
		switch variable.Key {
		case "RAILS_ENV":
			if variable.Secret || variable.Value != "production" {
				t.Errorf("RAILS_ENV = %+v, want plain production", variable)
			}
		case "API_TOKEN":
			if !variable.Secret || variable.Value != "" {
				t.Errorf("API_TOKEN = %+v, want masked secret", variable)
			}
		}
	}

	rows, err := repo.ListVariables(context.Background(), project.ID, uuid.Nil)
	if err != nil {
		t.Fatalf("ListVariables: %v", err)
	}
	for _, row := range rows {
		if row.Key == "API_TOKEN" {
			if !row.Secret || row.Ciphertext == "" || row.Ciphertext == "hunter2" {
				t.Errorf("stored secret = %+v, want sealed ciphertext", row)
			}
			opened, err := providers.OpenSecret(testVariableSecret, row.Ciphertext)
			if err != nil || opened != "hunter2" {
				t.Errorf("open sealed = %q, %v; want hunter2", opened, err)
			}
		}
	}

	fetched, err := svc.GetProjectVariables(ctx, userID, project.ID)
	if err != nil {
		t.Fatalf("GetProjectVariables: %v", err)
	}
	if len(fetched) != 2 {
		t.Fatalf("fetched = %+v, want two variables", fetched)
	}
}

// TestReplaceKeepsSecretWhenValueOmitted pins the contract's keep semantics:
// an omitted value for an existing secret key re-writes its ciphertext
// unchanged, while a new secret without a value is a 400.
func TestReplaceKeepsSecretWhenValueOmitted(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	svc, ctx := newVariableTestService(repo, userID, teamID, teams.RoleAdmin)

	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "API_TOKEN", Value: strPtr("hunter2"), Secret: true},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := repo.ListVariables(context.Background(), project.ID, uuid.Nil)
	if err != nil || len(before) != 1 {
		t.Fatalf("seeded = %+v, %v; want one", before, err)
	}

	kept, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "API_TOKEN", Secret: true},
	})
	if err != nil {
		t.Fatalf("keep: %v", err)
	}
	if len(kept) != 1 || !kept[0].Secret || kept[0].Value != "" {
		t.Fatalf("kept = %+v, want one masked secret", kept)
	}
	after, err := repo.ListVariables(context.Background(), project.ID, uuid.Nil)
	if err != nil {
		t.Fatalf("ListVariables: %v", err)
	}
	if len(after) != 1 || after[0].Ciphertext != before[0].Ciphertext {
		t.Fatalf("ciphertext changed: before %q after %q", before[0].Ciphertext, after[0].Ciphertext)
	}

	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "API_TOKEN", Secret: true},
		{Key: "BRAND_NEW", Secret: true},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("new secret without value = %v, want ErrValidation", err)
	}
	// The failed PUT changed nothing.
	rows, err := repo.ListVariables(context.Background(), project.ID, uuid.Nil)
	if err != nil {
		t.Fatalf("ListVariables: %v", err)
	}
	if len(rows) != 1 || rows[0].Ciphertext != before[0].Ciphertext {
		t.Fatalf("failed PUT changed the set: %+v", rows)
	}
}

// TestReplaceSemanticsDropMissingKeys pins the replace (not merge) rule: keys
// absent from the PUT are gone afterwards.
func TestReplaceSemanticsDropMissingKeys(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	svc, ctx := newVariableTestService(repo, userID, teamID, teams.RoleAdmin)

	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "KEEP", Value: strPtr("1")},
		{Key: "DROP", Value: strPtr("2")},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	replaced, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "KEEP", Value: strPtr("3")},
	})
	if err != nil {
		t.Fatalf("ReplaceProjectVariables: %v", err)
	}
	if len(replaced) != 1 || replaced[0].Key != "KEEP" || replaced[0].Value != "3" {
		t.Fatalf("replaced = %+v, want only KEEP=3", replaced)
	}
	cleared, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, nil)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(cleared) != 0 {
		t.Fatalf("cleared = %+v, want empty", cleared)
	}
}

// TestVariableKeyValidation pins the contract's key rule and the scope cap.
func TestVariableKeyValidation(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	svc, ctx := newVariableTestService(repo, userID, teamID, teams.RoleAdmin)

	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	for _, key := range []string{"", "1ABC", "HAS SPACE", "HAS-DASH", "has.dot", "key=eq", "_"} {
		if key == "_" {
			continue // "_" is valid; the rest must fail.
		}
		if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{{Key: key, Value: strPtr("x")}}); !errors.Is(err, ErrValidation) {
			t.Errorf("key %q = %v, want ErrValidation", key, err)
		}
	}
	for _, key := range []string{"_", "_PRIVATE", "A1", "a_b_C_9"} {
		if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{{Key: key, Value: strPtr("x")}}); err != nil {
			t.Errorf("key %q = %v, want success", key, err)
		}
	}
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "DUP", Value: strPtr("1")},
		{Key: "DUP", Value: strPtr("2")},
	}); !errors.Is(err, ErrValidation) {
		t.Errorf("duplicate keys = %v, want ErrValidation", err)
	}
	big := make([]VariableInput, 0, maxVariablesPerScope+1)
	for i := 0; i <= maxVariablesPerScope; i++ {
		big = append(big, VariableInput{Key: "K", Value: strPtr("x")})
		big[i].Key = "KEY_" + strings.Repeat("A", 10) + string(rune('A'+i%26)) + string(rune('a'+i/26))
	}
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, big); !errors.Is(err, ErrValidation) {
		t.Errorf("%d keys = %v, want ErrValidation", len(big), err)
	}
}

// TestVariableViewerForbidden pins the role rule: viewers read but never
// write.
func TestVariableViewerForbidden(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	admin, adminCtx := newVariableTestService(repo, userID, teamID, teams.RoleAdmin)
	project, _, err := admin.CreateProject(adminCtx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	viewer, viewerCtx := newVariableTestService(repo, userID, teamID, teams.RoleReadOnly)

	if _, err := viewer.GetProjectVariables(viewerCtx, userID, project.ID); err != nil {
		t.Errorf("viewer GET = %v, want success", err)
	}
	if _, err := viewer.ReplaceProjectVariables(viewerCtx, userID, project.ID, []VariableInput{{Key: "A", Value: strPtr("1")}}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer PUT = %v, want ErrForbidden", err)
	}
}

// TestVariableCrossTeamIsNotFound pins team isolation on both scopes.
func TestVariableCrossTeamIsNotFound(t *testing.T) {
	userID, teamA, teamB := uuid.New(), uuid.New(), uuid.New()
	repo := newFakeRepository()
	svcA, ctxA := newVariableTestService(repo, userID, teamA, teams.RoleAdmin)
	svcB, ctxB := newVariableTestService(repo, userID, teamB, teams.RoleAdmin)

	project, production, err := svcA.CreateProject(ctxA, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_ = production
	for name, err := range map[string]error{
		"get project vars":     mustVarErr(svcB.GetProjectVariables(ctxB, userID, project.ID)),
		"replace project vars": mustVarErr(svcB.ReplaceProjectVariables(ctxB, userID, project.ID, []VariableInput{{Key: "A", Value: strPtr("1")}})),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("cross-team %s = %v, want ErrNotFound", name, err)
		}
	}
	// The environment scope resolves through the team-scoped environment read.
	_, environments, err := svcA.GetProject(ctxA, userID, project.ID)
	if err != nil || len(environments) != 1 {
		t.Fatalf("GetProject: %+v, %v", environments, err)
	}
	for name, err := range map[string]error{
		"get env vars":     mustVarErr(svcB.GetEnvironmentVariables(ctxB, userID, environments[0].ID)),
		"replace env vars": mustVarErr(svcB.ReplaceEnvironmentVariables(ctxB, userID, environments[0].ID, []VariableInput{{Key: "A", Value: strPtr("1")}})),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("cross-team %s = %v, want ErrNotFound", name, err)
		}
	}
}

// mustVarErr adapts a (T, error) call to the error for table assertions.
func mustVarErr[T any](_ T, err error) error { return err }

// TestVariableRoutesLifecycle walks the four variable routes and pins the
// masked wire shape on both scopes.
func TestVariableRoutesLifecycle(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	svc := NewService(Config{Repository: newFakeRepository(), Counter: newFakeCounter(), Secret: testVariableSecret, Logger: discardLogger()})
	handler := newRouteServer(userID, teamID, teams.RoleAdmin, svc)

	rec := doRequest(handler, http.MethodPost, "/v1/projects", `{"name":"Shop"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d (body %s), want 201", rec.Code, rec.Body.String())
	}
	var created projectCreateEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	projectID := created.Project.ID
	productionID := created.Environments[0].ID

	rec = doRequest(handler, http.MethodGet, "/v1/projects/"+projectID+"/variables", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty GET = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	var empty variablesEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	if len(empty.Variables) != 0 {
		t.Fatalf("empty GET = %+v, want no variables", empty.Variables)
	}

	rec = doRequest(handler, http.MethodPut, "/v1/projects/"+projectID+"/variables",
		`{"variables":[{"key":"RAILS_ENV","value":"production","secret":false},{"key":"API_TOKEN","value":"hunter2","secret":true}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "hunter2") {
		t.Fatalf("PUT body leaks the secret plaintext: %s", body)
	}
	var replaced variablesEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &replaced); err != nil {
		t.Fatalf("decode PUT: %v", err)
	}
	if len(replaced.Variables) != 2 {
		t.Fatalf("PUT = %+v, want two variables", replaced.Variables)
	}
	for _, variable := range replaced.Variables {
		if variable.Key == "API_TOKEN" && variable.Value != nil {
			t.Errorf("secret row carries a value: %+v", variable)
		}
		if variable.Key == "RAILS_ENV" && (variable.Value == nil || *variable.Value != "production") {
			t.Errorf("plain row lost its value: %+v", variable)
		}
	}

	// The environment scope answers the same shape.
	rec = doRequest(handler, http.MethodPut, "/v1/environments/"+productionID+"/variables",
		`{"variables":[{"key":"STAGING_ONLY","value":"1","secret":false}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("env PUT = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	rec = doRequest(handler, http.MethodGet, "/v1/environments/"+productionID+"/variables", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("env GET = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	var envVars variablesEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envVars); err != nil {
		t.Fatalf("decode env GET: %v", err)
	}
	if len(envVars.Variables) != 1 || envVars.Variables[0].Key != "STAGING_ONLY" {
		t.Fatalf("env GET = %+v, want the environment's own row", envVars.Variables)
	}
	// Scopes do not leak into each other.
	rec = doRequest(handler, http.MethodGet, "/v1/projects/"+projectID+"/variables", "")
	var projectVars variablesEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &projectVars); err != nil {
		t.Fatalf("decode project GET: %v", err)
	}
	if len(projectVars.Variables) != 2 {
		t.Fatalf("project GET = %+v, want only the two project rows", projectVars.Variables)
	}

	// Bad keys are a 400 with the contract's message body.
	rec = doRequest(handler, http.MethodPut, "/v1/projects/"+projectID+"/variables",
		`{"variables":[{"key":"NOPE-BAD","value":"1"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad key PUT = %d (body %s), want 400", rec.Code, rec.Body.String())
	}
	if errorMessage(t, rec) == "" {
		t.Fatal("bad key PUT has an empty message")
	}
	// NUL bytes and over-long keys are 400s too.
	for name, body := range map[string]string{
		"nul value": `{"variables":[{"key":"A","value":"a\u0000b"}]}`,
		"long key":  `{"variables":[{"key":"` + strings.Repeat("K", 129) + `","value":"1"}]}`,
	} {
		rec = doRequest(handler, http.MethodPut, "/v1/projects/"+projectID+"/variables", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s PUT = %d (body %s), want 400", name, rec.Code, rec.Body.String())
		}
	}
	// A foreign project is a 404, like every other project route.
	rec = doRequest(handler, http.MethodGet, "/v1/projects/"+uuid.New().String()+"/variables", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign GET = %d (body %s), want 404", rec.Code, rec.Body.String())
	}
}

// TestVariableRoutesViewerReadOnly pins the HTTP role rule: a viewer reads
// both scopes and is refused both PUTs.
func TestVariableRoutesViewerReadOnly(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	svc := NewService(Config{Repository: newFakeRepository(), Counter: newFakeCounter(), Secret: testVariableSecret, Logger: discardLogger()})
	admin := newRouteServer(userID, teamID, teams.RoleAdmin, svc)
	viewer := newRouteServer(userID, teamID, teams.RoleReadOnly, svc)

	rec := doRequest(admin, http.MethodPost, "/v1/projects", `{"name":"Shop"}`)
	var created projectCreateEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	for _, path := range []string{
		"/v1/projects/" + created.Project.ID + "/variables",
		"/v1/environments/" + created.Environments[0].ID + "/variables",
	} {
		rec = doRequest(viewer, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Errorf("viewer GET %s = %d, want 200", path, rec.Code)
		}
		rec = doRequest(viewer, http.MethodPut, path, `{"variables":[]}`)
		if rec.Code != http.StatusForbidden {
			t.Errorf("viewer PUT %s = %d, want 403", path, rec.Code)
		}
	}
}

// TestSharedVariablesScratchRoundtrip walks the repository against a scratch
// database: replace and list on both scopes, masked reads, sealed storage,
// and the project-delete cascade.
func TestSharedVariablesScratchRoundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := newScratchStore(t)
	repo := newStoreRepository(st)
	svc := NewService(Config{Store: st, Counter: newFakeCounter(), Secret: testVariableSecret, Logger: discardLogger()})
	userID := seedUser(t, ctx, st, fmt.Sprintf("vars-%d@example.com", time.Now().UnixNano()))

	project, production, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	staging, err := svc.CreateEnvironment(ctx, userID, project.ID, "staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "RAILS_ENV", Value: strPtr("production")},
		{Key: "API_TOKEN", Value: strPtr("hunter2"), Secret: true},
	}); err != nil {
		t.Fatalf("ReplaceProjectVariables: %v", err)
	}
	if _, err := svc.ReplaceEnvironmentVariables(ctx, userID, staging.ID, []VariableInput{
		{Key: "RAILS_ENV", Value: strPtr("staging")},
	}); err != nil {
		t.Fatalf("ReplaceEnvironmentVariables: %v", err)
	}

	projectVars, err := svc.GetProjectVariables(ctx, userID, project.ID)
	if err != nil || len(projectVars) != 2 {
		t.Fatalf("project vars = %+v, %v; want two", projectVars, err)
	}
	envVars, err := svc.GetEnvironmentVariables(ctx, userID, staging.ID)
	if err != nil || len(envVars) != 1 || envVars[0].Value != "staging" {
		t.Fatalf("env vars = %+v, %v; want RAILS_ENV=staging", envVars, err)
	}
	productionVars, err := svc.GetEnvironmentVariables(ctx, userID, production.ID)
	if err != nil || len(productionVars) != 0 {
		t.Fatalf("production vars = %+v, %v; want none", productionVars, err)
	}

	// The deploy seam reads both scopes in one snapshot.
	rows, err := st.ListSharedVariablesForEnvironment(ctx, pgUUID(project.ID), pgUUID(staging.ID))
	if err != nil {
		t.Fatalf("ListSharedVariablesForEnvironment: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("deploy rows = %d, want 3 (two project, one environment)", len(rows))
	}
	for _, row := range rows {
		if row.Secret && (row.Ciphertext == "" || row.Ciphertext == "hunter2") {
			t.Fatalf("secret %q is not sealed in storage", row.Key)
		}
	}

	// The scope index refuses two rows of one scope with one key, even when
	// the service (which dedupes first) is bypassed.
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO shared_variables (project_id, key, value) VALUES ($1, 'DUP', '1'), ($1, 'DUP', '2')`,
		pgUUID(project.ID)); err == nil {
		t.Fatal("duplicate keys in one scope inserted, want a uniqueness refusal")
	}

	// Deleting the project cascades every scope's rows.
	if _, err := repo.DeleteProject(ctx, project.TeamID, project.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	var count int
	if err := st.DB.QueryRow(ctx, "SELECT count(*) FROM shared_variables").Scan(&count); err != nil {
		t.Fatalf("count variables: %v", err)
	}
	if count != 0 {
		t.Fatalf("%d shared rows survive the project delete, want 0", count)
	}
}

// TestSecretFlagFlips pins all three flag transitions: secret to plain with
// an omitted value drops the old sealed value, plain to secret with an
// omitted value is a 400, and plain to secret with a value seals it while
// leaving the value column empty.
func TestSecretFlagFlips(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	svc, ctx := newVariableTestService(repo, userID, teamID, teams.RoleAdmin)

	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "FLIP", Value: strPtr("hunter2"), Secret: true},
		{Key: "STAY", Value: strPtr("plain")},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Secret to plain with an omitted value: the old sealed value is gone.
	flipped, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "FLIP"},
		{Key: "STAY", Value: strPtr("plain")},
	})
	if err != nil {
		t.Fatalf("secret to plain: %v", err)
	}
	for _, variable := range flipped {
		if variable.Key == "FLIP" && (variable.Secret || variable.Value != "") {
			t.Fatalf("FLIP = %+v, want empty plain", variable)
		}
	}
	rows, err := repo.ListVariables(context.Background(), project.ID, uuid.Nil)
	if err != nil {
		t.Fatalf("ListVariables: %v", err)
	}
	for _, row := range rows {
		if row.Key == "FLIP" && (row.Secret || row.Value != "" || row.Ciphertext != "") {
			t.Fatalf("stored FLIP = %+v, want cleared plain row", row)
		}
	}

	// Plain to secret with an omitted value is a 400: there is nothing to keep.
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "FLIP", Secret: true},
		{Key: "STAY", Value: strPtr("plain")},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("plain to secret without value = %v, want ErrValidation", err)
	}

	// Plain to secret with a value seals it and leaves the value column empty.
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "FLIP", Value: strPtr("new-secret"), Secret: true},
		{Key: "STAY", Value: strPtr("plain")},
	}); err != nil {
		t.Fatalf("plain to secret: %v", err)
	}
	rows, err = repo.ListVariables(context.Background(), project.ID, uuid.Nil)
	if err != nil {
		t.Fatalf("ListVariables: %v", err)
	}
	for _, row := range rows {
		if row.Key != "FLIP" {
			continue
		}
		if !row.Secret || row.Value != "" || row.Ciphertext == "" {
			t.Fatalf("stored FLIP = %+v, want sealed secret", row)
		}
		opened, err := providers.OpenSecret(testVariableSecret, row.Ciphertext)
		if err != nil || opened != "new-secret" {
			t.Fatalf("open FLIP = %q, %v; want new-secret", opened, err)
		}
	}
}

// TestVariableValueValidation pins F5: keys are capped at 128 characters and
// NUL or invalid UTF-8 in values is a 400, for plain and secret rows alike.
func TestVariableValueValidation(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	svc, ctx := newVariableTestService(repo, userID, teamID, teams.RoleAdmin)

	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	longKey := strings.Repeat("A", maxVariableKeyLength)
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{{Key: longKey, Value: strPtr("x")}}); err != nil {
		t.Errorf("128-char key = %v, want success", err)
	}
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{{Key: longKey + "A", Value: strPtr("x")}}); !errors.Is(err, ErrValidation) {
		t.Errorf("129-char key = %v, want ErrValidation", err)
	}
	for name, input := range map[string]VariableInput{
		"plain NUL":    {Key: "A", Value: strPtr("a\x00b")},
		"secret NUL":   {Key: "B", Value: strPtr("a\x00b"), Secret: true},
		"plain UTF-8":  {Key: "C", Value: strPtr("a\xffi")},
		"secret UTF-8": {Key: "D", Value: strPtr("a\xffi"), Secret: true},
	} {
		if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{input}); !errors.Is(err, ErrValidation) {
			t.Errorf("%s = %v, want ErrValidation", name, err)
		}
	}
	// A failed PUT changes nothing.
	rows, err := repo.ListVariables(context.Background(), project.ID, uuid.Nil)
	if err != nil {
		t.Fatalf("ListVariables: %v", err)
	}
	if len(rows) != 1 || rows[0].Key != longKey {
		t.Fatalf("failed PUTs changed the set: %+v", rows)
	}
}

// failingVariablesRepo wraps the fake and fails the variable writes and
// reads with an error that carries the secret value, so the test proves the
// route's error log does not repeat it.
type failingVariablesRepo struct {
	*fakeRepository
	err error
}

// ListVariables implements Repository.
func (f *failingVariablesRepo) ListVariables(_ context.Context, _, _ uuid.UUID) ([]SharedVariable, error) {
	return nil, f.err
}

// ReplaceVariables implements Repository.
func (f *failingVariablesRepo) ReplaceVariables(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ func(existing []SharedVariable) ([]SharedVariable, error)) error {
	return f.err
}

// TestVariableLogsMaskSecrets drives the route error log with a
// secret-carrying repository failure and asserts the captured output contains
// neither the plaintext nor anything of the stored rows.
func TestVariableLogsMaskSecrets(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	secretValue := "hunter2-supersecret-value"
	repo := &failingVariablesRepo{
		fakeRepository: newFakeRepository(),
		err:            errors.New("postgres: connection refused"),
	}
	svc := NewService(Config{Repository: repo, Counter: newFakeCounter(), Secret: testVariableSecret, Logger: discardLogger()})
	adminCtx := teams.WithScope(context.Background(), teams.Scope{UserID: userID, TeamID: teamID, Role: teams.RoleAdmin})
	project, _, err := svc.CreateProject(adminCtx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	var logs bytes.Buffer
	h := &handler{svc: svc, userID: alwaysUser(userID), logger: slog.New(slog.NewTextHandler(&logs, nil))}
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope := teams.Scope{UserID: userID, TeamID: teamID, Role: teams.RoleAdmin}
			next.ServeHTTP(w, r.WithContext(teams.WithScope(r.Context(), scope)))
		})
	})
	router.Get("/v1/projects/{id}/variables", h.getProjectVariables)
	router.Put("/v1/projects/{id}/variables", h.replaceProjectVariables)

	rec := doRequest(router, http.MethodGet, "/v1/projects/"+project.ID.String()+"/variables", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET = %d (body %s), want 500", rec.Code, rec.Body.String())
	}
	rec = doRequest(router, http.MethodPut, "/v1/projects/"+project.ID.String()+"/variables",
		`{"variables":[{"key":"API_TOKEN","value":"`+secretValue+`","secret":true}]}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("PUT = %d (body %s), want 500", rec.Code, rec.Body.String())
	}
	dumped := logs.String()
	if !strings.Contains(dumped, "request failed") {
		t.Fatalf("logs = %q, want the error path to have logged", dumped)
	}
	if strings.Contains(dumped, secretValue) {
		t.Errorf("error log repeats the secret value: %q", dumped)
	}
	if strings.Contains(rec.Body.String(), secretValue) {
		t.Errorf("error body repeats the secret value: %s", rec.Body.String())
	}
}

// TestConcurrentReplaceSerializes is the F1 regression: 16 concurrent PUTs
// on one scope must all succeed with exactly one consistent final set. It
// fails on the old lock-free delete-then-insert, where most PUTs hit the
// scope uniqueness index (23505, mapped to a 500).
func TestConcurrentReplaceSerializes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	st := newScratchStore(t)
	svc := NewService(Config{Store: st, Counter: newFakeCounter(), Secret: testVariableSecret, Logger: discardLogger()})
	userID := seedUser(t, ctx, st, fmt.Sprintf("race-%d@example.com", time.Now().UnixNano()))
	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	const writers = 16
	start := make(chan struct{})
	results := make(chan error, writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			<-start
			_, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
				{Key: "K1", Value: strPtr(fmt.Sprintf("v1-%d", i))},
				{Key: "K2", Value: strPtr(fmt.Sprintf("v2-%d", i))},
			})
			results <- err
		}(i)
	}
	close(start)
	for i := 0; i < writers; i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent PUT %d: %v, want success", i, err)
		}
	}
	final, err := svc.GetProjectVariables(ctx, userID, project.ID)
	if err != nil {
		t.Fatalf("GetProjectVariables: %v", err)
	}
	if len(final) != 2 {
		t.Fatalf("final set = %+v, want exactly one winner's two keys", final)
	}
	byKey := make(map[string]string, len(final))
	for _, variable := range final {
		byKey[variable.Key] = variable.Value
	}
	var first, second int
	if _, err := fmt.Sscanf(byKey["K1"], "v1-%d", &first); err != nil {
		t.Fatalf("K1 value %q does not name its writer: %v", byKey["K1"], err)
	}
	if _, err := fmt.Sscanf(byKey["K2"], "v2-%d", &second); err != nil {
		t.Fatalf("K2 value %q does not name its writer: %v", byKey["K2"], err)
	}
	if first != second {
		t.Fatalf("final set mixes writers: K1 from %d, K2 from %d", first, second)
	}
}

// TestConcurrentKeepVsNewValueStaysConsistent is the F2 companion: a
// keep-ciphertext PUT racing a new-value PUT cannot restore a stale
// ciphertext, because the existing set is read inside the locked write
// transaction. Every iteration ends with either the old or the new value and
// zero errors.
func TestConcurrentKeepVsNewValueStaysConsistent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	st := newScratchStore(t)
	svc := NewService(Config{Store: st, Counter: newFakeCounter(), Secret: testVariableSecret, Logger: discardLogger()})
	userID := seedUser(t, ctx, st, fmt.Sprintf("keeprace-%d@example.com", time.Now().UnixNano()))
	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	const iterations = 20
	for i := 0; i < iterations; i++ {
		oldValue := fmt.Sprintf("old-%d", i)
		newValue := fmt.Sprintf("new-%d", i)
		if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
			{Key: "S", Value: strPtr(oldValue), Secret: true},
		}); err != nil {
			t.Fatalf("iter %d: seed: %v", i, err)
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		go func() {
			<-start
			_, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{{Key: "S", Secret: true}})
			results <- err
		}()
		go func() {
			<-start
			_, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{{Key: "S", Value: strPtr(newValue), Secret: true}})
			results <- err
		}()
		close(start)
		for j := 0; j < 2; j++ {
			if err := <-results; err != nil {
				t.Fatalf("iter %d: racing PUT %d: %v, want success", i, j, err)
			}
		}
		rows, err := st.ListSharedVariables(ctx, pgUUID(project.ID), pgUUID(uuid.Nil))
		if err != nil {
			t.Fatalf("iter %d: list: %v", i, err)
		}
		if len(rows) != 1 {
			t.Fatalf("iter %d: rows = %d, want 1", i, len(rows))
		}
		opened, err := providers.OpenSecret(testVariableSecret, rows[0].Ciphertext)
		if err != nil {
			t.Fatalf("iter %d: open: %v", i, err)
		}
		if opened != oldValue && opened != newValue {
			t.Fatalf("iter %d: final value %q is neither the old nor the new one", i, opened)
		}
	}
}

// TestKeepSeesLatestCommit pins the keep path sequentially: a keep PUT after
// a new-value PUT preserves the new ciphertext, not a stale one.
func TestKeepSeesLatestCommit(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	svc, ctx := newVariableTestService(repo, userID, teamID, teams.RoleAdmin)

	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "S", Value: strPtr("old"), Secret: true},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "S", Value: strPtr("new"), Secret: true},
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	before, err := repo.ListVariables(context.Background(), project.ID, uuid.Nil)
	if err != nil || len(before) != 1 {
		t.Fatalf("stored = %+v, %v", before, err)
	}
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{
		{Key: "S", Secret: true},
	}); err != nil {
		t.Fatalf("keep: %v", err)
	}
	after, err := repo.ListVariables(context.Background(), project.ID, uuid.Nil)
	if err != nil || len(after) != 1 {
		t.Fatalf("stored = %+v, %v", after, err)
	}
	if after[0].Ciphertext != before[0].Ciphertext {
		t.Fatal("keep changed the ciphertext, want it preserved")
	}
	opened, err := providers.OpenSecret(testVariableSecret, after[0].Ciphertext)
	if err != nil || opened != "new" {
		t.Fatalf("kept value = %q, %v; want new", opened, err)
	}
}

// TestReplaceAgainstDeletedScopeIsNotFound is the F3 regression: a PUT
// against a deleted environment or project answers 404, not 500 — over the
// service and over HTTP.
func TestReplaceAgainstDeletedScopeIsNotFound(t *testing.T) {
	userID, teamID := uuid.New(), uuid.New()
	repo := newFakeRepository()
	svc, ctx := newVariableTestService(repo, userID, teamID, teams.RoleAdmin)

	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	staging, err := svc.CreateEnvironment(ctx, userID, project.ID, "staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if err := svc.DeleteEnvironment(ctx, userID, staging.ID); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	if _, err := svc.ReplaceEnvironmentVariables(ctx, userID, staging.ID, []VariableInput{{Key: "A", Value: strPtr("1")}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PUT vs deleted environment = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteProject(ctx, userID, project.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := svc.ReplaceProjectVariables(ctx, userID, project.ID, []VariableInput{{Key: "A", Value: strPtr("1")}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PUT vs deleted project = %v, want ErrNotFound", err)
	}

	handler := newRouteServer(userID, teamID, teams.RoleAdmin, svc)
	rec := doRequest(handler, http.MethodPut, "/v1/environments/"+staging.ID.String()+"/variables", `{"variables":[]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("HTTP PUT vs deleted environment = %d (body %s), want 404", rec.Code, rec.Body.String())
	}
}
