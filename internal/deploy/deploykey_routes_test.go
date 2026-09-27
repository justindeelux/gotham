package deploy

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// deployKeyPath renders the deploy-key route of an application.
func deployKeyPath(appID uuid.UUID) string {
	return "/v1/applications/" + appID.String() + "/deploy-key"
}

func TestRoutesCreateDeployKey(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	svc := &fakeDeployService{deployKey: DeployKey{
		ID:            uuid.New(),
		ApplicationID: appID,
		Provider:      "github",
		Repo:          "acme/demo",
		ProviderKeyID: "4242",
		Fingerprint:   "SHA256:abc",
		PublicKey:     "ssh-ed25519 AAAA gotham:deploy:" + appID.String(),
	}}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, deployKeyPath(appID), nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var body deployKeyEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.DeployKey.Fingerprint != "SHA256:abc" || body.DeployKey.ProviderKeyID != "4242" {
		t.Errorf("body = %+v", body.DeployKey)
	}
	if body.DeployKey.PublicKey == "" || body.DeployKey.ApplicationID != appID.String() {
		t.Errorf("body = %+v, want the public half and the application id", body.DeployKey)
	}
	if svc.createdKeyFor != appID || svc.seenUser != userID {
		t.Errorf("service saw user %s / app %s, want %s / %s",
			svc.seenUser, svc.createdKeyFor, userID, appID)
	}
}

func TestRoutesDeleteDeployKey(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()

	t.Run("key removed", func(t *testing.T) {
		svc := &fakeDeployService{deletedKey: true}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, deployKeyPath(appID), nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		var body deleteKeyEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !body.Deleted {
			t.Error("deleted = false, want true")
		}
		if svc.seenApplication != appID {
			t.Errorf("service saw app %s, want %s", svc.seenApplication, appID)
		}
	})

	t.Run("application without a key", func(t *testing.T) {
		svc := &fakeDeployService{deletedKey: false}
		srv := newRouteServer(svc, alwaysUser(userID))

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, deployKeyPath(appID), nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body deleteKeyEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Deleted {
			t.Error("deleted = true, want false")
		}
	})
}

// TestRoutesDeployKeyErrors pins the statuses the deploy-key routes answer:
// a host failure is a gateway status without the provider's body, a missing
// connection a conflict, bad input a 400.
func TestRoutesDeployKeyErrors(t *testing.T) {
	userID, appID := uuid.New(), uuid.New()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", ErrNotFound, http.StatusNotFound},
		{"validation", ErrValidation, http.StatusBadRequest},
		{"provider failure", ErrProvider, http.StatusBadGateway},
		{"not connected", ErrNotConnected, http.StatusConflict},
		{"disabled", ErrDisabled, http.StatusServiceUnavailable},
		{"internal", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeDeployService{createKeyErr: tc.err, deleteKeyErr: tc.err}
			srv := newRouteServer(svc, alwaysUser(userID))

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, deployKeyPath(appID), nil))
			if rec.Code != tc.want {
				t.Fatalf("create status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
			if containsMessage(rec.Body.String(), "boom") {
				t.Errorf("response leaked an internal error: %s", rec.Body.String())
			}

			rec = httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, deployKeyPath(appID), nil))
			if rec.Code != tc.want {
				t.Fatalf("delete status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// containsMessage reports whether the response body carries a message.
func containsMessage(body, needle string) bool {
	var parsed errorBody
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return false
	}
	return parsed.Message == needle
}
