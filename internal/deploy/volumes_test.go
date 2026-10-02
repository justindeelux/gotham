package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// TestValidateHostBind pins the confinement rule: an explicit bind must be a
// direct child of <root>/<appID>, known-dangerous targets are always rejected,
// and a named volume (non-absolute) or empty managed path is accepted.
func TestValidateHostBind(t *testing.T) {
	root := t.TempDir()
	appID := uuid.New()
	appDir := filepath.Join(root, appID.String())

	cases := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{name: "direct child of the app directory", host: filepath.Join(appDir, "data"), wantErr: false},
		{name: "app directory itself", host: appDir, wantErr: true},
		{name: "nested child", host: filepath.Join(appDir, "a", "b"), wantErr: true},
		{name: "absolute path outside the root", host: "/data/app", wantErr: true},
		{name: "sibling of the app directory", host: filepath.Join(root, uuid.New().String(), "data"), wantErr: true},
		{name: "root itself", host: "/", wantErr: true},
		{name: "etc", host: "/etc", wantErr: true},
		{name: "proc", host: "/proc", wantErr: true},
		{name: "sys", host: "/sys", wantErr: true},
		{name: "dev", host: "/dev", wantErr: true},
		{name: "docker socket", host: "/var/run/docker.sock", wantErr: true},
		{name: "docker socket under another directory", host: "/srv/docker.sock", wantErr: true},
		{name: "dotdot traversal out of the app directory", host: filepath.Join(appDir, "..", "..", "etc"), wantErr: true},
		{name: "named volume", host: "shared", wantErr: false},
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

// TestValidateHostBindRejectsSymlinks pins the fail-closed walk: a symlink at
// the bind path or at any component (including the app directory itself) is
// refused, so Docker never resolves it to an arbitrary host location.
func TestValidateHostBindRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	appID := uuid.New()
	appDir := filepath.Join(root, appID.String())
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Symlink at the bind path itself (dangling and live).
	dangling := filepath.Join(appDir, "dangling")
	if err := os.Symlink("/nonexistent", dangling); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := validateHostBind(root, appID, "data", dangling); !errors.Is(err, ErrValidation) {
		t.Fatalf("dangling symlink err = %v, want ErrValidation", err)
	}
	live := filepath.Join(appDir, "live")
	if err := os.Symlink("/etc", live); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := validateHostBind(root, appID, "data", live); !errors.Is(err, ErrValidation) {
		t.Fatalf("live symlink err = %v, want ErrValidation", err)
	}

	// The app directory itself replaced by a symlink to elsewhere.
	otherRoot := t.TempDir()
	otherApp := uuid.New()
	if err := os.Symlink("/etc", filepath.Join(otherRoot, otherApp.String())); err != nil {
		t.Fatalf("symlink app dir: %v", err)
	}
	if err := validateHostBind(otherRoot, otherApp, "data", filepath.Join(otherRoot, otherApp.String(), "child")); !errors.Is(err, ErrValidation) {
		t.Fatalf("symlinked app dir err = %v, want ErrValidation", err)
	}
}

// TestValidateHostBindFailsClosedOnLstatError pins that a non-ENOENT Lstat
// error (here ENOTDIR because the app path is a regular file) is refused, not
// treated as "does not exist".
func TestValidateHostBindFailsClosedOnLstatError(t *testing.T) {
	root := t.TempDir()
	appID := uuid.New()
	appDir := filepath.Join(root, appID.String())
	if err := os.WriteFile(appDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := validateHostBind(root, appID, "data", filepath.Join(appDir, "child")); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for an unreadable/non-directory component", err)
	}
}

// TestManagedHostPath pins the managed default, the per-application named
// volume namespace and the pass-through of an explicit managed child.
func TestManagedHostPath(t *testing.T) {
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
	if want := appNamedVolumePrefix + appID.String() + "-shared-cache"; named != want {
		t.Errorf("named = %q, want %q", named, want)
	}

	child := filepath.Join(root, appID.String(), "data")
	explicit, err := managedHostPath(root, appID, "data", child)
	if err != nil {
		t.Fatalf("explicit child: %v", err)
	}
	if explicit != child {
		t.Errorf("explicit = %q, want %q", explicit, child)
	}
}
