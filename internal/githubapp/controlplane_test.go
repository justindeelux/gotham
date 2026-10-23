package githubapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func originRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/providers/github-app/manifest", nil)
	req.Host = "internal.local:8000"
	return req
}

func TestOriginControlPlane(t *testing.T) {
	withCP := &handler{controlPlaneURL: func(context.Context) string { return "https://cp.example/" }}
	if got, want := withCP.origin(originRequest()), "https://cp.example"; got != want {
		t.Errorf("origin = %q, want %q", got, want)
	}

	without := &handler{}
	if got, want := without.origin(originRequest()), "http://internal.local:8000"; got != want {
		t.Errorf("origin = %q, want %q", got, want)
	}

	empty := &handler{controlPlaneURL: func(context.Context) string { return "" }}
	if got, want := empty.origin(originRequest()), "http://internal.local:8000"; got != want {
		t.Errorf("origin = %q, want %q", got, want)
	}
}
