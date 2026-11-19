package projects

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func iconPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode test icon: %v", err)
	}
	return buf.Bytes()
}

func TestServiceProjectIconPresetReplaceAndClear(t *testing.T) {
	repo := newFakeRepository()
	userID := uuid.New()
	svc, ctx := newTestService(repo, newFakeCounter(), userID)
	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	updated, err := svc.SetProjectIcon(ctx, userID, project.ID, "preset:rocket")
	if err != nil {
		t.Fatalf("SetProjectIcon: %v", err)
	}
	if updated.Icon != "preset:rocket" {
		t.Fatalf("icon = %q, want preset:rocket", updated.Icon)
	}
	if _, err := svc.GetProjectIcon(ctx, userID, project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("preset icon bytes = %v, want ErrNotFound", err)
	}

	updated, err = svc.SetProjectIcon(ctx, userID, project.ID, "preset:box")
	if err != nil || updated.Icon != "preset:box" {
		t.Fatalf("replace preset = %+v, %v", updated, err)
	}

	updated, err = svc.SetProjectIcon(ctx, userID, project.ID, "")
	if err != nil || updated.Icon != "" {
		t.Fatalf("clear icon = %+v, %v", updated, err)
	}
	fetched, _, err := svc.GetProject(ctx, userID, project.ID)
	if err != nil || fetched.Icon != "" {
		t.Fatalf("reloaded icon = %q, %v", fetched.Icon, err)
	}
}

func TestServiceProjectIconRejectsUnknownPreset(t *testing.T) {
	repo := newFakeRepository()
	userID := uuid.New()
	svc, ctx := newTestService(repo, newFakeCounter(), userID)
	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	for _, icon := range []string{"rocket", "preset:", "preset:nope", "upload"} {
		if _, err := svc.SetProjectIcon(ctx, userID, project.ID, icon); !errors.Is(err, ErrValidation) {
			t.Errorf("SetProjectIcon(%q) = %v, want ErrValidation", icon, err)
		}
	}
}

func TestServiceProjectIconUpload(t *testing.T) {
	repo := newFakeRepository()
	userID := uuid.New()
	svc, ctx := newTestService(repo, newFakeCounter(), userID)
	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	first := iconPNG(t, 32, 32)
	updated, err := svc.UploadProjectIcon(ctx, userID, project.ID, first)
	if err != nil {
		t.Fatalf("UploadProjectIcon: %v", err)
	}
	if updated.Icon != ProjectIconUpload {
		t.Fatalf("icon = %q, want %q", updated.Icon, ProjectIconUpload)
	}
	stored, err := svc.GetProjectIcon(ctx, userID, project.ID)
	if err != nil {
		t.Fatalf("GetProjectIcon: %v", err)
	}
	if _, format, err := image.Decode(bytes.NewReader(stored)); err != nil || format != "png" {
		t.Fatalf("stored icon format = %q, %v; want png", format, err)
	}

	replaced, err := svc.UploadProjectIcon(ctx, userID, project.ID, iconPNG(t, 64, 64))
	if err != nil || replaced.Icon != ProjectIconUpload {
		t.Fatalf("replace upload = %+v, %v", replaced, err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(stored))
	if err != nil || cfg.Width != 32 {
		t.Fatalf("first icon = %+v, %v; want 32px", cfg, err)
	}
	current, err := svc.GetProjectIcon(ctx, userID, project.ID)
	if err != nil {
		t.Fatalf("GetProjectIcon after replace: %v", err)
	}
	cfg, _, err = image.DecodeConfig(bytes.NewReader(current))
	if err != nil || cfg.Width != 64 {
		t.Fatalf("replaced icon = %+v, %v; want 64px", cfg, err)
	}

	if _, err := svc.SetProjectIcon(ctx, userID, project.ID, ""); err != nil {
		t.Fatalf("clear icon: %v", err)
	}
	if _, err := svc.GetProjectIcon(ctx, userID, project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cleared icon bytes = %v, want ErrNotFound", err)
	}
}

func TestServiceProjectIconRejectsBadUploads(t *testing.T) {
	repo := newFakeRepository()
	userID := uuid.New()
	svc, ctx := newTestService(repo, newFakeCounter(), userID)
	project, _, err := svc.CreateProject(ctx, userID, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	cases := map[string][]byte{
		"empty":        nil,
		"not an image": []byte("definitely not an image"),
		"oversized":    make([]byte, MaxProjectIconBytes+1),
		"too wide":     iconPNG(t, maxProjectIconDimension+1, 1),
	}
	for name, data := range cases {
		if _, err := svc.UploadProjectIcon(ctx, userID, project.ID, data); !errors.Is(err, ErrValidation) {
			t.Errorf("UploadProjectIcon(%s) = %v, want ErrValidation", name, err)
		}
	}
}

func TestServiceProjectIconUnknownProject(t *testing.T) {
	repo := newFakeRepository()
	userID := uuid.New()
	svc, ctx := newTestService(repo, newFakeCounter(), userID)
	missing := uuid.New()
	if _, err := svc.SetProjectIcon(ctx, userID, missing, "preset:rocket"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetProjectIcon(missing) = %v, want ErrNotFound", err)
	}
	if _, err := svc.UploadProjectIcon(ctx, userID, missing, iconPNG(t, 8, 8)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UploadProjectIcon(missing) = %v, want ErrNotFound", err)
	}
	if _, err := svc.GetProjectIcon(ctx, userID, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetProjectIcon(missing) = %v, want ErrNotFound", err)
	}
}

func TestProjectIconRoutes(t *testing.T) {
	userID := uuid.New()
	svc := NewService(Config{Repository: newFakeRepository(), Counter: newFakeCounter(), Logger: discardLogger()})
	handler := newRouteServer(userID, svc)

	rec := doRequest(handler, http.MethodPost, "/v1/projects", `{"name":"Shop"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created projectCreateEnvelope
	if err := decodeJSON(t, rec, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	projectID := created.Project.ID

	rec = doRequest(handler, http.MethodPut, "/v1/projects/"+projectID+"/icon", `{"icon":"preset:rocket"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set preset = %d: %s", rec.Code, rec.Body.String())
	}
	var envelope projectEnvelope
	if err := decodeJSON(t, rec, &envelope); err != nil {
		t.Fatalf("decode preset: %v", err)
	}
	if envelope.Project.Icon != "preset:rocket" || envelope.Project.IconVersion == 0 {
		t.Fatalf("preset response = %+v", envelope.Project)
	}

	rec = doRequest(handler, http.MethodPut, "/v1/projects/"+projectID+"/icon", `{"icon":"preset:nope"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown preset = %d: %s", rec.Code, rec.Body.String())
	}
	if message := errorMessage(t, rec); !strings.Contains(message, "known preset") {
		t.Fatalf("unknown preset message = %q", message)
	}

	rec = doRequest(handler, http.MethodPut, "/v1/projects/"+projectID+"/icon", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing icon = %d: %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+projectID+"/icon", bytes.NewReader(iconPNG(t, 16, 16)))
	req.Header.Set("Content-Type", "image/png")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d: %s", rec.Code, rec.Body.String())
	}
	if err := decodeJSON(t, rec, &envelope); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if envelope.Project.Icon != ProjectIconUpload {
		t.Fatalf("upload icon = %q", envelope.Project.Icon)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+projectID+"/icon", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("serve icon = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes())); err != nil || format != "png" || cfg.Width != 16 {
		t.Fatalf("served icon = %+v %q, %v; want a 16px png", cfg, format, err)
	}

	rec = doRequest(handler, http.MethodPut, "/v1/projects/"+projectID+"/icon", `{"icon":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear = %d: %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+projectID+"/icon", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("serve cleared icon = %d, want 404", rec.Code)
	}
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) error {
	t.Helper()
	return json.Unmarshal(rec.Body.Bytes(), dst)
}
