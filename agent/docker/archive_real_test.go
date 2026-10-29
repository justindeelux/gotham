package docker

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// e2eVolume runs one short-lived helper container with the given volumes and
// script, then returns its stdout.
func e2eVolume(t *testing.T, client *DockerClient, volumes []string, script string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	request := &agentv1.CreateContainerRequest{
		Image:   "alpine:3.20",
		Command: []string{"sh", "-c", script},
		Volumes: volumes,
	}
	id, err := client.RunImage(ctx, request)
	if errors.Is(err, ErrDockerImageNotFound) {
		if _, pullErr := client.PullImage(ctx, "alpine:3.20", "", ""); pullErr != nil {
			t.Fatalf("pull helper image: %v", pullErr)
		}
		id, err = client.RunImage(ctx, request)
	}
	if err != nil {
		t.Fatalf("run helper: %v", err)
	}
	defer func() { _ = client.Remove(context.Background(), id) }()
	for {
		details, err := client.InspectContainer(ctx, id)
		if err != nil {
			t.Fatalf("inspect helper: %v", err)
		}
		if details.State == "exited" {
			if details.ExitCode != 0 {
				t.Fatalf("helper exited %d: %s", details.ExitCode, e2eLogs(ctx, client, id))
			}
			return e2eLogs(ctx, client, id)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func e2eLogs(ctx context.Context, client *DockerClient, id string) string {
	chunks, err := client.Logs(ctx, id, false, 0)
	if err != nil {
		return ""
	}
	var builder strings.Builder
	for message := range chunks {
		if message.Err != nil {
			break
		}
		builder.Write(message.Data)
	}
	return builder.String()
}

func e2eVolumeName(t *testing.T, client *DockerClient) string {
	t.Helper()
	name := "gotham-e2e-archive-" + uuid.NewString()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := client.RemoveVolume(ctx, name); err != nil {
			t.Logf("remove volume %s: %v", name, err)
		}
	})
	return name
}

func TestVolumeArchiveRoundTripE2E(t *testing.T) {
	client := e2eContainerClient(t)
	if _, err := client.PullImage(context.Background(), "alpine:3.20", "", ""); err != nil {
		t.Logf("pull helper image: %v (using a local image if present)", err)
	}
	service := newArchiveServiceClient(t, client)

	sourceVolume := e2eVolumeName(t, client)
	bindSource := t.TempDir()
	e2eVolume(t, client, []string{sourceVolume + ":/data"}, `
set -eu
mkdir -p /data/sub
printf 'hello volume' > /data/sub/a.txt
chmod 640 /data/sub/a.txt
printf 'executable' > /data/run.sh
chmod 750 /data/run.sh
ln -s sub/a.txt /data/link
touch -t 202001020304.05 /data/sub/a.txt
`)
	megabytes := 4
	if value := os.Getenv("GOTHAM_ARCHIVE_E2E_MB"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("GOTHAM_ARCHIVE_E2E_MB: %v", err)
		}
		megabytes = parsed
	}
	e2eVolume(t, client, []string{sourceVolume + ":/data"}, fmt.Sprintf(
		"dd if=/dev/urandom of=/data/big bs=1M count=%d status=none", megabytes))
	if err := os.WriteFile(filepath.Join(bindSource, "bind.txt"), []byte("hello bind"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/", filepath.Join(bindSource, "escape")); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	stream, err := service.ArchiveVolumes(ctx, &agentv1.ArchiveVolumesRequest{
		Sources: []*agentv1.ArchiveSource{
			{Name: "data", Source: &agentv1.ArchiveSource_Volume{Volume: sourceVolume}},
			{Name: "bind", Source: &agentv1.ArchiveSource_Path{Path: bindSource}},
		},
	})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Sample the agent's heap while the archive streams past it.
	var maxHeap atomic.Uint64
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				var stats runtime.MemStats
				runtime.ReadMemStats(&stats)
				if stats.HeapAlloc > maxHeap.Load() {
					maxHeap.Store(stats.HeapAlloc)
				}
			}
		}
	}()

	archive, err := os.CreateTemp(t.TempDir(), "archive-*.tar.gz")
	if err != nil {
		t.Fatalf("create archive file: %v", err)
	}
	archiveHasher := sha256.New()
	var streamed int64
	var result *agentv1.ArchiveResult
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			close(done)
			t.Fatalf("recv: %v", err)
		}
		if data := chunk.GetData(); len(data) > 0 {
			n, writeErr := archive.Write(data)
			streamed += int64(n)
			archiveHasher.Write(data[:n])
			if writeErr != nil {
				close(done)
				t.Fatalf("write archive: %v", writeErr)
			}
		}
		if final := chunk.GetResult(); final != nil {
			result = final
		}
	}
	close(done)
	if result == nil || !result.GetOk() {
		t.Fatalf("result = %v (stderr %q)", result, result.GetStderrTail())
	}
	info, err := archive.Stat()
	if err != nil {
		t.Fatalf("stat archive: %v", err)
	}
	digest := hex.EncodeToString(archiveHasher.Sum(nil))
	growth := int64(maxHeap.Load()) - int64(base.HeapAlloc)
	t.Logf("archive size %d bytes (streamed %d, reported %d, sha256 %s); agent heap growth %d bytes",
		info.Size(), streamed, result.GetBytes(), digest, growth)
	if info.Size() != streamed || uint64(streamed) != result.GetBytes() {
		t.Fatalf("streamed %d bytes, file %d, reported %d", streamed, info.Size(), result.GetBytes())
	}
	if digest != result.GetSha256() {
		t.Fatalf("streamed sha256 %s; helper reported %s", digest, result.GetSha256())
	}
	// The helper runs with json-file max-size=64k: if archive bytes still went
	// through the log driver, this incompressible archive could not survive.
	if growth > 64<<20 {
		t.Fatalf("agent heap grew by %d bytes; want bounded memory", growth)
	}

	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rewind archive: %v", err)
	}
	entries := readArchive(t, archive)
	if len(entries) == 0 || entries[0].name != "manifest.json" {
		t.Fatalf("first archive entry = %q; want the manifest", entries[0].name)
	}
	if entries[0].body == nil || !bytes.Contains(entries[0].body, []byte(`"format_version":1`)) {
		t.Fatalf("manifest = %q", entries[0].body)
	}
	if !bytes.Contains(entries[0].body, []byte(`"data"`)) || !bytes.Contains(entries[0].body, []byte(`"bind"`)) {
		t.Fatalf("manifest does not name both sources: %q", entries[0].body)
	}

	targetVolume := e2eVolumeName(t, client)
	e2eVolume(t, client, []string{targetVolume + ":/data"}, "mkdir -p /data")
	targetBind := t.TempDir()
	restore, err := service.RestoreVolumes(ctx)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := restore.Send(&agentv1.RestoreVolumesRequest{Part: &agentv1.RestoreVolumesRequest_Header{
		Header: &agentv1.RestoreVolumesHeader{
			Targets: []*agentv1.ArchiveSource{
				{Name: "data", Source: &agentv1.ArchiveSource_Volume{Volume: targetVolume}},
				{Name: "bind", Source: &agentv1.ArchiveSource_Path{Path: targetBind}},
			},
			Mode: agentv1.RestoreMode_RESTORE_MODE_REPLACE,
		},
	}}); err != nil {
		t.Fatalf("send header: %v", err)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rewind archive: %v", err)
	}
	buffer := make([]byte, archiveChunkSize)
	for {
		n, readErr := archive.Read(buffer)
		if n > 0 {
			if err := restore.Send(&agentv1.RestoreVolumesRequest{Part: &agentv1.RestoreVolumesRequest_Data{Data: buffer[:n]}}); err != nil {
				t.Fatalf("send data: %v", err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatalf("read archive: %v", readErr)
		}
	}
	if _, err := restore.CloseAndRecv(); err != nil {
		t.Fatalf("close and recv: %v", err)
	}

	volumeList := e2eVolume(t, client, []string{sourceVolume + ":/a:ro", targetVolume + ":/b"}, `
set -eu
cd /a
find . -type f -exec sha256sum {} \; | sort
echo ---
find . -type f -exec stat -c '%n %a %Y %u %g' {} \; | sort
echo ---
cd /b
find . -type f -exec sha256sum {} \; | sort
echo ---
find . -type f -exec stat -c '%n %a %Y %u %g' {} \; | sort
echo ---
readlink /b/link
`)
	halves := strings.Split(volumeList, "---\n")
	if len(halves) < 5 {
		t.Fatalf("comparison output = %q", volumeList)
	}
	if halves[0] != halves[2] || halves[1] != halves[3] {
		t.Fatalf("restored volume differs:\nsource hashes %q\nrestored hashes %q\nsource stat %q\nrestored stat %q",
			halves[0], halves[2], halves[1], halves[3])
	}
	if strings.TrimSpace(halves[4]) != "sub/a.txt" {
		t.Fatalf("restored symlink -> %q", halves[4])
	}
	restored, err := os.ReadFile(filepath.Join(targetBind, "bind.txt"))
	if err != nil || string(restored) != "hello bind" {
		t.Fatalf("restored bind content = %q, err = %v", restored, err)
	}
	if info, err := os.Lstat(filepath.Join(targetBind, "escape")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("escaping symlink was not preserved as a link: %v %v", info, err)
	}
}

func TestVolumeArchiveCancellationLeavesNothingBehindE2E(t *testing.T) {
	client := e2eContainerClient(t)
	service := newArchiveServiceClient(t, client)
	if _, err := client.PullImage(context.Background(), "alpine:3.20", "", ""); err != nil {
		t.Logf("pull helper image: %v", err)
	}

	volume := e2eVolumeName(t, client)
	e2eVolume(t, client, []string{volume + ":/data"}, `
set -eu
dd if=/dev/zero of=/data/big bs=1M count=64 status=none
`)

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := service.ArchiveVolumes(ctx, &agentv1.ArchiveVolumesRequest{
		Sources: []*agentv1.ArchiveSource{{Name: "data", Source: &agentv1.ArchiveSource_Volume{Volume: volume}}},
	})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	cancel()
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}
	waitNoArchiveHelpers(t, client)

	// A cancelled restore must not leave its staged archive behind.
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	restoreCtx, restoreCancel := context.WithCancel(context.Background())
	restore, err := service.RestoreVolumes(restoreCtx)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := restore.Send(&agentv1.RestoreVolumesRequest{Part: &agentv1.RestoreVolumesRequest_Header{
		Header: &agentv1.RestoreVolumesHeader{
			Targets: []*agentv1.ArchiveSource{{Name: "data", Source: &agentv1.ArchiveSource_Volume{Volume: volume}}},
			Mode:    agentv1.RestoreMode_RESTORE_MODE_REPLACE,
		},
	}}); err != nil {
		t.Fatalf("send header: %v", err)
	}
	if err := restore.Send(&agentv1.RestoreVolumesRequest{Part: &agentv1.RestoreVolumesRequest_Data{
		Data: bytes.Repeat([]byte("garbage"), 1024),
	}}); err != nil {
		t.Fatalf("send data: %v", err)
	}
	restoreCancel()
	if _, err := restore.CloseAndRecv(); err == nil {
		t.Fatal("cancelled restore reported success")
	}
	waitNoTemporaryArchives(t, tmp)
}

func waitNoArchiveHelpers(t *testing.T, client *DockerClient) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		containers, err := client.ListContainers(ctx, true, nil, false)
		cancel()
		if err != nil {
			t.Fatalf("list containers: %v", err)
		}
		found := false
		for _, container := range containers {
			if strings.HasPrefix(container.GetName(), "gotham-archive-") {
				found = true
				break
			}
		}
		if !found {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("a cancelled archive left its helper container behind")
}

func waitNoTemporaryArchives(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read temp dir: %v", err)
		}
		if len(entries) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	entries, _ := os.ReadDir(dir)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	t.Fatalf("a cancelled restore left temp files behind: %v", names)
}

type archiveEntry struct {
	name string
	body []byte
	size int64
}

func readArchive(t *testing.T, data io.Reader) []archiveEntry {
	t.Helper()
	compressed, err := gzip.NewReader(data)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer func() { _ = compressed.Close() }()
	reader := tar.NewReader(compressed)
	var entries []archiveEntry
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		entry := archiveEntry{name: header.Name, size: header.Size}
		// Only the manifest is small enough to inspect in memory; the rest of
		// a backup can be gigabytes.
		if header.Typeflag == tar.TypeReg && header.Name == "manifest.json" {
			body, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("read %s: %v", header.Name, err)
			}
			entry.body = body
		}
		entries = append(entries, entry)
	}
}
