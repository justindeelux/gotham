package deploy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestRouteListsApplicationContainers(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeDeployService{containers: []ApplicationContainer{
		{ID: "abc", Name: "demo-web", Service: "web", Image: "gotham/demo:1", State: "running", Status: "Up 1 minute"},
	}}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/applications/"+appID.String()+"/containers", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body applicationContainerListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Containers) != 1 || body.Containers[0].Name != "demo-web" || body.Containers[0].Service != "web" {
		t.Errorf("containers = %+v", body.Containers)
	}
	if svc.seenApplication != appID || svc.seenUser != userID {
		t.Errorf("target = %s/%s", svc.seenUser, svc.seenApplication)
	}
}

func TestRouteContainerActionScopesToApplication(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeDeployService{containerErr: fmt.Errorf("%w: foreign", ErrContainerNotFound)}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/v1/applications/"+appID.String()+"/containers/foreign/stop", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.seenContainer != "foreign" || svc.seenAction != "stop" {
		t.Errorf("seen = %s %s", svc.seenContainer, svc.seenAction)
	}
}

func TestRouteContainerActions(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			userID, appID := uuid.New(), uuid.New()
			svc := &fakeDeployService{}
			srv := newRouteServer(svc, alwaysUser(userID))

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
				"/v1/applications/"+appID.String()+"/containers/c1/"+action, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			if svc.seenContainer != "c1" || svc.seenAction != action {
				t.Errorf("seen = %s %s, want c1 %s", svc.seenContainer, svc.seenAction, action)
			}
		})
	}
}
