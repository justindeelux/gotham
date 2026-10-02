package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// TestValidateHostBind pins the confinement rule: a host bind must resolve
// inside <root>/<appID>, known-dangerous targets are always rejected, and a
// named volume (non-absolute) is not a bind so it is left alone.
func TestValidateHostBind(t *testing.T) {
	root := t.TempDir()
	appID := uuid.New()
	appDir := filepath.Join(root, appID.String())

	cases := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{name: "managed app directory", host: appDir, wantErr: false},
		{name: "descendant of the app directory", host: filepath.Join(appDir, "data"), wantErr: false},
		{name: "absolute path outside the root", host: "/data/app", wantErr: true},
		{name: "sibling of the app directory", host: filepath.Join(root, uuid.New().String()), wantErr: true},
		{name: "root itself", host: "/", wantErr: true},
		{name: "etc", host: "/etc", wantErr: true},
		{name: "proc", host: "/proc", wantErr: true},
		{name: "sys", host: "/sys", wantErr: true},
		{name: "dev", host: "/dev", wantErr: true},
		{name: "docker socket", host: "/var/run/docker.sock", wantErr: true},
		{name: "docker socket under another directory", host: "/srv/docker.sock", wantErr: true},
		{name: "dotdot traversal out of the app directory", host: filepath.Join(appDir, "..", "..", "etc"), wantErr: true},
		{name: "named volume", host: "gotham-data", wantErr: false},
		{name: "empty managed path", host: "", wantErr: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateHostBind(root, appID, "data", tc.host)
			if tc.wantErr && !errors.Is(err, ErrValidation) {
				t.Fatalf("validateHostBind(%q) = %v, want ErrValidation", tc.host, err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateHostBind(%q) = %v, want accepted", tc.host, err)
			}
		})
	}
}

// TestValidateHostBindRejectsSymlinkEscape pins the resolved-path check: a
// symlink inside the app directory that points elsewhere would be followed by
// Docker at mount time.
func TestValidateHostBindRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	appID := uuid.New()
	appDir := filepath.Join(root, appID.String())
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(appDir, "escape")
	if err := os.Symlink("/etc", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := validateHostBind(root, appID, "data", link); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for a symlink escaping the app root", err)
	}
}

// TestManagedHostPathDerives pins the default managed path and the named
// volume pass-through.
func TestManagedHostPathDerives(t *testing.T) {
	root := t.TempDir()
	appID := uuid.New()

	derived, err := managedHostPath(root, appID, "uploads", "")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if want := filepath.Join(root, appID.String(), "uploads"); derived != want {
		t.Errorf("derived = %q, want %q", derived, want)
	}

	named, err := managedHostPath(root, appID, "data", "shared-cache")
	if err != nil {
		t.Fatalf("named volume: %v", err)
	}
	if named != "shared-cache" {
		t.Errorf("named = %q, want it unchanged", named)
	}
}
