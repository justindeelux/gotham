package deploy

import (
	"os"
	"testing"
)

// TestEmbeddedScriptsMatchDeployFiles is the drift guard for the go:embed
// assets: the bytes compiled into the binary must equal the files on disk, so
// `gotham host-bootstrap` installs exactly the reviewed scripts.
func TestEmbeddedScriptsMatchDeployFiles(t *testing.T) {
	for _, tc := range []struct {
		file string
		got  []byte
	}{
		{"gotham-hostctl.sh", HostctlScript},
		{"gotham-toolctl.sh", ToolctlScript},
	} {
		want, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		if string(tc.got) != string(want) {
			t.Errorf("embedded %s differs from deploy/%s (rebuild the binary)", tc.file, tc.file)
		}
	}
}
