package files

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHistoryRoutes(t *testing.T) {
	id := uuid.New()
	versionID := uuid.New()
	created := time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)
	svc := &fakeService{
		versions: []Version{{ID: versionID, CreatedAt: created, Size: 12, Etag: `"v1"`}},
		versionBody: VersionBody{
			Version: Version{ID: versionID, CreatedAt: created, Size: 12, Etag: `"v1"`},
			Content: "old body",
		},
		restoreVersion: WriteResult{Entry: Entry{Name: "a.txt", Etag: `"v2"`}, WriteMode: "rename"},
	}
	h := newTestRouter(svc)

	t.Run("list", func(t *testing.T) {
		rec := doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/history?root=volumes&path=a.txt")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
		}
		var body versionsEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(body.Versions) != 1 || body.Versions[0].ID != versionID || body.Versions[0].Size != 12 {
			t.Fatalf("versions = %+v", body.Versions)
		}
	})

	t.Run("read", func(t *testing.T) {
		rec := doRequest(t, h, http.MethodGet,
			"/v1/servers/"+id.String()+"/files/history/"+versionID.String()+"?root=volumes&path=a.txt")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
		}
		var body versionBodyEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Content != "old body" || body.Version.ID != versionID {
			t.Fatalf("body = %+v", body)
		}
	})

	t.Run("restore", func(t *testing.T) {
		rec := doRequest(t, h, http.MethodPost,
			"/v1/servers/"+id.String()+"/files/history/"+versionID.String()+"/restore?root=volumes&path=a.txt")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
		}
		if !svc.called("restore_version") {
			t.Fatal("restore did not reach the service")
		}
	})

	t.Run("delete", func(t *testing.T) {
		rec := doRequest(t, h, http.MethodDelete,
			"/v1/servers/"+id.String()+"/files/history/"+versionID.String()+"?root=volumes&path=a.txt")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
		}
		var body deletedEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body.Deleted {
			t.Fatalf("body = %+v (err %v)", body, err)
		}
	})

	t.Run("bad version id", func(t *testing.T) {
		rec := doRequest(t, h, http.MethodGet,
			"/v1/servers/"+id.String()+"/files/history/not-a-uuid?root=volumes&path=a.txt")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("history unavailable", func(t *testing.T) {
		rec := doRequest(t, newTestRouter(&fakeService{historyErr: ErrHistoryUnavailable}), http.MethodGet,
			"/v1/servers/"+id.String()+"/files/history?root=volumes&path=a.txt")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		var body errorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != "history_unavailable" {
			t.Fatalf("body = %+v (err %v)", body, err)
		}
	})
}
