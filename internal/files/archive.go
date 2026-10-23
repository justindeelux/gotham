package files

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	ArchiveFormatZip   = "zip"
	ArchiveFormatTar   = "tar"
	ArchiveFormatTarGz = "tar.gz"
)

const maxArchiveSources = 1024

type CompressRequest struct {
	Root        string   `json:"root"`
	Sources     []string `json:"sources"`
	Destination string   `json:"destination"`
	Format      string   `json:"format"`
	Overwrite   bool     `json:"overwrite"`
}

type ExtractRequest struct {
	Root        string `json:"root"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Overwrite   bool   `json:"overwrite"`
}

func archiveFormat(format string) (agentv1.ArchiveFormat, error) {
	switch strings.TrimSpace(format) {
	case ArchiveFormatZip:
		return agentv1.ArchiveFormat_ARCHIVE_FORMAT_ZIP, nil
	case ArchiveFormatTar:
		return agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR, nil
	case ArchiveFormatTarGz:
		return agentv1.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ, nil
	default:
		return agentv1.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED, fmt.Errorf("%w: unknown archive format %q", ErrValidation, format)
	}
}

func (s *Service) Compress(ctx context.Context, serverID uuid.UUID, req CompressRequest) (entry Entry, err error) {
	defer func() {
		s.audit(ctx, serverID, "compress", req.Root, req.Destination, err,
			"dest_dir", path.Dir(req.Destination), "sources", len(req.Sources),
			"format", req.Format, "overwrite", req.Overwrite)
	}()
	if !Enabled() {
		return Entry{}, ErrDisabled
	}
	if err := validateRoot(req.Root); err != nil {
		return Entry{}, err
	}
	if strings.TrimSpace(req.Destination) == "" {
		return Entry{}, fmt.Errorf("%w: destination is required", ErrValidation)
	}
	if err := validatePathArg("destination", req.Destination); err != nil {
		return Entry{}, err
	}
	format, err := archiveFormat(req.Format)
	if err != nil {
		return Entry{}, err
	}
	if len(req.Sources) == 0 {
		return Entry{}, fmt.Errorf("%w: at least one source is required", ErrValidation)
	}
	if len(req.Sources) > maxArchiveSources {
		return Entry{}, fmt.Errorf("%w: too many sources", ErrValidation)
	}
	for _, source := range req.Sources {
		if err := validatePathArg("source", source); err != nil {
			return Entry{}, err
		}
	}

	ctx, client, cancel, err := s.rpcTransfer(ctx, serverID)
	if err != nil {
		return Entry{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	stream, err := client.Compress(ctx, &agentv1.CompressRequest{
		Root:        req.Root,
		Sources:     req.Sources,
		Destination: req.Destination,
		Format:      format,
		Overwrite:   req.Overwrite,
	})
	if err != nil {
		return Entry{}, mapRPCError(err)
	}
	if err := drainProgress(stream); err != nil {
		return Entry{}, err
	}
	stat, err := client.Stat(ctx, &agentv1.StatRequest{Root: req.Root, Path: req.Destination})
	if err != nil {
		return Entry{}, mapRPCError(err)
	}
	info := stat.GetEntry()
	if info == nil {
		return Entry{}, ErrNotFound
	}
	return newEntry(info), nil
}

func (s *Service) Extract(ctx context.Context, serverID uuid.UUID, req ExtractRequest) (err error) {
	defer func() {
		s.audit(ctx, serverID, "extract", req.Root, req.Source, err,
			"destination", req.Destination, "overwrite", req.Overwrite)
	}()
	if !Enabled() {
		return ErrDisabled
	}
	if err := validateRoot(req.Root); err != nil {
		return err
	}
	if err := validatePathArg("source", req.Source); err != nil {
		return err
	}
	if err := validatePathArg("destination", req.Destination); err != nil {
		return err
	}

	ctx, client, cancel, err := s.rpcTransfer(ctx, serverID)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.closeClient(client)

	stream, err := client.Extract(ctx, &agentv1.ExtractRequest{
		Root:        req.Root,
		Source:      req.Source,
		Destination: req.Destination,
		Overwrite:   req.Overwrite,
	})
	if err != nil {
		return mapRPCError(err)
	}
	return drainProgress(stream)
}
