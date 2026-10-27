package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const FeatureEnv = "FEATURE_FILES"

func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

const MaxUploadEnv = "FILES_MAX_UPLOAD_BYTES"

func maxUploadFromEnv() int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(MaxUploadEnv)), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

const (
	defaultRPCTimeout      = 30 * time.Second
	defaultTransferTimeout = 30 * time.Minute
	defaultTicketTTL       = 60 * time.Second
)

const MaxContentBytes int64 = 2 << 20

const TailLines = 1000

const maxFileArgBytes = 4096

type Root struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Writable  bool   `json:"writable"`
	FreeBytes uint64 `json:"free_bytes"`
}

type Entry struct {
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	Size          int64     `json:"size"`
	Mode          uint32    `json:"mode"`
	ModifiedAt    time.Time `json:"modified_at"`
	SymlinkTarget string    `json:"symlink_target,omitempty"`
	Etag          string    `json:"etag"`
	Owner         string    `json:"owner"`
	Group         string    `json:"group"`
}

type Listing struct {
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
	Cursor    string  `json:"cursor,omitempty"`
}

type TrashEntry struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	OriginalPath string    `json:"original_path"`
	DeletedAt    time.Time `json:"deleted_at"`
	Size         int64     `json:"size"`
	Kind         string    `json:"kind"`
}

type Content struct {
	Content   string `json:"content"`
	Encoding  string `json:"encoding"`
	Etag      string `json:"etag"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Mode      uint32 `json:"mode"`
	Truncated bool   `json:"truncated"`
	raw       []byte
}

type Download struct {
	Name   string
	Size   int64
	Etag   string
	Chunks <-chan []byte
	Err    <-chan error
}

type FileService interface {
	Roots(ctx context.Context, serverID uuid.UUID) ([]Root, error)
	List(ctx context.Context, serverID uuid.UUID, root, path, cursor string) (Listing, error)
	Stat(ctx context.Context, serverID uuid.UUID, root, path string) (Entry, error)
	Search(ctx context.Context, serverID uuid.UUID, root, path, query string, recursive bool) (SearchListing, error)
	Content(ctx context.Context, serverID uuid.UUID, root, path string) (Content, error)
	History(ctx context.Context, serverID uuid.UUID, root, path string) ([]Version, error)
	HistoryVersion(ctx context.Context, serverID uuid.UUID, root, path string, versionID uuid.UUID) (VersionBody, error)
	RestoreVersion(ctx context.Context, serverID uuid.UUID, root, path string, versionID uuid.UUID) (WriteResult, error)
	DeleteVersion(ctx context.Context, serverID uuid.UUID, root, path string, versionID uuid.UUID) error
	DirectorySize(ctx context.Context, serverID uuid.UUID, root, path string) (FolderSize, error)
	IssueDownloadTicket(ctx context.Context, serverID uuid.UUID, root, path string) (string, error)
	Download(ctx context.Context, serverID uuid.UUID, ticket string) (*Download, error)

	Write(ctx context.Context, serverID uuid.UUID, req WriteRequest, body io.Reader) (WriteResult, error)
	AuthorizeWrite(ctx context.Context, serverID uuid.UUID) error
	Mkdir(ctx context.Context, serverID uuid.UUID, root, path string, mode uint32) (Entry, error)
	Create(ctx context.Context, serverID uuid.UUID, root, path string, mode uint32) (Entry, error)
	Rename(ctx context.Context, serverID uuid.UUID, root, source, destination string, overwrite bool) (Entry, error)
	Chmod(ctx context.Context, serverID uuid.UUID, root, path string, mode uint32) (Entry, error)
	Copy(ctx context.Context, serverID uuid.UUID, root, source, destination string, overwrite bool) (Entry, error)
	Delete(ctx context.Context, serverID uuid.UUID, root, path string, recursive, permanent bool) error
	Compress(ctx context.Context, serverID uuid.UUID, req CompressRequest) (Entry, error)
	Extract(ctx context.Context, serverID uuid.UUID, req ExtractRequest) error

	Trash(ctx context.Context, serverID uuid.UUID, root string) (TrashListing, error)
	Restore(ctx context.Context, serverID uuid.UUID, root, id, destination string, overwrite bool) (Entry, error)
	Purge(ctx context.Context, serverID uuid.UUID, root, id string) error
	EmptyTrash(ctx context.Context, serverID uuid.UUID, root string) (uint64, error)

	PlanTransfer(ctx context.Context, serverID uuid.UUID, req TransferRequest) (TransferPlan, error)
	Transfer(ctx context.Context, serverID uuid.UUID, req TransferRequest) (*TransferRun, error)
}

type Config struct {
	Registry           Registry
	Dial               DialFunc
	Logger             *slog.Logger
	RPCTimeout         time.Duration
	TransferTimeout    time.Duration
	TicketTTL          time.Duration
	MaxContentBytes    int64
	MaxUploadBytes     int64
	HistoryMaxVersions int
	HistoryMaxBytes    int64
	Now                func() time.Time
}

type Service struct {
	mu                 sync.RWMutex
	registry           Registry
	dial               DialFunc
	logger             *slog.Logger
	rpcTimeout         time.Duration
	transferTimeout    time.Duration
	maxContentBytes    int64
	maxUploadBytes     int64
	history            HistoryStore
	historyMaxVersions int
	historyMaxBytes    int64
	tickets            *ticketStore
}

func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	rpcTimeout := cfg.RPCTimeout
	if rpcTimeout <= 0 {
		rpcTimeout = defaultRPCTimeout
	}
	transferTimeout := cfg.TransferTimeout
	if transferTimeout <= 0 {
		transferTimeout = defaultTransferTimeout
	}
	maxContent := cfg.MaxContentBytes
	if maxContent <= 0 {
		maxContent = MaxContentBytes
	}
	maxUpload := cfg.MaxUploadBytes
	if maxUpload <= 0 {
		maxUpload = DefaultMaxUploadBytes
	}
	ttl := cfg.TicketTTL
	if ttl <= 0 {
		ttl = defaultTicketTTL
	}
	historyVersions := cfg.HistoryMaxVersions
	if historyVersions <= 0 {
		historyVersions = historyMaxVersionsFromEnv()
	}
	if historyVersions <= 0 {
		historyVersions = DefaultHistoryMaxVersions
	}
	historyBytes := cfg.HistoryMaxBytes
	if historyBytes <= 0 {
		historyBytes = historyMaxBytesFromEnv()
	}
	if historyBytes <= 0 {
		historyBytes = DefaultHistoryMaxBytes
	}
	return &Service{
		registry:           cfg.Registry,
		dial:               cfg.Dial,
		logger:             logger,
		rpcTimeout:         rpcTimeout,
		transferTimeout:    transferTimeout,
		maxContentBytes:    maxContent,
		maxUploadBytes:     maxUpload,
		historyMaxVersions: historyVersions,
		historyMaxBytes:    historyBytes,
		tickets:            newTicketStore(ttl, cfg.Now),
	}
}

func NewDefaultService(registry Registry, logger *slog.Logger) *Service {
	if registry == nil {
		return nil
	}
	return NewService(Config{Registry: registry, Logger: logger, MaxUploadBytes: maxUploadFromEnv()})
}

func (s *Service) SetDial(dial DialFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dial = dial
}

func (s *Service) SetHistoryStore(history HistoryStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = history
}

func (s *Service) historyStore() HistoryStore {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.history
}

func (s *Service) MaxUploadBytes() int64 {
	return s.maxUploadBytes
}

func (s *Service) TransferTimeout() time.Duration {
	return s.transferTimeout
}

func (s *Service) Roots(ctx context.Context, serverID uuid.UUID) ([]Root, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, false)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.ListRoots(ctx, &agentv1.ListRootsRequest{})
	if err != nil {
		return nil, mapRPCError(err)
	}
	roots := make([]Root, 0, len(response.GetRoots()))
	for _, info := range response.GetRoots() {
		roots = append(roots, newRoot(info))
	}
	return roots, nil
}

func (s *Service) List(ctx context.Context, serverID uuid.UUID, root, path, cursor string) (Listing, error) {
	if !Enabled() {
		return Listing{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Listing{}, err
	}
	if err := validatePathArg("path", path); err != nil {
		return Listing{}, err
	}
	if err := validateFileArg("cursor", cursor); err != nil {
		return Listing{}, err
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, false)
	if err != nil {
		return Listing{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.List(ctx, &agentv1.ListRequest{Root: root, Path: path, Cursor: cursor})
	if err != nil {
		return Listing{}, mapRPCError(err)
	}
	entries := make([]Entry, 0, len(response.GetEntries()))
	for _, info := range response.GetEntries() {
		entries = append(entries, newEntry(info))
	}
	return Listing{Entries: entries, Truncated: response.GetTruncated(), Cursor: response.GetCursor()}, nil
}

func (s *Service) Stat(ctx context.Context, serverID uuid.UUID, root, path string) (Entry, error) {
	if !Enabled() {
		return Entry{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Entry{}, err
	}
	if err := validatePathArg("path", path); err != nil {
		return Entry{}, err
	}
	ctx, client, cancel, err := s.rpc(ctx, serverID, false)
	if err != nil {
		return Entry{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.Stat(ctx, &agentv1.StatRequest{Root: root, Path: path})
	if err != nil {
		return Entry{}, mapRPCError(err)
	}
	entry := response.GetEntry()
	if entry == nil {
		return Entry{}, ErrNotFound
	}
	return newEntry(entry), nil
}

func (s *Service) Content(ctx context.Context, serverID uuid.UUID, root, path string) (content Content, err error) {
	defer func() { s.audit(ctx, serverID, "read_content", root, path, err) }()
	if !Enabled() {
		return Content{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return Content{}, err
	}
	if err := validatePathArg("path", path); err != nil {
		return Content{}, err
	}
	return s.readForEditor(ctx, serverID, root, path)
}

func (s *Service) readForEditor(ctx context.Context, serverID uuid.UUID, root, path string) (Content, error) {
	ctx, client, cancel, err := s.rpc(ctx, serverID, true)
	if err != nil {
		return Content{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	stat, err := client.Stat(ctx, &agentv1.StatRequest{Root: root, Path: path})
	if err != nil {
		return Content{}, mapRPCError(err)
	}
	info := stat.GetEntry()
	if info == nil {
		return Content{}, ErrNotFound
	}
	if info.GetSize() > s.maxContentBytes {
		return s.readTail(ctx, client, root, path, info)
	}
	return s.readFull(ctx, client, root, path, info)
}

func (s *Service) readContent(ctx context.Context, serverID uuid.UUID, root, path string) (Content, error) {
	ctx, client, cancel, err := s.rpc(ctx, serverID, true)
	if err != nil {
		return Content{}, err
	}
	defer cancel()
	defer s.closeClient(client)

	stat, err := client.Stat(ctx, &agentv1.StatRequest{Root: root, Path: path})
	if err != nil {
		return Content{}, mapRPCError(err)
	}
	info := stat.GetEntry()
	if info == nil {
		return Content{}, ErrNotFound
	}
	if info.GetSize() > s.maxContentBytes {
		return Content{}, ErrTooLarge
	}
	return s.readFull(ctx, client, root, path, info)
}

func (s *Service) readFull(ctx context.Context, client FileClient, root, path string, info *agentv1.FileEntry) (Content, error) {
	body, err := s.streamBody(ctx, client, &agentv1.ReadRequest{Root: root, Path: path}, s.maxContentBytes)
	if err != nil {
		return Content{}, err
	}
	return newContent(info, body, false)
}

func (s *Service) readTail(ctx context.Context, client FileClient, root, path string, info *agentv1.FileEntry) (Content, error) {
	offset := info.GetSize() - s.maxContentBytes
	body, err := s.streamBody(ctx, client, &agentv1.ReadRequest{
		Root:   root,
		Path:   path,
		Offset: offset,
		Limit:  s.maxContentBytes,
	}, s.maxContentBytes)
	if err != nil {
		return Content{}, err
	}
	if len(body) == 0 && info.GetSize() > 0 {
		return Content{}, ErrConflict
	}
	return newContent(info, tailBytes(body), true)
}

func (s *Service) streamBody(ctx context.Context, client FileClient, req *agentv1.ReadRequest, maxBytes int64) ([]byte, error) {
	stream, err := client.Read(ctx, req)
	if err != nil {
		return nil, mapRPCError(err)
	}
	return readAllLimited(stream, maxBytes)
}

func tailBytes(window []byte) []byte {
	if i := bytes.IndexByte(window, '\n'); i >= 0 {
		if rest := window[i+1:]; len(rest) > 0 {
			window = rest
		} else {
			return trimContinuation(window[:i])
		}
	} else {
		return trimContinuation(window)
	}
	end := len(window)
	if end > 0 && window[end-1] == '\n' {
		end--
	}
	lines := 0
	for i := end - 1; i >= 0; i-- {
		if window[i] != '\n' {
			continue
		}
		lines++
		if lines == TailLines {
			return window[i+1:]
		}
	}
	return window
}

func trimContinuation(window []byte) []byte {
	for i := 0; i < 3 && len(window) > 0 && window[0]&0xC0 == 0x80; i++ {
		window = window[1:]
	}
	return window
}

func newContent(info *agentv1.FileEntry, body []byte, truncated bool) (Content, error) {
	encodingName, text, err := detectEncoding(body)
	if err != nil {
		return Content{}, err
	}
	sum := sha256.Sum256(body)
	content := Content{
		Content:   text,
		Encoding:  encodingName,
		Etag:      info.GetEtag(),
		SHA256:    hex.EncodeToString(sum[:]),
		Size:      int64(len(body)),
		Mode:      info.GetMode(),
		raw:       body,
		Truncated: truncated,
	}
	if truncated {
		content.Size = info.GetSize()
	}
	return content, nil
}

func (s *Service) IssueDownloadTicket(ctx context.Context, serverID uuid.UUID, root, path string) (string, error) {
	if !Enabled() {
		return "", ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return "", err
	}
	if err := validatePathArg("path", path); err != nil {
		return "", err
	}
	if _, err := s.resolve(ctx, serverID, true); err != nil {
		return "", err
	}
	return s.tickets.issue(ticket{
		ServerID: serverID,
		Root:     root,
		Path:     path,
	}), nil
}

func (s *Service) Download(ctx context.Context, serverID uuid.UUID, ticketID string) (download *Download, err error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	t, ok := s.tickets.consumeFor(ticketID, serverID)
	if !ok {
		return nil, ErrNotFound
	}
	defer func() { s.audit(ctx, serverID, "download", t.Root, t.Path, err) }()
	server, err := s.server(ctx, serverID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, s.transferTimeout)
	client, err := s.dialClient(ctx, server)
	if err != nil {
		cancel()
		return nil, err
	}

	stat, err := client.Stat(ctx, &agentv1.StatRequest{Root: t.Root, Path: t.Path})
	if err != nil {
		s.closeClient(client)
		cancel()
		return nil, mapRPCError(err)
	}
	info := stat.GetEntry()
	if info == nil {
		s.closeClient(client)
		cancel()
		return nil, ErrNotFound
	}

	stream, err := client.Read(ctx, &agentv1.ReadRequest{Root: t.Root, Path: t.Path})
	if err != nil {
		s.closeClient(client)
		cancel()
		return nil, mapRPCError(err)
	}

	chunks := make(chan []byte)
	streamErr := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(streamErr)
		defer cancel()
		defer s.closeClient(client)
		for {
			chunk, err := stream.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) && ctx.Err() == nil {
					streamErr <- mapRPCError(err)
					s.logger.Debug("files: download stream ended early", "error", err)
				}
				return
			}
			data := chunk.GetData()
			if len(data) == 0 {
				continue
			}
			select {
			case chunks <- data:
			case <-ctx.Done():
				return
			}
		}
	}()

	return &Download{
		Name:   info.GetName(),
		Size:   info.GetSize(),
		Etag:   info.GetEtag(),
		Chunks: chunks,
		Err:    streamErr,
	}, nil
}

func (s *Service) resolve(ctx context.Context, serverID uuid.UUID, _ bool) (*servers.Server, error) {
	server, err := s.server(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return server, nil
}

func (s *Service) server(ctx context.Context, serverID uuid.UUID) (*servers.Server, error) {
	if s.registry == nil {
		return nil, errors.New("files: server registry is not configured")
	}
	server, err := s.registry.Get(ctx, serverID)
	if err != nil {
		if errors.Is(err, servers.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("files: resolve server: %w", err)
	}
	return server, nil
}

func (s *Service) rpc(ctx context.Context, serverID uuid.UUID, write bool) (context.Context, FileClient, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, s.rpcTimeout)
	server, err := s.resolve(ctx, serverID, write)
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

func (s *Service) dialClient(ctx context.Context, server *servers.Server) (FileClient, error) {
	s.mu.RLock()
	dial := s.dial
	s.mu.RUnlock()
	if dial == nil {
		return nil, fmt.Errorf("%w: agent dialer is not configured", ErrAgentUnavailable)
	}
	client, err := dial(ctx, server)
	if err != nil {
		return nil, mapRPCError(err)
	}
	if client == nil {
		return nil, fmt.Errorf("%w: agent dial returned no client", ErrAgentUnavailable)
	}
	return client, nil
}

func (s *Service) closeClient(client FileClient) {
	closer, ok := client.(io.Closer)
	if !ok {
		return
	}
	if err := closer.Close(); err != nil {
		s.logger.Debug("files: closing agent client failed", "error", err)
	}
}

func validateRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("%w: root is required", ErrValidation)
	}
	return validatePathArg("root", root)
}

func validatePathArg(name, value string) error {
	if err := validateFileArg(name, value); err != nil {
		return err
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return fmt.Errorf("%w: %s must not contain a parent segment", ErrValidation, name)
		}
	}
	return nil
}

func validateFileArg(name, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrValidation, name)
	}
	if strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("%w: %s contains a NUL byte", ErrValidation, name)
	}
	if len(value) > maxFileArgBytes {
		return fmt.Errorf("%w: %s is too long", ErrValidation, name)
	}
	return nil
}

func readAllLimited(stream grpc.ServerStreamingClient[agentv1.ReadChunk], limit int64) ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return buf.Bytes(), nil
		}
		if err != nil {
			return nil, mapRPCError(err)
		}
		data := chunk.GetData()
		if int64(buf.Len())+int64(len(data)) > limit {
			return nil, ErrTooLarge
		}
		buf.Write(data)
	}
}

func newRoot(info *agentv1.FileRoot) Root {
	return Root{
		Name:      info.GetName(),
		Path:      info.GetPath(),
		Writable:  info.GetWritable(),
		FreeBytes: info.GetFreeBytes(),
	}
}

func newEntry(info *agentv1.FileEntry) Entry {
	entry := Entry{
		Name:          info.GetName(),
		Kind:          kindString(info.GetKind()),
		Size:          info.GetSize(),
		Mode:          info.GetMode(),
		SymlinkTarget: info.GetSymlinkTarget(),
		Etag:          info.GetEtag(),
		Owner:         info.GetOwner(),
		Group:         info.GetGroup(),
	}
	if ts := info.GetModifiedAt(); ts != nil {
		entry.ModifiedAt = ts.AsTime()
	}
	return entry
}

func kindString(kind agentv1.FileKind) string {
	switch kind {
	case agentv1.FileKind_FILE_KIND_FILE:
		return "file"
	case agentv1.FileKind_FILE_KIND_DIRECTORY:
		return "directory"
	case agentv1.FileKind_FILE_KIND_SYMLINK:
		return "symlink"
	case agentv1.FileKind_FILE_KIND_SPECIAL:
		return "special"
	default:
		return "unspecified"
	}
}

// DisabledReason is the agent's stable detail for a switched-off file manager.
// The agent package keeps its own copy because it must not import internal/.
const DisabledReason = "file_manager_disabled"

// IsDisabledOnNode reports the typed FailedPrecondition an agent returns when
// its file manager is switched off.
func IsDisabledOnNode(err error) bool {
	return status.Code(err) == codes.FailedPrecondition &&
		strings.Contains(status.Convert(err).Message(), DisabledReason)
}

func mapRPCError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case codes.InvalidArgument:
		if isInvalidArchiveError(status.Convert(err).Message()) {
			return fmt.Errorf("%w: %v", ErrInvalidArchive, err)
		}
		return fmt.Errorf("%w: %v", ErrValidation, err)
	case codes.PermissionDenied:
		return fmt.Errorf("%w: %v", ErrForbidden, err)
	case codes.FailedPrecondition:
		message := status.Convert(err).Message()
		if strings.Contains(message, DisabledReason) {
			return fmt.Errorf("%w: %v", ErrDisabledOnNode, err)
		}
		if strings.Contains(message, "archive entry") {
			return fmt.Errorf("%w: %v", ErrArchiveEntry, err)
		}
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case codes.AlreadyExists, codes.Aborted:
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case codes.ResourceExhausted:
		return classifyResourceExhausted(err)
	case codes.Unimplemented:
		return fmt.Errorf("%w: %v", ErrAgentOutdated, err)
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	default:
		return err
	}
}

func classifyResourceExhausted(err error) error {
	message := strings.ToLower(status.Convert(err).Message())
	switch {
	case strings.Contains(message, "low on free space"),
		strings.Contains(message, "do not fit"),
		strings.Contains(message, "quota"),
		strings.Contains(message, "no space left"),
		strings.Contains(message, "disk full"):
		return fmt.Errorf("%w: %v", ErrInsufficientStorage, err)
	case strings.Contains(message, "archive has more than"),
		strings.Contains(message, "archive expands"),
		strings.Contains(message, "archive nesting"),
		strings.Contains(message, "nests too deeply"),
		strings.Contains(message, "archive entry has a negative size"),
		strings.Contains(message, "archive is larger than"):
		return fmt.Errorf("%w: %v", ErrArchiveLimit, err)
	case strings.Contains(message, "upload limit"), strings.Contains(message, "too large"):
		return fmt.Errorf("%w: %v", ErrTooLarge, err)
	default:
		return fmt.Errorf("%w: %v", ErrBusy, err)
	}
}

func isInvalidArchiveError(message string) bool {
	message = strings.ToLower(message)
	for _, phrase := range []string{
		"reading zip",
		"reading tar",
		"reading gzip",
		"opening archive entry",
		"archive entry",
		"unsupported archive format",
		"is not a .zip",
	} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

func auditResult(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrValidation):
		return "invalid"
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, ErrTooLarge):
		return "too_large"
	case errors.Is(err, ErrArchiveLimit):
		return "archive_limit"
	case errors.Is(err, ErrArchiveEntry):
		return "archive_entry"
	case errors.Is(err, ErrInvalidArchive):
		return "invalid_archive"
	case errors.Is(err, ErrNotEditable):
		return "not_editable"
	case errors.Is(err, ErrUnrepresentable):
		return "unrepresentable"
	case errors.Is(err, ErrBusy):
		return "busy"
	case errors.Is(err, ErrInsufficientStorage):
		return "insufficient_storage"
	case errors.Is(err, ErrAgentOutdated):
		return "agent_outdated"
	case errors.Is(err, ErrDisabledOnNode):
		return "file_manager_disabled"
	case errors.Is(err, ErrAgentUnavailable):
		return "agent_unavailable"
	case errors.Is(err, ErrDisabled):
		return "disabled"
	default:
		return "error"
	}
}

func (s *Service) audit(_ context.Context, serverID uuid.UUID, op, root, relpath string, err error, extra ...any) {
	s.logger.Info("files: audit",
		append([]any{
			"op", op,
			"result", auditResult(err),
			"server_id", serverID.String(),
			"root", root,
			"relpath", relpath,
		}, extra...)...)
}

type ticket struct {
	ServerID uuid.UUID
	Root     string
	Path     string
	Expires  time.Time
}

type ticketStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	tickets map[string]ticket
}

func newTicketStore(ttl time.Duration, now func() time.Time) *ticketStore {
	if now == nil {
		now = time.Now
	}
	return &ticketStore{ttl: ttl, now: now, tickets: make(map[string]ticket)}
}

func (s *ticketStore) issue(t ticket) string {
	token := newTicketID()
	t.Expires = s.now().Add(s.ttl)
	s.mu.Lock()
	s.pruneLocked()
	s.tickets[token] = t
	s.mu.Unlock()
	return token
}

func (s *ticketStore) consumeFor(token string, serverID uuid.UUID) (ticket, bool) {
	if token == "" {
		return ticket{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	t, ok := s.tickets[token]
	if !ok || t.ServerID != serverID {
		return ticket{}, false
	}
	delete(s.tickets, token)
	return t, true
}

func (s *ticketStore) pruneLocked() {
	now := s.now()
	for token, t := range s.tickets {
		if !now.Before(t.Expires) {
			delete(s.tickets, token)
		}
	}
}

func newTicketID() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("files: generate download ticket: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}
