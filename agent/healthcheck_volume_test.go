package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TestToDockerHealthcheck pins the proto→Docker HealthConfig mapping: CMD is
// prepended, seconds become nanoseconds, and nil/empty disables the check.
func TestToDockerHealthcheck(t *testing.T) {
	tests := []struct {
		name string
		in   *agentv1.ContainerHealthcheck
		want *dockerHealthcheck
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty test", in: &agentv1.ContainerHealthcheck{}, want: nil},
		{
			name: "full",
			in: &agentv1.ContainerHealthcheck{
				Test:               []string{"pg_isready", "-h", "127.0.0.1"},
				IntervalSeconds:    10,
				TimeoutSeconds:     5,
				Retries:            3,
				StartPeriodSeconds: 60,
			},
			want: &dockerHealthcheck{
				Test:        []string{"CMD", "pg_isready", "-h", "127.0.0.1"},
				Interval:    int64(10 * time.Second),
				Timeout:     int64(5 * time.Second),
				StartPeriod: int64(60 * time.Second),
				Retries:     3,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toDockerHealthcheck(tt.in)
			if !equalHealthcheck(got, tt.want) {
				t.Errorf("toDockerHealthcheck = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func equalHealthcheck(a, b *dockerHealthcheck) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Interval != b.Interval || a.Timeout != b.Timeout || a.StartPeriod != b.StartPeriod || a.Retries != b.Retries {
		return false
	}
	if len(a.Test) != len(b.Test) {
		return false
	}
	for i := range a.Test {
		if a.Test[i] != b.Test[i] {
			return false
		}
	}
	return true
}

// TestBuildCreateBodyHealthcheck proves the healthcheck reaches the Docker
// create body, and that an absent one leaves an image's own healthcheck alone.
func TestBuildCreateBodyHealthcheck(t *testing.T) {
	body, err := buildCreateBody(&agentv1.CreateContainerRequest{
		Image: "postgres:18",
		Healthcheck: &agentv1.ContainerHealthcheck{
			Test:            []string{"pg_isready"},
			IntervalSeconds: 10,
			TimeoutSeconds:  5,
			Retries:         3,
		},
	})
	if err != nil {
		t.Fatalf("buildCreateBody: %v", err)
	}
	if body.Healthcheck == nil {
		t.Fatal("create body carries no healthcheck")
	}
	if len(body.Healthcheck.Test) != 2 || body.Healthcheck.Test[0] != "CMD" {
		t.Errorf("healthcheck test = %v, want [CMD pg_isready]", body.Healthcheck.Test)
	}

	plain, err := buildCreateBody(&agentv1.CreateContainerRequest{Image: "postgres:18"})
	if err != nil {
		t.Fatalf("buildCreateBody (plain): %v", err)
	}
	if plain.Healthcheck != nil {
		t.Errorf("plain create body healthcheck = %+v, want nil", plain.Healthcheck)
	}
}

// TestListContainersSetsHealth proves the agent surfaces the Docker health
// status on ContainerInfo for the control plane's readiness gate.
func TestListContainersSetsHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/containers/json") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"Id":"c1","Names":["/c1"],"Image":"postgres:18","State":"running","Status":"Up 2 minutes (healthy)"}]`))
	}))
	t.Cleanup(server.Close)
	client := newFakeDockerClient(t, server.URL)

	containers, err := client.ListContainers(context.Background(), true)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(containers) != 1 || containers[0].GetHealth() != "healthy" {
		t.Fatalf("containers = %+v, want one with health=healthy", containers)
	}
}

// TestDockerClientRemoveVolumeHTTP pins the HTTP shape of the volume delete:
// DELETE, force=true, path-escaped name, and 404-as-success idempotence.
func TestDockerClientRemoveVolumeHTTP(t *testing.T) {
	var mu sync.Mutex
	var method, requestURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		method, requestURI = r.Method, r.RequestURI
		mu.Unlock()
		if strings.Contains(r.RequestURI, "missing") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"no such volume"}`))
			return
		}
		if strings.Contains(r.RequestURI, "boom") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"daemon down"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	client := newFakeDockerClient(t, server.URL)

	if err := client.RemoveVolume(context.Background(), "gotham-db-normal"); err != nil {
		t.Fatalf("RemoveVolume: %v", err)
	}
	mu.Lock()
	if method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", method)
	}
	if !strings.Contains(requestURI, "force=true") {
		t.Errorf("request %q missing force=true", requestURI)
	}
	mu.Unlock()

	// A name needing escaping must be path-escaped, not injected into the path.
	if err := client.RemoveVolume(context.Background(), "gotham-db-a/b c"); err != nil {
		t.Fatalf("RemoveVolume(escaped): %v", err)
	}
	mu.Lock()
	gotURI := requestURI
	mu.Unlock()
	if strings.Contains(gotURI, "a/b") {
		t.Errorf("request %q was not path-escaped", gotURI)
	}

	// 404 is success: the sweep must be retryable after the first removal.
	if err := client.RemoveVolume(context.Background(), "missing"); err != nil {
		t.Errorf("RemoveVolume(404) = %v, want nil", err)
	}
	if err := client.RemoveVolume(context.Background(), "boom"); err == nil {
		t.Error("RemoveVolume(500) = nil, want an error")
	}
	if err := client.RemoveVolume(context.Background(), "  "); err == nil {
		t.Error("RemoveVolume(blank) = nil, want an error")
	}
}
