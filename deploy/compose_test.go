package deploy

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDevComposePortsAreLoopback pins that the dev databases are published on
// the loopback interface only, so a dev machine on an untrusted network does
// not expose Postgres or Redis.
func TestDevComposePortsAreLoopback(t *testing.T) {
	data, err := os.ReadFile("compose.dev.yml")
	if err != nil {
		t.Fatalf("read compose.dev.yml: %v", err)
	}

	var doc struct {
		Services map[string]struct {
			Ports []string `yaml:"ports"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse compose.dev.yml: %v", err)
	}

	for _, name := range []string{"postgres", "redis"} {
		svc, ok := doc.Services[name]
		if !ok {
			t.Fatalf("service %q missing from compose.dev.yml", name)
		}
		if len(svc.Ports) == 0 {
			t.Fatalf("service %q publishes no ports", name)
		}
		for _, port := range svc.Ports {
			if !strings.HasPrefix(port, "127.0.0.1:") {
				t.Errorf("service %q port %q is not loopback-bound", name, port)
			}
		}
	}
}
