package agent

import "testing"

func TestNewDockerTransport(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		wantBase string
		wantErr  bool
	}{
		{"empty uses default socket", "", "http://docker", false},
		{"unix scheme", "unix:///var/run/docker.sock", "http://docker", false},
		{"bare path", "/var/run/docker.sock", "http://docker", false},
		{"tcp scheme", "tcp://127.0.0.1:2375", "http://127.0.0.1:2375", false},
		{"http scheme", "http://127.0.0.1:2375", "http://127.0.0.1:2375", false},
		{"unsupported scheme", "npipe:////./pipe/docker", "", true},
		{"empty unix path", "unix://", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, base, err := newDockerTransport(tt.target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("newDockerTransport(%q) error = %v; wantErr=%v", tt.target, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if transport == nil {
				t.Error("transport is nil")
			}
			if base != tt.wantBase {
				t.Errorf("base = %q; want %q", base, tt.wantBase)
			}
		})
	}
}
