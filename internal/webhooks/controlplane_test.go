package webhooks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func callbackRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/applications/00000000-0000-0000-0000-000000000000/webhooks", nil)
	req.Host = "internal.local:8000"
	return req
}

func TestCallbackBaseURLControlPlane(t *testing.T) {
	withCP := NewService(Config{
		Repository:      newFakeRepository(),
		ControlPlaneURL: func(context.Context) string { return "https://cp.example/" },
		Logger:          discardLogger(),
	})
	if got, want := withCP.callbackBaseURL(callbackRequest()), "https://cp.example"+deliveryPathPrefix; got != want {
		t.Errorf("callbackBaseURL = %q, want %q", got, want)
	}

	without := NewService(Config{Repository: newFakeRepository(), Logger: discardLogger()})
	if got, want := without.callbackBaseURL(callbackRequest()), "http://internal.local:8000"+deliveryPathPrefix; got != want {
		t.Errorf("callbackBaseURL = %q, want %q", got, want)
	}

	empty := NewService(Config{
		Repository:      newFakeRepository(),
		ControlPlaneURL: func(context.Context) string { return "" },
		Logger:          discardLogger(),
	})
	if got, want := empty.callbackBaseURL(callbackRequest()), "http://internal.local:8000"+deliveryPathPrefix; got != want {
		t.Errorf("callbackBaseURL = %q, want %q", got, want)
	}
}
