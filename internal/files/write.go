package files

import (
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

// DefaultMaxUploadBytes is the control-plane ceiling for a single write or
// upload body (1 GiB), matching the agent's default GOTHAM_AGENT_FILE_MAX_UPLOAD.
// A request body above it answers 413 before the agent is dialed. Override with
// FILES_MAX_UPLOAD_BYTES.
const DefaultMaxUploadBytes int64 = 1 << 30 // 1 GiB

// writeChunkBytes is the body chunk size streamed to the agent. It stays well
// under the agent's 2 MiB gRPC receive cap and the dialer's 4 MiB send cap.
const writeChunkBytes = 256 << 10 // 256 KiB

// WriteRequest describes a create-or-replace. Size is the exact number of body
// bytes that will be read; a shorter or longer body is refused so a truncated
// upload never reaches the agent's commit. ExpectEtag is the etag read from the
// editor ("" means the target must not exist); Overwrite replaces an existing
// target the caller has not read. A non-empty ExpectEtag that no longer matches
// is a conflict even when Overwrite is set (the agent never clobbers a newer
// file).
type WriteRequest struct {
	Root       string
	Path       string
	ExpectEtag string
	Overwrite  bool
	Mode       uint32
	Size       int64
}

// WriteResult reports the committed file.
type WriteResult struct {
	Entry     Entry  `json:"entry"`
	Created   bool   `json:"created"`
	WriteMode string `json:"write_mode"`
}

// Write creates or replaces one file. The body is streamed to the agent in
// bounded chunks after a header declaring its exact size; a request-body error
// cancels the stream so the agent discards its staging file and never commits.
func (s *Service) Write(ctx context.Context, serverID uuid.UUID, req WriteRequest, body io.Reader) (WriteResult, error) {
	return s.writeFile(ctx, serverID, "write", req, body)
}

// AuthorizeWrite authorizes a mutation of the server without dialing the agent,
// so a streamed upload can refuse a forbidden caller once before reading its
// body. It is the same team/role check every write method runs via resolve.
func (s *Service) AuthorizeWrite(ctx context.Context, serverID uuid.UUID) error {
	if !Enabled() {
		return ErrDisabled
	}
	_, err := s.resolve(ctx, serverID, true)
	return err
}

// Create creates an empty file exclusively: an existing name is a conflict.
func (s *Service) Create(ctx context.Context, serverID uuid.UUID, root, path string, mode uint32) (Entry, error) {
	result, err := s.writeFile(ctx, serverID, "create", WriteRequest{Root: root, Path: path, Mode: mode}, nil)
	if err != nil {
		return Entry{}, err
	}
	return result.Entry, nil
}

// writeFile is the shared create-or-replace path; op is the audit label.
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
	// If-Match travels in the proto header, so invalid UTF-8 would fail
	// proto.Marshal and surface as a 500; refuse it as a 400 before the dial.
	if !utf8.ValidString(req.ExpectEtag) {
		return WriteResult{}, fmt.Errorf("%w: If-Match is not valid UTF-8", ErrValidation)
	}
	if req.Size < 0 {
		return WriteResult{}, fmt.Errorf("%w: size must not be negative", ErrValidation)
	}
	if s.maxUploadBytes > 0 && req.Size > s.maxUploadBytes {
		return WriteResult{}, ErrTooLarge
	}
	if body == nil {
		body = strings.NewReader("")
	}

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
			// The request body failed mid-transfer (client disconnect, a
			// truncated upload, a read deadline). Cancelling the stream makes
			// the agent drop its staging file, so a partial upload is never
			// renamed into place. readErr is wrapped so the route can still
			// recognise an *http.MaxBytesError and answer 413.
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

// Mkdir creates one directory.
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

// Rename renames or moves one entry within a single root.
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

// Chmod changes one entry's permission bits (owner/admin only). Only the low
// 0o777 bits are accepted: a request carrying setuid, setgid or sticky (or any
// other bit above 0o777) is refused (400) before the agent is dialed. The agent
// refuses a symbolic link and a special file. The wired ChmodResponse is empty,
// so the entry is re-stat-ed and returned, exactly as Copy does for its
// destination; the fresh entry carries the new mode and etag for the browser.
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

	if _, err := client.Chmod(ctx, &agentv1.ChmodRequest{Root: root, Path: path, Mode: mode}); err != nil {
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

// Copy copies one entry within a single root, draining the agent's progress
// stream and returning the new destination entry.
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

// Delete removes one entry, recursively when requested.
func (s *Service) Delete(ctx context.Context, serverID uuid.UUID, root, path string, recursive bool) (err error) {
	defer func() { s.audit(ctx, serverID, "delete", root, path, err) }()
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

	stream, err := client.Delete(ctx, &agentv1.DeleteRequest{Root: root, Path: path, Recursive: recursive})
	if err != nil {
		return mapRPCError(err)
	}
	return drainProgress(stream)
}

// rpcTransfer resolves and dials a server with the long transfer timeout
// instead of the unary read budget, so a large upload/copy/delete outlives the
// 30 s request window. write is always true: every transfer mutates.
func (s *Service) rpcTransfer(ctx context.Context, serverID uuid.UUID) (context.Context, FileClient, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, s.transferTimeout)
	server, err := s.resolve(ctx, serverID, true)
	if err != nil {
		cancel()
		// Return the (canceled) ctx so a deferred audit keeps its actor scope.
		return ctx, nil, nil, err
	}
	client, err := s.dialClient(ctx, server)
	if err != nil {
		cancel()
		return ctx, nil, nil, err
	}
	return ctx, client, cancel, nil
}

// sendWritePart sends one message on a Write client stream and maps the
// failure. When the agent rejects the stream before reading it — a read-only
// root or application directory (PermissionDenied), a body over the node's
// limit (ResourceExhausted), a low disk, an InvalidArgument header — it returns
// its status and closes the send side, so Send surfaces only io.EOF while the
// real status is available only from CloseAndRecv. Mapping the bare io.EOF
// would answer 500; draining the stream here recovers the actual status so the
// caller answers 403/400/413/507 as intended.
func sendWritePart(stream grpc.ClientStreamingClient[agentv1.WriteRequest, agentv1.WriteResponse], msg *agentv1.WriteRequest) error {
	err := stream.Send(msg)
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		if _, recvErr := stream.CloseAndRecv(); recvErr != nil {
			return mapRPCError(recvErr)
		}
		// A clean CloseAndRecv after an io.EOF Send means the stream ended
		// without a status we can classify; treat it as an unavailable agent
		// rather than surfacing a raw EOF (which would be a 500).
		return fmt.Errorf("%w: the agent closed the write stream", ErrAgentUnavailable)
	}
	return mapRPCError(err)
}

// drainProgress reads a Copy/Delete progress stream to its end, mapping a
// mid-stream agent error instead of mistaking a truncated stream for success.
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
