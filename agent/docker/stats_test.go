package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func statsServer(t *testing.T, body string) *DockerClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/abc/stats" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := NewDockerClient(server.URL)
	if err != nil {
		t.Fatalf("NewDockerClient: %v", err)
	}
	return client
}

func TestContainerStatsParsesSupportedSections(t *testing.T) {
	client := statsServer(t, `{
		"cpu_stats": {"cpu_usage": {"total_usage": 400}, "system_cpu_usage": 40000, "online_cpus": 4},
		"memory_stats": {"usage": 1000, "limit": 4096, "stats": {"inactive_file": 200}},
		"networks": {"eth0": {"rx_bytes": 10, "tx_bytes": 20}},
		"blkio_stats": {"io_service_bytes_recursive": [
			{"op": "read", "value": 100},
			{"op": "write", "value": 200}
		]}
	}`)
	stats, err := client.ContainerStats(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ContainerStats: %v", err)
	}
	if !stats.MemorySupported || !stats.NetSupported || !stats.DiskSupported {
		t.Errorf("supported = memory:%v net:%v disk:%v; want all true", stats.MemorySupported, stats.NetSupported, stats.DiskSupported)
	}
	if stats.MemoryUsage != 1000 || stats.MemoryCache != 200 || stats.MemoryLimit != 4096 {
		t.Errorf("memory = %+v", stats)
	}
	if stats.NetRx != 10 || stats.NetTx != 20 || stats.DiskRead != 100 || stats.DiskWrite != 200 {
		t.Errorf("counters = %+v", stats)
	}
}

func TestContainerStatsMarksMissingSectionsUnsupported(t *testing.T) {
	client := statsServer(t, `{
		"cpu_stats": {"cpu_usage": {"total_usage": 400}, "system_cpu_usage": 40000, "online_cpus": 4}
	}`)
	stats, err := client.ContainerStats(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ContainerStats: %v", err)
	}
	if stats.MemorySupported || stats.NetSupported || stats.DiskSupported {
		t.Errorf("missing sections = memory:%v net:%v disk:%v; want all false", stats.MemorySupported, stats.NetSupported, stats.DiskSupported)
	}
}

func TestContainerStatsEmptyMemoryObjectIsUnsupported(t *testing.T) {
	client := statsServer(t, `{
		"cpu_stats": {"cpu_usage": {"total_usage": 400}, "system_cpu_usage": 40000, "online_cpus": 4},
		"memory_stats": {}
	}`)
	stats, err := client.ContainerStats(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ContainerStats: %v", err)
	}
	if stats.MemorySupported {
		t.Error("empty memory_stats object counted as measured memory")
	}
}
