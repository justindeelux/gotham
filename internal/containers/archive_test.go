package containers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func archiveService(t *testing.T, mock *mockDockerClient) (*Service, *fakeRegistry) {
	t.Helper()
	registry := newFakeRegistry()
	registry.seed()
	return fixture(registry, mock, newFakeCache()), registry
}

func archiveDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestArchiveVolumesStreamsDataAndResult(t *testing.T) {
	body := []byte("tar-gz")
	mock := &mockDockerClient{archiveChunks: []*agentv1.ArchiveChunk{
		{Part: &agentv1.ArchiveChunk_Data{Data: body}},
		{Part: &agentv1.ArchiveChunk_Result{Result: &agentv1.ArchiveResult{
			Ok: true, Bytes: uint64(len(body)), Sha256: archiveDigest(body),
		}}},
	}}
	svc, registry := archiveService(t, mock)

	var out bytes.Buffer
	written, err := svc.ArchiveVolumes(context.Background(), registry.seed().ID, []ArchiveSource{{Name: "data", Volume: "vol"}}, &out)
	if err != nil {
		t.Fatalf("ArchiveVolumes: %v", err)
	}
	if written != int64(len("tar-gz")) || out.String() != "tar-gz" {
		t.Fatalf("written = %d, body = %q", written, out.String())
	}
}

func TestArchiveVolumesRejectsIntegrityMismatch(t *testing.T) {
	body := []byte("tar-gz")
	for name, result := range map[string]*agentv1.ArchiveResult{
		"size":      {Ok: true, Bytes: uint64(len(body) + 1), Sha256: archiveDigest(body)},
		"checksum":  {Ok: true, Bytes: uint64(len(body)), Sha256: archiveDigest([]byte("other"))},
		"no digest": {Ok: true, Bytes: uint64(len(body))},
	} {
		t.Run(name, func(t *testing.T) {
			mock := &mockDockerClient{archiveChunks: []*agentv1.ArchiveChunk{
				{Part: &agentv1.ArchiveChunk_Data{Data: body}},
				{Part: &agentv1.ArchiveChunk_Result{Result: result}},
			}}
			svc, registry := archiveService(t, mock)
			_, err := svc.ArchiveVolumes(context.Background(), registry.seed().ID,
				[]ArchiveSource{{Name: "data", Volume: "vol"}}, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "mismatch") && !strings.Contains(err.Error(), "checksum") {
				t.Fatalf("err = %v; want an integrity failure", err)
			}
		})
	}
}

func TestArchiveVolumesReportsHelperFailure(t *testing.T) {
	mock := &mockDockerClient{archiveChunks: []*agentv1.ArchiveChunk{
		{Part: &agentv1.ArchiveChunk_Result{Result: &agentv1.ArchiveResult{
			ExitCode:   2,
			StderrTail: "tar: boom",
		}}},
	}}
	svc, registry := archiveService(t, mock)

	_, err := svc.ArchiveVolumes(context.Background(), registry.seed().ID, []ArchiveSource{{Name: "data", Path: "/srv/data"}}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "tar: boom") {
		t.Fatalf("err = %v; want the helper stderr tail", err)
	}
}

func TestArchiveVolumesRefusesOldAgent(t *testing.T) {
	for name, mock := range map[string]*mockDockerClient{
		"unimplemented": {capsErr: status.Error(codes.Unimplemented, "unknown method")},
		"not available": {capsResp: &agentv1.VolumeArchiveCapabilitiesResponse{}},
		"missing capability": {capsResp: &agentv1.VolumeArchiveCapabilitiesResponse{
			Available: true, Capabilities: []string{"something_else.v1"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			svc, registry := archiveService(t, mock)
			_, err := svc.ArchiveVolumes(context.Background(), registry.seed().ID, []ArchiveSource{{Name: "data", Volume: "vol"}}, io.Discard)
			if !errors.Is(err, ErrVolumeArchiveUnsupported) {
				t.Fatalf("err = %v; want ErrVolumeArchiveUnsupported", err)
			}
			if !strings.Contains(err.Error(), "update the node agent") {
				t.Fatalf("err = %q; want an update hint", err)
			}
		})
	}
}

func TestArchiveVolumesStreamUnimplementedIsUnsupported(t *testing.T) {
	mock := &mockDockerClient{archiveErr: status.Error(codes.Unimplemented, "unknown method")}
	svc, registry := archiveService(t, mock)
	_, err := svc.ArchiveVolumes(context.Background(), registry.seed().ID, []ArchiveSource{{Name: "data", Volume: "vol"}}, io.Discard)
	if !errors.Is(err, ErrVolumeArchiveUnsupported) {
		t.Fatalf("err = %v; want ErrVolumeArchiveUnsupported", err)
	}
}

func TestArchiveVolumesValidatesSources(t *testing.T) {
	svc, registry := archiveService(t, &mockDockerClient{})
	ctx := context.Background()
	serverID := registry.seed().ID
	for name, sources := range map[string][]ArchiveSource{
		"empty":       nil,
		"no name":     {{Volume: "vol"}},
		"two origins": {{Name: "data", Volume: "vol", Path: "/srv/data"}},
		"no origin":   {{Name: "data"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.ArchiveVolumes(ctx, serverID, sources, io.Discard); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v; want ErrValidation", err)
			}
		})
	}
}

func TestRestoreVolumesSendsHeaderThenBody(t *testing.T) {
	mock := &mockDockerClient{restoreResp: &agentv1.RestoreVolumesResponse{RestoredSources: 1}}
	svc, registry := archiveService(t, mock)

	body := bytes.Repeat([]byte("x"), archiveChunkSize+1)
	if err := svc.RestoreVolumes(context.Background(), registry.seed().ID,
		[]ArchiveSource{{Name: "data", Volume: "vol"}}, RestoreModeReplace, bytes.NewReader(body)); err != nil {
		t.Fatalf("RestoreVolumes: %v", err)
	}
	if len(mock.restoreSent) != 3 {
		t.Fatalf("sent %d messages; want header + two chunks", len(mock.restoreSent))
	}
	header := mock.restoreSent[0].GetHeader()
	if header == nil || header.GetMode() != agentv1.RestoreMode_RESTORE_MODE_REPLACE {
		t.Fatalf("first message = %v; want a replace header", mock.restoreSent[0])
	}
	var received []byte
	for _, message := range mock.restoreSent[1:] {
		received = append(received, message.GetData()...)
	}
	if !bytes.Equal(received, body) {
		t.Fatalf("body roundtrip mismatch: %d vs %d bytes", len(received), len(body))
	}
}

func TestRestoreVolumesRejectsUnknownMode(t *testing.T) {
	svc, registry := archiveService(t, &mockDockerClient{})
	err := svc.RestoreVolumes(context.Background(), registry.seed().ID,
		[]ArchiveSource{{Name: "data", Volume: "vol"}}, RestoreMode("merge"), bytes.NewReader(nil))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v; want ErrValidation", err)
	}
}
