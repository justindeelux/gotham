package cleanup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
)

type fakeCleanupService struct {
	scans    []NodeScan
	scanIDs  []string
	scanAge  time.Duration
	result   CleanResult
	actor    uuid.UUID
	trigger  string
	serverID string
	cleanIDs []string
	cleanAge time.Duration
}

func (f *fakeCleanupService) Scan(_ context.Context, serverIDs []string, minAge time.Duration) []NodeScan {
	f.scanIDs = serverIDs
	f.scanAge = minAge
	return f.scans
}

func (f *fakeCleanupService) Clean(
	_ context.Context, actor uuid.UUID, trigger, serverID string, ids []string, minAge time.Duration,
) CleanResult {
	f.actor = actor
	f.trigger = trigger
	f.serverID = serverID
	f.cleanIDs = ids
	f.cleanAge = minAge
	return f.result
}

func memberForbidden(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Member") != "" {
			writeJSON(w, http.StatusForbidden, errorBody{Message: "forbidden"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func newCleanupRouter(svc CleanupService, userID UserIDFunc) http.Handler {
	r := chi.NewRouter()
	Mount(r, memberForbidden, userID, svc)
	return r
}

func ownerID(context.Context) (uuid.UUID, bool) { return testOwnerID, true }

var testOwnerID = uuid.New()

func doCleanupRequest(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCategoryRoutes(t *testing.T) {
	rec := doCleanupRequest(t, newCleanupRouter(&fakeCleanupService{}, ownerID), http.MethodGet,
		"/v1/cleanup/categories", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body categoryEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tiers := map[string]Category{}
	for _, category := range body.Categories {
		tiers[category.Key] = category
	}
	for key, want := range map[string]string{
		CategoryBuildCache:        TierSafe,
		CategoryDanglingImages:    TierSafe,
		CategoryStoppedContainers: TierSafe,
		CategoryJournal:           TierSafe,
		CategoryPackageCache:      TierSafe,
		CategoryUnusedImages:      TierCareful,
		CategoryOrphanedVolumes:   TierDangerous,
		CategoryBackupFiles:       TierCareful,
		CategoryHistory:           TierCareful,
	} {
		category, ok := tiers[key]
		if !ok {
			t.Errorf("missing category %q", key)
			continue
		}
		if category.Tier != want {
			t.Errorf("%s tier = %q, want %q", key, category.Tier, want)
		}
	}
	if !tiers[CategoryOrphanedVolumes].ManualOnly {
		t.Error("orphaned_volumes must be manual only")
	}
	if got := CategoryTier("unknown"); got != TierCareful {
		t.Errorf("unknown tier = %q, want careful", got)
	}
}

func TestCleanupRoutesRequireOwner(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/cleanup/categories", nil)
	req.Header.Set("X-Member", "1")
	rec := httptest.NewRecorder()
	newCleanupRouter(&fakeCleanupService{}, ownerID).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403", rec.Code)
	}
}

func TestScanRoute(t *testing.T) {
	serverID := uuid.New()
	svc := &fakeCleanupService{scans: []NodeScan{{
		ServerID: serverID.String(),
		Status:   StatusOK,
		Items: []Item{{
			ID:          "image:sha256:abc",
			Category:    CategoryDanglingImages,
			Name:        "abc",
			SizeBytes:   42,
			CreatedUnix: time.Now().Add(-48 * time.Hour).Unix(),
			Reason:      "dangling",
		}},
	}}}
	h := newCleanupRouter(svc, ownerID)

	rec := doCleanupRequest(t, h, http.MethodPost, "/v1/cleanup/scan",
		`{"server_ids":["`+serverID.String()+`"],"min_age_hours":48}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(svc.scanIDs) != 1 || svc.scanIDs[0] != serverID.String() {
		t.Errorf("scan ids = %v", svc.scanIDs)
	}
	if svc.scanAge != 48*time.Hour {
		t.Errorf("scan age = %v, want 48h", svc.scanAge)
	}
	var body scanEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Nodes) != 1 || len(body.Nodes[0].Items) != 1 {
		t.Fatalf("nodes = %+v", body.Nodes)
	}
	node := body.Nodes[0]
	if node.ServerID != serverID.String() || node.Status != StatusOK {
		t.Errorf("node = %+v", node)
	}
	if node.Notices == nil || len(node.Notices) != 0 {
		t.Errorf("notices = %v, want an empty array", node.Notices)
	}
	if node.Items[0].Tier != TierSafe || node.Items[0].SizeBytes != 42 ||
		node.Items[0].CreatedAt == "" {
		t.Errorf("item = %+v", node.Items[0])
	}
}

func TestScanRouteValidation(t *testing.T) {
	h := newCleanupRouter(&fakeCleanupService{}, ownerID)
	manyIDs := make([]string, maxScanNodes+1)
	for i := range manyIDs {
		manyIDs[i] = `"` + uuid.New().String() + `"`
	}
	cases := map[string]string{
		"no ids":       `{"server_ids":[],"min_age_hours":24}`,
		"too many ids": `{"server_ids":[` + strings.Join(manyIDs, ",") + `],"min_age_hours":24}`,
		"bad id":       `{"server_ids":["nope"],"min_age_hours":24}`,
		"negative age": `{"server_ids":["` + uuid.New().String() + `"],"min_age_hours":-1}`,
		"huge age":     `{"server_ids":["` + uuid.New().String() + `"],"min_age_hours":8761}`,
		"malformed":    `{`,
		"unknown":      `{"server_ids":["` + uuid.New().String() + `"],"extra":1}`,
	}
	for name, body := range cases {
		rec := doCleanupRequest(t, h, http.MethodPost, "/v1/cleanup/scan", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %s)", name, rec.Code, rec.Body.String())
		}
	}
}

func TestScanRouteMapsNotices(t *testing.T) {
	svc := &fakeCleanupService{scans: []NodeScan{{
		ServerID: "node-1",
		Status:   StatusOK,
		Notices: []CategoryNotice{
			{Category: CategoryJournal, Reason: ReasonHelperOutdated},
		},
	}}}
	rec := doCleanupRequest(t, newCleanupRouter(svc, ownerID), http.MethodPost, "/v1/cleanup/scan",
		`{"server_ids":["`+uuid.New().String()+`"],"min_age_hours":24}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body scanEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Nodes[0].Notices) != 1 ||
		body.Nodes[0].Notices[0].Reason != ReasonHelperOutdated {
		t.Errorf("notices = %+v; want the helper notice", body.Nodes[0].Notices)
	}
}

func TestScanRouteReportsUnknownNodeAsNotFound(t *testing.T) {
	svc := &fakeCleanupService{scans: []NodeScan{{
		ServerID: "missing",
		Status:   StatusFailed,
		Reason:   servers.ErrNotFound.Error(),
	}}}
	rec := doCleanupRequest(t, newCleanupRouter(svc, ownerID), http.MethodPost, "/v1/cleanup/scan",
		`{"server_ids":["`+uuid.New().String()+`"],"min_age_hours":24}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body scanEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Nodes[0].Status != StatusFailed || body.Nodes[0].Reason != ReasonNotFound {
		t.Errorf("node = %+v; want failed not_found", body.Nodes[0])
	}
}

func TestCleanRouteReportsUnknownNodeAsNotFound(t *testing.T) {
	svc := &fakeCleanupService{result: CleanResult{
		ServerID: "missing",
		Status:   StatusFailed,
		Reason:   servers.ErrNotFound.Error(),
	}}
	rec := doCleanupRequest(t, newCleanupRouter(svc, ownerID), http.MethodPost, "/v1/cleanup/clean",
		`{"server_id":"`+uuid.New().String()+`","ids":["image:x"],"min_age_hours":24}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body cleanEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != StatusFailed || body.Reason != ReasonNotFound {
		t.Errorf("clean = %+v; want failed not_found", body)
	}
}

func TestCleanRoute(t *testing.T) {
	serverID := uuid.New()
	svc := &fakeCleanupService{result: CleanResult{
		ServerID: serverID.String(),
		Status:   StatusOK,
		RunID:    "run-1",
		Items: []ItemResult{
			{ID: "image:a", Category: CategoryDanglingImages, Name: "a", Status: "removed", ReclaimedBytes: 100},
			{ID: "volume:v", Category: CategoryOrphanedVolumes, Name: "v", Status: "skipped", Message: "in use"},
		},
	}}
	h := newCleanupRouter(svc, ownerID)

	rec := doCleanupRequest(t, h, http.MethodPost, "/v1/cleanup/clean",
		`{"server_id":"`+serverID.String()+`","ids":["image:a","volume:v"],"min_age_hours":24}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.actor != testOwnerID || svc.trigger != TriggerManual || svc.serverID != serverID.String() {
		t.Errorf("clean call = actor %v trigger %q server %q", svc.actor, svc.trigger, svc.serverID)
	}
	if len(svc.cleanIDs) != 2 || svc.cleanAge != 24*time.Hour {
		t.Errorf("ids = %v age = %v", svc.cleanIDs, svc.cleanAge)
	}
	var body cleanEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.RunID != "run-1" || body.ReclaimedBytes != 100 {
		t.Errorf("response = %+v", body)
	}
	if len(body.Items) != 2 || body.Items[1].Status != StatusSkipped || body.Items[1].Message != "in use" {
		t.Errorf("items = %+v", body.Items)
	}
}

func TestCleanRouteValidation(t *testing.T) {
	h := newCleanupRouter(&fakeCleanupService{}, ownerID)
	manyIDs := make([]string, maxCleanIDs+1)
	for i := range manyIDs {
		manyIDs[i] = `"image:x"`
	}
	serverID := uuid.New().String()
	cases := map[string]string{
		"bad server":   `{"server_id":"nope","ids":["image:x"],"min_age_hours":24}`,
		"no ids":       `{"server_id":"` + serverID + `","ids":[],"min_age_hours":24}`,
		"too many ids": `{"server_id":"` + serverID + `","ids":[` + strings.Join(manyIDs, ",") + `],"min_age_hours":24}`,
		"empty id":     `{"server_id":"` + serverID + `","ids":[""],"min_age_hours":24}`,
		"bad age":      `{"server_id":"` + serverID + `","ids":["image:x"],"min_age_hours":-1}`,
	}
	for name, body := range cases {
		rec := doCleanupRequest(t, h, http.MethodPost, "/v1/cleanup/clean", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %s)", name, rec.Code, rec.Body.String())
		}
	}
}

func TestCleanRouteRequiresAnActor(t *testing.T) {
	h := newCleanupRouter(&fakeCleanupService{}, func(context.Context) (uuid.UUID, bool) {
		return uuid.Nil, false
	})
	rec := doCleanupRequest(t, h, http.MethodPost, "/v1/cleanup/clean",
		`{"server_id":"`+uuid.New().String()+`","ids":["image:x"],"min_age_hours":24}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

var _ CleanupService = (*fakeCleanupService)(nil)
