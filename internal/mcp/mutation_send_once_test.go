package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync/atomic"
	"testing"
)

func TestMutationDroppedResponseOnReusedConnectionSendsOnce(t *testing.T) {
	var posts atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			acknowledge(w, r)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		posts.Add(1)
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer api.Close()
	client, err := NewClient(testConfig(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client.http.Transport = transport
	defer transport.CloseIdleConnections()
	var out struct{}
	if err := client.readJSON(context.Background(), "/warm", &out); err != nil {
		t.Fatal(err)
	}
	var reused atomic.Bool
	var connections atomic.Int32
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		if connections.Add(1) == 1 {
			reused.Store(info.Reused)
		}
	}})
	err = client.mutateJSON(ctx, "/api/v1/mcp/projects", "stable-1", map[string]string{"name": "project"}, &out)
	if !reused.Load() {
		t.Fatal("POST did not use the warmed connection")
	}
	if err == nil || sectionError(err).Code != "upstream_unavailable" {
		t.Fatalf("dropped response error=%v", err)
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("dropped response caused %d POSTs; want exactly one", got)
	}
}
