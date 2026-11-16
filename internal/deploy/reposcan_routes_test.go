package deploy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func (f *fakeDeployService) ScanRepository(_ context.Context, userID uuid.UUID, in ScanRepositoryInput) (RepositoryScan, error) {
	f.seenUser, f.seenScan = userID, in
	if f.scanErr != nil {
		return RepositoryScan{}, f.scanErr
	}
	return f.scanResult, nil
}

const scanPath = "/v1/applications/scan"

func scanBody(fields func(map[string]any)) string {
	body := map[string]any{
		"source_type": SourceGitPublic,
		"provider":    "",
		"repo":        "",
		"clone_url":   "https://github.com/acme/demo.git",
		"branch":      "main",
	}
	if fields != nil {
		fields(body)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func TestRoutesScanRepository(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeDeployService{scanResult: RepositoryScan{
		BuildPack:       "docker_compose",
		MarkerFile:      "docker-compose.yml",
		ComposeFiles:    []string{"docker-compose.yml"},
		ComposeFile:     "docker-compose.yml",
		ComposeServices: []string{"web", "worker"},
	}}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, scanPath,
		strings.NewReader(scanBody(func(body map[string]any) {
			body["source_type"] = SourceGitHubApp
			body["provider"] = "github"
			body["repo"] = "acme/demo"
			body["clone_url"] = "https://github.com/acme/demo.git"
			body["github_app_id"] = appID.String()
		}))))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	assertJSONKeys(t, rec.Body.Bytes(),
		"detected_build_pack", "marker_file", "compose_files", "compose_file", "compose_services")

	if svc.seenUser != userID {
		t.Errorf("service saw user %s, want %s", svc.seenUser, userID)
	}
	if svc.seenScan.SourceType != SourceGitHubApp || svc.seenScan.GitHubAppID != appID {
		t.Errorf("scan input = %+v, want the github_app source", svc.seenScan)
	}
	if svc.seenScan.Branch != "main" {
		t.Errorf("branch = %q, want main", svc.seenScan.Branch)
	}
}

func TestRoutesScanRepositoryCarriesPrivateCredential(t *testing.T) {
	userID := uuid.New()
	svc := &fakeDeployService{scanResult: RepositoryScan{ComposeFiles: []string{}, ComposeServices: []string{}}}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, scanPath,
		strings.NewReader(scanBody(func(body map[string]any) {
			body["source_type"] = SourceGitPrivate
			body["clone_url"] = "https://git.example.com/acme/demo.git"
			body["credential_username"] = "ada"
			body["credential_token"] = "tok-123"
		}))))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.seenScan.CredentialUsername != "ada" || svc.seenScan.CredentialToken != "tok-123" {
		t.Errorf("scan input = %+v, want the inline credential", svc.seenScan)
	}
	var payload struct {
		ComposeFiles    []string `json:"compose_files"`
		ComposeServices []string `json:"compose_services"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.ComposeFiles == nil || payload.ComposeServices == nil {
		t.Errorf("body = %s, want empty arrays instead of null", rec.Body.String())
	}
}

func TestRoutesScanRepositorySoftFailure(t *testing.T) {
	svc := &fakeDeployService{scanResult: RepositoryScan{
		ComposeFiles:    []string{},
		ComposeServices: []string{},
		Error:           "git clone failed",
	}}
	srv := newRouteServer(svc, alwaysUser(uuid.New()))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, scanPath, strings.NewReader(scanBody(nil))))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var payload repositoryScanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Error != "git clone failed" || payload.DetectedBuildPack != "" {
		t.Errorf("payload = %+v, want the scan error and no pack", payload)
	}
}

func TestRoutesScanRepositoryRejectsBadInput(t *testing.T) {
	svc := &fakeDeployService{scanErr: ErrValidation}
	srv := newRouteServer(svc, alwaysUser(uuid.New()))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, scanPath, strings.NewReader(scanBody(nil))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("validation status = %d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, scanPath,
		strings.NewReader(`{"source_type":"git_public","nope":1}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, want 400", rec.Code)
	}
}
