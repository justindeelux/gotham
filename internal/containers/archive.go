package containers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// VolumeArchiveCapability is the feature identifier an agent must advertise
// before the control plane sends it volume data.
const VolumeArchiveCapability = "volume_archive.v1"

var ErrVolumeArchiveUnsupported = errors.New("containers: the node agent does not support volume archives; update the node agent")

const archiveChunkSize = 64 << 10

// ArchiveSource is one directory a volume archive covers. Set exactly one of
// Volume (a named Docker volume) or Path (an absolute bind path on the node).
type ArchiveSource struct {
	Name   string
	Volume string
	Path   string
}

func (s ArchiveSource) toProto() *agentv1.ArchiveSource {
	source := &agentv1.ArchiveSource{Name: s.Name}
	if s.Volume != "" {
		source.Source = &agentv1.ArchiveSource_Volume{Volume: s.Volume}
	} else {
		source.Source = &agentv1.ArchiveSource_Path{Path: s.Path}
	}
	return source
}

func (s ArchiveSource) validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: archive source name is required", ErrValidation)
	}
	if (s.Volume == "") == (s.Path == "") {
		return fmt.Errorf("%w: archive source %q must set a volume or a path", ErrValidation, s.Name)
	}
	return nil
}

// RestoreMode selects how a restore treats files already in the target.
type RestoreMode string

const (
	RestoreModeReplace RestoreMode = "replace"
	RestoreModeOverlay RestoreMode = "overlay"
)

func (m RestoreMode) toProto() (agentv1.RestoreMode, error) {
	switch m {
	case RestoreModeReplace:
		return agentv1.RestoreMode_RESTORE_MODE_REPLACE, nil
	case RestoreModeOverlay:
		return agentv1.RestoreMode_RESTORE_MODE_OVERLAY, nil
	default:
		return agentv1.RestoreMode_RESTORE_MODE_UNSPECIFIED, fmt.Errorf("%w: unknown restore mode %q", ErrValidation, m)
	}
}

// ArchiveVolumes streams one tar.gz from the node into dst and returns the
// number of archive bytes written. A node that predates the transport yields
// ErrVolumeArchiveUnsupported instead of falling back to a local archive.
func (s *Service) ArchiveVolumes(ctx context.Context, serverID uuid.UUID, sources []ArchiveSource, dst io.Writer) (int64, error) {
	if len(sources) == 0 {
		return 0, fmt.Errorf("%w: at least one archive source is required", ErrValidation)
	}
	request := make([]*agentv1.ArchiveSource, 0, len(sources))
	for _, source := range sources {
		if err := source.validate(); err != nil {
			return 0, err
		}
		request = append(request, source.toProto())
	}

	ctx, cancel := context.WithTimeout(ctx, s.archiveTimeout)
	defer cancel()
	client, err := s.archiveClient(ctx, serverID)
	if err != nil {
		return 0, err
	}
	defer s.closeClient(client)
	if err := s.requireVolumeArchive(ctx, client); err != nil {
		return 0, err
	}

	stream, err := client.ArchiveVolumes(ctx, &agentv1.ArchiveVolumesRequest{Sources: request})
	if err != nil {
		return 0, archiveError(err)
	}
	var written int64
	var result *agentv1.ArchiveResult
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return written, archiveError(recvErr)
		}
		if data := chunk.GetData(); len(data) > 0 {
			n, writeErr := dst.Write(data)
			written += int64(n)
			if writeErr != nil {
				return written, fmt.Errorf("containers: write archive: %w", writeErr)
			}
			if n != len(data) {
				return written, fmt.Errorf("containers: write archive: %w", io.ErrShortWrite)
			}
		}
		if final := chunk.GetResult(); final != nil {
			result = final
		}
	}
	if result == nil {
		return written, errors.New("containers: the agent ended the archive without a result")
	}
	if !result.GetOk() {
		tail := strings.TrimSpace(result.GetStderrTail())
		if tail == "" {
			tail = result.GetError()
		}
		return written, fmt.Errorf("containers: volume archive failed (exit %d): %s", result.GetExitCode(), tail)
	}
	return written, nil
}

// RestoreVolumes streams a tar.gz from src into the node's target sources.
func (s *Service) RestoreVolumes(ctx context.Context, serverID uuid.UUID, sources []ArchiveSource, mode RestoreMode, src io.Reader) error {
	if len(sources) == 0 {
		return fmt.Errorf("%w: at least one restore target is required", ErrValidation)
	}
	targets := make([]*agentv1.ArchiveSource, 0, len(sources))
	for _, source := range sources {
		if err := source.validate(); err != nil {
			return err
		}
		targets = append(targets, source.toProto())
	}
	restoreMode, err := mode.toProto()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, s.archiveTimeout)
	defer cancel()
	client, err := s.archiveClient(ctx, serverID)
	if err != nil {
		return err
	}
	defer s.closeClient(client)
	if err := s.requireVolumeArchive(ctx, client); err != nil {
		return err
	}

	stream, err := client.RestoreVolumes(ctx)
	if err != nil {
		return archiveError(err)
	}
	if err := stream.Send(&agentv1.RestoreVolumesRequest{
		Part: &agentv1.RestoreVolumesRequest_Header{Header: &agentv1.RestoreVolumesHeader{
			Targets: targets,
			Mode:    restoreMode,
		}},
	}); err != nil {
		return archiveError(err)
	}
	buffer := make([]byte, archiveChunkSize)
	for {
		n, readErr := src.Read(buffer)
		if n > 0 {
			if err := stream.Send(&agentv1.RestoreVolumesRequest{
				Part: &agentv1.RestoreVolumesRequest_Data{Data: buffer[:n]},
			}); err != nil {
				return archiveError(err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("containers: read archive: %w", readErr)
		}
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		return archiveError(err)
	}
	return nil
}

func (s *Service) archiveClient(ctx context.Context, serverID uuid.UUID) (DockerClient, error) {
	server, err := s.resolve(ctx, serverID, false)
	if err != nil {
		return nil, err
	}
	return s.dialClient(ctx, server)
}

func (s *Service) requireVolumeArchive(ctx context.Context, client DockerClient) error {
	response, err := client.VolumeArchiveCapabilities(ctx, &agentv1.VolumeArchiveCapabilitiesRequest{})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return ErrVolumeArchiveUnsupported
		}
		return mapRPCError(err)
	}
	if !response.GetAvailable() {
		return ErrVolumeArchiveUnsupported
	}
	for _, capability := range response.GetCapabilities() {
		if capability == VolumeArchiveCapability {
			return nil
		}
	}
	return ErrVolumeArchiveUnsupported
}

func archiveError(err error) error {
	if status.Code(err) == codes.Unimplemented {
		return ErrVolumeArchiveUnsupported
	}
	return mapRPCError(err)
}
