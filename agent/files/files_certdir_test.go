package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func TestFileProtectedPathsProtectsCertSecrets(t *testing.T) {
	t.Parallel()
	cfg := Config{
		CertDir:    "/srv/agent/certs",
		KeyFile:    "/srv/keys/agent.key",
		CA:         "/srv/ca/ca.pem",
		BinaryPath: "/srv/agent/bin/gotham-agent",
	}
	paths := fileProtectedPaths(cfg)
	if certDir := findProtectedPath(t, paths, cfg.CertDir); certDir.exclusive {
		t.Errorf("cert dir %q is exclusive, want non-exclusive like the state root", cfg.CertDir)
	}
	for _, want := range []string{
		filepath.Join(cfg.CertDir, certFileName),
		filepath.Join(cfg.CertDir, keyFileName),
		cfg.KeyFile,
		cfg.CA,
	} {
		if p := findProtectedPath(t, paths, want); p.exclusive {
			t.Errorf("secret file %q is exclusive, want non-exclusive", want)
		}
	}
}

func TestValidateFileRootCertDirComposeChild(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	certDir := filepath.Join(base, "agent")
	compose := filepath.Join(certDir, "compose")
	if err := os.MkdirAll(compose, 0o755); err != nil {
		t.Fatal(err)
	}
	protected := fileProtectedPaths(Config{CertDir: certDir, ComposeRoot: compose})

	if _, _, err := validateFileRoot(compose, protected); err != nil {
		t.Errorf("validateFileRoot(compose under cert dir) = %v, want nil (this is the startup abort)", err)
	}
	for _, dir := range []string{certDir, base} {
		if _, _, err := validateFileRoot(dir, protected); err == nil {
			t.Errorf("validateFileRoot(%q) = nil, want an error", dir)
		}
	}
}

func TestNewFileServerDefaultCertDirStarts(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	certDir := filepath.Join(base, "gotham-agent")
	compose := filepath.Join(certDir, "compose")
	volumes := filepath.Join(base, "gotham", "volumes")
	for _, dir := range []string{compose, volumes} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{certFileName, keyFileName} {
		if err := os.WriteFile(filepath.Join(certDir, name), []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := Config{ManagedVolumeRoot: volumes, ComposeRoot: compose, CertDir: certDir}

	server, err := NewFileServer(cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewFileServer(default cert dir layout) = %v, want nil", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	resp, err := server.ListRoots(context.Background(), &agentv1.ListRootsRequest{})
	if err != nil {
		t.Fatalf("ListRoots: %v", err)
	}
	if len(resp.GetRoots()) != 2 {
		t.Fatalf("roots = %d, want 2 (volumes + compose)", len(resp.GetRoots()))
	}
	realCompose, err := filepath.EvalSymlinks(compose)
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.GetRoots()[1]; got.GetName() != RootCompose || got.GetPath() != realCompose || got.GetWritable() {
		t.Fatalf("compose root = %+v, want read-only %q", got, realCompose)
	}
}

func TestFileServiceCertSecretsUnreachable(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	certDir := filepath.Join(base, "agent")
	compose := filepath.Join(certDir, "compose")
	if err := os.MkdirAll(compose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certDir, keyFileName), []byte("private-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ManagedVolumeRoot: t.TempDir(), ComposeRoot: compose, CertDir: certDir}
	server, err := NewFileServer(cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewFileServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	ctx := context.Background()

	t.Run("traversal", func(t *testing.T) {
		if _, err := server.Stat(ctx, &agentv1.StatRequest{Root: RootCompose, Path: "../" + keyFileName}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Stat(../%s) code = %v, want InvalidArgument", keyFileName, status.Code(err))
		}
	})

	t.Run("symlink file", func(t *testing.T) {
		link := filepath.Join(compose, "leak")
		if err := os.Symlink(filepath.Join("..", keyFileName), link); err != nil {
			t.Fatal(err)
		}
		if _, _, err := server.roots[RootCompose].OpenRegular("leak"); !errors.Is(err, fsroot.ErrEscape) {
			t.Fatalf("OpenRegular(leak) = %v, want ErrEscape", err)
		}
		if err := server.Read(&agentv1.ReadRequest{Root: RootCompose, Path: "leak"}, &collectReadStream{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("Read(leak) code = %v, want PermissionDenied", status.Code(err))
		}
	})

	t.Run("symlink directory", func(t *testing.T) {
		if err := os.Symlink("..", filepath.Join(compose, "up")); err != nil {
			t.Fatal(err)
		}
		if _, err := server.List(ctx, &agentv1.ListRequest{Root: RootCompose, Path: "up"}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("List(up) code = %v, want PermissionDenied", status.Code(err))
		}
	})
}

func TestFileServiceConfinedAfterParentRename(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	certDir := filepath.Join(base, "agent")
	compose := filepath.Join(certDir, "compose")
	if err := os.MkdirAll(compose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certDir, keyFileName), []byte("private-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(compose, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Config{ManagedVolumeRoot: t.TempDir(), ComposeRoot: compose, CertDir: certDir}
	server, err := NewFileServer(cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewFileServer: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	renamed := certDir + "-renamed"
	if err := os.Rename(certDir, renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(renamed, certDir); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	list, err := server.List(ctx, &agentv1.ListRequest{Root: RootCompose})
	if err != nil {
		t.Fatalf("List after parent rename: %v", err)
	}
	var names []string
	for _, entry := range list.GetEntries() {
		names = append(names, entry.GetName())
	}
	if !reflect.DeepEqual(names, []string{"keep.txt"}) {
		t.Fatalf("List after parent rename = %v, want [keep.txt]", names)
	}
	if _, err := server.Stat(ctx, &agentv1.StatRequest{Root: RootCompose, Path: keyFileName}); status.Code(err) != codes.NotFound {
		t.Fatalf("Stat(%s) code = %v, want NotFound", keyFileName, status.Code(err))
	}
	if err := server.Read(&agentv1.ReadRequest{Root: RootCompose, Path: "../" + keyFileName}, &collectReadStream{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Read(../%s) code = %v, want InvalidArgument", keyFileName, status.Code(err))
	}
}

func findProtectedPath(t *testing.T, paths []protectedPath, want string) protectedPath {
	t.Helper()
	for _, p := range paths {
		if p.path == want {
			return p
		}
	}
	t.Fatalf("protected paths do not include %q", want)
	return protectedPath{}
}
