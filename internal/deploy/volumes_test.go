package deploy

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// TestStorageNormalizationIsIdempotent pins P1: feeding an already-normalized
// row (as a GET→PUT round-trip does) back through normalizeStorages must not
// prefix a named volume a second time or move a derived bind.
func TestStorageNormalizationIsIdempotent(t *testing.T) {
	appID := uuid.New()
	root := t.TempDir()

	named, err := normalizeStorages(appID, []Storage{{Name: "data", HostPath: "cache", ContainerPath: "/var/cache"}})
	if err != nil {
		t.Fatalf("normalize named: %v", err)
	}
	if want := appNamedVolumePrefix + appID.String() + "-cache"; named[0].HostPath != want {
		t.Fatalf("named host = %q, want %q", named[0].HostPath, want)
	}
	again, err := normalizeStorages(appID, named)
	if err != nil {
		t.Fatalf("renormalize named: %v", err)
	}
	if again[0].HostPath != named[0].HostPath {
		t.Fatalf("named round-trip doubled the prefix: %q -> %q", named[0].HostPath, again[0].HostPath)
	}
	specs, err := volumeSpecs(root, appID, again)
	if err != nil {
		t.Fatalf("volumeSpecs: %v", err)
	}
	if want := named[0].HostPath + ":/var/cache"; len(specs) != 1 || specs[0] != want {
		t.Fatalf("specs = %v, want [%s]", specs, want)
	}

	derived, err := normalizeStorages(appID, []Storage{{Name: "data", HostPath: "", ContainerPath: "/data"}})
	if err != nil {
		t.Fatalf("normalize derived: %v", err)
	}
	derivedAgain, err := normalizeStorages(appID, derived)
	if err != nil {
		t.Fatalf("renormalize derived: %v", err)
	}
	if derivedAgain[0].HostPath != derived[0].HostPath {
		t.Fatalf("derived round-trip moved the path: %q -> %q", derived[0].HostPath, derivedAgain[0].HostPath)
	}
}

// TestWarnStorageChanges pins P3/P4: a blanked explicit path and a re-namespaced
// named volume warn, while a re-save that resolves to the stored derived path
// does not.
func TestWarnStorageChanges(t *testing.T) {
	newService := func(t *testing.T, repo *fakeRepository) (*Service, *bytes.Buffer) {
		t.Helper()
		var logs bytes.Buffer
		svc := NewService(Config{
			Repository: repo,
			Secret:     testSecretKey,
			Logger:     slog.New(slog.NewTextHandler(&logs, nil)),
		})
		t.Cleanup(func() { _ = svc.Close() })
		return svc, &logs
	}

	t.Run("explicit path blanked warns", func(t *testing.T) {
		userID := uuid.New()
		app := testApplication(userID)
		repo := &fakeRepository{app: app}
		repo.storages = []Storage{{ApplicationID: app.ID, Name: "data", HostPath: "/data/app", ContainerPath: "/data"}}
		svc, logs := newService(t, repo)

		if _, err := svc.ReplaceStorages(context.Background(), userID, app.ID, []Storage{{Name: "data", HostPath: "", ContainerPath: "/data"}}); err != nil {
			t.Fatalf("replace: %v", err)
		}
		if !strings.Contains(logs.String(), "storage host path blanked") {
			t.Errorf("logs = %q, want a blanked-path warning", logs.String())
		}
	})

	t.Run("unchanged derived path does not warn", func(t *testing.T) {
		userID := uuid.New()
		app := testApplication(userID)
		repo := &fakeRepository{app: app}
		derived := filepath.Join(managedVolumeRoot(), app.ID.String(), "data")
		repo.storages = []Storage{{ApplicationID: app.ID, Name: "data", HostPath: derived, ContainerPath: "/data"}}
		svc, logs := newService(t, repo)

		if _, err := svc.ReplaceStorages(context.Background(), userID, app.ID, []Storage{{Name: "data", HostPath: "", ContainerPath: "/data"}}); err != nil {
			t.Fatalf("replace: %v", err)
		}
		if strings.Contains(logs.String(), "storage host path blanked") {
			t.Errorf("logs = %q, want no warning when the derived path is unchanged", logs.String())
		}
	})

	t.Run("bare named volume renaming warns", func(t *testing.T) {
		userID := uuid.New()
		app := testApplication(userID)
		repo := &fakeRepository{app: app}
		repo.storages = []Storage{{ApplicationID: app.ID, Name: "data", HostPath: "cache", ContainerPath: "/data"}}
		svc, logs := newService(t, repo)

		if _, err := svc.ReplaceStorages(context.Background(), userID, app.ID, []Storage{{Name: "data", HostPath: "cache", ContainerPath: "/data"}}); err != nil {
			t.Fatalf("replace: %v", err)
		}
		if !strings.Contains(logs.String(), "named volume renamed") {
			t.Errorf("logs = %q, want a named-volume rename warning", logs.String())
		}
	})
}

// TestWarnManagedVolumeRoot pins L6/P7: the control plane warns when the root
// is unset or is set to a relative value.
func TestWarnManagedVolumeRoot(t *testing.T) {
	record := func(t *testing.T) (*slog.Logger, *bytes.Buffer) {
		t.Helper()
		var logs bytes.Buffer
		return slog.New(slog.NewTextHandler(&logs, nil)), &logs
	}

	t.Run("unset warns", func(t *testing.T) {
		t.Setenv(envManagedVolumeRoot, "")
		logger, logs := record(t)
		warnManagedVolumeRoot(logger)
		if !strings.Contains(logs.String(), "is unset") {
			t.Errorf("logs = %q, want an unset warning", logs.String())
		}
	})

	t.Run("relative warns", func(t *testing.T) {
		t.Setenv(envManagedVolumeRoot, "data/volumes")
		logger, logs := record(t)
		warnManagedVolumeRoot(logger)
		if !strings.Contains(logs.String(), "must be absolute") {
			t.Errorf("logs = %q, want a relative-value warning", logs.String())
		}
	})

	t.Run("absolute does not warn", func(t *testing.T) {
		t.Setenv(envManagedVolumeRoot, "/srv/gotham/volumes")
		logger, logs := record(t)
		warnManagedVolumeRoot(logger)
		if logs.Len() != 0 {
			t.Errorf("logs = %q, want none for an absolute root", logs.String())
		}
	})
}

// TestManagedHostPathRejectsForgedNamespace pins Q1: a value that already
// carries this application's prefix is accepted only when its suffix is
// sanitized; a forged suffix (a colon, a space, an empty or traversal suffix)
// is rejected instead of reaching the Docker spec verbatim.
func TestManagedHostPathRejectsForgedNamespace(t *testing.T) {
	root := t.TempDir()
	appID := uuid.New()
	prefix := appNamedVolumePrefix + appID.String() + "-"

	for _, host := range []string{prefix + "x:/etc", prefix + "a b", prefix, prefix + ".."} {
		if _, err := managedHostPath(root, appID, "data", host); !errors.Is(err, ErrValidation) {
			t.Errorf("managedHostPath(%q) err = %v, want ErrValidation", host, err)
		}
		if _, err := normalizeStorages(appID, []Storage{{Name: "data", HostPath: host, ContainerPath: "/data"}}); !errors.Is(err, ErrValidation) {
			t.Errorf("normalizeStorages(%q) err = %v, want ErrValidation", host, err)
		}
	}

	sanitized := prefix + "a-b"
	got, err := managedHostPath(root, appID, "data", sanitized)
	if err != nil || got != sanitized {
		t.Errorf("sanitized = %q, %v; want it unchanged", got, err)
	}
}

// TestWarnLegacyStorageResolution pins Q3: a pre-change bare named volume warns
// once at deploy, while an already-normalized row does not.
func TestWarnLegacyStorageResolution(t *testing.T) {
	appID := uuid.New()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	warnLegacyStorageResolution(logger, appID, []Storage{{Name: "data", HostPath: "cache"}})
	if !strings.Contains(logs.String(), "re-namespaced at deploy") {
		t.Errorf("logs = %q, want a deploy-time rename warning", logs.String())
	}

	logs.Reset()
	normalized := appNamedVolumePrefix + appID.String() + "-cache"
	warnLegacyStorageResolution(logger, appID, []Storage{{Name: "data", HostPath: normalized}})
	if logs.Len() != 0 {
		t.Errorf("logs = %q, want none for an already-normalized row", logs.String())
	}
}
