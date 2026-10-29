package docker

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func newArchiveServiceClient(t *testing.T, docker Client, options ...DockerServerOption) agentv1.DockerServiceClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	agentv1.RegisterDockerServiceServer(server, NewDockerServer(docker, discardLogger(), options...))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return agentv1.NewDockerServiceClient(conn)
}

func TestVolumeArchiveCapabilities(t *testing.T) {
	client := newArchiveServiceClient(t, &fakeDockerClient{})
	response, err := client.VolumeArchiveCapabilities(context.Background(), &agentv1.VolumeArchiveCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("VolumeArchiveCapabilities: %v", err)
	}
	if !response.GetAvailable() {
		t.Fatal("available = false")
	}
	if len(response.GetCapabilities()) != 1 || response.GetCapabilities()[0] != volumeArchiveCapability {
		t.Fatalf("capabilities = %v", response.GetCapabilities())
	}
}

func TestArchiveVolumesStreamsStdoutAndIgnoresStderr(t *testing.T) {
	fake := &fakeDockerClient{logs: []byte("tar-bytes"), logsStderr: []byte("tar: warning")}
	client := newArchiveServiceClient(t, fake)

	stream, err := client.ArchiveVolumes(context.Background(), &agentv1.ArchiveVolumesRequest{
		Sources: []*agentv1.ArchiveSource{bindPathSource(t.TempDir())},
	})
	if err != nil {
		t.Fatalf("ArchiveVolumes: %v", err)
	}
	var data bytes.Buffer
	var result *agentv1.ArchiveResult
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		if part := chunk.GetData(); part != nil {
			data.Write(part)
		}
		if final := chunk.GetResult(); final != nil {
			result = final
		}
	}
	if data.String() != "tar-bytes" {
		t.Fatalf("data = %q; stderr must not leak into the archive", data.String())
	}
	if result == nil || !result.GetOk() {
		t.Fatalf("result = %v", result)
	}
	if result.GetStderrTail() != "tar: warning" {
		t.Fatalf("stderr_tail = %q", result.GetStderrTail())
	}
	if !hasMount(fake.lastCreate, ":/gotham-archive/sources/data:ro") {
		t.Fatalf("mounts = %v; want a read-only source mount", fake.lastCreate.GetVolumes())
	}
	if len(fake.lastCreate.GetVolumes()) != 1 {
		t.Fatalf("mounts = %v; only the declared source may be mounted", fake.lastCreate.GetVolumes())
	}
}

func TestArchiveVolumesRejectsHostileSourcesBeforeStartingAContainer(t *testing.T) {
	root := t.TempDir()
	denied := filepath.Join(root, "agent-state")
	if err := os.MkdirAll(denied, 0o755); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(root, "escape")
	if err := os.Symlink("/", escape); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		source *agentv1.ArchiveSource
		code   codes.Code
	}{
		"parent traversal name": {source: namedVolumeSource("../data"), code: codes.InvalidArgument},
		"slash in name":         {source: namedVolumeSource("a/b"), code: codes.InvalidArgument},
		"no source":             {source: &agentv1.ArchiveSource{Name: "data"}, code: codes.InvalidArgument},
		"relative path":         {source: bindPathSource("srv/data"), code: codes.InvalidArgument},
		"root path":             {source: bindPathSource("/"), code: codes.PermissionDenied},
		"docker socket":         {source: bindPathSource("/var/run/docker.sock"), code: 0},
		"agent state":           {source: bindPathSource(denied), code: codes.PermissionDenied},
		"symlink to root":       {source: bindPathSource(escape), code: codes.PermissionDenied},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakeDockerClient{}
			client := newArchiveServiceClient(t, fake, WithArchiveDeniedPaths(denied))
			stream, err := client.ArchiveVolumes(context.Background(), &agentv1.ArchiveVolumesRequest{
				Sources: []*agentv1.ArchiveSource{tc.source},
			})
			if err == nil {
				_, err = stream.Recv()
			}
			if err == nil {
				t.Fatal("expected the request to be refused")
			}
			if got := status.Code(err); tc.code != 0 && got != tc.code {
				t.Fatalf("code = %v; want %v (%v)", got, tc.code, err)
			}
			if fake.creates != 0 {
				t.Fatalf("created %d helper containers; want none", fake.creates)
			}
		})
	}
}

func TestArchiveVolumesRejectsDuplicateAndTooManySources(t *testing.T) {
	client := newArchiveServiceClient(t, &fakeDockerClient{})
	source := namedVolumeSource("data")
	stream, err := client.ArchiveVolumes(context.Background(), &agentv1.ArchiveVolumesRequest{
		Sources: []*agentv1.ArchiveSource{source, source},
	})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("duplicate sources: %v", err)
	}

	sources := make([]*agentv1.ArchiveSource, 0, archiveMaxSources+1)
	for i := 0; i <= archiveMaxSources; i++ {
		sources = append(sources, namedVolumeSource("s"+string(rune('a'+i))))
	}
	stream, err = client.ArchiveVolumes(context.Background(), &agentv1.ArchiveVolumesRequest{Sources: sources})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("too many sources: %v", err)
	}
}

func TestRestoreVolumesExtractsIntoWritableTargets(t *testing.T) {
	archive := buildTestArchive(t, []testEntry{
		{name: "manifest.json", body: manifestFor("data")},
		{name: "sources/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "sources/data/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "sources/data/a.txt", body: []byte("hello"), mode: 0o640},
		{name: "sources/data/link", typeflag: tar.TypeSymlink, linkname: "a.txt"},
	})
	for name, mode := range map[string]agentv1.RestoreMode{
		"replace": agentv1.RestoreMode_RESTORE_MODE_REPLACE,
		"overlay": agentv1.RestoreMode_RESTORE_MODE_OVERLAY,
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeDockerClient{details: ContainerDetails{State: "exited"}}
			client := newArchiveServiceClient(t, fake)
			stream, err := client.RestoreVolumes(context.Background())
			if err != nil {
				t.Fatalf("RestoreVolumes: %v", err)
			}
			if err := stream.Send(&agentv1.RestoreVolumesRequest{
				Part: &agentv1.RestoreVolumesRequest_Header{Header: &agentv1.RestoreVolumesHeader{
					Targets: []*agentv1.ArchiveSource{bindPathSource(t.TempDir())},
					Mode:    mode,
				}},
			}); err != nil {
				t.Fatalf("send header: %v", err)
			}
			for _, chunk := range chunkBytes(archive, 7) {
				if err := stream.Send(&agentv1.RestoreVolumesRequest{
					Part: &agentv1.RestoreVolumesRequest_Data{Data: chunk},
				}); err != nil {
					t.Fatalf("send data: %v", err)
				}
			}
			response, err := stream.CloseAndRecv()
			if err != nil {
				t.Fatalf("close and recv: %v", err)
			}
			if response.GetRestoredSources() != 1 {
				t.Fatalf("restored_sources = %d", response.GetRestoredSources())
			}
			if len(fake.imports) != 1 || fake.imports[0] != "created "+volumeArchiveMountRoot {
				t.Fatalf("imports = %v", fake.imports)
			}
			if !hasMount(fake.lastCreate, ":/gotham-archive/sources/data:rw") {
				t.Fatalf("mounts = %v; want a writable target mount", fake.lastCreate.GetVolumes())
			}
			command := strings.Join(fake.lastCreate.GetCommand(), " ")
			if mode == agentv1.RestoreMode_RESTORE_MODE_REPLACE && !strings.Contains(command, "find") {
				t.Fatalf("command = %q; replace must clear the target", command)
			}
			if mode == agentv1.RestoreMode_RESTORE_MODE_OVERLAY && !strings.Contains(command, "true") {
				t.Fatalf("command = %q; overlay must not clear the target", command)
			}
		})
	}
}

func TestRestoreVolumesRejectsUnsafeArchivesBeforeStartingAContainer(t *testing.T) {
	manifest := testEntry{name: "manifest.json", body: manifestFor("data")}
	for name, archive := range map[string][]byte{
		"parent traversal": buildTestArchive(t, []testEntry{manifest, {name: "sources/data/../../evil", body: []byte("x"), mode: 0o644}}),
		"absolute path":    buildTestArchive(t, []testEntry{manifest, {name: "/etc/passwd", body: []byte("x"), mode: 0o644}}),
		"other source":     buildTestArchive(t, []testEntry{manifest, {name: "sources/other/x", body: []byte("x"), mode: 0o644}}),
		"device entry":     buildTestArchive(t, []testEntry{manifest, {name: "sources/data/dev", typeflag: tar.TypeChar, mode: 0o600}}),
		"missing manifest": buildTestArchive(t, []testEntry{{name: "sources/data/x", body: []byte("x"), mode: 0o644}}),
		"wrong version":    buildTestArchive(t, []testEntry{{name: "manifest.json", body: []byte(`{"format_version":2,"sources":[{"name":"data"}]}`)}}),
		"wrong sources":    buildTestArchive(t, []testEntry{{name: "manifest.json", body: manifestFor("other")}}),
		"no entries":       buildTestArchive(t, []testEntry{manifest}),
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeDockerClient{}
			client := newArchiveServiceClient(t, fake)
			stream, err := client.RestoreVolumes(context.Background())
			if err != nil {
				t.Fatalf("RestoreVolumes: %v", err)
			}
			if err := stream.Send(&agentv1.RestoreVolumesRequest{
				Part: &agentv1.RestoreVolumesRequest_Header{Header: &agentv1.RestoreVolumesHeader{
					Targets: []*agentv1.ArchiveSource{bindPathSource(t.TempDir())},
					Mode:    agentv1.RestoreMode_RESTORE_MODE_REPLACE,
				}},
			}); err != nil {
				t.Fatalf("send header: %v", err)
			}
			if err := stream.Send(&agentv1.RestoreVolumesRequest{
				Part: &agentv1.RestoreVolumesRequest_Data{Data: archive},
			}); err != nil {
				t.Fatalf("send data: %v", err)
			}
			if _, err := stream.CloseAndRecv(); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("err = %v; want InvalidArgument", err)
			}
			if fake.creates != 0 {
				t.Fatalf("created %d helper containers; want none", fake.creates)
			}
		})
	}
}

func TestRestoreVolumesRequiresHeaderAndMode(t *testing.T) {
	client := newArchiveServiceClient(t, &fakeDockerClient{})
	stream, err := client.RestoreVolumes(context.Background())
	if err != nil {
		t.Fatalf("RestoreVolumes: %v", err)
	}
	if err := stream.Send(&agentv1.RestoreVolumesRequest{
		Part: &agentv1.RestoreVolumesRequest_Data{Data: []byte("x")},
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := stream.CloseAndRecv(); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing header: %v", err)
	}

	stream, err = client.RestoreVolumes(context.Background())
	if err != nil {
		t.Fatalf("RestoreVolumes: %v", err)
	}
	if err := stream.Send(&agentv1.RestoreVolumesRequest{
		Part: &agentv1.RestoreVolumesRequest_Header{Header: &agentv1.RestoreVolumesHeader{
			Targets: []*agentv1.ArchiveSource{namedVolumeSource("data")},
		}},
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := stream.CloseAndRecv(); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing mode: %v", err)
	}
}

func TestLimitedTailKeepsTheEnd(t *testing.T) {
	var tail limitedTail
	tail.Write([]byte("a"))
	tail.Write(bytes.Repeat([]byte("b"), archiveStderrLimit))
	if len(tail.data) != archiveStderrLimit || tail.data[len(tail.data)-1] != 'b' {
		t.Fatalf("tail = %d bytes; want the last %d", len(tail.data), archiveStderrLimit)
	}
}

func namedVolumeSource(name string) *agentv1.ArchiveSource {
	return &agentv1.ArchiveSource{Name: name, Source: &agentv1.ArchiveSource_Volume{Volume: "vol"}}
}

func bindPathSource(path string) *agentv1.ArchiveSource {
	return &agentv1.ArchiveSource{Name: "data", Source: &agentv1.ArchiveSource_Path{Path: path}}
}

func hasMount(request *agentv1.CreateContainerRequest, suffix string) bool {
	if request == nil {
		return false
	}
	for _, mount := range request.GetVolumes() {
		if strings.HasSuffix(mount, suffix) {
			return true
		}
	}
	return false
}

type testEntry struct {
	name     string
	body     []byte
	typeflag byte
	mode     int64
	linkname string
}

func buildTestArchive(t *testing.T, entries []testEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{
			Name:     entry.name,
			Mode:     mode,
			Size:     int64(len(entry.body)),
			Typeflag: typeflag,
			Linkname: entry.linkname,
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("write header %q: %v", entry.name, err)
		}
		if _, err := writer.Write(entry.body); err != nil {
			t.Fatalf("write body %q: %v", entry.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buffer.Bytes()
}

func manifestFor(names ...string) []byte {
	var builder strings.Builder
	builder.WriteString(`{"format_version":1,"sources":[`)
	for index, name := range names {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(`{"name":"` + name + `","size_bytes":1,"mtime_unix":1}`)
	}
	builder.WriteString(`]}`)
	return []byte(builder.String())
}

func chunkBytes(data []byte, size int) [][]byte {
	chunks := make([][]byte, 0, (len(data)+size-1)/size)
	for len(data) > 0 {
		part := size
		if len(data) < part {
			part = len(data)
		}
		chunks = append(chunks, data[:part])
		data = data[part:]
	}
	return chunks
}
