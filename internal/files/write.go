package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const DefaultMaxUploadBytes int64 = 1 << 30

const writeChunkBytes = 256 << 10

type WriteRequest struct {
	Root       string
	Path       string
	ExpectEtag string
	Overwrite  bool
	Mode       uint32
	Size       int64
	Encoding   string
	FromEditor bool
}

type WriteResult struct {
	Entry     Entry  `json:"entry"`
	Created   bool   `json:"created"`
	WriteMode string `json:"write_mode"`
}

func (s *Service) Write(ctx context.Context, serverID uuid.UUID, req WriteRequest, body io.Reader) (WriteResult, error) {
	return s.writeFile(ctx, serverID, "write", req, body)
}

func (s *Service) AuthorizeWrite(ctx context.Context, serverID uuid.UUID) error {
	if !Enabled() {
		return ErrDisabled
	}
	_, err := s.resolve(ctx, serverID, true)
	return err
}

func (s *Service) Create(ctx context.Context, serverID uuid.UUID, root, path string, mode uint32) (Entry, error) {
	result, err := s.writeFile(ctx, serverID, "create", WriteRequest{Root: root, Path: path, Mode: mode}, nil)
	if err != nil {
		return Entry{}, err
	}
	return result.Entry, nil
}

func (s *Service) writeFile(ctx context.Context, serverID uuid.UUID, op string, req WriteRequest, body io.Reader) (result WriteResult, err error) {
	defer func() { s.audit(ctx, serverID, op, req.Root, req.Path, err) }()
	if !Enabled() {
		return WriteResult{}, ErrDisabled
	}
	if err := validateRoot(req.Root); err != nil {
		return WriteResult{}, err
	}
	if err := validatePathArg("path", req.Path); err != nil {
		return WriteResult{}, err
	}
	if !utf8.ValidString(req.ExpectEtag) {
		return WriteResult{}, fmt.Errorf("%w: If-Match is not valid UTF-8", ErrValidation)
	}
	if req.Size < 0 {
		return WriteResult{}, fmt.Errorf("%w: size must not be negative", ErrValidation)
	}
	if s.maxUploadBytes > 0 && req.Size > s.maxUploadBytes {
		return WriteResult{}, ErrTooLarge
	}
	if req.FromEditor && op == "write" {
		if err := s.refuseEditorWrite(ctx, serverID, req.Root, req.Path); err != nil {
			return WriteResult{}, err
		}
	}
	if body == nil {
		body = strings.NewReader("")
	}
	encodingName, err := normalizeEncoding(req.Encoding)
	if err != nil {
		return WriteResult{}, err
	}
	if encodingName != EncodingUTF8 {
		if req.Size > s.maxContentBytes {
			return WriteResult{}, ErrTooLarge
		}
		text, readErr := io.ReadAll(io.LimitReader(body, s.maxContentBytes+1))
		if readErr != nil {
			return WriteResult{}, fmt.Errorf("%w: reading request body: %w", ErrValidation, readErr)
		}
		if int64(len(text)) != req.Size {
			return WriteResult{}, fmt.Errorf("%w: received %d of %d body bytes", ErrValidation, len(text), req.Size)
		}
		encoded, encodeErr := encodeContent(encodingName, string(text))
		if encodeErr != nil {
			return WriteResult{}, encodeErr
		}
		body = bytes.NewReader(encoded)
		req.Size = int64(len(encoded))
	}

	var prior *Content
	if op == "write" && req.ExpectEtag != "" && s.historyStore() != nil {
		current, readErr := s.readContent(ctx, serverID, req.Root, req.Path)
		switch {
		case readErr == nil && current.Etag == req.ExpectEtag:
			prior = &current
		case readErr == nil:
			s.logger.Debug("files: skipping version snapshot; the pre-save etag does not match the save",
				"server_id", serverID.String(), "root", req.Root, "relpath", req.Path)
		case errors.Is(readErr, ErrTooLarge), errors.Is(readErr, ErrNotEditable), errors.Is(readErr, ErrNotFound):
		default:
			s.logger.Warn("files: could not read the pre-save body for history",
				"server_id", serverID.String(), "root", req.Root, "relpath", req.Path, "error", readErr)
		}
	}

	result, err = s.streamWrite(ctx, serverID, req, body)
	if err != nil {
		return WriteResult{}, err
	}
	if prior != nil {
		s.recordVersion(ctx, serverID, req.Root, req.Path, *prior)
	}
	return result, nil
}

func (s *Service) refuseEditorWrite(ctx context.Context, serverID uuid.UUID, root, path string) error {
	existing, err := s.Stat(ctx, serverID, root, path)
	switch {
	case err == nil && existing.Size > s.maxContentBytes:
		return ErrTooLarge
	case err == nil, errors.Is(err, ErrNotFound):
		return nil
	default:
		return err
	}
}

func (s *Service) streamWrite(ctx context.Context, serverID uuid.UUID, req WriteRequest, body io.Reader) (WriteResult, error) {
	ctx, client, cancel, err := s.rpcTransfer(ctx, serverID)
	if err != nil {
		return WriteResult{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	stream, err := client.Write(ctx)
	if err != nil {
		return WriteResult{}, mapRPCError(err)
	}
	header := &agentv1.WriteRequest{Part: &agentv1.WriteRequest_Header{Header: &agentv1.WriteHeader{
		Root:       req.Root,
		Path:       req.Path,
		Size:       req.Size,
		ExpectEtag: req.ExpectEtag,
		Overwrite:  req.Overwrite,
		Mode:       req.Mode,
	}}}
	if err := sendWritePart(stream, header); err != nil {
		cancel()
		return WriteResult{}, err
	}

	hasher := sha256.New()
	buf := make([]byte, writeChunkBytes)
	var sent int64
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			sent += int64(n)
			if sent > req.Size {
				cancel()
				return WriteResult{}, fmt.Errorf("%w: body exceeds the declared size", ErrValidation)
			}
			_, _ = hasher.Write(buf[:n])
			if err := sendWritePart(stream, &agentv1.WriteRequest{Part: &agentv1.WriteRequest_Data{Data: buf[:n]}}); err != nil {
				cancel()
				return WriteResult{}, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			cancel()
			return WriteResult{}, fmt.Errorf("%w: reading request body: %w", ErrValidation, readErr)
		}
	}
	if sent != req.Size {
		cancel()
		return WriteResult{}, fmt.Errorf("%w: received %d of %d body bytes", ErrValidation, sent, req.Size)
	}
	commit := &agentv1.WriteRequest{Part: &agentv1.WriteRequest_Commit{Commit: &agentv1.WriteCommit{
		Sha256: hex.EncodeToString(hasher.Sum(nil)),
	}}}
	if err := sendWritePart(stream, commit); err != nil {
		cancel()
		return WriteResult{}, err
	}
	response, err := stream.CloseAndRecv()
	if err != nil {
		return WriteResult{}, mapRPCError(err)
	}
	info := response.GetEntry()
	if info == nil {
		return WriteResult{}, fmt.Errorf("%w: the agent committed no entry", ErrAgentUnavailable)
	}
	return WriteResult{Entry: newEntry(info), Created: response.GetCreated(), WriteMode: response.GetWriteMode()}, nil
}

func (s *Service) Mkdir(ctx context.Context, serverID uuid.UUID, root, path string, mode uint32) (entry Entry, err error) {
	defer func() { s.audit(ctx, serverID, "mkdir", root, path, err) }()
	if !Enabled() {
		return Entry{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Entry{}, err
	}
	if err := validatePathArg("path", path); err != nil {
		return Entry{}, err
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, true)
	if err != nil {
		return Entry{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.Mkdir(ctx, &agentv1.MkdirRequest{Root: root, Path: path, Mode: mode})
	if err != nil {
		return Entry{}, mapRPCError(err)
	}
	info := response.GetEntry()
	if info == nil {
		return Entry{}, ErrNotFound
	}
	return newEntry(info), nil
}

func (s *Service) Rename(ctx context.Context, serverID uuid.UUID, root, source, destination string, overwrite bool) (entry Entry, err error) {
	defer func() {
		s.audit(ctx, serverID, "rename", root, source, err, "destination", destination)
	}()
	if !Enabled() {
		return Entry{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Entry{}, err
	}
	if err := validatePathArg("source", source); err != nil {
		return Entry{}, err
	}
	if err := validatePathArg("destination", destination); err != nil {
		return Entry{}, err
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, true)
	if err != nil {
		return Entry{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.Rename(ctx, &agentv1.RenameRequest{Root: root, Source: source, Destination: destination, Overwrite: overwrite})
	if err != nil {
		return Entry{}, mapRPCError(err)
	}
	info := response.GetEntry()
	if info == nil {
		return Entry{}, ErrNotFound
	}
	return newEntry(info), nil
}

func (s *Service) Chmod(ctx context.Context, serverID uuid.UUID, root, path string, mode uint32) (entry Entry, err error) {
	defer func() { s.audit(ctx, serverID, "chmod", root, path, err, "mode", mode) }()
	if !Enabled() {
		return Entry{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Entry{}, err
	}
	if err := validatePathArg("path", path); err != nil {
		return Entry{}, err
	}
	if mode&^uint32(0o777) != 0 {
		return Entry{}, fmt.Errorf("%w: mode %#o carries bits above the 0o777 permission mask", ErrValidation, mode)
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, true)
	if err != nil {
		return Entry{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	if _, err := client.Chmod(ctx, &agentv1.ChmodRequest{Root: root, Path: path, Mode: &mode}); err != nil {
		return Entry{}, mapRPCError(err)
	}
	stat, err := client.Stat(ctx, &agentv1.StatRequest{Root: root, Path: path})
	if err != nil {
		return Entry{}, mapRPCError(err)
	}
	info := stat.GetEntry()
	if info == nil {
		return Entry{}, ErrNotFound
	}
	return newEntry(info), nil
}

func (s *Service) Copy(ctx context.Context, serverID uuid.UUID, root, source, destination string, overwrite bool) (entry Entry, err error) {
	defer func() {
		s.audit(ctx, serverID, "copy", root, source, err, "destination", destination)
	}()
	if !Enabled() {
		return Entry{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Entry{}, err
	}
	if err := validatePathArg("source", source); err != nil {
		return Entry{}, err
	}
	if err := validatePathArg("destination", destination); err != nil {
		return Entry{}, err
	}
	ctx, client, cancel, err := s.rpcTransfer(ctx, serverID)
	if err != nil {
		return Entry{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	stream, err := client.Copy(ctx, &agentv1.CopyRequest{Root: root, Source: source, Destination: destination, Overwrite: overwrite})
	if err != nil {
		return Entry{}, mapRPCError(err)
	}
	if err := drainProgress(stream); err != nil {
		return Entry{}, err
	}
	stat, err := client.Stat(ctx, &agentv1.StatRequest{Root: root, Path: destination})
	if err != nil {
		return Entry{}, mapRPCError(err)
	}
	info := stat.GetEntry()
	if info == nil {
		return Entry{}, ErrNotFound
	}
	return newEntry(info), nil
}

func (s *Service) Delete(ctx context.Context, serverID uuid.UUID, root, path string, recursive, permanent bool) (err error) {
	defer func() { s.audit(ctx, serverID, "delete", root, path, err, "permanent", permanent) }()
	if !Enabled() {
		return ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return err
	}
	if err := validatePathArg("path", path); err != nil {
		return err
	}
	ctx, client, cancel, err := s.rpcTransfer(ctx, serverID)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.closeClient(client)

	stream, err := client.Delete(ctx, &agentv1.DeleteRequest{Root: root, Path: path, Recursive: recursive, Permanent: permanent})
	if err != nil {
		return mapRPCError(err)
	}
	return drainProgress(stream)
}

func (s *Service) rpcTransfer(ctx context.Context, serverID uuid.UUID) (context.Context, FileClient, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, s.transferTimeout)
	server, err := s.resolve(ctx, serverID, true)
	if err != nil {
		cancel()
		return ctx, nil, nil, err
	}
	client, err := s.dialClient(ctx, server)
	if err != nil {
		cancel()
		return ctx, nil, nil, err
	}
	return ctx, client, cancel, nil
}

func sendWritePart(stream grpc.ClientStreamingClient[agentv1.WriteRequest, agentv1.WriteResponse], msg *agentv1.WriteRequest) error {
	err := stream.Send(msg)
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		if _, recvErr := stream.CloseAndRecv(); recvErr != nil {
			return mapRPCError(recvErr)
		}
		return fmt.Errorf("%w: the agent closed the write stream", ErrAgentUnavailable)
	}
	return mapRPCError(err)
}

func drainProgress(stream grpc.ServerStreamingClient[agentv1.Progress]) error {
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return mapRPCError(err)
		}
	}
}
