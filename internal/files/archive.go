package files

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Archive format tokens the HTTP API accepts for compress. The format is
// otherwise inferred from the archive's extension on extract.
const (
	ArchiveFormatZip   = "zip"
	ArchiveFormatTar   = "tar"
	ArchiveFormatTarGz = "tar.gz"
)

// maxArchiveSources bounds how many sources one compress request may name, so
// the request cannot build an unbounded proto message.
const maxArchiveSources = 1024

// CompressRequest describes one archive build (POST /files/compress).
type CompressRequest struct {
	Root        string   `json:"root"`
	Sources     []string `json:"sources"`
	Destination string   `json:"destination"`
	// Format is one of "zip", "tar" or "tar.gz".
	Format    string `json:"format"`
	Overwrite bool   `json:"overwrite"`
}

// ExtractRequest describes one archive unpack (POST /files/extract).
type ExtractRequest struct {
	Root        string `json:"root"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Overwrite   bool   `json:"overwrite"`
}

// archiveFormat maps the API's format token onto the agent's enum.
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

// Compress builds one archive from sources inside a single root, drains the
// agent's progress stream and returns the created archive's entry. It is a
// mutation: the caller must hold a write-capable role (resolve enforces it) and
// the agent must own a writable root.
func (s *Service) Compress(ctx context.Context, serverID uuid.UUID, req CompressRequest) (entry Entry, err error) {
	defer func() { s.audit(ctx, serverID, "compress", req.Root, req.Destination, err) }()
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

// Extract unpacks one archive into a destination directory inside a single
// root, draining the agent's progress stream. It is a mutation, gated like
// Compress.
func (s *Service) Extract(ctx context.Context, serverID uuid.UUID, req ExtractRequest) (err error) {
	defer func() { s.audit(ctx, serverID, "extract", req.Root, req.Source, err) }()
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
